package supplypg

import (
	"os"
	"testing"
)

// 真库集成测试的统一 DSN 入口。
//
// 为什么统一：之前每个测试各自读一个环境变量（CUSTOMER_REGISTRY_TEST_DSN /
// CUSTOMER_RESOURCE_LINK_TEST_DSN / CUSTOMER_ONBOARDING_TEST_DSN）。只设了其中一个时，
// 另外几组会 t.Skip，而 `go test` 默认把跳过的测试显示成 **PASS**——
// 谁扫一眼"全绿"就以为真库测试跑过了，这比测试失败更危险。
//
// 现在的口径：
//   - 优先读 TEST_DSN；
//   - 没设再读各自的旧变量（兼容已有脚本/文档）；
//   - 都没有 → t.Log 打一条**显式**痕迹再 t.Skip，`go test -v` 里一眼能看到没跑。
func integrationTestDSN(t *testing.T, legacyEnv string) string {
	t.Helper()
	if dsn := os.Getenv("TEST_DSN"); dsn != "" {
		return dsn
	}
	if legacyEnv != "" {
		if dsn := os.Getenv(legacyEnv); dsn != "" {
			return dsn
		}
	}
	if legacyEnv != "" {
		t.Logf("未设置 TEST_DSN（也未设置兼容变量 %s），跳过真库集成测试——这条 SKIP 不是通过", legacyEnv)
	} else {
		t.Log("未设置 TEST_DSN，跳过真库集成测试——这条 SKIP 不是通过")
	}
	t.Skipf("set TEST_DSN to run %s", t.Name())
	return ""
}
