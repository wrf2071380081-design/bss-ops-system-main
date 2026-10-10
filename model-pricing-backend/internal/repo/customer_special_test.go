package repo

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/customer"
)

// ============================================================
// 行级数据域（红线 7 / 批次 2b）
// ============================================================

// scopeSQLOf 用 DryRun 生成「对 customer_quote 单行做数据域过滤」的 SQL。
//
// 与 customer_portal_test.go 同手法：不连真库，只断言生成的 WHERE/JOIN，
// 覆盖 SELF / DEPT / DEPT_SUB / ALL 四种分支（本地库没有 DEPT_SUB 账号，
// 无法用 HTTP 实测，只能在这里兜住）。
func scopeSQLOf(t *testing.T, scope customer.OwnerScope) string {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	var cnt int64
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyQuoteOwnerScope(tx.Table("customer_quote cq").Where("cq.id = ?", 2), scope).Count(&cnt)
	})
}

func TestApplyQuoteOwnerScope_SELF(t *testing.T) {
	// SELF 必须拼归属人等值条件——批次 2b 修的三个 IDOR 就靠这一句。
	sql := scopeSQLOf(t, customer.OwnerScope{DataScope: auth.ScopeSELF, StaffID: 7})
	if !strings.Contains(sql, "cq.owner_sales_operator_id = 7") {
		t.Fatalf("SELF 缺归属过滤：%s", sql)
	}
}

func TestApplyQuoteOwnerScope_DEPT(t *testing.T) {
	sql := scopeSQLOf(t, customer.OwnerScope{DataScope: auth.ScopeDEPT, StaffID: 7, MyOrgID: 3})
	if !strings.Contains(sql, "JOIN internal_staff os_ ON os_.id = cq.owner_sales_operator_id") {
		t.Fatalf("DEPT 缺 internal_staff JOIN：%s", sql)
	}
	if !strings.Contains(sql, "os_.org_unit_id = 3") {
		t.Fatalf("DEPT 缺本部门过滤：%s", sql)
	}
}

func TestApplyQuoteOwnerScope_DeptSub(t *testing.T) {
	sql := scopeSQLOf(t, customer.OwnerScope{
		DataScope: auth.ScopeDEPTSUB, StaffID: 7, MyOrgID: 3, ScopePaths: []string{"/1/3/"},
	})
	if !strings.Contains(sql, "JOIN org_unit ou_ ON ou_.id = os_.org_unit_id") {
		t.Fatalf("DEPT_SUB 缺 org_unit JOIN：%s", sql)
	}
	if !strings.Contains(sql, "ou_.path LIKE ANY") {
		t.Fatalf("DEPT_SUB 缺子树 LIKE ANY：%s", sql)
	}
}

func TestApplyQuoteOwnerScope_DeptSubEmptyPathsFallsBackToMyOrg(t *testing.T) {
	// ScopePaths 为空时退回本部门（与 customer.applyCustomerOwnerScope 同口径）。
	sql := scopeSQLOf(t, customer.OwnerScope{DataScope: auth.ScopeDEPTSUB, StaffID: 7, MyOrgID: 3})
	if strings.Contains(sql, "ou_.path LIKE ANY") {
		t.Fatalf("paths 为空不应走 LIKE ANY：%s", sql)
	}
	if !strings.Contains(sql, "os_.org_unit_id = 3") {
		t.Fatalf("paths 为空应退回本部门：%s", sql)
	}
}

func TestApplyQuoteOwnerScope_ALL(t *testing.T) {
	// ALL 不加任何归属条件。
	sql := scopeSQLOf(t, customer.OwnerScope{DataScope: auth.ScopeALL, StaffID: 7})
	if strings.Contains(sql, "owner_sales_operator_id =") {
		t.Fatalf("ALL 不应加归属过滤：%s", sql)
	}
	if !strings.Contains(sql, "cq.id = 2") {
		t.Fatalf("ALL 丢了主键条件：%s", sql)
	}
}
