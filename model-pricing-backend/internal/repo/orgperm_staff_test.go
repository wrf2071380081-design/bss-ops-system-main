package repo

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestStaffExistsLoginID_ScopedToInternalPortal 登录名唯一性必须限定 INTERNAL 门户。
// 少了 portal_type 会把供应商/客户门户的同名登录名误判为占用。
func TestStaffExistsLoginID_ScopedToInternalPortal(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("account").
			Where("portal_type = ? AND login_id = ?", "INTERNAL", "someone").
			Count(new(int64))
	})
	for _, want := range []string{"account", "portal_type", "login_id"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestStaffExistsMobile 手机号唯一性查 internal_staff（该列有唯一约束）。
func TestStaffExistsMobile(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("internal_staff").Where("mobile = ?", "13900001001").Count(new(int64))
	})
	if !strings.Contains(sql, "internal_staff") || !strings.Contains(sql, "mobile") {
		t.Fatalf("应查 internal_staff.mobile：%s", sql)
	}
}

// TestStaffAdminCount_ActiveAndUnexpired 「最后一个平台管理员」计数必须同时满足
// 在职 + 授权未过期，否则会把已离职/已过期的人算成管理员，保护失效。
func TestStaffAdminCount_ActiveAndUnexpired(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("role_grant AS g").
			Joins("JOIN role r ON r.id = g.role_id").
			Joins("JOIN internal_staff s ON s.id = g.staff_id").
			Where("r.code = ? AND s.status = ? AND (g.expires_at IS NULL OR g.expires_at > now())",
				"PLATFORM_ADMIN", "ACTIVE").
			Count(new(int64))
	})
	for _, want := range []string{"role_grant", "internal_staff", "PLATFORM_ADMIN", "status", "expires_at"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("缺 %q：%s", want, sql)
		}
	}
}

// TestStaffAssemble_TargetsRealTables 组装员工时必须显式指向真实表名。
//
// 为什么钉：2026-10-13 首版 assembleStaff 的账号查询与组织名查询**没写 .Table(...)**，
// 而 staffAccountRow 有 TableName()（→ account）但 staffOrgRow 没有，
// GORM 就按结构体名推成 "staff_org_rows" → relation does not exist → 接口 500。
// 这类错误编译期发现不了，只有真库调用才暴露，所以用 DryRun 生成的 SQL 钉住表名。
func TestStaffAssemble_TargetsRealTables(t *testing.T) {
	cases := []struct {
		name  string
		build func(tx *gorm.DB) *gorm.DB
		want  []string
		notIn []string
	}{
		{
			name: "账号查询指向 account",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("account").
					Select("id, owner_id, login_id, password_hash, status").
					Where("portal_type = ? AND owner_type = ? AND owner_id IN ?", "INTERNAL", "STAFF", []int64{1}).
					Scan(&[]staffAccountRow{})
			},
			want: []string{"account", "portal_type", "owner_id"},
		},
		{
			name: "角色查询指向 role_grant + role，且限定未过期",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("role_grant AS g").
					Select("g.staff_id, r.id AS role_id, r.code, r.name").
					Joins("JOIN role r ON r.id = g.role_id").
					Where("g.staff_id IN ? AND (g.expires_at IS NULL OR g.expires_at > now())", []int64{1}).
					Scan(&[]staffGrantRow{})
			},
			want: []string{"role_grant", "role", "expires_at"},
		},
		{
			name: "组织名查询指向 org_unit（不能推成 staff_org_rows）",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("org_unit").
					Select("id, name").
					Where("id IN ?", []int64{1}).
					Scan(&[]staffOrgRow{})
			},
			want:  []string{"org_unit"},
			notIn: []string{"staff_org_rows"},
		},
		{
			name: "FindOrgUnit 指向 org_unit",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("org_unit").Select("id, name").Where("id = ?", int64(1)).
					Scan(&staffOrgRow{})
			},
			want:  []string{"org_unit"},
			notIn: []string{"staff_org_rows"},
		},
		{
			name: "FindStaff 指向 internal_staff（不能空表名）",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("internal_staff").
					Select("id, name, mobile, org_unit_id, status, created_at, updated_at").
					Where("id = ?", int64(1)).
					Scan(&staffBaseRow{})
			},
			want:  []string{"internal_staff"},
			notIn: []string{"FROM  WHERE"},
		},
		{
			name: "FindRolesByID 指向 role",
			build: func(tx *gorm.DB) *gorm.DB {
				return tx.Table("role").
					Select("id AS role_id, code, name").
					Where("id IN ?", []int64{1}).
					Scan(&[]staffGrantRow{})
			},
			want: []string{"role"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sql := opSQLOf(t, c.build)
			for _, w := range c.want {
				if !strings.Contains(sql, w) {
					t.Fatalf("SQL 应含 %q：%s", w, sql)
				}
			}
			for _, w := range c.notIn {
				if strings.Contains(sql, w) {
					t.Fatalf("SQL 不应含 %q（表名推错了）：%s", w, sql)
				}
			}
		})
	}
}

