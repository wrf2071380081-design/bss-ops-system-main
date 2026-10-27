package api

import (
	"github.com/gin-gonic/gin"
	infra "model_bss/internal/infra/middleware"
	"testing"
)

func TestResourceBatchRequiresRuntimeDependency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{})
	rec := supplyHTTPCall(r, "POST", "/api/internal/supplier-entities/1/resource-actions", `{}`, "batch-red", "trace")
	if rec.Code != 503 {
		t.Fatalf("expected unavailable stage writer, got %d", rec.Code)
	}
}
