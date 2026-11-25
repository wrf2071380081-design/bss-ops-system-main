package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

// supplyHTTPLenientStore 模拟"读得出来、但资料不符合当前规则"的存储：
// 详情接口应该 200 + data_warnings，而不是整条 500。
type supplyHTTPLenientStore struct {
	supplyHTTPReadStore
	warnings []string
}

func (s supplyHTTPLenientStore) GetModelLenient(context.Context, string) (*supply.StandardModel, []string, error) {
	return s.model, s.warnings, nil
}

func TestSupplyModelDetailReportsNonCompliantDataInsteadOfFailing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", supplyHTTPStaff())
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{
		Reader: supply.NewModelReader(supplyHTTPLenientStore{
			supplyHTTPReadStore: supplyHTTPReadStore{model: supplyHTTPModel()},
			warnings:            []string{"checked_at: verification date is in the future"},
		}),
	})
	rec := supplyHTTPCall(r, http.MethodGet, "/api/internal/standard-models/9007199254740993", "", "", "detail-warn")
	if rec.Code != http.StatusOK {
		t.Fatalf("内容不合规不该让详情失败: %d %s", rec.Code, rec.Body.String())
	}
	data, ok := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if !ok {
		t.Fatalf("详情响应结构异常: %s", rec.Body.String())
	}
	if data["load_error"] != nil {
		t.Fatalf("能读出内容时不该给降级标记: %v", data["load_error"])
	}
	warnings, ok := data["data_warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("缺少 data_warnings: %v", data["data_warnings"])
	}
	first, _ := warnings[0].(map[string]any)
	if first["path"] != "checked_at" {
		t.Fatalf("警告路径应为字段路径: %v", first)
	}
	if text, _ := first["message"].(string); text == "" {
		t.Fatalf("警告缺少中文说明: %v", first)
	}
	// 数据本身要完整下发，维护人才有东西可改。
	if data["name"] != "待补齐模型" || data["revision"] == "" {
		t.Fatalf("详情数据被截断: %v", data)
	}
}

// 结构损坏（连快照都读不出）时，详情仍然给 200 + load_error：让前端能显示
// 「无法修复，请联系运维」，而不是白屏。
func TestSupplyModelDetailReturnsPlaceholderWhenStructureIsBroken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) {
		c.Set("auth.operator", supplyHTTPStaff())
		c.Next()
	})
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{
		Reader: supply.NewModelReader(supplyHTTPBrokenStore{supplyHTTPReadStore{model: supplyHTTPModel()}}),
	})
	rec := supplyHTTPCall(r, http.MethodGet, "/api/internal/standard-models/9007199254740993", "", "", "detail-broken")
	if rec.Code != http.StatusOK {
		t.Fatalf("结构损坏应给占位而不是 500: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := supplyHTTPEnvelope(t, rec)["data"].(map[string]any)
	if text, _ := data["load_error"].(string); text == "" {
		t.Fatalf("结构损坏应带 load_error: %v", data)
	}
}

type supplyHTTPBrokenStore struct{ supplyHTTPReadStore }

func (s supplyHTTPBrokenStore) GetModelLenient(context.Context, string) (*supply.StandardModel, []string, error) {
	return nil, nil, supply.ErrReadContract
}
