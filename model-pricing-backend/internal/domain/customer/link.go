package customer

import (
	"errors"
	"fmt"
	"time"
)

// 客户「接入模型」绑定（G4 地基，迁移 000049）。
//
// 业务口径：客户买到的不是抽象的「标准模型」，而是「某供应商在某部署节点提供的
// 某个标准模型的实例」——即供应域的 resource（带采购系数、正式配置、测试证据、
// 可报价状态）。所以绑定必须同时记 standard_model_id 与 resource_id。
//
// 为什么独立成表而不是复用 000033 的 customer / customer_link：
//   - customer_link.customer_id 指向 customer(id)，而客户真身是 customer_profile(id)；
//   - 000033 那套 customer / quote / customer_link 在 Go 侧零引用，继续写入只会让
//     「客户到底是谁」继续分裂。
//
// 状态机（唯一入口 NextState；非法迁移一律返回领域错误，不接受上层直写 state）：
//
//	—（开通完成）──────────────────────────► ACTIVE（activated_at = now）
//	ACTIVE            --DISABLE_REQUEST-->  DISABLE_PENDING
//	DISABLE_PENDING   --DISABLE_CONFIRM-->  DISABLED
//	DISABLED          --RESTORE_REQUEST-->  RESTORE_PENDING
//	RESTORE_PENDING   --RESTORE_CONFIRM-->  ACTIVE
//	ACTIVE|DISABLED   --SUPPLY_OFFLINE(系统)--> SUPPLY_OFFLINE_PENDING
//	SUPPLY_OFFLINE_PENDING --OFFLINE_CONFIRM--> SUPPLY_OFFLINE（archived = TRUE）
//	SUPPLY_OFFLINE_PENDING --SUPPLY_RESTORED(系统)--> state_before_offline（ACTIVE/DISABLED）
//	ACTIVE            --重新配置来源--> 旧记录 archived = TRUE + 新建一条 ACTIVE
//
// 为什么 SUPPLY_OFFLINE_PENDING 不能自动停用客户（用户 2026-11-03 明确要求）：
// 供应资源下架（供应商主动下架 / 计划下架 / 测试证据失效）不等于客户侧服务必须立刻
// 中断。自动停用会误伤正在使用该来源的客户，业务上需要给销售和客服留出迁移时间。
// 因此系统只打「来源待处理」标记并保留客户可用性，必须人工确认（OFFLINE_CONFIRM）
// 才真正停用并归档。

// 绑定状态（customer_resource_link.state，与 ck_crl_state 一一对应）。
const (
	LinkStateActive               = "ACTIVE"                 // 服务中：客户可用该来源
	LinkStateDisablePending       = "DISABLE_PENDING"        // 禁用待确认（此时仍可用）
	LinkStateDisabled             = "DISABLED"               // 已禁用
	LinkStateRestorePending       = "RESTORE_PENDING"        // 恢复待确认
	LinkStateSupplyOfflinePending = "SUPPLY_OFFLINE_PENDING" // 来源已下架，待人工确认停用（客户仍可用）
	LinkStateSupplyOffline        = "SUPPLY_OFFLINE"         // 已确认停用（随 archived = TRUE）
)

// 绑定动作。前五个对客户/内部开放（走 POST /resource-links/:id/actions），
// 后两个只由供应域事件触发（本轮无 HTTP 入口，留给下一轮下架联动接线）。
const (
	LinkActionDisableRequest = "DISABLE_REQUEST" // 客户请求禁用
	LinkActionDisableConfirm = "DISABLE_CONFIRM" // 人工确认禁用
	LinkActionRestoreRequest = "RESTORE_REQUEST" // 客户请求恢复
	LinkActionRestoreConfirm = "RESTORE_CONFIRM" // 人工确认恢复
	LinkActionOfflineConfirm = "OFFLINE_CONFIRM" // 人工确认停用（来源下架后）
	// 系统触发，无 HTTP 入口。
	LinkActionSupplyOffline  = "SUPPLY_OFFLINE"  // 供应资源下架
	LinkActionSupplyRestored = "SUPPLY_RESTORED" // 供应资源重新上架（回原状态）
)

// 客户侧看到的服务状态（投影 service_state）。
//
// 约定：Project() 在任何输入下都必须落在一个确定值上，**绝不返回空字符串**——
// 前端拿 service_state 做分支判断，空串会掉进「未知状态」分支且不会报错。
// 「接入中」本轮没有数据源（开通流程在 G4-B），常量先留着避免前端枚举漂移；
// 它的产出时机是：客户已发起开通但绑定尚未建立（下一轮由流程投影补充）。
const (
	ServiceStateServing        = "服务中"
	ServiceStateAccessing      = "接入中" // G4-B 才产出，本轮不会出现
	ServiceStateDisabled       = "已禁用"
	ServiceStateOfflinePending = "来源待处理"
	ServiceStateNoSource       = "无可用来源"
)

