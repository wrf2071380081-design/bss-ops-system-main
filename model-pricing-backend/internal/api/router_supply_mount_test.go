package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
)

func TestPublicRouterMountsConfiguredASupplyRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", supplyHTTPStaff())
		c.Next()
	})
	group := r.Group("/api/internal")
	deps := Deps{SupplyRoutes: &SupplyDeps{
		Reader:    supply.NewModelReader(supplyHTTPReadStore{supplyHTTPModel()}),
		IdemStore: newSupplyHTTPIdemStore(),
	}}

	registerSupplyRoutes(group, group, "internal", deps)
	rec := supplyHTTPCall(r, http.MethodGet, "/api/internal/standard-models", "", "", "router-supply")
	if rec.Code != http.StatusOK {
		t.Fatalf("A standard model API was not mounted by the public router: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
