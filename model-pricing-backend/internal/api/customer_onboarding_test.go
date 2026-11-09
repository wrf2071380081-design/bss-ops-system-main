package api

import (
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
)

// TestCustomerOnboardingErrorMapping 钉住开通流程的领域错误 → HTTP 状态。
//
// 关键口径：所有「用户能自己解决」的状态冲突都必须是 409（先完成流程 / 换资源 / 补资料），
// 校验类 400，找不到 404，越权由中间件 403。
func TestCustomerOnboardingErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   int
		status int
	}{
		{"已有进行中流程 409", customer.ErrOnboardingInProgress, apperr.ErrConflict.Code, 409},
		{"节点已完成 409", customer.ErrOnboardingNodeCompleted, apperr.ErrConflict.Code, 409},
		{"前序未完成 409", customer.ErrOnboardingNodeNotReady, apperr.ErrConflict.Code, 409},
		{"合同授信资料未齐 409", customer.ErrOnboardingContractDocs, apperr.ErrConflict.Code, 409},
		{"绑定撞唯一索引 409", customer.ErrLinkModelAlreadyActive, apperr.ErrConflict.Code, 409},
		{"同资源已绑定 409", customer.ErrLinkDuplicateResource, apperr.ErrConflict.Code, 409},
		{"流程不存在 404", customer.ErrOnboardingFlowNotFound, apperr.ErrNotFound.Code, 404},
		{"节点不存在 404", customer.ErrOnboardingNodeNotFound, apperr.ErrNotFound.Code, 404},
		{"客户不存在 404", customer.ErrCustomerProfileNotFound, apperr.ErrNotFound.Code, 404},
		{"资源不存在 404", customer.ErrLinkResourceNotFound, apperr.ErrNotFound.Code, 404},
		{"没有模型 400", customer.ErrOnboardingModelsRequired, apperr.ErrInvalidParams.Code, 400},
		{"模型参数非法 400", customer.ErrOnboardingModelInvalid, apperr.ErrInvalidParams.Code, 400},
		{"资源重复 400", customer.ErrOnboardingModelDuplicated, apperr.ErrInvalidParams.Code, 400},
		{"资源未准入 400", customer.ErrOnboardingResourceNotAdmitted, apperr.ErrInvalidParams.Code, 400},
		{"跨部署节点 400", customer.ErrOnboardingRegionMismatch, apperr.ErrInvalidParams.Code, 400},
		{"缺办理人 400", customer.ErrOnboardingAssigneeRequired, apperr.ErrInvalidParams.Code, 400},
		{"首单缺合同办理人 400", customer.ErrOnboardingContractAssignee, apperr.ErrInvalidParams.Code, 400},
		{"缺说明 400", customer.ErrOnboardingReasonRequired, apperr.ErrInvalidParams.Code, 400},
		{"模型与资源不匹配 400", customer.ErrLinkModelMismatch, apperr.ErrInvalidParams.Code, 400},
		{"能力未接线 500", customer.ErrOnboardingUnavailable, apperr.ErrSystem.Code, 500},
	}
	for _, c := range cases {
		got := customerOnboardingErrToAppErr(c.err)
		require.Equalf(t, c.code, got.Code, "%s：业务码不符", c.name)
		require.Equalf(t, c.status, got.Status, "%s：HTTP 状态不符", c.name)
	}
}

// TestRouterCustomerOnboardingMounts 钉住三条路由的权限与幂等挂载。
func TestRouterCustomerOnboardingMounts(t *testing.T) {
	src, err := os.ReadFile("router_trade.go")
	require.NoError(t, err, "读 router_trade.go 失败（测试必须在 internal/api 目录下运行）")
	text := string(src)

	mountOf := func(method, route string) string {
		re := regexp.MustCompile(`(?s)protected\.` + method + `\("` + regexp.QuoteMeta(route) +
			`",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
		m := re.FindStringSubmatch(text)
		require.NotNilf(t, m, "router_trade.go 里找不到 %s %s 的 RequirePerm 挂载", method, route)
		return m[1]
	}

	require.Equal(t, "M8Edit", mountOf("POST", "/customers/:id/onboarding"), "发起开通必须 M8:E")
	require.Equal(t, "M8View", mountOf("GET", "/customers/:id/onboarding"), "读进度必须 M8:V")
	require.Equal(t, "M8Edit", mountOf("POST", "/customer-nodes/:id/complete"), "完成节点必须 M8:E")

	// 两条写接口必须挂幂等（红线 5）。
	for _, route := range []string{"/customers/:id/onboarding", "/customer-nodes/:id/complete"} {
		re := regexp.MustCompile(`(?s)protected\.POST\("` + regexp.QuoteMeta(route) + `",\s*` +
			`middleware\.RequirePerm\(perm\.\w+\),\s*middleware\.Idempotency\(`)
		require.NotNilf(t, re.FindString(text), "POST %s 必须挂 middleware.Idempotency", route)
	}
}

// TestRegisterTradeRoutesWithOnboardingDoesNotConflict 真注册一次路由树：
//
//	GET /customers/:id 与 GET /customers/:id/onboarding 是「参数 + 静态子路径」共存，
//	gin 冲突是**启动期 panic**，必须用真实注册来证明。
func TestRegisterTradeRoutesWithOnboardingDoesNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	root := engine.Group("/api")
	protected := root.Group("/internal")

	noop := func(*gin.Context) {}
	protected.GET("/customers", noop)
	protected.GET("/customers/:id", noop)
	protected.GET("/customers/:id/quote-context", noop)
	protected.GET("/customers/:id/resource-links", noop)
	protected.POST("/customers", noop)
	protected.POST("/customers/:id/transfer", noop)
	protected.POST("/resource-links", noop)
	protected.POST("/resource-links/:id/actions", noop)

	require.NotPanics(t, func() {
		registerTradeRoutes(root, protected, "internal", Deps{
			CustomerOnboardingSvc: customer.NewOnboardingService(nil, nil),
		})
	}, "客户开通三条路由不能与既有客户路由冲突")

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"POST /api/internal/customers/:id/onboarding",
		"GET /api/internal/customers/:id/onboarding",
		"POST /api/internal/customer-nodes/:id/complete",
	} {
		require.Truef(t, registered[want], "%s 未注册", want)
	}
	require.Equal(t, "M8:E", perm.M8Edit)
}