// 资源已准入状态（resource.state 的 ADMITTED）。可用来源统计只认这个状态。
const resourceStateAdmitted = "ADMITTED"

// 绑定领域错误。HTTP 层据此映射 400/403/404/409（见 api/customer_resource_link.go）。
var (
	ErrLinkNotFound              = errors.New("客户绑定不存在")
	ErrLinkStoreUnavailable      = errors.New("客户绑定存储不可用")
	ErrLinkCustomerNotFound      = errors.New("客户不存在")
	ErrLinkStandardModelNotFound = errors.New("标准模型不存在")
	ErrLinkResourceNotFound      = errors.New("供应资源不存在")
	// ErrLinkReferenceMissing 是外键冲突无法定位到具体约束时的兜底文案：
	// 宁可说「引用的对象不存在」，也不要把资源/模型缺失误报成「客户不存在」。
	ErrLinkReferenceMissing     = errors.New("客户绑定引用的对象不存在（客户 / 标准模型 / 供应资源已被删除）")
	ErrLinkResourceNotAdmitted  = errors.New("供应资源当前不是已准入状态，不能作为客户来源")
	ErrLinkModelMismatch        = errors.New("供应资源与标准模型不匹配")
	ErrLinkDuplicateResource    = errors.New("该客户已绑定此供应资源")
	ErrLinkModelAlreadyActive   = errors.New("该客户此模型已有生效来源，请先归档旧绑定")
	ErrLinkRevisionConflict     = errors.New("客户绑定已被其他人修改，请刷新后重试")
	ErrLinkArchived             = errors.New("客户绑定已归档，不能再变更")
	ErrLinkTransitionInvalid    = errors.New("客户绑定的状态迁移不合法")
	ErrLinkActionUnsupported    = errors.New("不支持的客户绑定动作")
	ErrLinkStateTraceMissing    = errors.New("缺少下架前的状态记录，无法自动回退到原状态，请人工处理")
	ErrLinkCustomerRequired     = errors.New("请指定客户")
	ErrLinkModelRequired        = errors.New("请指定标准模型")
	ErrLinkResourceRequired     = errors.New("请指定供应资源")
	ErrLinkRevisionRequired     = errors.New("请提供 expected_revision 以做并发保护")
	ErrLinkQuoteInvalid         = errors.New("报价 ID 必须是正整数")
	ErrLinkRebindOnlyActive     = errors.New("只有服务中的绑定可以重新配置来源")
	ErrLinkOperatorIDRequired   = errors.New("缺少操作员标识")
	ErrLinkCustomerIDOutOfRange = errors.New("客户 ID 非法")
)

var linkStateLabels = map[string]string{
	LinkStateActive:               "服务中",
	LinkStateDisablePending:       "禁用待确认",
	LinkStateDisabled:             "已禁用",
	LinkStateRestorePending:       "恢复待确认",
	LinkStateSupplyOfflinePending: "来源已下架待处理",
	LinkStateSupplyOffline:        "已停用",
}

var linkActionLabels = map[string]string{
	LinkActionDisableRequest: "请求禁用",
	LinkActionDisableConfirm: "确认禁用",
	LinkActionRestoreRequest: "请求恢复",
	LinkActionRestoreConfirm: "确认恢复",
	LinkActionOfflineConfirm: "确认停用",
	LinkActionSupplyOffline:  "供应资源下架",
	LinkActionSupplyRestored: "供应资源重新上架",
}

// StateLabel 返回状态的中文名（用于错误文案与接口回显，未知状态原样返回）。
func StateLabel(state string) string {
	if label, ok := linkStateLabels[state]; ok {
		return label
	}
	return state
}

// ActionLabel 返回动作的中文名。
func ActionLabel(action string) string {
	if label, ok := linkActionLabels[action]; ok {
		return label
	}
	return action
}

// IsLinkActionAllowed 判断该动作是否可以由客户/内部通过动作接口提交。
// 系统动作（下架/重新上架）只能由供应域事件驱动，不接受外部直接调用。
func IsLinkActionAllowed(action string) bool {
	switch action {
	case LinkActionDisableRequest, LinkActionDisableConfirm, LinkActionRestoreRequest,
		LinkActionRestoreConfirm, LinkActionOfflineConfirm:
		return true
	default:
		return false
	}
}

type linkTransition struct {
	from   string
	action string
	to     string
}

