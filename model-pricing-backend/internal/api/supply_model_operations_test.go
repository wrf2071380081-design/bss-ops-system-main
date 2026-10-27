package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"net/http/httptest"
	"strings"
	"testing"
)

type modelOperationHTTPStore struct{ supplyHTTPReadStore }

func (s modelOperationHTTPStore) ListModelOperations(context.Context, supply.ModelOperationQuery) ([]supply.ModelOperationItem, int64, error) {
	return []supply.ModelOperationItem{{ID: "9007199254740995", ModelID: s.model.ID, ModelName: "历史名称", Action: "UPDATE", CreatedAt: supplyHTTPNow}}, 1, nil
}
func (s modelOperationHTTPStore) GetModelOperation(_ context.Context, modelID, id string) (*supply.ModelOperationDetail, error) {
	if modelID != s.model.ID || id != "9007199254740995" {
		return nil, supply.ErrNotFound
	}
	return &supply.ModelOperationDetail{ModelOperationItem: supply.ModelOperationItem{ID: id, ModelID: modelID, ModelName: "历史名称", Action: "UPDATE", CreatedAt: supplyHTTPNow}}, nil
}
func TestSupplyModelOperationHTTPScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		identity, path string
		code           int
	}{{"staff", "/operations", 200}, {"staff", "/9007199254740993/operations/9007199254740995", 200}, {"staff", "/12/operations/9007199254740995", 404}, {"staff", "/operations?sort_by=after", 400}, {"supplier", "/operations", 403}, {"anonymous", "/operations", 401}} {
		t.Run(tc.identity+tc.path, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/internal")
			group.Use(func(c *gin.Context) {
				if tc.identity != "anonymous" {
					portal, kind := "INTERNAL", "STAFF"
					if tc.identity == "supplier" {
						portal, kind = "SUPPLIER", "SUPPLIER"
					}
					c.Set("auth.operator", &middleware.Operator{PortalType: portal, OperatorType: kind, StaffID: 7})
				}
				c.Next()
			})
			store := modelOperationHTTPStore{supplyHTTPReadStore{supplyHTTPModel()}}
			RegisterSupplyRoutes(group, SupplyDeps{Reader: supply.NewModelReader(store)})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/api/internal/standard-models"+tc.path, nil))
			if w.Code != tc.code {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if tc.code == 200 && !strings.Contains(w.Body.String(), `"id":"9007199254740995"`) {
				t.Fatal(w.Body.String())
			}
		})
	}
}
