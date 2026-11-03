package workbench

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// InboxQuery 统一收件箱查询。
// AssigneeID 可由人员选择器指定（只读查看他人）；为空时由 handler 填当前登录人。
type InboxQuery struct {
	State      string // TODO / READ / DONE / READ_DONE
	BizType    string // TODO_TASK / ONBOARDING_NODE / PLAN / NOTICE
	Region     string // CN / OVERSEAS
	Keyword    string
	AssigneeID int64
	Page       int
	Size       int
}

// InboxItem 收件箱单行。
type InboxItem struct {
	BizType    string    `json:"biz_type"`
	BizID      int64     `json:"biz_id"`
	ItemType   string    `json:"item_type"` // TODO / READ
	State      string    `json:"state"`     // TODO / DONE / READ / READ_DONE
	Title      string    `json:"title"`
	BizObject  string    `json:"biz_object"`
	Region     string    `json:"region,omitempty"`
	Priority   string    `json:"priority"`
	OccurredAt time.Time `json:"occurred_at"`
	PlanID     int64     `json:"plan_id,omitempty"`
	// 责任人：原型工作台的「责任人」列（姓名 + 关系副标题）。
	AssigneeID   int64  `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`
	AssigneeRole string `json:"assignee_role"`
	Deeplink     string `json:"deeplink,omitempty"`
	Readable     bool   `json:"readable"` // 只有本人、且是待阅，才可标记已读
	// FlowKind / FlowOwnerID 只在 ONBOARDING_NODE 行上填充，**仅用于拼 deeplink**：
	// v_work_item 只给 biz_id（= onboarding_flow.id），没有 kind/owner_id，
	// 而客户节点的跳转目标需要 owner_id（customer_profile.id）。
	// 标 json:"-" 刻意不外泄：它是内部路由解析用的，不属于前端契约。
	FlowKind    string `json:"-"`
	FlowOwnerID int64  `json:"-"`
}

// FlowOwner 是「流程 → 主体」的最小投影，用于拼收件箱 deeplink。
type FlowOwner struct {
	Kind    string // SUPPLIER / CUSTOMER
	OwnerID int64  // SUPPLIER→supplier.id；CUSTOMER→customer_profile.id
}

// InboxResult 收件箱分页结果。
type InboxResult struct {
	List  []InboxItem `json:"list"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
}

// Person 人员选择器的一行。
type Person struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PeopleResult 人员选择器结果。
type PeopleResult struct {
	List []Person `json:"list"`
}

// OperationQuery 操作记录查询。
type OperationQuery struct {
	OperatorID int64
	EntityType string
	EntityID   int64
	Page       int
	Size       int
}

// OperationItem 操作记录单行。
type OperationItem struct {
	ID           int64     `json:"id"`
	Action       string    `json:"action"`
	EntityType   string    `json:"entity_type"`
	EntityID     int64     `json:"entity_id"`
	OperatorName string    `json:"operator_name"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// OperationResult 操作记录分页结果。
type OperationResult struct {
	List  []OperationItem `json:"list"`
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Size  int             `json:"size"`
}

// InboxStore 收件箱仓储。
type InboxStore interface {
	ListInbox(ctx context.Context, q InboxQuery) ([]InboxItem, int64, error)
	// FlowOwners 按 flow id 批量取「主体类型 + 主体 id」，供 ONBOARDING_NODE 拼 deeplink。
	// 必须是**批量**接口：收件箱一页可能有多条节点待办，逐行查会成为 N+1。
	FlowOwners(ctx context.Context, flowIDs []int64) (map[int64]FlowOwner, error)
	ListPeople(ctx context.Context) ([]Person, error)
	ListOperations(ctx context.Context, q OperationQuery) ([]OperationItem, int64, error)
	// MarkNoticeRead 标记公告已读；返回 false 表示公告不存在。
	MarkNoticeRead(ctx context.Context, noticeID, staffID int64) (bool, error)
}

// InboxService 工作台收件箱服务。
type InboxService struct {
	store InboxStore
}

// NewInboxService 构造。
func NewInboxService(store InboxStore) *InboxService {
	return &InboxService{store: store}
}

// Inbox 查询收件箱。viewerID 是当前登录人；AssigneeID 可指定他人（只读）。
func (s *InboxService) Inbox(ctx context.Context, q InboxQuery, viewerID int64) (*InboxResult, error) {
	if q.State == "" {
		q.State = "TODO"
	}
	switch q.State {
	case "TODO", "READ", "DONE", "READ_DONE":
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidState, q.State)
	}
	if q.AssigneeID == 0 {
		q.AssigneeID = viewerID
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Size <= 0 {
		q.Size = 20
	}
	if q.Size > 100 {
		q.Size = 100
	}
	list, total, err := s.store.ListInbox(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list inbox: %w", err)
	}
	// 先补 ONBOARDING_NODE 的 kind/owner（每页最多 1 次批量查询），再逐行拼 deeplink。
	if err := s.attachFlowOwners(ctx, list); err != nil {
		return nil, fmt.Errorf("attach flow owners: %w", err)
	}
	for i := range list {
		list[i].AssigneeRole = inboxAssigneeRole(list[i])
		list[i].Deeplink = inboxItemDeeplink(list[i])
		list[i].Readable = list[i].BizType == "NOTICE" && list[i].State == "READ" && q.AssigneeID == viewerID
	}
	return &InboxResult{List: list, Total: total, Page: q.Page, Size: q.Size}, nil
}

// attachFlowOwners 给 ONBOARDING_NODE 行补上流程主体类型与主体 id。
//
// 为什么不在 SQL 里 join：`workbenchInboxSource` 由 6 条分支（5 个 UNION ALL）组成，
// 每条都显式列同一套列；往里加列要同时改 6 条分支的列数与类型，一不小心就是运行期
// UNION 类型不匹配，而且会无谓牵动供应商那 5 条分支。这里改成"取到当页再批量补一次"：
// 查询次数固定为 1（页内无节点待办时为 0），与行数无关。
func (s *InboxService) attachFlowOwners(ctx context.Context, list []InboxItem) error {
	ids := make([]int64, 0, len(list))
	seen := make(map[int64]bool, len(list))
	for i := range list {
		if list[i].BizType != "ONBOARDING_NODE" || list[i].BizID <= 0 || seen[list[i].BizID] {
			continue
		}
		seen[list[i].BizID] = true
		ids = append(ids, list[i].BizID)
	}
	if len(ids) == 0 {
		return nil
	}
	owners, err := s.store.FlowOwners(ctx, ids)
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].BizType != "ONBOARDING_NODE" {
			continue
		}
		if owner, ok := owners[list[i].BizID]; ok {
			list[i].FlowKind = owner.Kind
			list[i].FlowOwnerID = owner.OwnerID
		}
	}
	return nil
}

