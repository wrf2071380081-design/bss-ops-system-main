package api

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- 批次 7a：供应商门户档案接口的挂载形态钉子 ----
//
// 为什么钉：CLAUDE.md 坑位表写得很明确——
// "供应商门户路由挂 RequirePerm → 禁止。SUPPLIER/CUSTOMER 角色在 000007_seed 里
//
//	零内部权限点，挂了必然 403；只会走 AuthN，隔离靠服务层按 supplier_id 硬过滤"。
//
// 也就是说：给 /api/supplier/profile 加一个 RequirePerm 看起来"更安全"，
// 实际会让**所有供应商账号永久 403**，页面直接白屏。这个坑没有任何类型系统能拦，
// 只能靠源码断言钉住。
//
// 守的是什么：
//  1. GET /profile 必须注册在供应商门户（`portal == "supplier"` 块内）
//  2. 该路由**不得**挂 middleware.RequirePerm
//  3. 隔离靠身份解析，不靠权限点——所以 router.go 的 supplier 块里不应出现
//     任何 RequirePerm（现有 10 条供应商门户路由全部如此）
func TestRouterSupplierPortalProfileNotGuardedByRequirePerm(t *testing.T) {
	src, err := os.ReadFile("router.go")
	require.NoError(t, err, "读 router.go 失败（测试必须在 internal/api 目录下运行）")
	text := string(src)

	// 定位供应商门户路由块（以 `portal == "supplier"` 注释行开头，到下一个 `portal == "customer"` 或块结束）。
	start := strings.Index(text, "// 业务接口：供应商门户")
	require.GreaterOrEqual(t, start, 0, "router.go 里找不到供应商门户路由块注释")
	rest := text[start:]
	// 结束边界：优先取"客户门户"注释；若该注释已不存在，则退到第一个内部门户块
	// （`if portal == "internal"`）。不加这条退路时，后面新增的内部路由块会被
	// 一并纳入切片，于是内部块正常的 RequirePerm 会被误判成供应商门户违规。
	for _, marker := range []string{"// 业务接口：客户门户", "\n\tif portal == \"internal\""} {
		if cut := strings.Index(rest, marker); cut > 0 {
			rest = rest[:cut]
		}
	}
	// 去掉注释行再断言：本块的注释里本身就写着「不挂 RequirePerm」，
	// 直接对原文做 NotContains 会被注释命中（踩过一次）。
	codeOnly := strings.Join(nonCommentLines(rest), "\n")

	require.Contains(t, codeOnly, `protected.GET("/profile"`,
		"GET /api/supplier/profile 应注册在供应商门户块内")

	// 该路由行本身不得带 RequirePerm（注册语句可能跨行，取到行尾 `)` 为止）。
	lineRe := regexp.MustCompile(`(?s)protected\.GET\("/profile".*?\)`)
	m := lineRe.FindStringSubmatch(rest)
	require.NotNil(t, m, "找不到 /profile 的注册语句")
	require.NotContainsf(t, m[0], "RequirePerm",
		"供应商门户路由禁止挂 RequirePerm——SUPPLIER 角色零内部权限点，挂了所有供应商都会 403")

	// 整段供应商门户路由都不应出现 RequirePerm。
	require.NotContainsf(t, codeOnly, "RequirePerm",
		"供应商门户整段路由都不应挂 RequirePerm（隔离靠登录态解析 supplier_id，不靠权限点）")
}

// nonCommentLines 返回去掉整行注释（// 开头）后的行。
// 用于源码断言：注释里经常出现被禁止的关键字（如"不挂 RequirePerm"），
// 不剥离会产生假阳性。
func nonCommentLines(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		out = append(out, line)
	}
	return out
}
