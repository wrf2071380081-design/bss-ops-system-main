package repo

import (
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/domain/supplier"
)

// withdrawSQLOf 用 DryRun 生成撤回条件更新的 SQL（不连真库）。
// 与 supplier_portal_test.go / orgperm_test.go 同一手法。
func withdrawSQLOf(t *testing.T, p supplier.WithdrawParams) string {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	// 关键：字段集用生产代码的 withdrawQuoteSet，不要在测试里另写一份 map
	// ——另写一份会让「目标状态被改坏」的变异永远漏网（M3 首轮教训）。
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return withdrawQuoteUpdate(tx, p).Updates(withdrawQuoteSet(p))
	})
}

func withdrawParams() supplier.WithdrawParams {
	return supplier.WithdrawParams{
		QuoteID: 42, SupplierID: 7, Reason: "报错了重提",
		OperatorID: 9, OperatorRole: "SUPPLIER", RequestID: "req-1",
		Now: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC),
	}
}

// TestWithdrawUpdate_OwnershipKeyIsSupplierID 行级过滤键必须是 supplier_profile 侧解析出的
// supplier_id，且必须与 id 出现在**同一条** UPDATE 的 WHERE 里。
//
// 变异验证：把 supplier_id 从 WHERE 里删掉（改成先查后改）→ 本用例必须失败。
// 这是本批最关键的越权防线：漏了它，任何供应商都能撤回别人的报价单。
func TestWithdrawUpdate_OwnershipKeyIsSupplierID(t *testing.T) {
	sql := withdrawSQLOf(t, withdrawParams())
	if !strings.Contains(sql, "supplier_id") {
		t.Fatalf("撤回必须带 supplier_id 行级过滤：%s", sql)
	}
	if !strings.Contains(sql, "UPDATE") {
		t.Fatalf("应为 UPDATE：%s", sql)
	}
	// 参数按 (id, supplier_id, status) 顺序绑定：确认 supplier_id=7 真的进了 WHERE
	if !strings.Contains(sql, "7") || !strings.Contains(sql, "42") {
		t.Fatalf("id / supplier_id 未绑定进 SQL：%s", sql)
	}
}

// TestWithdrawUpdate_StatusWhitelist 状态必须走白名单（SUBMITTED/APPROVING）。
//
// 变异验证：把 APPROVED_PENDING 加进白名单（允许撤回已审批单）→ 本用例必须失败。
// 裁决口径是"审批完成前可撤回"，已审批/已生效单撤回属另一条业务流。
func TestWithdrawUpdate_StatusWhitelist(t *testing.T) {
	sql := withdrawSQLOf(t, withdrawParams())
	for _, want := range []string{"SUBMITTED", "APPROVING"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("状态白名单缺 %q：%s", want, sql)
		}
	}
	for _, forbidden := range []string{"APPROVED_PENDING", "EFFECTIVE", "EXPIRED", "REJECTED"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("不可撤回状态 %q 出现在 SQL 里：%s", forbidden, sql)
		}
	}
}

// TestWithdrawUpdate_TargetStatusIsVoided 目标状态必须是 VOIDED。
// 变异验证：改成 REJECTED 或 EXPIRED → 本用例必须失败。
func TestWithdrawUpdate_TargetStatusIsVoided(t *testing.T) {
	sql := withdrawSQLOf(t, withdrawParams())
	if !strings.Contains(sql, supplier.QuoteStatusVoided) {
		t.Fatalf("目标状态应为 VOIDED：%s", sql)
	}
}