// inboxAssigneeRole 原型「责任人」列的副标题：这个人在这一行里是什么身份。
func inboxAssigneeRole(item InboxItem) string {
	switch item.BizType {
	case "NOTICE":
		return "阅读接收人"
	case "SUBMISSION_APPROVAL", "QUOTE_APPROVAL":
		return "审批人"
	case "SUBMISSION_HANDLING", "SUBMISSION_CONTRACT", "ONBOARDING_NODE":
		return "办理人"
	case "PLAN":
		return "发起人"
	default:
		return "处理人"
	}
}

func inboxItemDeeplink(item InboxItem) string {
	if item.BizType == "PLAN" && item.PlanID > 0 && (item.Region == "CN" || item.Region == "OVERSEAS") {
		return fmt.Sprintf("/internal/model-plaza/%d?region=%s&tab=plans&plan_id=%d", item.BizID, item.Region, item.PlanID)
	}
	return inboxDeeplink(item)
}

// People 人员选择器（只读查看他人收件箱用）。
func (s *InboxService) People(ctx context.Context) (*PeopleResult, error) {
	list, err := s.store.ListPeople(ctx)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	return &PeopleResult{List: list}, nil
}

// Operations 操作记录。
func (s *InboxService) Operations(ctx context.Context, q OperationQuery) (*OperationResult, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Size <= 0 {
		q.Size = 20
	}
	if q.Size > 100 {
		q.Size = 100
	}
	list, total, err := s.store.ListOperations(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list operations: %w", err)
	}
	return &OperationResult{List: list, Total: total, Page: q.Page, Size: q.Size}, nil
}

// MarkNoticeRead 标记本人公告已读。
func (s *InboxService) MarkNoticeRead(ctx context.Context, noticeID, staffID int64) error {
	ok, err := s.store.MarkNoticeRead(ctx, noticeID, staffID)
	if err != nil {
		return fmt.Errorf("mark notice read: %w", err)
	}
	if !ok {
		return ErrNoticeNotFound
	}
	return nil
}

// ErrInvalidState 收件箱状态非法（handler 映射为 400）。
var ErrInvalidState = fmt.Errorf("invalid state")

// ErrNoticeNotFound 公告不存在（handler 映射为 404）。
var ErrNoticeNotFound = fmt.Errorf("notice not found")

// inboxDeeplink 收件箱跳转。目标页面尚未落地的先返回空，由前端提示"详情页建设中"。
func inboxDeeplink(item InboxItem) string {
	switch item.BizType {
	case "SUBMISSION_APPROVAL":
		return fmt.Sprintf("/internal/workbench?submission_id=%d", item.BizID)
	case "SUBMISSION_HANDLING", "SUBMISSION_CONTRACT":
		return fmt.Sprintf("/internal/workbench?node_id=%d", item.BizID)
	case "PLAN":
		return fmt.Sprintf("/internal/model-plaza/%d", item.BizID)
	case "ONBOARDING_NODE":
		// 客户开通节点：落到客户详情「档案与办理」Tab（?tab=onboarding）。
		// owner_id 是 customer_profile.id（迁移 000050 已把注释纠偏为这个语义）。
		if item.FlowKind == "CUSTOMER" && item.FlowOwnerID > 0 {
			return fmt.Sprintf("/internal/customers/%d?tab=onboarding", item.FlowOwnerID)
		}
		// 供应商节点（以及取不到 kind 的防御分支）保持原有行为，一字不改。
		// 旧版通用办理节点没有独立页面，落到供应商列表并按供应商名过滤。
		return "/internal/suppliers?keyword=" + url.QueryEscape(item.BizObject)
	case "QUOTE_APPROVAL":
		return fmt.Sprintf("/internal/supplier-quotes?quote_id=%d", item.BizID)
	default:
		return ""
	}
}
