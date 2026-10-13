// Package repo 的 orgperm_staff.go：M11 账号管理写侧仓储（2026-10-13 补）。
//
// 硬口径：
//  1. **事务边界在 repo**。建人 = internal_staff + account + role_grant + audit_log
//     四连写，同一事务（红线 8：与业务写同生共死）。其它写操作同样是
//     「业务变更 + audit」单事务。
//  2. **口令只以 bcrypt 密文落库**，绝不明文进 account.password_hash。
//     本文件不碰明文以外的任何编解码——哈希在 repo 内完成，service 只传明文。
//  3. **读侧不 N+1**：ListStaff / FindStaff 都是「一次查 staff 行 + 一次 IN 查账号
//     + 一次 IN 查角色」三条 SQL，在内存里挂回。
//  4. 行模型一律平铺 + 显式 gorm:"column:..."（CLAUDE.md 坑位表）。
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"model_bss/internal/domain/orgperm"
	"model_bss/internal/infra/db"
)

// bcryptCost 口令哈希成本。与库里既有账号一致（cost 10，见 000032 迁移注释）。
// 不要降到 4：那是迁移种子的演示值，线上不可接受。
const bcryptCost = 10

// StaffRepo 账号管理写侧仓储。
type StaffRepo struct {
	base *gorm.DB
}

// NewStaffRepo 构造。
func NewStaffRepo(base *gorm.DB) *StaffRepo { return &StaffRepo{base: base} }

func (r *StaffRepo) txOf(ctx context.Context) *gorm.DB {
	if tx := db.FromContext(ctx); tx != nil {
		return tx
	}
	return r.base
}

// ------------------------------------------------------------
// 行模型
// ------------------------------------------------------------

type staffBaseRow struct {
	ID        int64     `gorm:"column:id"`
	Name      string    `gorm:"column:name"`
	Mobile    string    `gorm:"column:mobile"`
	OrgUnitID int64     `gorm:"column:org_unit_id"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (staffBaseRow) TableName() string { return "internal_staff" }

type staffAccountRow struct {
	ID           int64  `gorm:"column:id"`
	OwnerID      int64  `gorm:"column:owner_id"`
	LoginID      string `gorm:"column:login_id"`
	PasswordHash string `gorm:"column:password_hash"`
	Status       string `gorm:"column:status"`
}

func (staffAccountRow) TableName() string { return "account" }

type staffGrantRow struct {
	StaffID int64  `gorm:"column:staff_id"`
	RoleID  int64  `gorm:"column:role_id"`
	Code    string `gorm:"column:code"`
	Name    string `gorm:"column:name"`
}

type staffOrgRow struct {
	ID   int64  `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

// ------------------------------------------------------------
// 读侧（供 service 的写前置校验与详情回显）
// ------------------------------------------------------------

// ListStaff 分页列员工。三条 SQL：staff 行 → 账号 IN → 角色 IN。
func (r *StaffRepo) ListStaff(ctx context.Context, q orgperm.StaffQuery) ([]orgperm.StaffRow, int64, error) {
	tx := r.txOf(ctx)
	query := tx.Table("internal_staff").Where("1 = 1")
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		// keyword 同时命中姓名 / 手机号 / 登录名（登录名在 account 表，用子查询）。
		query = query.Where(
			"(name LIKE ? OR mobile LIKE ? OR id IN ("+
				"SELECT owner_id FROM account WHERE portal_type = ? AND login_id LIKE ?))",
			like, like, orgperm.PortalInternalAccount, like)
	}
	if st := strings.ToUpper(strings.TrimSpace(q.Status)); st != "" {
		query = query.Where("status = ?", st)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count staff: %w", err)
	}

	rows := make([]staffBaseRow, 0)
	if err := query.
		Select("id, name, mobile, org_unit_id, status, created_at, updated_at").
		Order("id ASC").
		Limit(q.Size).
		Offset((q.Page - 1) * q.Size).
		Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list staff: %w", err)
	}
	if len(rows) == 0 {
		return []orgperm.StaffRow{}, total, nil
	}
	return r.assembleStaff(ctx, rows, total)
}

