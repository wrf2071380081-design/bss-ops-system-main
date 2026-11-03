package customer

import (
	"context"
	"strings"
	"time"
)

// 客户「接入模型」绑定的应用服务（G4 地基）。
//
// 分层：
//   - 本文件负责：口径校验（资源是否已准入、模型是否一致）、状态机调用（NextState）、
//     乐观锁前置判断、留痕文案。所有「能不能做」的判断都在这里，仓储只做 SQL。
//   - 仓储（internal/infra/supplypg/customer_resource_link_store.go）负责：唯一索引
//     冲突映射（23505 → ErrLinkDuplicateResource / ErrLinkModelAlreadyActive）、
//     事务内归档 + 新建（重新配置来源）、投影聚合查询。
//
// 与下一轮开通流程的边界：本轮只提供 Create（开通完成时被内部调用）与动作接口，
// 不创建 onboarding_flow kind='CUSTOMER'，也不消费 flow 状态。

// ResourceLinkStore 是绑定持久化端口（domain 只认接口，便于单测打桩）。
type ResourceLinkStore interface {
	// CreateLink 插入一条绑定；撞唯一索引时返回领域冲突错误。
	CreateLink(ctx context.Context, in CreateLinkRecord) (*ResourceLink, error)
	// GetLink 按主键取一条（含已归档行，由调用方判断 archived）。
	GetLink(ctx context.Context, id int64) (*ResourceLink, error)
	// ApplyTransition 带 revision 乐观锁的状态迁移。
	ApplyTransition(ctx context.Context, in TransitionRecord) (*ResourceLink, error)
	// RebindLink 在同一事务内归档旧绑定并插入新绑定（重新配置来源）。
	RebindLink(ctx context.Context, in RebindRecord) (*ResourceLink, error)
	// LoadResourceFacts 取资源事实（模型、部署节点、准入状态）。
	LoadResourceFacts(ctx context.Context, resourceID int64) (*ResourceFacts, error)
	// ListCustomerModelServices 按客户聚合出「接入模型」投影原始行。
	ListCustomerModelServices(ctx context.Context, customerProfileID int64) ([]CustomerModelServiceRow, error)
	// ListLinksByResource 按资源列出未归档的绑定（供应域下架/上架事件驱动联动用）。
	// 返回带 revision 的快照：联动逐条迁移时要用它做乐观锁，冲突就跳过这一条。
	ListLinksByResource(ctx context.Context, resourceID int64) ([]ResourceLink, error)
}

// ResourceLinkService 客户接入模型绑定服务。
type ResourceLinkService struct {
	store ResourceLinkStore
}

// NewResourceLinkService 构造。store 为 nil 时所有方法返回 ErrLinkStoreUnavailable
// （与仓库既有纪律一致：没接仓储就不静默成功）。
func NewResourceLinkService(store ResourceLinkStore) *ResourceLinkService {
	return &ResourceLinkService{store: store}
}

func (s *ResourceLinkService) available() error {
	if s == nil || s.store == nil {
		return ErrLinkStoreUnavailable
	}
	return nil
}

