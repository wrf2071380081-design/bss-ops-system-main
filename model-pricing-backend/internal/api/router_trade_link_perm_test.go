package api

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"model_bss/pkg/perm"
)

// ---- G4 地基（000049）：客户「接入模型」绑定接口的挂载钉子 ----
//
// 守的是什么：
//
//	GET  /customers/:id/resource-links   → perm.M8View（M8:V）客户管理·查看
//	POST /resource-links                 → perm.M8Edit（M8:E）客户管理·编辑 + 幂等
//	POST /resource-links/:id/actions     → perm.M8Edit（M8:E）客户管理·编辑 + 幂等
//
// 为什么钉：这三条路由挂在 router_trade.go（C 域新路由的家），不在 router.go 里，
// 但一旦有人把权限点改错（比如照抄成 M4:V 报价权限）或者漏挂 Idempotency（红线 5），
// 编译与既有测试都不会报警。写法与 router_cost_perm_test.go 同形态：读源码文本。
//
// 覆盖范围：本测试只钉「挂载点」这一层；状态机与唯一索引由
// internal/domain/customer/link_test.go 与 supplypg 的集成测试覆盖。
func TestRouterCustomerResourceLinkPermissionMounts(t *testing.T) {
	src, err := os.ReadFile("router_trade.go")
	require.NoError(t, err, "读 router_trade.go 失败（测试必须在 internal/api 目录下运行）")
	text := string(src)

	// mountOf 取指定方法+路由注册后紧跟的第一个 middleware.RequirePerm(perm.X) 标识符名。
	mountOf := func(method, route string) string {
		re := regexp.MustCompile(`(?s)protected\.` + method + `\("` +
			regexp.QuoteMeta(route) + `",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
		m := re.FindStringSubmatch(text)
		require.NotNilf(t, m, "router_trade.go 里找不到 %s %s 的 RequirePerm 挂载（路由被改名或挂载被拆行到无法匹配？）", method, route)
		return m[1]
	}

	require.Equal(t, "M8View", mountOf("GET", "/customers/:id/resource-links"),
		"客户接入模型投影必须挂客户查看权限（M8:V），不能借报价权限（M4:V）")
	require.Equal(t, "M8Edit", mountOf("POST", "/resource-links"),
		"建绑定必须挂客户编辑权限（M8:E）")
	require.Equal(t, "M8Edit", mountOf("POST", "/resource-links/:id/actions"),
		"状态动作必须挂客户编辑权限（M8:E）")

	// 常量真值兜底：上面比的是标识符名，这里确认标识符指向的权限点值本身没被改。
	require.Equal(t, "M8:V", perm.M8View)
	require.Equal(t, "M8:E", perm.M8Edit)

	// 红线 5：两条写接口必须挂 Idempotency（关键字后面紧跟幂等中间件）。
	for _, route := range []string{"/resource-links", "/resource-links/:id/actions"} {
		re := regexp.MustCompile(`(?s)protected\.POST\("` + regexp.QuoteMeta(route) + `",\s*` +
			`middleware\.RequirePerm\(perm\.\w+\),\s*middleware\.Idempotency\(`)
		require.NotNilf(t, re.FindString(text), "POST %s 必须挂 middleware.Idempotency（红线 5）", route)
	}

	// 系统动作（下架 / 重新上架）不允许有 HTTP 入口：
	// 它们只能由供应域事件调用 service 方法，挂出来就等于把下架/上架的开关交给了前端。
	require.Empty(t, regexp.MustCompile(`protected\.\w+\("/resource-links[^"]*supply`).FindString(text),
		"不得为系统触发的下架/上架动作开放 HTTP 路由")
}
