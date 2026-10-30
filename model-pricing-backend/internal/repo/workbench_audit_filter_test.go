package repo

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestAuditSourceFilterExpandsInternal 锁住「内部人员」筛选的取值口径。
//
// audit_log.source_type 里人工操作有两种写法：INTERNAL（新写入口显式传）与 HUMAN
// （audit.Record 在 SourceType 为空时的默认值，历史数据大多是它）。前端「内部人员」
// 下拉传的是 INTERNAL，一旦退化成等值匹配，筛出来只剩少数新行（2026-10-30 验收发现）。
func TestAuditSourceFilterExpandsInternal(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	dryRun := func(sourceType string) string {
		return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return applyAuditSourceFilter(tx.Model(&auditRow{}), sourceType).Find(&[]auditRow{})
		})
	}

	internalSQL := dryRun("INTERNAL")
	if !strings.Contains(internalSQL, "source_type IN") {
		t.Fatalf("INTERNAL 应展开成 IN 条件，实际 SQL: %s", internalSQL)
	}
	for _, value := range []string{"INTERNAL", "HUMAN"} {
		if !strings.Contains(internalSQL, value) {
			t.Fatalf("INTERNAL 筛选缺少取值 %s，实际 SQL: %s", value, internalSQL)
		}
	}
	if strings.Contains(internalSQL, "source_type = 'INTERNAL'") {
		t.Fatalf("INTERNAL 退化成了等值匹配，会漏掉 HUMAN 行: %s", internalSQL)
	}

	workerSQL := dryRun("WORKER")
	if !strings.Contains(workerSQL, "source_type = 'WORKER'") {
		t.Fatalf("WORKER 应保持等值匹配，实际 SQL: %s", workerSQL)
	}

	if emptySQL := dryRun(""); strings.Contains(emptySQL, "source_type") {
		t.Fatalf("空来源不应追加任何来源条件，实际 SQL: %s", emptySQL)
	}
}
