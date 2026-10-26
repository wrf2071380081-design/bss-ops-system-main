package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func TestResourceProjectionEqualityIgnoresReadOnlyAdmissionTimestamp(t *testing.T) {
	base := ResourceProjection{ID: "1", SupplierID: "2", StandardModelID: "3", Region: "CN", SupplierModelID: "vendor-model", Coefficient: "0.7000", BuyerID: "4", State: "ADMITTED", EverAdmitted: true, CreatedAt: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)}
	derived := base
	admittedAt := base.CreatedAt.Add(time.Hour)
	derived.AdmittedAt = &admittedAt
	if !resourceProjectionEqual(base, derived) {
		t.Fatal("historical admission timestamp must not be treated as mutable resource control state")
	}
}

func TestResourceReadRejectsMalformedRequestsBeforeTransaction(t *testing.T) {
	if got, err := NewResourceReadStore(`public;DROP TABLE resource`, nil, nil); err == nil || got != nil {
		t.Fatal("unsafe schema accepted")
	}
	calls := 0
	s, err := NewResourceReadStore("public", func(context.Context, func(*sql.Tx) error) error { calls++; return supply.ErrDependencyUnavailable }, func(context.Context, *sql.Tx, ResourceProjection) (*ResourceReadFacts, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "0", "1 OR TRUE", "9223372036854775808"} {
		if _, err := s.GetResource(context.Background(), id); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	base := supply.ResourceListQuery{View: "flat", Page: 1, Size: 20, SortBy: "id", Order: "asc", NullsLast: true, TieBreakBy: "id"}
	for _, change := range []func(*supply.ResourceListQuery){
		func(q *supply.ResourceListQuery) { q.SortBy = "id;SELECT 1" }, func(q *supply.ResourceListQuery) { q.Order = "desc;SELECT 1" },
		func(q *supply.ResourceListQuery) { q.Page = 0 }, func(q *supply.ResourceListQuery) { q.Size = 101 }, func(q *supply.ResourceListQuery) { q.SupplierID = "1 OR TRUE" },
		func(q *supply.ResourceListQuery) { q.Region = "US" }, func(q *supply.ResourceListQuery) { q.State = "PLAN_OFFLINE" },
		func(q *supply.ResourceListQuery) { q.Keyword = "\x00" }, func(q *supply.ResourceListQuery) { q.NullsLast = false }, func(q *supply.ResourceListQuery) { q.TieBreakBy = "name" },
	} {
		q := base
		change(&q)
		if _, _, err := s.ListResourceRows(context.Background(), q); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("query accepted %+v: %v", q, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached transaction %d times", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetResource(ctx, "1"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden", err)
	}
	if calls != 0 {
		t.Fatal("canceled request reached transaction")
	}
}

func TestResourceReadQueriesBindValuesAndKeepDecimalOrdering(t *testing.T) {
	q := supply.ResourceListQuery{View: "flat", Page: 1, Size: 20, SortBy: "id", Order: "asc", NullsLast: true, TieBreakBy: "id", Keyword: "%_' OR TRUE--", Company: "O'Reilly", SupplierID: "9007199254740993", Region: "CN"}
	where, args, err := resourceReadQuery(q, "flat")
	if err != nil || len(args) != 4 || strings.Contains(where, "O'Reilly") || strings.Contains(where, "OR TRUE") || !strings.Contains(where, "strpos(") {
		t.Fatalf("unbound predicate %s %#v %v", where, args, err)
	}
	if args[0] != q.Keyword || args[1] != q.Company || args[2] != q.SupplierID || args[3] != q.Region {
		t.Fatalf("lost filter values %#v", args)
	}
	a := resourceReadEntry{view: &supply.ResourceSnapshot{ID: "9007199254740993", Test: supply.ResourceTestView{CacheStats: supply.ResourceCacheStats{Status: "PROVIDED", HitTokens: "9007199254740992", InputTokens: "9007199254740994"}}}}
	b := resourceReadEntry{view: &supply.ResourceSnapshot{ID: "2", Test: supply.ResourceTestView{CacheStats: supply.ResourceCacheStats{Status: "PROVIDED", HitTokens: "9007199254740993", InputTokens: "9007199254740994"}}}}
	q.SortBy = "cache_hit_rate"
	if resourceFlatCompare(a, b, q) >= 0 {
		t.Fatal("cache ratio rounded or converted to float before ordering")
	}
	q.SortBy = "id"
	if resourceFlatCompare(a, b, q) <= 0 {
		t.Fatal("bigint ID compared lexically")
	}
	q.SortBy = "tps"
	q.Order = "desc"
	if resourceFlatCompare(a, b, q) <= 0 {
		t.Fatal("NULL ties must keep ascending numeric IDs")
	}
}
