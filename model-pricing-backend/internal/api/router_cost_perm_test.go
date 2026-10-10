package api

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"model_bss/pkg/perm"
)

// ---- 批次 3a：成本域写接口的权限点挂载钉子 ----
//
// 为什么采用「读 router.go 源码」这种形态：本包没有路由级测试基建——
// api.New 需要完整 Deps（Config / Log / Health / SessionStore …），为两条路由搭一整套
// 造价过高；而 3a 的核心产物恰恰就是「PUT /cost/params 从 M5:E 改挂 M5:C」这一行。
// 不钉住它，把挂载点改回 M5:E、让三个动作重新挤在同一个权限点上，不会有任何测试报警。
//
// 守的是什么（与迁移 000030_split_m5_permission、pkg/perm 的三个 Can* 一致）：
//
//	PUT  /cost/params                      → perm.M5Config（M5:C）成本参数配置，仅 PRICING_OP
//	POST /cost/baselines/:sku/lock-primary → perm.M5Edit  （M5:E）主供应商锁定，仅 PROCUREMENT
//
// 两者必须是**不同**的权限点——共用就退回了 3a 之前「三个动作共用一个 M5:E」的状态。
//
// 覆盖范围说明：本测试只钉挂载点（第一层）。角色维度的允许/禁止集合由
// pkg/perm 的 TestCostActions_* 钉住，两层合起来才是完整防线。
func TestRouterCostWritePermissionMountPoints(t *testing.T) {
	src, err := os.ReadFile("router.go")
	require.NoError(t, err, "读 router.go 失败（测试必须在 internal/api 目录下运行）")

	// mountOf 取指定路由注册后紧跟的第一个 middleware.RequirePerm(perm.X) 的标识符名。
	mountOf := func(route string) string {
		re := regexp.MustCompile(`(?s)protected\.(?:PUT|POST)\("` +
			regexp.QuoteMeta(route) + `",\s*middleware\.RequirePerm\(perm\.(\w+)\)`)
		m := re.FindStringSubmatch(string(src))
		require.NotNilf(t, m, "router.go 里找不到 %s 的 RequirePerm 挂载（路由被改名或挂载被拆行到无法匹配？）", route)
		return m[1]
	}

	gotParam := mountOf("/cost/params")
	gotLock := mountOf("/cost/baselines/:sku/lock-primary")

	require.Equal(t, "M5Config", gotParam,
		"PUT /cost/params 必须挂 perm.M5Config（M5:C＝成本参数配置）；挂 M5:E 就退回了 3a 之前的状态")
	require.Equal(t, "M5Edit", gotLock,
		"POST /cost/baselines/:sku/lock-primary 必须挂 perm.M5Edit（M5:E＝主供应商锁定）")
	require.NotEqual(t, gotParam, gotLock,
		"成本参数配置与主供应商锁定不能共用同一个权限点——3a 就是为了把它们拆开")

	// 常量真值兜底：上面比的是标识符名，这里再确认标识符指向的权限点值本身没被改。
	// （若有人把 M5Config 的值改成 "M5:E"，上面的断言仍会通过，只有这里会红。）
	require.Equal(t, "M5:C", perm.M5Config)
	require.Equal(t, "M5:E", perm.M5Edit)
}
