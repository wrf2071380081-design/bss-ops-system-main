package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"net/url"
	"testing"
)

type modelFilterHTTPStore struct {
	supplyHTTPReadStore
	query supply.ModelListQuery
}

func (s *modelFilterHTTPStore) ListModels(_ context.Context, q supply.ModelListQuery) ([]*supply.StandardModel, int64, error) {
	s.query = q
	return []*supply.StandardModel{s.model}, 1, nil
}
func (s *modelFilterHTTPStore) ModelFilterOptions(_ context.Context, q supply.ModelOptionsQuery) (supply.ModelFilterOptions, error) {
	return supply.ModelFilterOptions{Companies: []string{"Company, LLC"}, Suppliers: []supply.ModelSupplierOption{{ID: "21", Name: "甲", Region: "CN"}}}, nil
}
func TestModelHTTPMultiFiltersAndOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	store := &modelFilterHTTPStore{supplyHTTPReadStore: supplyHTTPReadStore{supplyHTTPModel()}}
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{Reader: supply.NewModelReader(store)})
	q := url.Values{"companies": {`["Company, LLC","OpenAI"]`}, "model_types": {`["TEXT","IMAGE"]`}, "supplier_ids": {`["21"]`}, "regions": {`["CN"]`}}
	rec := supplyHTTPCall(r, "GET", "/api/internal/standard-models?"+q.Encode(), "", "", "")
	if rec.Code != 200 || len(store.query.Companies) != 2 || store.query.Companies[0] != "Company, LLC" || len(store.query.SupplierIDs) != 1 {
		t.Fatalf("lost filters: %d %s %+v", rec.Code, rec.Body, store.query)
	}
	rec = supplyHTTPCall(r, "GET", "/api/internal/standard-models/filter-options?regions="+url.QueryEscape(`["CN"]`), "", "", "")
	if rec.Code != 200 {
		t.Fatalf("options %d %s", rec.Code, rec.Body)
	}
	for _, raw := range []string{`null`, `[1]`, `["CN"] true`, `{}`, `["CN","CN"]`, `["UNKNOWN"]`} {
		rec = supplyHTTPCall(r, "GET", "/api/internal/standard-models?regions="+url.QueryEscape(raw), "", "", "")
		if rec.Code != 400 {
			t.Fatalf("accepted %s: %d", raw, rec.Code)
		}
	}
	rec = supplyHTTPCall(r, "GET", "/api/internal/standard-models?company=A&companies="+url.QueryEscape(`["B"]`), "", "", "")
	if rec.Code != 400 {
		t.Fatalf("ambiguous %d", rec.Code)
	}
	rec = supplyHTTPCall(supplyHTTPRouter(supplyHTTPStaff(), nil, nil), "GET", "/api/internal/standard-models/filter-options", "", "", "")
	if rec.Code != 503 {
		t.Fatalf("missing options capability: %d", rec.Code)
	}
}
