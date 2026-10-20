package supply

import (
	"context"
	"errors"
	"testing"
)

type planListFixture struct {
	records []PlanReadRecord
	total   int64
	query   PlanListQuery
}

func (s *planListFixture) GetPlanSummary(context.Context, string) (PlanReadRecord, error) {
	return s.records[0], nil
}
func (s *planListFixture) ListPlanSummaries(_ context.Context, q PlanListQuery) ([]PlanReadRecord, int64, error) {
	s.query = q
	return s.records, s.total, nil
}
func planListRecord() PlanReadRecord {
	return PlanReadRecord{Plan: &ResourcePlan{ID: "81", ResourceID: "11", Region: "CN", Kind: "OFFLINE", Status: "PENDING", CreatedBy: "7", CreatorName: "甲", Revision: 1, ExpectedResourceRevision: 2, DueAt: modelNow, CreatedAt: modelNow, Reason: "维护"}, ResourceID: "11", ResourceRegion: "CN", ResourceBuyerID: "8", ResourceRevision: 2}
}
func TestPlanListValidatesFiltersAndUsesOriginalInitiator(t *testing.T) {
	record := planListRecord()
	s := &planListFixture{records: []PlanReadRecord{record}, total: 1}
	page, err := NewPlanReader(s).List(context.Background(), "7", PlanListQuery{ResourceID: "11", Region: "CN", Status: "PENDING"})
	if err != nil || len(page.List) != 1 || !page.List[0].IsInitiator || !page.List[0].ReadOnly || s.query.Page != 1 || s.query.Size != 20 || s.query.SortBy != "created_at" || s.query.Order != "desc" || s.query.TieBreakBy != "id" {
		t.Fatalf("invalid list %+v %+v %v", page, s.query, err)
	}
	page.List[0].Reason = "changed"
	if record.Plan.Reason != "维护" {
		t.Fatal("alias")
	}
}
func TestPlanListRejectsInvalidQueryAndMissingCapability(t *testing.T) {
	s := &planListFixture{records: []PlanReadRecord{planListRecord()}, total: 1}
	r := NewPlanReader(s)
	for _, q := range []PlanListQuery{{ResourceID: "01"}, {Region: "cn"}, {Status: "DONE"}, {Kind: "RANDOM"}, {Size: 101}, {Page: -1}} {
		if _, err := r.List(context.Background(), "7", q); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
	if _, err := NewPlanReader(planReadSingleStore{planListRecord()}).List(context.Background(), "7", PlanListQuery{}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing capability %v", err)
	}
}

type planReadSingleStore struct{ record PlanReadRecord }

func (s planReadSingleStore) GetPlanSummary(context.Context, string) (PlanReadRecord, error) {
	return s.record, nil
}
func TestPlanListRejectsWrongScopeDuplicatesAndImpossiblePage(t *testing.T) {
	record := planListRecord()
	for _, s := range []*planListFixture{{records: []PlanReadRecord{record}, total: 1}, {records: []PlanReadRecord{record, record}, total: 2}, {records: []PlanReadRecord{record}, total: -1}} {
		q := PlanListQuery{ResourceID: "12"}
		if len(s.records) == 2 {
			q.ResourceID = ""
		}
		if _, err := NewPlanReader(s).List(context.Background(), "7", q); !errors.Is(err, ErrReadContract) {
			t.Fatalf("bad store accepted %v", err)
		}
	}
	s := &planListFixture{records: []PlanReadRecord{record}, total: 1}
	if _, err := NewPlanReader(s).List(context.Background(), "7", PlanListQuery{Page: 2}); !errors.Is(err, ErrReadContract) {
		t.Fatalf("impossible page %v", err)
	}
}
