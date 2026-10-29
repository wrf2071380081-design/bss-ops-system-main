package supply

import (
	"context"
	"strings"
	"time"
)

type Notice struct {
	ID                    string  `json:"id"`
	ResourceID            string  `json:"resource_id"`
	ResourceName          string  `json:"resource_name"`
	Region                string  `json:"region"`
	Kind                  string  `json:"kind"`
	Title                 string  `json:"title"`
	Content               string  `json:"content"`
	Reason                string  `json:"reason"`
	SourceEventID         string  `json:"source_event_id,omitempty"`
	AffectedStatus        string  `json:"affected_status"`
	AffectedCustomerCount *uint64 `json:"affected_customer_count,omitempty,string"`
	// Name 是工作台事项行的业务对象名（类型 · 对象），与公告标题分开。
	Name string `json:"name,omitempty"`
	// 原型「原通知」详情：计划名、供应商、预计执行时间、执行人、受影响客户名单。
	PlanName          string     `json:"plan_name,omitempty"`
	SupplierName      string     `json:"supplier_name,omitempty"`
	ScheduledAt       *time.Time `json:"scheduled_at,omitempty"`
	ExecutorName      string     `json:"executor_name,omitempty"`
	AffectedCustomers []string   `json:"affected_customers,omitempty"`
	PublishedAt       time.Time  `json:"published_at"`
}

func (n *Notice) Clone() *Notice {
	if n == nil {
		return nil
	}
	copy := *n
	if n.AffectedCustomerCount != nil {
		count := *n.AffectedCustomerCount
		copy.AffectedCustomerCount = &count
	}
	if n.ScheduledAt != nil {
		at := *n.ScheduledAt
		copy.ScheduledAt = &at
	}
	if n.AffectedCustomers != nil {
		copy.AffectedCustomers = append([]string{}, n.AffectedCustomers...)
	}
	return &copy
}
func validNotice(n *Notice) bool {
	return n != nil && validID(n.ID) && validID(n.ResourceID) && (n.Region == "CN" || n.Region == "OVERSEAS") && strings.TrimSpace(n.Kind) != "" && strings.TrimSpace(n.Title) != "" && !n.PublishedAt.IsZero() && ((n.AffectedStatus == "PENDING" && n.AffectedCustomerCount == nil) || (n.AffectedStatus == "READY" && n.AffectedCustomerCount != nil))
}

type NoticeRead struct {
	NoticeID string    `json:"notice_id"`
	StaffID  string    `json:"staff_id"`
	ReadAt   time.Time `json:"read_at"`
}

func (r *NoticeRead) Clone() *NoticeRead {
	if r == nil {
		return nil
	}
	copy := *r
	return &copy
}

