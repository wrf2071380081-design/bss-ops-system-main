package api

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type pricePlanHTTPWriter struct {
	calls, creates, revises                                int
	actor, resourceID, planID, assigneeID, supplierModelID string
	expected                                               uint64
	amount                                                 string
	err                                                    error
}

func (w *pricePlanHTTPWriter) CreatePriceChange(_ context.Context, meta supply.WriteMeta, resourceID string, expected uint64, supplierModelID, amount, assigneeID string, due time.Time) (*supply.ResourcePlan, error) {
	w.calls++
	w.creates++
	w.actor, w.resourceID, w.expected, w.amount, w.assigneeID, w.supplierModelID = meta.ActorID, resourceID, expected, amount, assigneeID, supplierModelID
	if w.err != nil {
		return nil, w.err
	}
	return &supply.ResourcePlan{ID: "81", ResourceID: resourceID, Region: "CN", Kind: "PRICE_CHANGE", Status: "PENDING", CreatedBy: meta.ActorID, AssigneeID: assigneeID, SupplierModelID: supplierModelID, BeforeSupplierModelID: "vendor-model-v1", AfterSupplierModelID: supplierModelID, Revision: 1, ExpectedResourceRevision: expected, BeforeCoefficient: "0.6800", AfterCoefficient: amount, DueAt: due, CreatedAt: supplyHTTPNow, Reason: meta.Reason}, nil
}

func (w *pricePlanHTTPWriter) RevisePriceChange(_ context.Context, meta supply.WriteMeta, id string, expected uint64, supplierModelID, amount, assigneeID string, due time.Time) (*supply.ResourcePlan, error) {
	w.calls++
	w.revises++
	w.actor, w.planID, w.expected, w.amount, w.assigneeID, w.supplierModelID = meta.ActorID, id, expected, amount, assigneeID, supplierModelID
	if w.err != nil {
		return nil, w.err
	}
	return &supply.ResourcePlan{ID: id, ResourceID: "11", Region: "CN", Kind: "PRICE_CHANGE", Status: "PENDING", CreatedBy: meta.ActorID, AssigneeID: assigneeID, SupplierModelID: supplierModelID, BeforeSupplierModelID: "vendor-model-v1", AfterSupplierModelID: supplierModelID, Revision: expected + 1, ExpectedResourceRevision: 2, BeforeCoefficient: "0.6800", AfterCoefficient: amount, DueAt: due, CreatedAt: supplyHTTPNow, Reason: meta.Reason}, nil
}

func pricePlanWriteRouter(w *pricePlanHTTPWriter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	deps := SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), PricePlanWriter: w}
	RegisterSupplyRoutes(r.Group("/api/internal"), deps)
	return r
}

const pricePlanCreateBody = `{"resource_id":"11","supplier_model_id":"vendor-model-v1","expected_resource_revision":"2","after_coefficient":"0.5000","assignee_id":"8","due_at":"2027-01-01T00:00:00Z","reason":"内部采购调整"}`

func TestPricePlanHTTPCreateUsesAuthenticatedCreatorAndExactAssignedExecutor(t *testing.T) {
	w := &pricePlanHTTPWriter{}
	r := pricePlanWriteRouter(w)
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/plans/price-change", pricePlanCreateBody, "price-plan-create", "trace")
		if rec.Code != 200 {
			t.Fatalf("create %d %s", rec.Code, rec.Body)
		}
	}
	if w.calls != 1 || w.creates != 1 || w.actor != "7" || w.resourceID != "11" || w.expected != 2 || w.supplierModelID != "vendor-model-v1" || w.amount != "0.5000" || w.assigneeID != "8" {
		t.Fatalf("bad price plan forwarding: %+v", w)
	}
}

func TestPricePlanHTTPRevisionAndStrictAuthority(t *testing.T) {
	w := &pricePlanHTTPWriter{}
	r := pricePlanWriteRouter(w)
	rec := supplyHTTPCall(r, "PUT", "/api/internal/plans/81/price-change", `{"expected_revision":"3","supplier_model_id":"vendor-model-v1","after_coefficient":"0.4500","assignee_id":"9","due_at":"2027-01-02T00:00:00Z","reason":"调整安排"}`, "price-plan-revise", "trace")
	if rec.Code != 200 || w.calls != 1 || w.revises != 1 || w.actor != "7" || w.planID != "81" || w.expected != 3 || w.supplierModelID != "vendor-model-v1" || w.amount != "0.4500" || w.assigneeID != "9" {
		t.Fatalf("bad revision response=%d body=%s writer=%+v", rec.Code, rec.Body, w)
	}
	for i, body := range []string{`{}`, `{"resource_id":"11","expected_resource_revision":2,"after_coefficient":"0.5","assignee_id":"8","due_at":"2027-01-01T00:00:00Z","reason":"x"}`, `{"resource_id":"11","expected_resource_revision":"2","after_coefficient":"0.5","assignee_id":"8","due_at":"tomorrow","reason":"x"}`, `{"resource_id":"11","expected_resource_revision":"2","after_coefficient":"0.5","assignee_id":"8","due_at":"2027-01-01T00:00:00Z","reason":"x","created_by":"8"}`} {
		bad := supplyHTTPCall(r, "POST", "/api/internal/plans/price-change", body, "invalid-"+string(rune('a'+i)), "trace")
		if bad.Code != 400 {
			t.Fatalf("invalid input %d returned %d %s", i, bad.Code, bad.Body)
		}
	}
	if w.calls != 1 {
		t.Fatalf("invalid request reached writer: %+v", w)
	}
}
