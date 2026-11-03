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

// TestCustomerRegistryErrorMapping 钉住客户创建的领域错误 → HTTP 状态映射。
//
// 关键口径：
//   - 重名 / USCC 被占用 → 409（用户自己改个名字就能继续，属于可解决冲突）
//   - 校验类 → 400（字段填错）
//   - 数据域越权 → 403；不存在 → 404
//   - 能力未接线 → 500（不是用户的错）
func TestCustomerRegistryErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   int
		status int
	}{
		{"重名 409", customer.ErrCustomerNameDuplicate, apperr.ErrConflict.Code, 409},
		{"USCC 被占用 409", customer.ErrCustomerUSCCDuplicate, apperr.ErrConflict.Code, 409},
		{"客户不存在 404", customer.ErrCustomerProfileNotFound, apperr.ErrNotFound.Code, 404},
		{"越权 403", customer.ErrCustomerOutOfScope, apperr.ErrForbidden.Code, 403},
		{"名称必填 400", customer.ErrCustomerNameRequired, apperr.ErrInvalidParams.Code, 400},
		{"名称超长 400", customer.ErrCustomerNameTooLong, apperr.ErrInvalidParams.Code, 400},
		{"主体类型 400", customer.ErrCustomerSubjectType, apperr.ErrInvalidParams.Code, 400},
		{"USCC 格式 400", customer.ErrCustomerUSCCInvalid, apperr.ErrInvalidParams.Code, 400},
		{"区域 400", customer.ErrCustomerRegionInvalid, apperr.ErrInvalidParams.Code, 400},
		{"币种 400", customer.ErrCustomerCurrencyInvalid, apperr.ErrInvalidParams.Code, 400},
		{"税率 400", customer.ErrCustomerTaxRateInvalid, apperr.ErrInvalidParams.Code, 400},
		{"缺销售 400", customer.ErrCustomerOwnerRequired, apperr.ErrInvalidParams.Code, 400},
		{"销售不存在 400", customer.ErrCustomerOwnerNotFound, apperr.ErrInvalidParams.Code, 400},
		{"结算方式 400", customer.ErrCustomerPaymentType, apperr.ErrInvalidParams.Code, 400},
		{"账期 400", customer.ErrCustomerBillingCycle, apperr.ErrInvalidParams.Code, 400},
		{"能力未接线 500", customer.ErrCustomerCreateUnavailable, apperr.ErrSystem.Code, 500},
	}
	for _, c := range cases {
		got := customerRegistryErrToAppErr(c.err)
		require.Equalf(t, c.code, got.Code, "%s：业务码不符", c.name)
		require.Equalf(t, c.status, got.Status, "%s：HTTP 状态不符", c.name)
	}
}

// TestRouterCustomerRegistryMounts 钉住两条路由的权限与幂等挂载（G4-B1）。
//
// 为什么读源码文本：本包无路由级测试基建（api.New 需要整套 Deps），而这里要钉的
// 恰恰是「挂哪个权限点 + 有没有挂幂等」这两行；改错不会有任何编译/运行期报警。
func TestRouterCustomerRegistryMounts(t *testing.T) {
	src, err := os.ReadFile("router_trade.go")
	require.NoError(t, err, "读 router_trade.go 失败（测试必须在 internal/api 目录下运行）")
	text := string(src)

	// POST /customers 必须 M8:E + 幂等。
	postPattern := regexp.MustCompile(`(?s)protected\.POST\("/customers",\s*` +
		`middleware\.RequirePerm\(perm\.(\w+)\),\s*middleware\.Idempotency\(`)
	m := postPattern.FindStringSubmatch(text)
	require.NotNil(t, m, "POST /customers 必须同时挂 RequirePerm 与 Idempotency")
	require.Equal(t, "M8Edit", m[1], "POST /customers 必须挂客户编辑权限 M8:E")

	// GET /customers/:id 必须 M8:V（注意不能误匹配到 /customers/:id/resource-links）。
	getPattern := regexp.MustCompile(`(?s)protected\.GET\("/customers/:id",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
	g := getPattern.FindStringSubmatch(text)
	require.NotNil(t, g, "GET /customers/:id 必须挂 RequirePerm")
	require.Equal(t, "M8View", g[1], "GET /customers/:id 必须挂客户查看权限 M8:V")

	require.Equal(t, "M8:V", perm.M8View)
	require.Equal(t, "M8:E", perm.M8Edit)
}

// TestRegisterTradeRoutesWithCustomerDetailDoesNotConflict 真注册一次路由树：
//
//	GET /customers/:id 是新增的「静态 + 参数」路径，必须和已有的
//	GET /customers/:id/quote-context、GET /customers/:id/resource-links 共存而不 panic
//	（gin 的 httprouter 在冲突时是**启动期 panic**，单元测试不覆盖就会直接炸服务）。
func TestRegisterTradeRoutesWithCustomerDetailDoesNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	root := engine.Group("/api")
	protected := root.Group("/internal")

	noop := func(*gin.Context) {}
	// 复刻 router.go 里已存在的客户路由，制造真实的冲突场景。
	protected.GET("/customers", noop)
	protected.GET("/customers/:id/quote-context", noop)
	protected.POST("/customers/:id/transfer", noop)
	protected.POST("/customer-quotes", noop)

	require.NotPanics(t, func() {
		registerTradeRoutes(root, protected, "internal", Deps{
			CustomerRegistrySvc: customer.NewRegistryService(nil),
		})
	}, "新增 GET /customers/:id 不能与既有 /customers/:id/* 冲突")

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["POST /api/internal/customers"], "POST /customers 未注册")
	require.True(t, registered["GET /api/internal/customers/:id"], "GET /customers/:id 未注册")
}
