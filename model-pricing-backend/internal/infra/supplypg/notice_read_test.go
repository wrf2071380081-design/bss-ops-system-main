package supplypg

import (
	"context"
	"errors"
	"math"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
)

func TestNoticeSelectionBindsInputsAndKeepsNumericStaffPredicate(t *testing.T) {
	keyword := `x%' OR 1=1 --`
	q := supply.NoticeQuery{StaffIDs: []string{"7", "9007199254740993"}, Region: "CN", ResourceID: "31", Keyword: keyword, ReadStatus: "UNREAD", Page: 2, Size: 10}
	selection, err := selectNotices(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(selection.where, keyword) || strings.Contains(selection.where, "9007199254740993") || len(selection.args) != 5 || selection.args[4] != keyword || !strings.Contains(selection.where, "nr.notice_id IS NULL") || selection.staffWhere != "s.id IN ($1::bigint,$2::bigint)" {
		t.Fatalf("unsafe/wrong predicates %+v", selection)
	}
	for _, mutate := range []func(*supply.NoticeQuery){func(q *supply.NoticeQuery) { q.Page = math.MaxInt }, func(q *supply.NoticeQuery) { q.Size = 101 }, func(q *supply.NoticeQuery) { q.StaffIDs = []string{"7", "7"} }, func(q *supply.NoticeQuery) { q.StaffIDs = []string{"0"} }, func(q *supply.NoticeQuery) { q.StaffIDs = nil }, func(q *supply.NoticeQuery) { q.AllStaff = true }, func(q *supply.NoticeQuery) { q.ReadStatus = "unknown" }, func(q *supply.NoticeQuery) { q.Region = "ALL" }, func(q *supply.NoticeQuery) { q.ResourceID = "01" }} {
		bad := q
		mutate(&bad)
		if _, err := selectNotices(bad); err == nil {
			t.Fatalf("accepted invalid query %+v", bad)
		}
	}
}
func TestNoticeReadMissingDBIsUnavailableNotEmptyOrUnread(t *testing.T) {
	for _, store := range []*NoticeReadStore{nil, NewNoticeReadStore(nil)} {
		if _, _, err := store.ListNoticeReadStates(context.Background(), supply.NoticeQuery{}); !errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("list unavailable: %v", err)
		}
		if _, err := store.GetNoticeReadState(context.Background(), "1", "7"); !errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("detail unavailable: %v", err)
		}
	}
}
