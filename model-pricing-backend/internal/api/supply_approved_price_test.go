package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApprovedPriceRouteFailsClosedWithoutProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: 7})
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{})
	req := httptest.NewRequest("POST", "/api/internal/resources/11/apply-approved-price", strings.NewReader(`{"expected_revision":"2","source_row_id":"71","source_revision":"3","coefficient":"0.5","external_confirmed":true,"completion_note":"external change completed","reason":"apply approved submission"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("approved-price action must exist and reject missing providers, got HTTP %d", rec.Code)
	}
}
