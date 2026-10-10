package api

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"model_bss/pkg/perm"
)

// ---- 批次 5：M11 组织与权限接口的权限点挂载钉子 ----
//
// 为什么钉：批次 3b 排查时才发现 **M11 在整个 router.go 里零挂载**——
// 前端 internal.orgPermissions 路由守卫早就配了 M11:V，后端却 404，
// 而这个缺口没有任何测试能发现。本批把两个只读接口挂上后，必须钉住，
// 否则「因为只是查看就放宽成不挂权限点」的改动会静默通过。
//
// 守的是什么：
//
//	GET /org-permissions/organizations → perm.M11View（M11:V）
//	GET /org-permissions/roles         → perm.M11View（M11:V）
//	router.go 里不得出现 M11 的**写**权限点挂载（M11:E / M11:A / M11:C / M11:P）
//
// 第三条是硬约束：本批**不实现任何写操作**（任务书），所以写权限点在路由层
// 必须保持零挂载。若将来要做「改权限树」，必须同时改本测试并说明理由。
//
// 形态说明：与 router_cost_perm_test.go 同手法（读 router.go 源码做正则断言）——
// api.New 需要完整 Deps，为两条只读路由搭一整套基建不划算。
func TestRouterOrgPermissionMountPoints(t *testing.T) {
	src, err := os.ReadFile("router.go")
	require.NoError(t, err, "读 router.go 失败（测试必须在 internal/api 目录下运行）")
	text := string(src)

	// mountOf 取指定路由注册后紧跟的第一个 middleware.RequirePerm(perm.X) 的标识符名。
	mountOf := func(route string) string {
		re := regexp.MustCompile(`(?s)protected\.GET\("` +
			regexp.QuoteMeta(route) + `",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
		m := re.FindStringSubmatch(text)
		require.NotNilf(t, m, "router.go 里找不到 %s 的 RequirePerm 挂载（路由被改名或挂载被拆行到无法匹配？）", route)
		return m[1]
	}

	require.Equal(t, "M11View", mountOf("/org-permissions/organizations"),
		"组织树必须挂 perm.M11View（M11:V）——返回的是全系统权限配置，不能因为只读就不挂")
	require.Equal(t, "M11View", mountOf("/org-permissions/roles"),
		"角色矩阵必须挂 perm.M11View（M11:V）")

	// 常量真值兜底：上面比的是标识符名，这里再确认 M11View 的值本身没被改。
	require.Equal(t, "M11:V", perm.M11View)

	// 本批不实现写操作 → router.go 里不允许出现 M11 写权限点挂载。
	for ident, point := range map[string]string{
		"M11Edit":      perm.M11Edit,
		"M11Approve":   perm.M11Approve,
		"M11Config":    perm.M11Config,
		"M11Privilege": perm.M11Privilege,
	} {
		require.NotContainsf(t, text, "perm."+ident,
			"router.go 不应挂载 M11 写权限点 %s（%s）：批次 5 只有只读接口，"+
				"若确实要开放写操作，请同时更新本测试并说明审批链路", ident, point)
	}
}
