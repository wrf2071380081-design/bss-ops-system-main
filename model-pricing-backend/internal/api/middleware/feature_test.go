package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"model_bss/internal/infra/config"
)

// TestFeatureGate 验证：模块关闭时精确拦截对应 pattern，且不误伤同前缀的保留接口。
func TestFeatureGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(f config.Features) *gin.Engine {
		r := gin.New()
		r.Use(FeatureGate(f))
		g := r.Group("/api")
		ok := func(c *gin.Context) { c.Status(http.StatusOK) }
		g.POST("/internal/approvals/:id/decision", ok)
		g.POST("/internal/model-applications", ok)              // 提交：保留
		g.POST("/internal/model-applications/:id/decision", ok) // 决策：随 approval 关
		g.GET("/internal/cost/params", ok)
		g.POST("/customer/login", ok) // 登录：永远保留
		g.GET("/customer/home", ok)   // 门户：随 customer_portal 关
		return r
	}

	cases := []struct {
		name     string
		features config.Features
		method   string
		path     string
		want     int
	}{
		{"approval 关 → 决策 404", config.Features{}, "POST", "/api/internal/approvals/1/decision", http.StatusNotFound},
		{"approval 关 → 申报提交保留", config.Features{}, "POST", "/api/internal/model-applications", http.StatusOK},
		{"approval 开 → 决策 200", config.Features{Approval: true}, "POST", "/api/internal/approvals/1/decision", http.StatusOK},
		{"cost 关 → 成本参数 404", config.Features{}, "GET", "/api/internal/cost/params", http.StatusNotFound},
		{"cost 开 → 成本参数 200", config.Features{Cost: true}, "GET", "/api/internal/cost/params", http.StatusOK},
		{"customer_portal 关 → 首页 404", config.Features{}, "GET", "/api/customer/home", http.StatusNotFound},
		{"customer_portal 开 → 首页 200", config.Features{CustomerPortal: true}, "GET", "/api/customer/home", http.StatusOK},
		{"登录永远保留", config.Features{}, "POST", "/api/customer/login", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(tc.features)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d, body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
