package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
)

type supplierOperationHTTPStore struct { query supply.SourceOperationQuery }

func (s *supplierOperationHTTPStore) ListSourceOperations(_ context.Context, q supply.SourceOperationQuery) ([]supply.SourceOperation, int64, error) {
	s.query = q
	return []supply.SourceOperation{{ID: "9", EntityType: "contract", EntityID: "8", SupplierID: q.SupplierID, Region: "CN", Action: "UPDATE", CreatedAt: time.Now()}}, 1, nil
}

func TestSupplierOperationHTTPCategoryAndSafeSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &supplierOperationHTTPStore{}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{SourceOperations: supply.NewSourceOperationReader(store)})
	rec := supplyHTTPCall(r, "GET", "/api/internal/suppliers/3/operations?entity_type=contract", "", "", "")
	if rec.Code != 200 || store.query.SupplierID != "3" || store.query.EntityType != "contract" || !strings.Contains(rec.Body.String(), `"resource_id":""`) || strings.Contains(rec.Body.String(), `"after"`) {
		t.Fatalf("supplier history category/projection mismatch: %d %s", rec.Code, rec.Body.String())
	}
	rec = supplyHTTPCall(r, "GET", "/api/internal/suppliers/3/operations?entity_type=account", "", "", "")
	if rec.Code != 400 { t.Fatalf("unknown supplier history category accepted: %d", rec.Code) }
}
