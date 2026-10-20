package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"testing"
	"time"
)

type planHTTPWriter struct {
	calls           int
	actor, id, kind string
	expected        uint64
	err             error
}

func (w *planHTTPWriter) result(meta supply.WriteMeta, id string, expected uint64, status string) (*supply.ResourcePlan, error) {
	w.calls++
	w.actor = meta.ActorID
	w.id = id
	w.expected = expected
	if w.err != nil {
		return nil, w.err
	}
	return &supply.ResourcePlan{ID: "81", ResourceID: "11", Region: "CN", Kind: "OFFLINE", Status: status, CreatedBy: "7", Revision: expected + 1, ExpectedResourceRevision: 2, DueAt: supplyHTTPNow, CreatedAt: supplyHTTPNow, Reason: meta.Reason}, nil
}
func (w *planHTTPWriter) Create(_ context.Context, meta supply.WriteMeta, id string, expected uint64, kind string, due time.Time) (*supply.ResourcePlan, error) {
	w.kind = kind
	p, err := w.result(meta, id, expected, "PENDING")
	if p != nil {
		p.Revision = 1
		p.DueAt = due
	}
	return p, err
}
func (w *planHTTPWriter) Revise(_ context.Context, meta supply.WriteMeta, id string, expected uint64, due time.Time) (*supply.ResourcePlan, error) {
	return w.result(meta, id, expected, "PENDING")
}
func (w *planHTTPWriter) Cancel(_ context.Context, meta supply.WriteMeta, id string, expected uint64) (*supply.ResourcePlan, error) {
	return w.result(meta, id, expected, "CANCELED")
}
func planWriteRouter(w *planHTTPWriter, missing bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	deps := SupplyDeps{IdemStore: newSupplyHTTPIdemStore()}
	if !missing {
		deps.PlanWriter = w
	}
	RegisterSupplyRoutes(r.Group("/api/internal"), deps)
	return r
}

const planCreateBody = `{"resource_id":"11","expected_resource_revision":"2","kind":"OFFLINE","due_at":"2027-01-01T00:00:00Z","reason":"维护"}`

func TestPlanWriteHTTPCreateUsesAuthenticatedActorAndIdempotency(t *testing.T) {
	w := &planHTTPWriter{}
	r := planWriteRouter(w, false)
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/plans", planCreateBody, "plan-key", "trace")
		if rec.Code != 200 {
			t.Fatalf("create %d %s", rec.Code, rec.Body)
		}
	}
	if w.calls != 1 || w.actor != "7" || w.id != "11" || w.expected != 2 || w.kind != "OFFLINE" {
		t.Fatalf("bad actor/input %+v", w)
	}
}
func TestPlanWriteHTTPReviseAndCancelForwardPlanRevision(t *testing.T) {
	w := &planHTTPWriter{}
	r := planWriteRouter(w, false)
	for _, input := range []struct{ method, path, body, key string }{{"PUT", "/api/internal/plans/81", `{"expected_revision":"3","due_at":"2027-01-02T00:00:00Z","reason":"改期"}`, "revise"}, {"POST", "/api/internal/plans/81/cancel", `{"expected_revision":"4","reason":"取消"}`, "cancel"}} {
		rec := supplyHTTPCall(r, input.method, input.path, input.body, input.key, "trace")
		if rec.Code != 200 {
			t.Fatalf("%s %d %s", input.path, rec.Code, rec.Body)
		}
	}
	if w.expected != 4 || w.id != "81" || w.actor != "7" {
		t.Fatalf("bad forwarding %+v", w)
	}
}
func TestPlanWriteHTTPRejectsSelfReportedAuthorityAndInvalidDates(t *testing.T) {
	w := &planHTTPWriter{}
	r := planWriteRouter(w, false)
	for i, body := range []string{`{}`, `{"resource_id":"11","expected_resource_revision":2,"kind":"OFFLINE","due_at":"2027-01-01T00:00:00Z","reason":"维护"}`, `{"resource_id":"11","expected_resource_revision":"2","kind":"OFFLINE","due_at":"tomorrow","reason":"维护"}`, `{"resource_id":"11","expected_resource_revision":"2","kind":"OFFLINE","due_at":"2027-01-01T00:00:00Z","reason":"维护","created_by":"8"}`, `{"resource_id":"11","expected_resource_revision":"2","kind":"PRICE_CHANGE","due_at":"2027-01-01T00:00:00Z","reason":"维护"}`} {
		rec := supplyHTTPCall(r, "POST", "/api/internal/plans", body, "bad-"+string(rune('a'+i)), "trace")
		if rec.Code != 400 {
			t.Fatalf("unsafe request %d %s", rec.Code, rec.Body)
		}
	}
	if w.calls != 0 {
		t.Fatal("invalid request reached writer")
	}
}
func TestPlanWriteHTTPMissingDependencyAndDomainErrors(t *testing.T) {
	rec := supplyHTTPCall(planWriteRouter(nil, true), "POST", "/api/internal/plans", planCreateBody, "missing", "trace")
	if rec.Code != 503 {
		t.Fatalf("missing %d", rec.Code)
	}
	for _, item := range []struct {
		err    error
		status int
	}{{supply.ErrForbidden, 403}, {supply.ErrRevisionConflict, 409}} {
		w := &planHTTPWriter{err: item.err}
		rec = supplyHTTPCall(planWriteRouter(w, false), "POST", "/api/internal/plans", planCreateBody, "domain", "trace")
		if rec.Code != item.status {
			t.Fatalf("domain %d", rec.Code)
		}
	}
}
