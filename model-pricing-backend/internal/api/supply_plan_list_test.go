package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"testing"
)

type planHTTPListStore struct{ q supply.PlanListQuery }

func (s *planHTTPListStore) GetPlanSummary(context.Context, string) (supply.PlanReadRecord, error) {
	return supply.PlanReadRecord{}, supply.ErrNotFound
}
func (s *planHTTPListStore) ListPlanSummaries(_ context.Context, q supply.PlanListQuery) ([]supply.PlanReadRecord, int64, error) {
	s.q = q
	return []supply.PlanReadRecord{{Plan: &supply.ResourcePlan{ID: "81", ResourceID: "11", Region: "CN", Kind: "OFFLINE", Status: "PENDING", CreatedBy: "7", Revision: 1, ExpectedResourceRevision: 2, DueAt: supplyHTTPNow, CreatedAt: supplyHTTPNow, Reason: "维护"}, ResourceID: "11", ResourceRegion: "CN", ResourceBuyerID: "8", ResourceRevision: 2}}, 1, nil
}
func TestPlanHTTPListScopeAndReadProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	s := &planHTTPListStore{}
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{Plans: supply.NewPlanReader(s)})
	rec := supplyHTTPCall(r, "GET", "/api/internal/plans?resource_id=11&region=CN&status=PENDING", "", "", "")
	if rec.Code != 200 || s.q.ResourceID != "11" || s.q.Region != "CN" || s.q.Status != "PENDING" {
		t.Fatalf("invalid list %d %s %+v", rec.Code, rec.Body, s.q)
	}
	for _, query := range []string{"staff_id=8", "status=DONE", "region=cn", "page=0", "region=CN&region=CN"} {
		rec = supplyHTTPCall(r, "GET", "/api/internal/plans?"+query, "", "", "")
		if rec.Code != 400 {
			t.Fatalf("accepted %s: %d", query, rec.Code)
		}
	}
}