// Create 建一条绑定：开通完成的那一刻调用（新绑定一律从 ACTIVE 开始）。
//
// 校验顺序刻意从便宜到贵：先看参数，再看资源事实，最后才写库。
func (s *ResourceLinkService) Create(ctx context.Context, in CreateLinkInput, operatorID int64, requestID string) (*ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if in.CustomerProfileID <= 0 {
		return nil, ErrLinkCustomerRequired
	}
	if in.StandardModelID <= 0 {
		return nil, ErrLinkModelRequired
	}
	if in.ResourceID <= 0 {
		return nil, ErrLinkResourceRequired
	}
	if in.QuoteID != nil && *in.QuoteID <= 0 {
		return nil, ErrLinkQuoteInvalid
	}
	facts, err := s.resourceFacts(ctx, in.ResourceID, in.StandardModelID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = "开通完成，建立客户来源绑定"
	}
	created, err := s.store.CreateLink(ctx, CreateLinkRecord{
		CustomerProfileID: in.CustomerProfileID,
		StandardModelID:   facts.StandardModelID,
		ResourceID:        facts.ID,
		QuoteID:           in.QuoteID,
		State:             LinkStateActive,
		ActivationNote:    note,
		ActivatedAt:       &now,
		CreatedBy:         operatorID,
		RequestID:         requestID,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Act 执行客户/内部可提交的状态动作（禁用、恢复、确认停用）。
//
// 判断顺序很关键：archived → **revision** → 状态机。
// 反过来的话，同一请求重复提交（携带已过期的 revision）会读到别人改过之后的新状态，
// 先撞状态机、返回「禁用待确认 不允许执行『请求禁用』」(400)，让前端以为按钮点错了，
// 而真实原因是并发/重复提交——这类必须返回 409 让前端提示「刷新后重试」。
func (s *ResourceLinkService) Act(ctx context.Context, in LinkActionInput, operatorID int64, requestID string) (*ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if in.ID <= 0 {
		return nil, ErrLinkNotFound
	}
	if !IsLinkActionAllowed(in.Action) {
		return nil, ErrLinkActionUnsupported
	}
	if in.ExpectedRevision <= 0 {
		return nil, ErrLinkRevisionRequired
	}
	current, err := s.store.GetLink(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if current.Archived {
		return nil, ErrLinkArchived
	}
	if current.Revision != in.ExpectedRevision {
		return nil, ErrLinkRevisionConflict
	}
	to, err := NextState(current.State, in.Action, current.StateBeforeOffline)
	if err != nil {
		return nil, err
	}
	note := actionNote(in.Action, strings.TrimSpace(in.Reason))
	actor := operatorID
	record := TransitionRecord{
		ID:               in.ID,
		ExpectedRevision: in.ExpectedRevision,
		To:               to,
		Archived:         ArchivedAfter(in.Action),
		Note:             note,
		RequestID:        requestID,
		Action:           in.Action,
		FromState:        current.State,
		ActorID:          &actor,
	}
	// 确认停用后绑定归档，state_before_offline 已无意义，清空以免误导后续读取。
	if record.Archived {
		record.StateBeforeOffline = ""
	}
	return s.store.ApplyTransition(ctx, record)
}

// MarkSupplyOffline 供应资源下架（系统触发，无 HTTP 入口）。
//
// 只打「来源待处理」标记并保留 state_before_offline：
// 客户仍然可用（用户要求避免误伤），人工确认后才真正停用。
func (s *ResourceLinkService) MarkSupplyOffline(ctx context.Context, id int64, expectedRevision int, reason, requestID string) (*ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, ErrLinkNotFound
	}
	current, err := s.store.GetLink(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Archived {
		return nil, ErrLinkArchived
	}
	// 与 Act 同一顺序纪律：先判 revision 再跑状态机，避免并发/重复事件被误报成「非法迁移」。
	if current.Revision != expectedRevision {
		return nil, ErrLinkRevisionConflict
	}
	to, err := NextState(current.State, LinkActionSupplyOffline, current.StateBeforeOffline)
	if err != nil {
		return nil, err
	}
	trace := "供应资源下架，标记为来源待处理（客户暂仍可用，待人工确认停用）"
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		trace += "：" + trimmed
	}
	return s.store.ApplyTransition(ctx, TransitionRecord{
		ID:                 id,
		ExpectedRevision:   expectedRevision,
		To:                 to,
		StateBeforeOffline: current.State,
		Note:               trace,
		RequestID:          requestID,
		Action:             LinkActionSupplyOffline,
		FromState:          current.State,
		// 系统事件触发：没有内部员工操作者，审计里 operator_id 留 NULL。
		ActorID: nil,
	})
}

// MarkSupplyRestored 供应资源重新上架（系统触发，无 HTTP 入口）：回到下架前的状态。
//
// 回原状态而不是直接 ACTIVE：客户可能在下架前就已经禁用了这个来源。
func (s *ResourceLinkService) MarkSupplyRestored(ctx context.Context, id int64, expectedRevision int, requestID string) (*ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, ErrLinkNotFound
	}
	current, err := s.store.GetLink(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Archived {
		return nil, ErrLinkArchived
	}
	if current.Revision != expectedRevision {
		return nil, ErrLinkRevisionConflict
	}
	to, err := NextState(current.State, LinkActionSupplyRestored, current.StateBeforeOffline)
	if err != nil {
		return nil, err
	}
	return s.store.ApplyTransition(ctx, TransitionRecord{
		ID:                 id,
		ExpectedRevision:   expectedRevision,
		To:                 to,
		StateBeforeOffline: "", // 离开待处理态后清空留痕列
		Note:               "供应资源重新上架，恢复到原状态（" + StateLabel(to) + "）",
		RequestID:          requestID,
		Action:             LinkActionSupplyRestored,
		FromState:          current.State,
		ActorID:            nil, // 系统事件触发
	})
}

// ListLinksByResource 列出某条资源上未归档的客户绑定。
//
// 供应域「资源下架/上架」事件驱动联动用：一条资源可能被 N 个客户绑定，
// 联动按返回的 revision 逐条做乐观锁迁移（冲突就跳过该条）。
func (s *ResourceLinkService) ListLinksByResource(ctx context.Context, resourceID int64) ([]ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if resourceID <= 0 {
		return nil, ErrLinkNotFound
	}
	return s.store.ListLinksByResource(ctx, resourceID)
}

// Rebind 重新配置来源：归档旧绑定 + 在同一事务内新建一条 ACTIVE。
//
// 必须先归档旧的：uk_crl_customer_model 只允许一条未归档 ACTIVE，
// 不归档就插入会直接撞唯一索引（而且那正是我们希望数据库兜住的事）。
func (s *ResourceLinkService) Rebind(ctx context.Context, in RebindInput, operatorID int64, requestID string) (*ResourceLink, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if in.ID <= 0 {
		return nil, ErrLinkNotFound
	}
	if in.ResourceID <= 0 {
		return nil, ErrLinkResourceRequired
	}
	if in.ExpectedRevision <= 0 {
		return nil, ErrLinkRevisionRequired
	}
	current, err := s.store.GetLink(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if current.Archived {
		return nil, ErrLinkArchived
	}
	if current.Revision != in.ExpectedRevision {
		return nil, ErrLinkRevisionConflict
	}
	if current.State != LinkStateActive {
		return nil, ErrLinkRebindOnlyActive
	}
	if current.ResourceID == in.ResourceID {
		return nil, ErrLinkDuplicateResource
	}
	facts, err := s.resourceFacts(ctx, in.ResourceID, current.StandardModelID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = "重新配置来源，旧来源已归档"
	}
	return s.store.RebindLink(ctx, RebindRecord{
		ArchiveID:        in.ID,
		ExpectedRevision: in.ExpectedRevision,
		Note:             "重新配置来源，归档旧的来源绑定",
		RequestID:        requestID,
		Next: CreateLinkRecord{
			CustomerProfileID: current.CustomerProfileID,
			StandardModelID:   facts.StandardModelID,
			ResourceID:        facts.ID,
			QuoteID:           in.QuoteID,
			State:             LinkStateActive,
			ActivationNote:    note,
			ActivatedAt:       &now,
			CreatedBy:         operatorID,
			RequestID:         requestID,
		},
	})
}

// ListCustomerModels 客户详情「接入模型」投影。
//
// available_count 口径：同 standard_model_id + **同 region** + resource.state='ADMITTED'。
// 不跨部署节点统计：国内与海外是两个独立的供货盘子，把它们相加会让「无可用来源」
// 被另一地库存掩盖。region 取客户自己绑定的那条资源（ACTIVE 优先）。
func (s *ResourceLinkService) ListCustomerModels(ctx context.Context, customerProfileID int64) ([]CustomerModelService, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if customerProfileID <= 0 {
		return nil, ErrLinkCustomerIDOutOfRange
	}
	rows, err := s.store.ListCustomerModelServices(ctx, customerProfileID)
	if err != nil {
		return nil, err
	}
	out := make([]CustomerModelService, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Project())
	}
	return out, nil
}

// resourceFacts 取资源事实并校验「存在 / 模型一致 / 已准入」。
func (s *ResourceLinkService) resourceFacts(ctx context.Context, resourceID, standardModelID int64) (*ResourceFacts, error) {
	facts, err := s.store.LoadResourceFacts(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if facts == nil {
		return nil, ErrLinkResourceNotFound
	}
	if facts.StandardModelID != standardModelID {
		return nil, ErrLinkModelMismatch
	}
	if facts.State != resourceStateAdmitted {
		return nil, ErrLinkResourceNotAdmitted
	}
	return facts, nil
}

// actionNote 生成动作留痕文案（人工原因追加在后面，不覆盖系统语义）。
func actionNote(action, reason string) string {
	base := map[string]string{
		LinkActionDisableRequest: "客户请求禁用该来源",
		LinkActionDisableConfirm: "人工确认禁用，客户不能再使用该来源",
		LinkActionRestoreRequest: "客户请求恢复该来源",
		LinkActionRestoreConfirm: "人工确认恢复，来源重新可用",
		LinkActionOfflineConfirm: "来源已下架，人工确认停用并归档",
	}[action]
	if base == "" {
		base = ActionLabel(action)
	}
	if reason != "" {
		return base + "：" + reason
	}
	return base
}
