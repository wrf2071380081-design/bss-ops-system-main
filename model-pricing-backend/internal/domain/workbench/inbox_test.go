package workbench

import (
	"context"
	"errors"
	"testing"
)

type fakeInboxStore struct {
	inbox     []InboxItem
	total     int64
	people    []Person
	ops       []OperationItem
	opTotal   int64
	lastInbox InboxQuery
}

func (f *fakeInboxStore) ListInbox(_ context.Context, q InboxQuery) ([]InboxItem, int64, error) {
	f.lastInbox = q
	return f.inbox, f.total, nil
}
func (f *fakeInboxStore) ListPeople(context.Context) ([]Person, error) { return f.people, nil }
func (f *fakeInboxStore) ListOperations(context.Context, OperationQuery) ([]OperationItem, int64, error) {
	return f.ops, f.opTotal, nil
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

func TestMarkNoticeReadNotFound(t *testing.T) {
	svc := NewInboxService(&fakeInboxStore{})
	if err := svc.MarkNoticeRead(context.Background(), 999, 1); !errors.Is(err, ErrNoticeNotFound) {
		t.Fatalf("err = %v, want ErrNoticeNotFound", err)
	}
}
