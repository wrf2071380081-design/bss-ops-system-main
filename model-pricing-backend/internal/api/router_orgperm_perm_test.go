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
//	写侧（2026-10-13 账号管理）→ perm.M11Edit（M11:E），且服务层再收敛到 PLATFORM_ADMIN
//
// 2026-10-13 变更说明（原第三条硬约束已按本条放宽）：
// 批次 5 时 M11 写权限点零挂载，因为**当时平台没有任何账号管理入口**——
// 连 PLATFORM_ADMIN 都无法在界面上建人/授权/改口令，只能手工插库。
// 本批补齐账号管理写侧（/org-permissions/staff*），授权口径：
//   - 第一层：中间件 RequirePerm(M11:E)，000031 后该点仅 PLATFORM_ADMIN 持有；
//   - 第二层：服务层 perm.AnyRoleCan(CanManageStaff) 按角色 code 收敛到 PLATFORM_ADMIN
//     （见 StaffHandler.canManage / pkg/perm.CanManageStaff，防御性冗余）。
//   - 数据域：不做行级过滤（账号管理是全系统职能，无「谁的员工」归属概念）。
//   - 审计：STAFF_CREATE / STAFF_UPDATE / STAFF_PASSWORD_RESET / STAFF_ROLE_GRANT /
//     STAFF_ROLE_REVOKE 全写 audit_log，口令只记「已重置」不记明文或密文。
//
// 若将来要开放 M11:A / M11:C / M11:P，必须再次更新本测试并说明对应的审批链路。
//
// 形态说明：与 router_cost_perm_test.go 同手法（读 router.go 源码做正则断言）——
// api.New 需要完整 Deps，为几条路由搭一整套基建不划算。
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

	// 账号管理写侧（2026-10-13）：只允许挂 M11Edit，且必须同时挂幂等中间件（红线 5）。
	// 逐一断言每个写路由的权限点，避免「新增写路由忘了挂权限点」静默通过。
	for _, route := range []string{
		"/org-permissions/staff",
		"/org-permissions/staff/:id",
		"/org-permissions/staff/:id/password",
		"/org-permissions/staff/:id/roles",
		"/org-permissions/staff/:id/roles/:roleId",
	} {
		re := regexp.MustCompile(`(?s)protected\.(GET|POST|PUT|DELETE)\("` +
			regexp.QuoteMeta(route) + `",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
		m := re.FindStringSubmatch(text)
		require.NotNilf(t, m, "router.go 里找不到 %s 的 RequirePerm 挂载", route)
		require.Equalf(t, "M11Edit", m[2],
			"%s 必须挂 perm.M11Edit（M11:E）——账号管理是最高危写操作", route)

		// 同一路由注册块内必须出现 Idempotency（写接口幂等，红线 5）。
		// 用「路由起点后 500 字符内出现 Idempotency」判定，不依赖结尾括号形态
		// （注册块可能是 `h.X,\n\t\t\t)` 也可能是单行 `h.X)`）。
		startRe := regexp.MustCompile(`(?s)protected\.` + m[1] + `\("` +
			regexp.QuoteMeta(route) + `",`)
		loc := startRe.FindStringIndex(text)
		require.NotNilf(t, loc, "取不到 %s 的注册位置", route)
		end := loc[1] + 500
		if end > len(text) {
			end = len(text)
		}
		require.Containsf(t, text[loc[1]:end], "middleware.Idempotency(",
			"%s 是写接口，必须挂幂等中间件（红线 5）", route)
	}

	// M11:A / M11:C / M11:P 仍保持零挂载——账号管理不需要审批/配置/特权语义。
	for ident, point := range map[string]string{
		"M11Approve":   perm.M11Approve,
		"M11Config":    perm.M11Config,
		"M11Privilege": perm.M11Privilege,
	} {
		require.NotContainsf(t, text, "perm."+ident,
			"router.go 不应挂载 %s（%s）：账号管理只用 M11:E；"+
				"若确实要开放，请同时更新本测试并说明审批链路", ident, point)
	}
}
