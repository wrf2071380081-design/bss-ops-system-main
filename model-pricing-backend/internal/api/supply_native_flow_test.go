package api

import (
	"github.com/gin-gonic/gin"
	infra "model_bss/internal/infra/middleware"
	"testing"
)

func TestNativeFlowRoutesFailClosedAndKeepLegacySimulationAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterNativeSupplyFlowRoutes(r.Group("/api/internal"), nil, nil)
	for _, path := range []string{"/api/internal/flows/submissions/1", "/api/internal/flows/nodes/1", "/api/internal/flows/suppliers/1/progress", "/api/internal/flows/suppliers/1/submissions"} {
		rec := supplyHTTPCall(r, "GET", path, "", "", "trace")
		if rec.Code != 503 {
			t.Fatalf("%s got %d", path, rec.Code)
		}
	}
	rec := supplyHTTPCall(r, "POST", "/api/internal/flows/submissions/1/approve", `{}`, "native-approve", "trace")
	if rec.Code != 503 {
		t.Fatal(rec.Code)
	}
	for _, path := range []string{"/api/internal/flows/submissions/1/reject", "/api/internal/flows/nodes/1/complete-contract"} {
		rec := supplyHTTPCall(r, "POST", path, `{}`, "native-guard", "trace")
		if rec.Code != 503 {
			t.Fatalf("native guarded action %s got %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/internal/approvals/submissions/1/approve", "/api/internal/approvals/submissions/1/simulate"} {
		rec := supplyHTTPCall(r, "POST", path, `{}`, "legacy-absent", "trace")
		if rec.Code != 404 {
			t.Fatalf("legacy write unexpectedly restored: %s %d", path, rec.Code)
		}
	}
}