// linkTransitions 是状态机的**唯一**真相来源（表驱动，便于逐行覆盖测试）。
// SUPPLY_RESTORED 不在表内：它要回到 state_before_offline，由 NextState 单独处理。
var linkTransitions = []linkTransition{
	{LinkStateActive, LinkActionDisableRequest, LinkStateDisablePending},
	{LinkStateDisablePending, LinkActionDisableConfirm, LinkStateDisabled},
	{LinkStateDisabled, LinkActionRestoreRequest, LinkStateRestorePending},
	{LinkStateRestorePending, LinkActionRestoreConfirm, LinkStateActive},
	{LinkStateActive, LinkActionSupplyOffline, LinkStateSupplyOfflinePending},
	{LinkStateDisabled, LinkActionSupplyOffline, LinkStateSupplyOfflinePending},
	{LinkStateSupplyOfflinePending, LinkActionOfflineConfirm, LinkStateSupplyOffline},
}

// NextState 计算迁移目标状态；非法迁移返回 ErrLinkTransitionInvalid（带中文说明）。
//
// stateBeforeOffline 只在 action = SUPPLY_RESTORED 时使用：资源重新上架必须回到
// 下架前的状态——直接回 ACTIVE 会把「客户本来就禁用了」的来源偷偷打开。
func NextState(from, action, stateBeforeOffline string) (string, error) {
	if action == LinkActionSupplyRestored {
		if from != LinkStateSupplyOfflinePending {
			return "", transitionError(from, action)
		}
		if stateBeforeOffline != LinkStateActive && stateBeforeOffline != LinkStateDisabled {
			return "", ErrLinkStateTraceMissing
		}
		return stateBeforeOffline, nil
	}
	for _, t := range linkTransitions {
		if t.from == from && t.action == action {
			return t.to, nil
		}
	}
	return "", transitionError(from, action)
}

func transitionError(from, action string) error {
	return fmt.Errorf("%w：%s 不允许执行「%s」", ErrLinkTransitionInvalid, StateLabel(from), ActionLabel(action))
}

// ArchivedAfter 判断动作执行后是否需要同时归档（唯一需要归档的是人工确认停用）。
func ArchivedAfter(action string) bool { return action == LinkActionOfflineConfirm }