type NoticeReadRecord struct {
	Notice             *Notice
	StaffID, StaffName string
	Read               *NoticeRead
}
type NoticeReadPermissions struct {
	CanMarkRead bool `json:"can_mark_read"`
}
type NoticeReadView struct {
	Notice      *Notice               `json:"notice"`
	StaffID     string                `json:"staff_id"`
	StaffName   string                `json:"staff_name"`
	ReadStatus  string                `json:"read_status"`
	ReadAt      *time.Time            `json:"read_at"`
	Permissions NoticeReadPermissions `json:"permissions"`
}
type NoticeQuery struct {
	StaffIDs                                []string
	AllStaff                                bool
	ReadStatus, Region, ResourceID, Keyword string
	Page, Size                              int
	SortBy, NoticeIDOrder, StaffIDOrder     string
}
type NoticeReadPage struct {
	List  []*NoticeReadView `json:"list"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

// The SQL adapter validates selected staff against the internal directory and
// filters the notice/person relation before paging. It must not impersonate staff.
type NoticeReadStore interface {
	ListNoticeReadStates(context.Context, NoticeQuery) ([]NoticeReadRecord, int64, error)
	GetNoticeReadState(context.Context, string, string) (NoticeReadRecord, error)
}
type NoticeReader struct{ store NoticeReadStore }

func NewNoticeReader(store NoticeReadStore) *NoticeReader { return &NoticeReader{store: store} }
func normalizeNoticeQuery(actor string, q NoticeQuery) (NoticeQuery, error) {
	if !validID(actor) {
		return q, invalid("actor_id", "expected authenticated staff bigint string")
	}
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return q, err
	}
	if len(q.StaffIDs) > 100 || (q.AllStaff && len(q.StaffIDs) > 0) {
		return q, invalid("staff_ids", "ambiguous or oversized staff selection")
	}
	if !q.AllStaff && len(q.StaffIDs) == 0 {
		q.StaffIDs = []string{actor}
	}
	ids := make([]string, 0, len(q.StaffIDs))
	seen := map[string]bool{}
	for _, id := range q.StaffIDs {
		if !validID(id) || seen[id] {
			return q, invalid("staff_ids", "expected distinct staff bigint strings")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	q.StaffIDs = ids
	if q.ReadStatus == "" {
		q.ReadStatus = "ALL"
	}
	if q.ReadStatus != "ALL" && q.ReadStatus != "READ" && q.ReadStatus != "UNREAD" {
		return q, invalid("read_status", "expected ALL, READ or UNREAD")
	}
	if q.Region != "" && q.Region != "CN" && q.Region != "OVERSEAS" {
		return q, invalid("region", "expected CN or OVERSEAS")
	}
	if q.ResourceID != "" && !validID(q.ResourceID) {
		return q, invalid("resource_id", "expected bigint string")
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	q.SortBy = "published_at"
	q.NoticeIDOrder = "desc"
	q.StaffIDOrder = "asc"
	return q, nil
}
func noticeReadView(actor string, row NoticeReadRecord) (*NoticeReadView, error) {
	if !validNotice(row.Notice) || !validID(row.StaffID) || strings.TrimSpace(row.StaffName) == "" {
		return nil, ErrReadContract
	}
	view := &NoticeReadView{Notice: row.Notice.Clone(), StaffID: row.StaffID, StaffName: row.StaffName, ReadStatus: "UNREAD", Permissions: NoticeReadPermissions{CanMarkRead: actor == row.StaffID}}
	view.Notice.PublishedAt = view.Notice.PublishedAt.UTC()
	if row.Read != nil {
		if row.Read.NoticeID != row.Notice.ID || row.Read.StaffID != row.StaffID || row.Read.ReadAt.IsZero() {
			return nil, ErrReadContract
		}
		at := row.Read.ReadAt.UTC()
		view.ReadAt = &at
		view.ReadStatus = "READ"
		view.Permissions.CanMarkRead = false
	}
	return view, nil
}
func (r *NoticeReader) List(ctx context.Context, actor string, q NoticeQuery) (*NoticeReadPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	q, err := normalizeNoticeQuery(actor, q)
	if err != nil {
		return nil, err
	}
	rows, total, err := r.store.ListNoticeReadStates(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < int64(len(rows)) || len(rows) > q.Size || (int64(q.Page-1)*int64(q.Size) >= total && len(rows) > 0) {
		return nil, ErrReadContract
	}
	result := &NoticeReadPage{List: []*NoticeReadView{}, Total: total, Page: q.Page, Size: q.Size}
	selected := map[string]bool{}
	for _, id := range q.StaffIDs {
		selected[id] = true
	}
	seen := map[string]bool{}
	for _, row := range rows {
		view, err := noticeReadView(actor, row)
		if err != nil {
			return nil, err
		}
		key := view.Notice.ID + ":" + view.StaffID
		if seen[key] || (!q.AllStaff && !selected[view.StaffID]) || (q.Region != "" && view.Notice.Region != q.Region) || (q.ResourceID != "" && view.Notice.ResourceID != q.ResourceID) || (q.ReadStatus != "ALL" && view.ReadStatus != q.ReadStatus) || (q.Keyword != "" && !strings.Contains(strings.ToLower(view.Notice.Title+" "+view.Notice.Content+" "+view.Notice.ResourceName), strings.ToLower(q.Keyword))) {
			return nil, ErrReadContract
		}
		seen[key] = true
		result.List = append(result.List, view)
	}
	return result, nil
}
func (r *NoticeReader) Get(ctx context.Context, actor, id, staff string) (*NoticeReadView, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actor) || !validID(id) {
		return nil, invalid("id", "expected notice and authenticated staff bigint strings")
	}
	if staff == "" {
		staff = actor
	}
	if !validID(staff) {
		return nil, invalid("staff_id", "expected bigint string")
	}
	row, err := r.store.GetNoticeReadState(ctx, id, staff)
	if err != nil {
		return nil, err
	}
	view, err := noticeReadView(actor, row)
	if err != nil {
		return nil, err
	}
	if view.Notice.ID != id || view.StaffID != staff {
		return nil, ErrReadContract
	}
	return view, nil
}