// FindStaff 按 id 取单个员工（含账号与角色）。
func (r *StaffRepo) FindStaff(ctx context.Context, id int64) (*orgperm.StaffRow, error) {
	var row staffBaseRow
	err := r.txOf(ctx).
		Table("internal_staff").
		Select("id, name, mobile, org_unit_id, status, created_at, updated_at").
		Where("id = ?", id).
		Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("find staff %d: %w", id, err)
	}
	if row.ID == 0 {
		return nil, nil // 不存在：返回 nil, nil（由 service 判 ErrStaffNotFound）
	}
	out, _, err := r.assembleStaff(ctx, []staffBaseRow{row}, 0)
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// assembleStaff 把 staff 行与账号、角色、组织名在内存里挂回（不 N+1）。
func (r *StaffRepo) assembleStaff(ctx context.Context, rows []staffBaseRow, total int64) ([]orgperm.StaffRow, int64, error) {
	ids := make([]int64, 0, len(rows))
	orgIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		orgIDs = append(orgIDs, row.OrgUnitID)
	}

	// 账号（INTERNAL / STAFF 一次 IN 查回）
	accts := make([]staffAccountRow, 0)
	if err := r.txOf(ctx).
		Table("account").
		Select("id, owner_id, login_id, password_hash, status").
		Where("portal_type = ? AND owner_type = ? AND owner_id IN ?",
			orgperm.PortalInternalAccount, orgperm.OwnerTypeStaff, ids).
		Scan(&accts).Error; err != nil {
		return nil, 0, fmt.Errorf("list staff accounts: %w", err)
	}
	acctByOwner := make(map[int64]staffAccountRow, len(accts))
	for _, a := range accts {
		acctByOwner[a.OwnerID] = a
	}

	// 角色（未过期授权，一次 IN 查回）
	grants := make([]staffGrantRow, 0)
	if err := r.txOf(ctx).
		Select("g.staff_id, r.id AS role_id, r.code, r.name").
		Table("role_grant AS g").
		Joins("JOIN role r ON r.id = g.role_id").
		Where("g.staff_id IN ? AND (g.expires_at IS NULL OR g.expires_at > now())", ids).
		Scan(&grants).Error; err != nil {
		return nil, 0, fmt.Errorf("list staff grants: %w", err)
	}
	rolesByStaff := make(map[int64][]orgperm.StaffRoleRow, len(grants))
	for _, g := range grants {
		rolesByStaff[g.StaffID] = append(rolesByStaff[g.StaffID], orgperm.StaffRoleRow{
			ID: g.RoleID, Code: g.Code, Name: g.Name,
		})
	}

	// 组织名（一次 IN 查回）
	orgs := make([]staffOrgRow, 0)
	if err := r.txOf(ctx).
		Table("org_unit").
		Select("id, name").
		Where("id IN ?", orgIDs).
		Scan(&orgs).Error; err != nil {
		return nil, 0, fmt.Errorf("list org names: %w", err)
	}
	orgName := make(map[int64]string, len(orgs))
	for _, o := range orgs {
		orgName[o.ID] = o.Name
	}

	out := make([]orgperm.StaffRow, 0, len(rows))
	for _, row := range rows {
		sr := orgperm.StaffRow{
			ID:        row.ID,
			Name:      row.Name,
			Mobile:    row.Mobile,
			OrgUnitID: row.OrgUnitID,
			OrgName:   orgName[row.OrgUnitID],
			Status:    row.Status,
			Roles:     rolesByStaff[row.ID],
		}
		if sr.Roles == nil {
			sr.Roles = []orgperm.StaffRoleRow{}
		}
		if a, ok := acctByOwner[row.ID]; ok {
			sr.LoginID = a.LoginID
			sr.AccountStatus = a.Status
		}
		out = append(out, sr)
	}
	return out, total, nil
}

// ExistsLoginID INTERNAL 门户下登录名是否已存在。
func (r *StaffRepo) ExistsLoginID(ctx context.Context, loginID string) (bool, error) {
	var n int64
	if err := r.txOf(ctx).
		Table("account").
		Where("portal_type = ? AND login_id = ?", orgperm.PortalInternalAccount, loginID).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("exists login_id: %w", err)
	}
	return n > 0, nil
}

