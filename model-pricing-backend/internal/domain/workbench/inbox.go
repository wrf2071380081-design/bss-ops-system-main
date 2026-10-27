package workbench

import (
	"context"
	"fmt"
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
	Deeplink   string    `json:"deeplink,omitempty"`
	Readable   bool      `json:"readable"` // 只有本人、且是待阅，才可标记已读
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
	for i := range list {
		list[i].Deeplink = inboxItemDeeplink(list[i])
		list[i].Readable = list[i].BizType == "NOTICE" && list[i].State == "READ" && q.AssigneeID == viewerID
	}
	return &InboxResult{List: list, Total: total, Page: q.Page, Size: q.Size}, nil
}

func inboxItemDeeplink(item InboxItem) string {
	if item.BizType == "PLAN" && item.PlanID > 0 && (item.Region == "CN" || item.Region == "OVERSEAS") {
		return fmt.Sprintf("/internal/model-plaza/%d?region=%s&tab=plans&plan_id=%d", item.BizID, item.Region, item.PlanID)
	}
	return inboxDeeplink(item.BizType, item.BizID)
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
func inboxDeeplink(bizType string, bizID int64) string {
	switch bizType {
	case "SUBMISSION_APPROVAL":
		return fmt.Sprintf("/internal/workbench?submission_id=%d", bizID)
	case "SUBMISSION_HANDLING", "SUBMISSION_CONTRACT":
		return fmt.Sprintf("/internal/workbench?node_id=%d", bizID)
	case "PLAN":
		return fmt.Sprintf("/internal/model-plaza/%d", bizID)
	case "ONBOARDING_NODE":
		return fmt.Sprintf("/internal/supplier-quotes/%d", bizID)
	case "QUOTE_APPROVAL":
		return fmt.Sprintf("/internal/supplier-quotes?quote_id=%d", bizID)
	default:
		return ""
	}
}
