package api

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// B3a 真库集成测试的统一入口（package api）。
//
// 与 internal/infra/supplypg 的 integrationTestDSN 同一纪律：只读 TEST_DSN，
// 没设就 t.Log 打一条显式痕迹再 Skip —— `go test` 默认把 SKIP 显示成 PASS，
// 不打日志的话"全绿但没跑"会被误读成验过了。
func openB3ATestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		t.Log("未设置 TEST_DSN，跳过真库集成测试——这条 SKIP 不是通过")
		t.Skipf("set TEST_DSN to run %s", t.Name())
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