// 重新配置来源：旧绑定归档后，新绑定一律从 ACTIVE 起（客户立即切换到新来源）。
//
// ResourceLink 是 customer_resource_link 的领域模型（仓储层负责填充）。
type ResourceLink struct {
	ID                 int64      `json:"id"`
	CustomerProfileID  int64      `json:"customer_profile_id"`
	StandardModelID    int64      `json:"standard_model_id"`
	ResourceID         int64      `json:"resource_id"`
	QuoteID            *int64     `json:"quote_id,omitempty"`
	State              string     `json:"state"`
	StateBeforeOffline string     `json:"state_before_offline,omitempty"`
	ActivationNote     string     `json:"activation_note,omitempty"`
	ActivatedAt        *time.Time `json:"activated_at,omitempty"`
	Archived           bool       `json:"archived"`
	CreatedBy          int64      `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Revision           int        `json:"revision"`
	RequestID          string     `json:"request_id,omitempty"`
}

// ResourceFacts 是判断「这条资源能不能给这个客户用」所需的最小事实集。
type ResourceFacts struct {
	ID              int64
	StandardModelID int64
	Region          string
	State           string
}

// CreateLinkInput 是建绑定的输入（开通完成时内部调用；本轮同时开放手工登记）。
type CreateLinkInput struct {
	CustomerProfileID int64  `json:"customer_profile_id"`
	StandardModelID   int64  `json:"standard_model_id"`
	ResourceID        int64  `json:"resource_id"`
	QuoteID           *int64 `json:"quote_id"`
	Note              string `json:"note"`
}

// LinkActionInput 是状态动作的输入。
type LinkActionInput struct {
	ID               int64  `json:"-"`
	Action           string `json:"action"`
	Reason           string `json:"reason"`
	ExpectedRevision int    `json:"expected_revision"`
}

// RebindInput 是「重新配置来源」的输入：归档旧绑定 + 新建一条 ACTIVE。
type RebindInput struct {
	ID               int64  `json:"-"`
	ExpectedRevision int    `json:"expected_revision"`
	ResourceID       int64  `json:"resource_id"`
	QuoteID          *int64 `json:"quote_id"`
	Note             string `json:"note"`
}

// CreateLinkRecord / TransitionRecord / RebindRecord 是仓储写入参数（领域层产出，
// 仓储层不再做业务判断）。
type CreateLinkRecord struct {
	CustomerProfileID int64
	StandardModelID   int64
	ResourceID        int64
	QuoteID           *int64
	State             string
	ActivationNote    string
	ActivatedAt       *time.Time
	CreatedBy         int64
	RequestID         string
}

type TransitionRecord struct {
	ID                 int64
	ExpectedRevision   int
	To                 string
	StateBeforeOffline string
	Archived           bool
	Note               string
	RequestID          string
}

type RebindRecord struct {
	ArchiveID        int64
	ExpectedRevision int
	Note             string
	RequestID        string
	Next             CreateLinkRecord
}

// CustomerModelServiceRow 是投影查询的原始聚合行（仓储层产出）。
type CustomerModelServiceRow struct {
	StandardModelID     int64
	ModelName           string
	Region              string
	PrimaryState        string // 优先取 ACTIVE 行的状态；无 ACTIVE 时取最早一条绑定
	BoundCount          int64
	ActiveCount         int64
	DisabledCount       int64
	OfflinePendingCount int64
	AvailableCount      int64
}

// CustomerModelService 是客户详情「接入模型」Tab 的投影行。
//
// 字段约定：
//   - service_state：**必定是非空枚举值**（服务中/接入中/已禁用/来源待处理/无可用来源），
//     由 Project() 兜底，不会出现空串。
//   - region：可用资源统计的部署节点。取法优先级 = **ACTIVE 绑定的那条资源的 region
//     → 没有 ACTIVE 时取最早一条绑定的 region**。因为 uk_crl_customer_model 保证
//     同一客户同一模型最多一条 ACTIVE，有 ACTIVE 时 region 唯一不含糊。
//     没有未归档绑定时为**空串**（不是 null），此时 service_state = 无可用来源、
//     available_count = 0，前端可据此直接显示「未接入」。
//   - available_count：同 standard_model_id + **同 region** + resource.state='ADMITTED' 的资源数。
type CustomerModelService struct {
	StandardModelID int64    `json:"standard_model_id"`
	ModelName       string   `json:"model_name"`
	Region          string   `json:"region"`
	ServiceState    string   `json:"service_state"`
	BoundCount      int64    `json:"bound_count"`
	AvailableCount  int64    `json:"available_count"`
	RiskFlags       []string `json:"risk_flags"`
}

// Project 把聚合行解释成客户可见的服务状态与风险标记。
//
// 状态优先级的理由：
//   - 只要存在「来源已下架待处理」，第一优先显示「来源待处理」——这是唯一需要人工
//     介入的状态，埋在别的状态里会让客服漏处理。
//   - DISABLE_PENDING 仍然算「服务中」：客户可用性还没被收回（禁用是两步确认）。
//   - 没有未归档绑定时才是「无可用来源」。
//
// 兜底约定：任何输入（空状态、脏数据里的未知状态、甚至未来新增的状态）都必须落在一个
// 确定的枚举值上；末尾还有一道空串断言，避免以后有人加分支时漏掉 default。
func (r CustomerModelServiceRow) Project() CustomerModelService {
	out := CustomerModelService{
		StandardModelID: r.StandardModelID,
		ModelName:       r.ModelName,
		Region:          r.Region, // 无绑定时为 ""（不是 null），见 CustomerModelService 注释
		BoundCount:      r.BoundCount,
		AvailableCount:  r.AvailableCount,
		RiskFlags:       make([]string, 0, 3),
	}
	switch {
	case r.OfflinePendingCount > 0:
		out.ServiceState = ServiceStateOfflinePending
	case r.PrimaryState == LinkStateActive, r.PrimaryState == LinkStateDisablePending:
		out.ServiceState = ServiceStateServing
	case r.PrimaryState == LinkStateDisabled, r.PrimaryState == LinkStateRestorePending:
		out.ServiceState = ServiceStateDisabled
	case r.PrimaryState == LinkStateSupplyOffline, r.PrimaryState == LinkStateSupplyOfflinePending:
		// 正常流程里 SUPPLY_OFFLINE 一定伴随 archived=true、不会进入投影；
		// 真出现只可能是脏数据，按「已禁用」处理比「无可用来源」更接近事实。
		out.ServiceState = ServiceStateDisabled
	default:
		out.ServiceState = ServiceStateNoSource
	}
	if out.ServiceState == "" {
		out.ServiceState = ServiceStateNoSource
	}
	if r.OfflinePendingCount > 0 {
		out.RiskFlags = append(out.RiskFlags, "来源已下架待处理")
	}
	if r.PrimaryState == LinkStateDisablePending {
		out.RiskFlags = append(out.RiskFlags, "禁用待确认")
	}
	if r.DisabledCount > 0 {
		out.RiskFlags = append(out.RiskFlags, "绑定资源已禁用")
	}
	if r.AvailableCount <= 0 {
		out.RiskFlags = append(out.RiskFlags, "无可用来源")
	}
	return out
}
