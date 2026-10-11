package repo

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// portalProfileSQLOf 用 DryRun 生成门户档案查询的 SQL（不连真库）。
// 与 customer_portal_test.go / orgperm_test.go 同一手法。
func portalProfileSQLOf(t *testing.T, supplierID int64) string {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return portalProfileQuery(tx, supplierID).Scan(&[]portalProfileRow{})
	})
}

// TestPortalProfileQuery_ScopeKeyIsSupplierID 行级过滤键必须是 supplier_profile.id
// ——**不是**内部门户的 owner_procurement_operator_id / owner_sales_operator_id。
// 批次 7a 任务书专门点名"核对时别串"，这里钉住。
func TestPortalProfileQuery_ScopeKeyIsSupplierID(t *testing.T) {
	sql := portalProfileSQLOf(t, 7)
	if !strings.Contains(sql, "sp.id = 7") {
		t.Fatalf("过滤键应为 sp.id（登录态解析出的 supplier_profile.id）：%s", sql)
	}
	for _, forbidden := range []string{"owner_procurement_operator_id", "owner_sales_operator_id"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("供应商门户档案不应按内部门户的归属键 %q 过滤：%s", forbidden, sql)
		}
	}
}

// TestPortalProfileQuery_LeftJoinPrimaryOperator 联系人必须 LEFT JOIN 主操作员：
// 用 LEFT 而不是 INNER，否则没有主操作员的主体会整行丢失（档案都查不出来）。
func TestPortalProfileQuery_LeftJoinPrimaryOperator(t *testing.T) {
	sql := portalProfileSQLOf(t, 1)
	for _, want := range []string{"subject_operator", "LEFT JOIN", "is_primary = true", "COALESCE(so.name", "COALESCE(so.mobile"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestPortalProfileQuery_NoSensitiveColumns 供应商视角不得查成本/毛利/商务敏感列。
// credit_line / credit_used / deposit_amount / min_recharge 属商务敏感，
// 本批 DTO 不含，SQL 层也不该 SELECT（红线：DTO 物理不含，不是运行时删）。
func TestPortalProfileQuery_NoSensitiveColumns(t *testing.T) {
	sql := portalProfileSQLOf(t, 1)
	for _, want := range []string{"supplier_profile sp", "legal_subject ls", "settlement_currency", "qual_status"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
	for _, forbidden := range []string{"credit_line", "credit_used", "deposit_amount", "min_recharge"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("门户档案不应 SELECT 商务敏感列 %q：%s", forbidden, sql)
		}
	}
}

// TestSupplierRepoGetPortalProfile_ZeroIDSkipsQuery supplierID<=0（身份解析异常的兜底）
// 时直接返回空，不带着 0 去查库。
func TestSupplierRepoGetPortalProfile_ZeroIDSkipsQuery(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	r := NewSupplierRepo(db)
	got, err := r.GetPortalProfile(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("supplierID<=0 应返回 nil，实际 %+v", got)
	}
}