// ExistsMobile 手机号是否已被占用。
func (r *StaffRepo) ExistsMobile(ctx context.Context, mobile string) (bool, error) {
	var n int64
	if err := r.txOf(ctx).Table("internal_staff").
		Where("mobile = ?", mobile).Count(&n).Error; err != nil {
		return false, fmt.Errorf("exists mobile: %w", err)
	}
	return n > 0, nil
}

// FindOrgUnit 组织节点（不存在返回 nil, nil）。
func (r *StaffRepo) FindOrgUnit(ctx context.Context, id int64) (*orgperm.OrgUnitRefRow, error) {
	var row staffOrgRow
	if err := r.txOf(ctx).
		Table("org_unit").
		Select("id, name").Where("id = ?", id).Scan(&row).Error; err != nil {
		return nil, fmt.Errorf("find org unit %d: %w", id, err)
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &orgperm.OrgUnitRefRow{ID: row.ID, Name: row.Name}, nil
}

// FindRolesByID 批量取角色。调用方负责校验「返回行数 == 入参去重后个数」。
func (r *StaffRepo) FindRolesByID(ctx context.Context, ids []int64) ([]orgperm.RoleRefRow, error) {
	if len(ids) == 0 {
		return []orgperm.RoleRefRow{}, nil
	}
	rows := make([]staffGrantRow, 0)
	if err := r.txOf(ctx).
		Select("id AS role_id, code, name").
		Table("role").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find roles by id: %w", err)
	}
	// data_scope 单独取（role 表该列与上面的别名列混在一句里会让 GORM 扫描歧义）
	scopes := make(map[int64]string, len(rows))
	var scopeRows []struct {
		ID   int64  `gorm:"column:id"`
		Desc string `gorm:"column:data_scope"`
	}
	if err := r.txOf(ctx).Table("role").
		Select("id, data_scope").Where("id IN ?", ids).Scan(&scopeRows).Error; err != nil {
		return nil, fmt.Errorf("find role data_scope: %w", err)
	}
	for _, s := range scopeRows {
		scopes[s.ID] = s.Desc
	}

	out := make([]orgperm.RoleRefRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, orgperm.RoleRefRow{
			ID: row.RoleID, Code: row.Code, Name: row.Name, DataScope: scopes[row.RoleID],
		})
	}
	return out, nil
}

// CountActivePlatformAdmins 未过期授权中持 PLATFORM_ADMIN 的在职员工数。
func (r *StaffRepo) CountActivePlatformAdmins(ctx context.Context) (int64, error) {
	var n int64
	if err := r.txOf(ctx).
		Table("role_grant AS g").
		Joins("JOIN role r ON r.id = g.role_id").
		Joins("JOIN internal_staff s ON s.id = g.staff_id").
		Where("r.code = ? AND s.status = ? AND (g.expires_at IS NULL OR g.expires_at > now())",
			orgperm.RolePlatformAdmin, orgperm.StaffStatusActive).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count active platform admins: %w", err)
	}
	return n, nil
}

// StaffHasRole 员工是否已持某角色（未过期授权）。
func (r *StaffRepo) StaffHasRole(ctx context.Context, staffID, roleID int64) (bool, error) {
	var n int64
	if err := r.txOf(ctx).Table("role_grant").
		Where("staff_id = ? AND role_id = ? AND (expires_at IS NULL OR expires_at > now())",
			staffID, roleID).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("staff has role: %w", err)
	}
	return n > 0, nil
}

// ------------------------------------------------------------
// 写侧（单事务）
// ------------------------------------------------------------

