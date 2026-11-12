package workbench

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeInboxStore struct {
	inbox     []InboxItem
	total     int64
	people    []Person
	ops       []OperationItem
	opTotal   int64
	lastInbox InboxQuery
	// owners 是 FlowOwners 的返回值；flowOwnerCalls 记录调用次数（用于断言"每页只 +1 次查询"）。
	owners         map[int64]FlowOwner
	flowOwnerCalls int
	flowOwnerIDs   []int64
	// 操作记录筛选/对象名的测试支撑。
	lastOperations OperationQuery
	names          map[int64]string
	nameCalls      []string
	actions        []string
	entityTypes    []string
}

func TestNativeSubmissionTasksStayInsideWorkbench(t *testing.T) {
	for _, kind := range []string{"SUBMISSION_APPROVAL", "SUBMISSION_HANDLING", "SUBMISSION_CONTRACT"} {
		got := inboxItemDeeplink(InboxItem{BizType: kind, BizID: 42})
		key := "submission_id"
		if kind != "SUBMISSION_APPROVAL" {
			key = "node_id"
		}
		if want := "/internal/workbench?" + key + "=42"; got != want {
			t.Fatalf("%s: got %q want %q", kind, got, want)
		}
	}
}

func (f *fakeInboxStore) ListInbox(_ context.Context, q InboxQuery) ([]InboxItem, int64, error) {
	f.lastInbox = q
	return f.inbox, f.total, nil
}
func (f *fakeInboxStore) FlowOwners(_ context.Context, flowIDs []int64) (map[int64]FlowOwner, error) {
	f.flowOwnerCalls++
	f.flowOwnerIDs = append(f.flowOwnerIDs, flowIDs...)
	if f.owners == nil {
		return map[int64]FlowOwner{}, nil
	}
	return f.owners, nil
}
func (f *fakeInboxStore) ListPeople(context.Context) ([]Person, error) { return f.people, nil }
func (f *fakeInboxStore) ListOperations(_ context.Context, q OperationQuery) ([]OperationItem, int64, error) {
	f.lastOperations = q
	return f.ops, f.opTotal, nil
}
func (f *fakeInboxStore) ObjectNames(_ context.Context, entityType string, ids []int64) (map[int64]string, error) {
	f.nameCalls = append(f.nameCalls, entityType)
	if f.names == nil {
		return map[int64]string{}, nil
	}
	out := map[int64]string{}
	for _, id := range ids {
		if name, ok := f.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}
func (f *fakeInboxStore) OperationFacets(context.Context) ([]string, []string, error) {
	return f.actions, f.entityTypes, nil
}
func (f *fakeInboxStore) MarkNoticeRead(_ context.Context, noticeID, _ int64) (bool, error) {
	return noticeID == 1, nil
}

func TestInboxDefaultsAndReadable(t *testing.T) {
	store := &fakeInboxStore{inbox: []InboxItem{{BizType: "NOTICE", BizID: 1, State: "READ"}}, total: 1}
	svc := NewInboxService(store)

	res, err := svc.Inbox(context.Background(), InboxQuery{}, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastInbox.State != "TODO" {
		t.Fatalf("default state = %q, want TODO", store.lastInbox.State)
	}
	if store.lastInbox.AssigneeID != 7 {
		t.Fatalf("assignee = %d, want viewer 7", store.lastInbox.AssigneeID)
	}
	if res.Page != 1 || res.Size != 20 {
		t.Fatalf("paging = %d/%d, want 1/20", res.Page, res.Size)
	}
	if !res.List[0].Readable {
		t.Fatal("本人的待阅应可标记已读")
	}

	resOther, err := svc.Inbox(context.Background(), InboxQuery{State: "READ", AssigneeID: 8}, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resOther.List[0].Readable {
		t.Fatal("查看他人收件箱时不应可标记已读")
	}
}

func TestInboxInvalidState(t *testing.T) {
	svc := NewInboxService(&fakeInboxStore{})
	if _, err := svc.Inbox(context.Background(), InboxQuery{State: "BAD"}, 1); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

func TestInboxPlanDeeplinkCarriesPlanIDAndRegion(t *testing.T) {
	store := &fakeInboxStore{inbox: []InboxItem{{BizType: "PLAN", BizID: 31, PlanID: 81, Region: "OVERSEAS", State: "TODO"}}, total: 1}
	result, err := NewInboxService(store).Inbox(context.Background(), InboxQuery{}, 7)
	if err != nil || len(result.List) != 1 {
		t.Fatalf("inbox result=%+v err=%v", result, err)
	}
	if got, want := result.List[0].Deeplink, "/internal/model-plaza/31?region=OVERSEAS&tab=plans&plan_id=81"; got != want {
		t.Fatalf("plan deeplink=%q, want %q", got, want)
	}
}

func TestInboxQuoteApprovalDeeplinkOpensExistingApprovalQueue(t *testing.T) {
	store := &fakeInboxStore{inbox: []InboxItem{{BizType: "QUOTE_APPROVAL", BizID: 42, State: "TODO"}}, total: 1}
	result, err := NewInboxService(store).Inbox(context.Background(), InboxQuery{}, 7)
	if err != nil || len(result.List) != 1 {
		t.Fatalf("inbox result=%+v err=%v", result, err)
	}
	if got, want := result.List[0].Deeplink, "/internal/supplier-quotes?quote_id=42"; got != want {
		t.Fatalf("quote approval deeplink=%q, want %q", got, want)
	}
}

func TestInboxAssigneeRoleAndDeeplinkPerBizType(t *testing.T) {
	cases := []struct {
		item     InboxItem
		wantRole string
		wantLink string
	}{
		{InboxItem{BizType: "NOTICE", BizID: 9}, "阅读接收人", ""},
		{InboxItem{BizType: "SUBMISSION_APPROVAL", BizID: 42}, "审批人", "/internal/workbench?submission_id=42"},
		{InboxItem{BizType: "SUBMISSION_HANDLING", BizID: 42}, "办理人", "/internal/workbench?node_id=42"},
		{InboxItem{BizType: "SUBMISSION_CONTRACT", BizID: 42}, "办理人", "/internal/workbench?node_id=42"},
		{InboxItem{BizType: "QUOTE_APPROVAL", BizID: 42}, "审批人", "/internal/supplier-quotes?quote_id=42"},
		{InboxItem{BizType: "PLAN", BizID: 42}, "发起人", "/internal/model-plaza/42"},
	}
	for _, c := range cases {
		if got := inboxAssigneeRole(c.item); got != c.wantRole {
			t.Fatalf("%s role = %q, want %q", c.item.BizType, got, c.wantRole)
		}
		if got := inboxItemDeeplink(c.item); got != c.wantLink {
			t.Fatalf("%s deeplink = %q, want %q", c.item.BizType, got, c.wantLink)
		}
	}
	// 旧版通用办理节点没有独立页面，落到供应商列表并按供应商名过滤。
	legacy := InboxItem{BizType: "ONBOARDING_NODE", BizID: 7, BizObject: "云桥算力"}
	if got, want := inboxItemDeeplink(legacy), "/internal/suppliers?keyword=%E4%BA%91%E6%A1%A5%E7%AE%97%E5%8A%9B"; got != want {
		t.Fatalf("legacy node deeplink = %q, want %q", got, want)
	}
	if got := inboxAssigneeRole(legacy); got != "办理人" {
		t.Fatalf("legacy node role = %q, want 办理人", got)
	}
}

func TestMarkNoticeReadNotFound(t *testing.T) {
	svc := NewInboxService(&fakeInboxStore{})
	if err := svc.MarkNoticeRead(context.Background(), 999, 1); !errors.Is(err, ErrNoticeNotFound) {
		t.Fatalf("err = %v, want ErrNoticeNotFound", err)
	}
}

// TestOperationsResolveObjectNames 操作记录：筛选条件透传 + 对象展示名按类型补齐。
// 原型「对象」列是「供应商 / 模型」，取不到名字时要留空（前端退回「类型 #id」），不能整页失败。
func TestOperationsResolveObjectNames(t *testing.T) {
	store := &fakeInboxStore{
		ops: []OperationItem{
			{ID: 1, Action: "RESOURCE_REVISION", EntityType: "resource", EntityID: 16},
			{ID: 2, Action: "REJECT", EntityType: "submission", EntityID: 18},
			{ID: 3, Action: "UPDATE", EntityType: "unknown_type", EntityID: 9},
		},
		opTotal: 3,
		names:   map[int64]string{16: "云桥算力 / DeepSeek-V3", 18: "云桥算力 · S00000018"},
	}
	svc := NewInboxService(store)

	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 11, 12, 0, 0, 0, 0, time.Local)
	res, err := svc.Operations(context.Background(), OperationQuery{
		Action:     "RESOURCE_REVISION",
		EntityType: "resource",
		Keyword:    "云桥",
		DateFrom:   &from,
		DateTo:     &to,
		Page:       1,
		Size:       20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.lastOperations
	if got.Action != "RESOURCE_REVISION" || got.EntityType != "resource" || got.Keyword != "云桥" {
		t.Fatalf("filters not forwarded: %+v", got)
	}
	if got.DateFrom == nil || got.DateTo == nil || !got.DateFrom.Equal(from) || !got.DateTo.Equal(to) {
		t.Fatalf("date range not forwarded: %+v", got)
	}
	if res.List[0].ObjectName != "云桥算力 / DeepSeek-V3" {
		t.Fatalf("resource object name = %q", res.List[0].ObjectName)
	}
	if res.List[1].ObjectName != "云桥算力 · S00000018" {
		t.Fatalf("submission object name = %q", res.List[1].ObjectName)
	}
	if res.List[2].ObjectName != "" {
		t.Fatalf("unknown type should keep empty name, got %q", res.List[2].ObjectName)
	}
}

// TestOperationOptionsFallback 筛选条没有数据时也要返回空数组，前端下拉才不会渲染成 undefined。
func TestOperationOptionsFallback(t *testing.T) {
	svc := NewInboxService(&fakeInboxStore{})
	res, err := svc.OperationOptions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Actions == nil || res.EntityTypes == nil || res.Operators == nil {
		t.Fatalf("options must be non-nil: %+v", res)
	}
	if len(res.Actions) != 0 || len(res.EntityTypes) != 0 || len(res.Operators) != 0 {
		t.Fatalf("options should be empty: %+v", res)
	}
}
