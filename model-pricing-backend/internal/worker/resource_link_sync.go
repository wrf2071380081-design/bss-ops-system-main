package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"model_bss/internal/domain/customer"
)

// 供应域「资源上下架」事件 → 客户绑定状态联动（G3-0）。
//
// 背景：G4-A 把客户绑定的状态机（ACTIVE/DISABLED ↔ SUPPLY_OFFLINE_PENDING → SUPPLY_OFFLINE）
// 造好了，但**生产代码 0 处调用** MarkSupplyOffline/MarkSupplyRestored——
// 也就是"资源下架了、客户绑定还是 ACTIVE"，客户详情仍显示「服务中」。
// 这里把它接到 outbox 消费侧（`RESOURCE_OFFLINED` / `RESOURCE_ONLINED`），
// 而不是塞进 plan 执行服务：供应域不依赖客户域，任何一方的改动都不该牵动另一方。
//
// 三条纪律：
//  1. **不绕过状态机**：只调 MarkSupplyOffline/MarkSupplyRestored（迁移合法性 + 审计都在里面）；
//  2. **一条资源可能被 N 个客户绑定**：逐条迁移，单条失败（并发 409 / 已归档）只跳过该条，不中断整批；
//  3. **幂等**：重复投递时，已在 SUPPLY_OFFLINE_PENDING 的绑定会返回 ErrLinkTransitionInvalid，
//     视为"已处理"跳过——不产生新的审计行，也不会把状态改坏。

// LinkStateTransitioner 是联动需要的客户绑定能力（只有这三个方法，便于单测打桩）。
type LinkStateTransitioner interface {
	ListLinksByResource(ctx context.Context, resourceID int64) ([]customer.ResourceLink, error)
	MarkSupplyOffline(ctx context.Context, id int64, expectedRevision int, reason, requestID string) (*customer.ResourceLink, error)
	MarkSupplyRestored(ctx context.Context, id int64, expectedRevision int, requestID string) (*customer.ResourceLink, error)
}

// isResourceStateEvent 判断事件是否属于「资源状态变化」（只有这两类需要联动）。
func isResourceStateEvent(eventType string) bool {
	return eventType == "RESOURCE_OFFLINED" || eventType == "RESOURCE_ONLINED"
}

// flexID 兼容「事件里 ID 是 JSON 字符串」与「裸数字」两种形态。
//
// 为什么必须是自定义类型：
//   - 供应域写 outbox 时 ID 一律走 `strconv.FormatUint` / 领域 string ID
//     （`plan_execution.go` 的 payloadValues），即 **JSON 字符串** `"1550"`；
//     原因是 19 位量级的 ID 用 JSON number 会让 JS 消费端丢精度，这是对外 webhook 的既定契约。
//   - 声明成 `int64` → `json: cannot unmarshal string into Go struct field ... of type int64`（G3-0 真实线上表现：联动 100% 失败）。
//   - 声明成 `json.Number` **也不行**：json.Number 只接受裸数字字面量，遇到带引号的 `"1550"` 依然报错。
//   - 所以这里两种形态都收：先去掉可能的引号，再 ParseInt。
//
// 空值（null / ""）解析失败 → 交由上层按「事件缺 resource_id」报错，不静默当 0 处理。
type flexID int64

// UnmarshalJSON 实现 json.Unmarshaler：接受 "1550" 与 1550 两种写法。
func (f *flexID) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		return fmt.Errorf("flexID: null")
	}
	text = strings.Trim(text, `"`)
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("flexID: empty")
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("flexID: %w", err)
	}
	*f = flexID(value)
	return nil
}

// resourceStatePayload 是 event_outbox.payload 里联动需要的字段（其余字段忽略）。
//
// ResourceID 用 flexID：生产方（供应域 plan 执行）发的是 JSON 字符串，
// 见 flexID 注释——这里**不能**改成 int64，否则整条联动会静默失败。
type resourceStatePayload struct {
	ResourceID   flexID `json:"resource_id"`
	Region       string `json:"region"`
	State        string `json:"state"`
	PreviousStat string `json:"previous_state"`
}