// CreateStaffTx 单事务四连写：internal_staff → account → role_grant → audit_log。
func (r *StaffRepo) CreateStaffTx(ctx context.Context, p orgperm.CreateStaffTxParams) (*orgperm.StaffRow, error) {
	now := time.Now().UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var newID int64
	err = r.txOf(ctx).Transaction(func(tx *gorm.DB) error {
		staff := staffBaseRow{
			Name:      p.Name,
			Mobile:    p.Mobile,
			OrgUnitID: p.OrgID,
			Status:    orgperm.StaffStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&staff).Error; err != nil {
			return fmt.Errorf("insert internal_staff: %w", err)
		}
		newID = staff.ID

		// 账号显式写全（portal_type/owner_type/created_at/updated_at），
		// 避免依赖 DB 默认值在不同环境不一致。
		if err := tx.Exec(
			`INSERT INTO account(portal_type, owner_type, owner_id, login_id, password_hash, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			orgperm.PortalInternalAccount, orgperm.OwnerTypeStaff, staff.ID, p.LoginID,
			string(hash), orgperm.StaffStatusActive, now, now,
		).Error; err != nil {
			return fmt.Errorf("insert account: %w", err)
		}

		for _, role := range p.Roles {
			if err := tx.Exec(
				`INSERT INTO role_grant(staff_id, role_id, granted_by, granted_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				staff.ID, role.ID, p.Operator.OperatorID, now, now, now,
			).Error; err != nil {
				return fmt.Errorf("insert role_grant role=%d: %w", role.ID, err)
			}
		}

		after := map[string]any{
			"name":        p.Name,
			"mobile":      p.Mobile,
			"org_unit_id": p.OrgID,
			"login_id":    p.LoginID,
			"roles":       roleCodes(p.Roles),
			"status":      orgperm.StaffStatusActive,
		}
		return insertStaffAudit(tx, p.Operator, "STAFF_CREATE", "INTERNAL_STAFF", staff.ID, nil, after, now)
	})
	if err != nil {
		return nil, err
	}
	// ⚠️ 不在事务提交后再回查（FindStaff）——那会引入「事务已提交、回显却 500」的
	// 幽灵成功：建人已落库，但响应层拿不到结果。这里直接用入参 + 新 id 在内存组装，
	// 数据与写库事务同源，无二次查询依赖。
	roles := make([]orgperm.StaffRoleRow, 0, len(p.Roles))
	for _, role := range p.Roles {
		roles = append(roles, orgperm.StaffRoleRow{
			ID: role.ID, Code: role.Code, Name: role.Name, DataScope: role.DataScope,
		})
	}
	return &orgperm.StaffRow{
		ID:            newID,
		Name:          p.Name,
		Mobile:        p.Mobile,
		OrgUnitID:     p.OrgID,
		OrgName:       p.OrgName,
		Status:        orgperm.StaffStatusActive,
		LoginID:       p.LoginID,
		AccountStatus: orgperm.StaffStatusActive,
		Roles:         roles,
	}, nil
}

// UpdateStaffTx 单事务：改 internal_staff + audit。
func (r *StaffRepo) UpdateStaffTx(ctx context.Context, p orgperm.UpdateStaffTxParams) error {
	now := time.Now().UTC()
	return r.txOf(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE internal_staff SET name = ?, mobile = ?, org_unit_id = ?, status = ?, updated_at = ?
			 WHERE id = ?`,
			p.Name, p.Mobile, p.OrgID, p.Status, now, p.StaffID,
		)
		if res.Error != nil {
			return fmt.Errorf("update internal_staff: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return orgperm.ErrStaffNotFound
		}
		after := map[string]any{
			"name":        p.Name,
			"mobile":      p.Mobile,
			"org_unit_id": p.OrgID,
			"status":      p.Status,
		}
		return insertStaffAudit(tx, p.Operator, "STAFF_UPDATE", "INTERNAL_STAFF", p.StaffID,
			p.Before, after, now)
	})
}

// ResetPasswordTx 单事务：改 account.password_hash + audit。
//
// 审计里**绝不写口令明文或密文**，只记「已重置」这一事实。
func (r *StaffRepo) ResetPasswordTx(ctx context.Context, p orgperm.ResetPasswordTxParams) error {
	now := time.Now().UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return r.txOf(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE account SET password_hash = ?, updated_at = ?
			 WHERE portal_type = ? AND owner_type = ? AND owner_id = ?`,
			string(hash), now, orgperm.PortalInternalAccount, orgperm.OwnerTypeStaff, p.StaffID,
		)
		if res.Error != nil {
			return fmt.Errorf("update account password: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return orgperm.ErrStaffNotFound
		}
		return insertStaffAudit(tx, p.Operator, "STAFF_PASSWORD_RESET", "ACCOUNT", p.StaffID,
			map[string]any{"password": "***"}, map[string]any{"password": "***"}, now)
	})
}

