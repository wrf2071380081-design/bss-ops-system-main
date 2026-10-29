package repo

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestAffectedCustomerSQLKeepsResourceFilter 锁住「按资源查受影响客户」的 SQL 形状：
// 必须带 resource_id 过滤、排除 DRAFT、join 客户主体，且数据域条件挂在同一段 WHERE 里。
// 2026-10-29 曾因 Where 拼接位置导致生成的 SQL 语法错误，这里用 DryRun 固化形状。
func TestAffectedCustomerSQLKeepsResourceFilter(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []affectedQuoteRow
		return tx.Table("customer_quote_item cqi").
			Joins("JOIN customer_quote cq ON cq.id = cqi.customer_quote_id").
			Joins("JOIN customer_profile cp ON cp.id = cq.customer_id").
			Joins("JOIN legal_subject ls ON ls.id = cp.subject_id").
			Where("cqi.resource_id = ? AND cq.status <> 'DRAFT'", 16).
			Select(`cp.id AS customer_id, ls.legal_name AS customer_name, cp.level_code AS level_code,
			cp.owner_sales_operator_id AS owner_sales_operator_id,
			cq.id AS quote_id, cq.version_no AS version_no, cq.status AS quote_status, cq.quote_type AS quote_type,
			cq.valid_until::text AS valid_until, cqi.price_item_key AS price_item_key,
			cqi.unit_price AS unit_price, cqi.currency AS currency`).
			Order("cp.id ASC, cq.id ASC").
			Scan(&rows)
	})
	if strings.Contains(sql, "?") {
		t.Fatalf("unbound placeholder left in SQL: %s", sql)
	}
	for _, part := range []string{
		"FROM customer_quote_item cqi",
		"JOIN customer_quote cq",
		"JOIN customer_profile cp",
		"cqi.resource_id = 16",
		"cq.status <> 'DRAFT'",
		"WHERE",
	} {
		if !strings.Contains(sql, part) {
			t.Fatalf("missing %q in SQL: %s", part, sql)
		}
	}
}