// SyncResourceLinks 按事件把某条资源上的客户绑定推进到对应状态。
//
// 返回 error 只表示**系统性失败**（读取绑定列表失败等），调用方据此决定事件是否重试；
// 单条绑定的并发冲突/已归档属于预期内分支，只记日志不返回错误。
func SyncResourceLinks(
	ctx context.Context,
	links LinkStateTransitioner,
	eventID int64,
	eventType string,
	rawPayload []byte,
	log *zap.Logger,
) error {
	if links == nil {
		return fmt.Errorf("resource link sync: linker unavailable")
	}
	var action string
	switch eventType {
	case "RESOURCE_OFFLINED":
		action = "offline"
	case "RESOURCE_ONLINED":
		action = "online"
	default:
		return nil // 与本联动无关的事件直接放行
	}

	var payload resourceStatePayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("resource link sync: decode payload of event %d: %w", eventID, err)
	}
	resourceID := int64(payload.ResourceID)
	if resourceID <= 0 {
		return fmt.Errorf("resource link sync: event %d has no resource_id", eventID)
	}

	// request_id 用事件 id：从审计行能反查是哪一次下架/上架引发的迁移。
	requestID := fmt.Sprintf("event:%d", eventID)
	affected, err := links.ListLinksByResource(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("resource link sync: list links of resource %d: %w", resourceID, err)
	}
	if len(affected) == 0 {
		log.Info("resource link sync: no active customer link",
			zap.String("event_type", eventType), zap.Int64("event_id", eventID), zap.Int64("resource_id", resourceID))
		return nil
	}

	log.Info("resource link sync: start",
		zap.String("event_type", eventType), zap.Int64("event_id", eventID),
		zap.Int64("resource_id", resourceID), zap.Int("links", len(affected)))

	for index := range affected {
		link := affected[index]
		var transitionErr error
		if action == "offline" {
			transitionErr = callOffline(ctx, links, link, requestID)
		} else {
			transitionErr = callRestore(ctx, links, link, requestID)
		}
		if transitionErr != nil {
			// 单条失败不中断整批：跳过并留痕，其余继续。
			// 全部用 info/warn（线上默认级别看不到 debug），便于事后判断"这条是跳过还是没跑到"。
			switch {
			case errors.Is(transitionErr, customer.ErrLinkTransitionInvalid):
				log.Info("resource link sync: already processed, skip",
					zap.Int64("event_id", eventID), zap.Int64("link_id", link.ID), zap.String("state", link.State))
			case errors.Is(transitionErr, customer.ErrLinkArchived):
				log.Warn("resource link sync: link archived, skip",
					zap.Int64("event_id", eventID), zap.Int64("link_id", link.ID))
			case errors.Is(transitionErr, customer.ErrLinkRevisionConflict):
				log.Warn("resource link sync: revision changed concurrently, skip",
					zap.Int64("event_id", eventID), zap.Int64("link_id", link.ID), zap.Int("revision", link.Revision))
			case errors.Is(transitionErr, customer.ErrLinkNotFound):
				log.Warn("resource link sync: link missing, skip",
					zap.Int64("event_id", eventID), zap.Int64("link_id", link.ID))
			default:
				log.Error("resource link sync: transition failed",
					zap.Int64("event_id", eventID), zap.Int64("link_id", link.ID), zap.Error(transitionErr))
			}
		}
	}
	return nil
}

func callOffline(ctx context.Context, links LinkStateTransitioner, link customer.ResourceLink, requestID string) error {
	// 已在待处理态的绑定不必再迁移：直接返回"非法迁移"，由上层按已处理跳过。
	if link.State == customer.LinkStateSupplyOfflinePending {
		return customer.ErrLinkTransitionInvalid
	}
	_, err := links.MarkSupplyOffline(ctx, link.ID, link.Revision, "供应资源下架（事件驱动）", requestID)
	return err
}

func callRestore(ctx context.Context, links LinkStateTransitioner, link customer.ResourceLink, requestID string) error {
	// 只有处于待处理态才需要恢复；其余状态说明这条链接压根没被下架影响过。
	if link.State != customer.LinkStateSupplyOfflinePending {
		return customer.ErrLinkTransitionInvalid
	}
	_, err := links.MarkSupplyRestored(ctx, link.ID, link.Revision, requestID)
	return err
}
