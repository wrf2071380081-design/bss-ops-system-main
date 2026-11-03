package api

import (
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"model_bss/internal/domain/customer"
	"model_bss/pkg/perm"
)

// TestRouterCustomerOperationLogsMount 钉住操作记录接口的挂载：M8:V + 路径。
func TestRouterCustomerOperationLogsMount(t *testing.T) {
	src, err := os.ReadFile("router_trade.go")
	require.NoError(t, err, "读 router_trade.go 失败（测试必须在 internal/api 目录下运行）")

	re := regexp.MustCompile(`protected\.GET\("` + regexp.QuoteMeta("/customers/:id/operation-logs") +
		`",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
	m := re.FindStringSubmatch(string(src))
	require.NotNil(t, m, "router_trade.go 里找不到 GET /customers/:id/operation-logs 的 RequirePerm 挂载")
	require.Equal(t, "M8View", m[1], "操作记录必须挂客户查看权限 M8:V")
	require.Equal(t, "M8:V", perm.M8View)
}

// TestRegisterTradeRoutesWithOperationLogsDoesNotConflict 真注册一次：
// 操作记录 / 开通进度 / 客户档案三条同前缀路由必须共存（gin 冲突是启动期 panic）。
func TestRegisterTradeRoutesWithOperationLogsDoesNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	root := engine.Group("/api")
	protected := root.Group("/internal")

	// 只预注册 **router.go** 里那几条客户路由（它们的归属文件不在 registerTradeRoutes 内）；
	// /customers/:id、/customers/:id/onboarding、/resource-links 等都由 registerTradeRoutes 自己注册，
	// 重复注册会以「handlers are already registered」panic —— 那是测试写错，不是路由冲突。
	noop := func(*gin.Context) {}
	protected.GET("/customers", noop)
	protected.GET("/customers/:id/quote-context", noop)
	protected.POST("/customers/:id/transfer", noop)
	protected.POST("/customer-quotes", noop)

	require.NotPanics(t, func() {
		registerTradeRoutes(root, protected, "internal", Deps{
			CustomerRegistrySvc: customer.NewRegistryService(nil),
			CustomerWorkspace:   NewCustomerWorkspaceRuntime(nil),
		})
	}, "操作记录路由不能与既有 /customers/:id/* 冲突")

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["GET /api/internal/customers/:id/operation-logs"], "操作记录路由未注册")
}
