package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/domain/finance"
)

// financeDryRunDB 构造不做真实连接的 DryRun 会话（沿用 customer_portal_test.go 惯例）。
func financeDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// TestFinanceOwnerScopeSQL: 四档数据域过滤的 SQL 形态（SELF/DEPT/DEPT_SUB 有过滤，ALL 无）。
func TestFinanceOwnerScopeSQL(t *testing.T) {
	db := financeDryRunDB(t)
	var dummy []accountScanRow

	sqlFor := func(scope finance.OwnerScope) string {
		return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return applyFinanceOwnerScope(tx.Table("customer_profile cp"), scope).Find(&dummy)
		})
	}

	self := sqlFor(finance.OwnerScope{DataScope: "SELF", StaffID: 9})
	require.Contains(t, self, "cp.owner_sales_operator_id = ")

	dept := sqlFor(finance.OwnerScope{DataScope: "DEPT", MyOrgID: 3})
	require.Contains(t, dept, "os_.org_unit_id = ")
	require.Contains(t, dept, "JOIN internal_staff os_")

	deptSub := sqlFor(finance.OwnerScope{DataScope: "DEPT_SUB", MyOrgID: 3, ScopePaths: []string{"/1/"}})
	require.Contains(t, deptSub, "ou_.path LIKE ANY")

	all := sqlFor(finance.OwnerScope{DataScope: "ALL"})
	require.NotContains(t, all, "cp.owner_sales_operator_id = ")
	require.NotContains(t, all, "os_.org_unit_id = ")
}

// TestFinanceAccountBase_KeywordStatusFilters: 关键词（名称/uscc）+ 状态过滤拼接。
func TestFinanceAccountBase_KeywordStatusFilters(t *testing.T) {
	db := financeDryRunDB(t)
	r := NewFinanceRepo(db)
	var dummy []accountScanRow

	sqlStr := db.ToSQL(func(_ *gorm.DB) *gorm.DB {
		return r.accountBase(context.Background(), finance.OwnerScope{DataScope: "ALL"}, "示例", "WARNING").Find(&dummy)
	})
	require.Contains(t, sqlStr, "ls.legal_name ILIKE")
	require.Contains(t, sqlStr, "cp.credit_status = ")
	require.Contains(t, sqlStr, "JOIN legal_subject ls")
}

// TestAccountSelect_NullUsccCoalesced: uscc 为 NULL 时用空串兜底（避免扫描失败）。
func TestAccountSelect_NullUsccCoalesced(t *testing.T) {
	require.Contains(t, accountSelect, "COALESCE(ls.uscc, '')")
}
