package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"strings"
	"testing"
)

type approvedPriceHTTPFake struct {
	calls  int
	meta   supply.WriteMeta
	source supply.CoefficientSource
	err    error
}

func (f *approvedPriceHTTPFake) Change(_ context.Context, meta supply.WriteMeta, id string, expected uint64, coefficient string, source supply.CoefficientSource) (*supply.Resource, error) {
	f.calls++
	f.meta = meta
	f.source = source
	if f.err != nil {
		return nil, f.err
	}
	return &supply.Resource{ID: id, Revision: expected + 1, Coefficient: coefficient}, nil
}
func approvedPriceRouter(f *approvedPriceHTTPFake) *gin.Engine {
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), ApprovedPriceWriter: f})
	return r
}
func TestApprovedPriceHTTPActorReceiptReplayAndRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"expected_revision":"2","source_row_id":"71","source_revision":"3","coefficient":"0.5000","external_confirmed":true,"completion_note":"external operation ticket 123","reason":"apply assigned price change"}`
	f := &approvedPriceHTTPFake{}
	r := approvedPriceRouter(f)
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/apply-approved-price", body, "price-once", "price-trace")
		if rec.Code != 200 {
			t.Fatalf("receipt %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if f.calls != 1 || f.meta.ActorID != "7" || f.meta.RequestID != "price-trace" || f.source.Kind != "SUBMISSION" || f.source.ID != "71" || f.source.Revision != 3 {
		t.Fatal("wrong actor/source or duplicate application")
	}
	for i, bad := range []string{strings.Replace(body, `"external_confirmed":true`, `"external_confirmed":false`, 1), strings.Replace(body, `"source_revision":"3"`, `"source_revision":3`, 1), strings.Replace(body, `"reason":`, `"approved":true,"reason":`, 1)} {
		rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/apply-approved-price", bad, "bad-price-"+string(rune('a'+i)), "trace")
		if rec.Code != 400 {
			t.Fatalf("unsafe request %d got %d", i, rec.Code)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{supply.ErrForbidden, 403}, {supply.ErrRevisionConflict, 409}, {supply.ErrDependencyUnavailable, 503}} {
		rejected := &approvedPriceHTTPFake{err: tc.err}
		rec := supplyHTTPCall(approvedPriceRouter(rejected), "POST", "/api/internal/resources/11/apply-approved-price", body, "reject-price", "trace")
		if rec.Code != tc.status {
			t.Fatalf("%v got %d", tc.err, rec.Code)
		}
	}
}