// GrantRolesTx 单事务：逐角色授权 + 一条汇总审计。
func (r *StaffRepo) GrantRolesTx(ctx context.Context, p orgperm.GrantRolesTxParams) error {
	now := time.Now().UTC()
	return r.txOf(ctx).Transaction(func(tx *gorm.DB) error {
		for _, role := range p.Roles {
			// NOT EXISTS 兜底：role_grant 无唯一约束，重复授权必须靠这里防
			res := tx.Exec(
				`INSERT INTO role_grant(staff_id, role_id, granted_by, granted_at, created_at, updated_at)
				 SELECT ?, ?, ?, ?, ?, ?
				  WHERE NOT EXISTS (
				        SELECT 1 FROM role_grant g WHERE g.staff_id = ? AND g.role_id = ?
				          AND (g.expires_at IS NULL OR g.expires_at > now())
				      )`,
				p.StaffID, role.ID, p.Operator.OperatorID, now, now, now, p.StaffID, role.ID,
			)
			if res.Error != nil {
				return fmt.Errorf("insert role_grant role=%d: %w", role.ID, res.Error)
			}
		}
		after := map[string]any{"granted_roles": roleCodes(p.Roles)}
		return insertStaffAudit(tx, p.Operator, "STAFF_ROLE_GRANT", "INTERNAL_STAFF", p.StaffID,
			nil, after, now)
	})
}

// RevokeRoleTx 单事务：回收授权 + audit。
//
// 按「staff_id + role_id + 未过期」删，不用 id——同一个人对同一角色的历史授权行
// 理论上可能有多条（role_grant 无唯一约束），全部收掉才是用户预期。
func (r *StaffRepo) RevokeRoleTx(ctx context.Context, p orgperm.RevokeRoleTxParams) error {
	now := time.Now().UTC()
	return r.txOf(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`DELETE FROM role_grant
			 WHERE staff_id = ? AND role_id = ? AND (expires_at IS NULL OR expires_at > now())`,
			p.StaffID, p.RoleID,
		)
		if res.Error != nil {
			return fmt.Errorf("delete role_grant: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return orgperm.ErrRoleNotGranted
		}
		before := map[string]any{"revoked_role": p.RoleCode}
		return insertStaffAudit(tx, p.Operator, "STAFF_ROLE_REVOKE", "INTERNAL_STAFF", p.StaffID,
			before, nil, now)
	})
}

// ------------------------------------------------------------
// 内部工具
// ------------------------------------------------------------

// insertStaffAudit 写一条账号管理审计。前后值 JSON 化，失败即回滚整个事务。
func insertStaffAudit(tx *gorm.DB, op orgperm.Operator, action, targetType string,
	targetID int64, before, after map[string]any, now time.Time) error {

	var beforeJSON, afterJSON []byte
	var err error
	if before != nil {
		if beforeJSON, err = json.Marshal(before); err != nil {
			return fmt.Errorf("marshal audit before: %w", err)
		}
	}
	if after != nil {
		if afterJSON, err = json.Marshal(after); err != nil {
			return fmt.Errorf("marshal audit after: %w", err)
		}
	}
	row := auditLogRow{
		OperatorID:   op.OperatorID,
		OperatorRole: op.OperatorRole,
		Action:       action,
		TargetType:   targetType,
		TargetID:     targetID,
		BeforeValue:  beforeJSON,
		AfterValue:   afterJSON,
		SourceType:   "HUMAN",
		CreatedAt:    now,
		UpdatedAt:    now,
		RequestID:    strPtrIfNotEmpty(op.RequestID),
		CreatedBy:    int64Ptr(op.OperatorID),
		UpdatedBy:    int64Ptr(op.OperatorID),
	}
	// 10c 裁决 3：内部角色 → internal_operator_id（与 audit.go 同口径）
	if op.OperatorID != 0 && isInternalRole(op.OperatorRole) {
		row.InternalOperatorID = int64Ptr(op.OperatorID)
	}
	return tx.Create(&row).Error
}

// roleCodes 取角色 code 列表（审计用）。
func roleCodes(roles []orgperm.RoleRefRow) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Code)
	}
	return out
}
