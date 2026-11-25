package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

// supplyHTTPDegradedStore 让列表第二行读不出来（模拟快照/价格不合契约的历史数据）。
type supplyHTTPDegradedStore struct {
	supplyHTTPReadStore
	failure *supply.ModelFailure
}

func (s supplyHTTPDegradedStore) ListModels(context.Context, supply.ModelListQuery) ([]supply.ModelEntry, int64, error) {
	return []supply.ModelEntry{{Model: s.model}, {Failure: s.failure}}, 2, nil
}

// 2026-11-25 事故回归：单行读不出来必须是「这一行降级」，不能把整页打成 500。
func TestSupplyModelListDegradesUnreadableRowInsteadOfFailing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", supplyHTTPStaff())
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{
		Reader: supply.NewModelReader(supplyHTTPDegradedStore{
			supplyHTTPReadStore: supplyHTTPReadStore{model: supplyHTTPModel()},
			failure: &supply.ModelFailure{
				ID: "9007199254740995", Name: "坏行", Company: "Anthropic", ModelType: "TEXT",
				Status: "ACTIVE", UpdatedAt: supplyHTTPNow, Reason: "inconsistent snapshot",
			},
		}),
	})
	rec := supplyHTTPCall(r, http.MethodGet, "/api/internal/standard-models?page=1&size=20", "", "", "degrade-case")
	if rec.Code != http.StatusOK {
		t.Fatalf("单行异常不该让列表失败: %d %s", rec.Code, rec.Body.String())
	}
	env := supplyHTTPEnvelope(t, rec)
	list, ok := env["data"].(map[string]any)["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("列表行数不对: %v", env["data"])
	}
	good, bad := list[0].(map[string]any), list[1].(map[string]any)
	if good["load_error"] != nil {
		t.Fatalf("正常行不该带 load_error: %v", good["load_error"])
	}
	if text, _ := bad["load_error"].(string); text == "" {
		t.Fatalf("降级行缺少 load_error: %v", bad)
	}
	if bad["id"] != "9007199254740995" || bad["name"] != "坏行" || bad["company"] != "Anthropic" || bad["model_type"] != "TEXT" {
		t.Fatalf("降级行丢了可展示字段: %v", bad)
	}
	if bad["verify_status"] != "INCOMPLETE" || bad["status"] != "ACTIVE" {
		t.Fatalf("降级行状态不合法: %v / %v", bad["status"], bad["verify_status"])
	}
	// 降级行不可编辑：进详情只会再失败一次。
	permissions, _ := bad["permissions"].(map[string]any)
	if permissions["can_edit"] != false || permissions["can_verify"] != false {
		t.Fatalf("降级行不该给编辑/核验权: %v", permissions)
	}
}
