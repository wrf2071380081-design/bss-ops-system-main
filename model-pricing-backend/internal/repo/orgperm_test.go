package repo

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// opSQLOf 用 DryRun 生成 SQL 文本（不连真库）。
// 与 customer_portal_test.go / customer_special_test.go 同一手法。
func opSQLOf(t *testing.T, build func(tx *gorm.DB) *gorm.DB) string {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db.ToSQL(build)
}

// TestOpQueryOrgUnits_OnlyActiveAndNoPath 组织树只取 ACTIVE 节点，
// 且**不 SELECT path**（库里 path 有脏值，建树按 parent_id）。
func TestOpQueryOrgUnits_OnlyActiveAndNoPath(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return opQueryOrgUnits(tx).Scan(&[]opOrgUnitRow{})
	})
	for _, want := range []string{"org_unit", "status = 'ACTIVE'", "parent_id", "leader_staff_id"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
	if strings.Contains(strings.ToLower(sql), "path") {
		t.Fatalf("不应 SELECT path（库里 path 有脏值，建树按 parent_id）：%s", sql)
	}
}

// TestOpQueryStaffCounts_GroupedAndActiveOnly 人数必须一次 group by 取回（防 N+1），
// 且只数在职员工。
func TestOpQueryStaffCounts_GroupedAndActiveOnly(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return opQueryStaffCounts(tx).Scan(&[]opStaffCountRow{})
	})
	// GORM 会给 GROUP BY 的列加双引号，所以分两段断言（不要写死 `GROUP BY org_unit_id`）。
	for _, want := range []string{"internal_staff", "GROUP BY", `org_unit_id`, "status = 'ACTIVE'"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestOpQueryRoles_NoSensitiveColumns 角色查询不得带出密码/令牌/快照类列
// （本视图返回的是全系统权限配置，越权读到的后果比业务数据更严重）。
func TestOpQueryRoles_NoSensitiveColumns(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return opQueryRoles(tx).Scan(&[]opRoleRow{})
	})
	for _, want := range []string{"role", "data_scope", "field_mask"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"password", "token", "role_snapshot", "secret"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("角色查询不得包含 %q：%s", forbidden, sql)
		}
	}
}

// TestOpQueryRolePermissions_JoinsPermissionPoint 权限点必须 join permission_point
// 取中文名，code 由 module_code||':'||action_code 拼出（与 pkg/perm 常量同构）。
func TestOpQueryRolePermissions_JoinsPermissionPoint(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return opQueryRolePermissions(tx).Scan(&[]opRolePermRow{})
	})
	for _, want := range []string{"role_permission", "permission_point", "module_code", "action_code"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestOpQueryGrantCounts_ExcludesExpired 授权人数只数未过期的，
// 与登录时 ListRolesByOperator 的口径一致。
func TestOpQueryGrantCounts_ExcludesExpired(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return opQueryGrantCounts(tx).Scan(&[]opGrantCountRow{})
	})
	for _, want := range []string{"role_grant", "GROUP BY", `role_id`, "expires_at"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestOrgPermRepo_EmptyStaffIDsSkipsQuery ids 为空时不发 SQL（GORM `IN ?` 空切片
// 会生成 `IN (NULL)`，虽然不报错但语义不对，所以代码里直接短路返回）。
func TestOrgPermRepo_EmptyStaffIDsSkipsQuery(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=localhost user=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	r := NewOrgPermRepo(db)
	got, err := r.ListStaffNames(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("ids 为空应返回空 map，实际 %v", got)
	}
}

// TestParseHideFields field_mask 解析：NULL/空/非法 → nil；正常 → hide 数组。
func TestParseHideFields(t *testing.T) {
	if got := parseHideFields(nil); got != nil {
		t.Fatalf("NULL 应返回 nil，实际 %v", got)
	}
	if got := parseHideFields([]byte{}); got != nil {
		t.Fatalf("空应返回 nil，实际 %v", got)
	}
	if got := parseHideFields([]byte("not-json")); got != nil {
		t.Fatalf("非法 JSON 应返回 nil，实际 %v", got)
	}
	got := parseHideFields([]byte(`{"hide":["unit_cost","margin_before"]}`))
	if len(got) != 2 || got[0] != "unit_cost" || got[1] != "margin_before" {
		t.Fatalf("hide 解析错误：%v", got)
	}
}