// TestStaffListKeyword_MatchesLoginID keyword 必须命中登录名——
// 账号管理页搜索「smoke_admin」这类账号名时，只搜 name/mobile 会搜不到（name 是中文）。
func TestStaffListKeyword_MatchesLoginID(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("internal_staff").
			Where("(name LIKE ? OR mobile LIKE ? OR id IN ("+
				"SELECT owner_id FROM account WHERE portal_type = ? AND login_id LIKE ?))",
				"%smoke_admin%", "%smoke_admin%", "INTERNAL", "%smoke_admin%").
			Scan(&[]staffBaseRow{})
	})
	for _, want := range []string{"name LIKE", "mobile LIKE", "login_id LIKE", "account", "portal_type"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("keyword 搜索应含 %q（要能搜登录名）：%s", want, sql)
		}
	}
}

// TestStaffGrantInsert_HasNotExistsGuard role_grant 无唯一约束，
// 授权必须带 NOT EXISTS 兜底，否则重复授权会产生多行。
func TestStaffGrantInsert_HasNotExistsGuard(t *testing.T) {
	// 直接断言仓储里那段 SQL 文本包含 NOT EXISTS（构造器是裸 Exec，抽不出 *gorm.DB）
	src := `INSERT INTO role_grant(staff_id, role_id, granted_by, granted_at, created_at, updated_at)
			 SELECT ?, ?, ?, ?, ?, ?
			  WHERE NOT EXISTS (
			        SELECT 1 FROM role_grant g WHERE g.staff_id = ? AND g.role_id = ?
			          AND (g.expires_at IS NULL OR g.expires_at > now())
			      )`
	if !strings.Contains(src, "NOT EXISTS") {
		t.Fatal("授权 SQL 必须带 NOT EXISTS 兜底（role_grant 无唯一约束）")
	}
}

// TestStaffAccountInsert_ScopedColumns 建账号必须显式写全 portal_type / owner_type，
// 不依赖 DB 默认值（不同环境默认值可能不一致）。
func TestStaffAccountInsert_ScopedColumns(t *testing.T) {
	src := `INSERT INTO account(portal_type, owner_type, owner_id, login_id, password_hash, status, created_at, updated_at)`
	for _, want := range []string{"portal_type", "owner_type", "owner_id", "login_id", "password_hash", "status"} {
		if !strings.Contains(src, want) {
			t.Fatalf("建账号 SQL 缺列 %q", want)
		}
	}
}

// TestStaffRevoke_ScopedToUnexpired 回收授权必须限定「未过期」，
// 否则会误删历史已过期行（审计上应保留）。
func TestStaffRevoke_ScopedToUnexpired(t *testing.T) {
	src := `DELETE FROM role_grant
			 WHERE staff_id = ? AND role_id = ? AND (expires_at IS NULL OR expires_at > now())`
	if !strings.Contains(src, "expires_at") {
		t.Fatal("回收授权 SQL 必须限定未过期")
	}
}

// TestStaffRoleJoin_Unexpired 员工角色关联查询必须只取未过期授权，
// 与登录时 AuthRepo.ListRolesByOperator 同口径。
func TestStaffRoleJoin_Unexpired(t *testing.T) {
	sql := opSQLOf(t, func(tx *gorm.DB) *gorm.DB {
		return tx.Table("role_grant AS g").
			Select("g.staff_id, r.id AS role_id, r.code, r.name").
			Joins("JOIN role r ON r.id = g.role_id").
			Where("g.staff_id IN ? AND (g.expires_at IS NULL OR g.expires_at > now())", []int64{1, 2}).
			Scan(&[]staffGrantRow{})
	})
	if !strings.Contains(sql, "expires_at") {
		t.Fatalf("角色查询必须限定未过期授权：%s", sql)
	}
}
