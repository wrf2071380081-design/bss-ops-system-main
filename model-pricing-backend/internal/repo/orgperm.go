// Package repo 的 orgperm.go：M11 组织与权限只读视图的仓储（批次 5）。
//
// 两条硬要求（任务书 §数据来源）：
//  1. **避免 N+1**：每个方法都是「一次查询取回全部关联数据」，返回 map 供 service
//     在内存里挂回。绝不按节点 / 按角色循环查库——本视图最坏情况
//     （组织 100 节点、角色 20 个）也必须只发 6 条 SQL。
//  2. **只读**：本文件不含任何 INSERT / UPDATE / DELETE。
//
// 行模型一律「平铺 + 显式 gorm:"column:..."」（CLAUDE.md 坑位表：
// 内嵌带 TableName 的行 + 别名列会扫不上）。
package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"model_bss/internal/domain/orgperm"
	"model_bss/internal/infra/db"
)

// OrgPermRepo 组织与权限只读仓储。
type OrgPermRepo struct {
	base *gorm.DB
}

// NewOrgPermRepo 构造。
func NewOrgPermRepo(base *gorm.DB) *OrgPermRepo { return &OrgPermRepo{base: base} }

func (r *OrgPermRepo) txOf(ctx context.Context) *gorm.DB {
	if tx := db.FromContext(ctx); tx != nil {
		return tx
	}
	return r.base
}

// ------------------------------------------------------------
// 行模型
// ------------------------------------------------------------

type opOrgUnitRow struct {
	ID            int64  `gorm:"column:id"`
	ParentID      *int64 `gorm:"column:parent_id"`
	Name          string `gorm:"column:name"`
	UnitType      string `gorm:"column:unit_type"`
	LeaderStaffID *int64 `gorm:"column:leader_staff_id"`
}

type opStaffCountRow struct {
	OrgUnitID int64 `gorm:"column:org_unit_id"`
	Cnt       int64 `gorm:"column:cnt"`
}

type opStaffNameRow struct {
	ID   int64  `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

type opRoleRow struct {
	ID        int64  `gorm:"column:id"`
	Code      string `gorm:"column:code"`
	Name      string `gorm:"column:name"`
	DataScope string `gorm:"column:data_scope"`
	FieldMask []byte `gorm:"column:field_mask"`
}

type opRolePermRow struct {
	RoleID    int64  `gorm:"column:role_id"`
	Code      string `gorm:"column:code"`
	PointName string `gorm:"column:point_name"`
}

type opGrantCountRow struct {
	RoleID int64 `gorm:"column:role_id"`
	Cnt    int64 `gorm:"column:cnt"`
}

// 常量：口径字符串（避免在多处重复字面值被改歪）。
const (
	opStatusActive = "ACTIVE"
)

// ------------------------------------------------------------
// 查询构造器
//
// 单独抽成包级函数，是为了让 orgperm_test.go 能用 GORM DryRun 直接断言生成的 SQL
// （与 customer_portal_test.go / customer_special_test.go 同一手法，不连真库）。
// 每个方法本体只负责「取 tx → 构造 → Scan → 转 DTO」。
// ------------------------------------------------------------

// opQueryOrgUnits status='ACTIVE' 的组织节点，按 id 升序。
func opQueryOrgUnits(tx *gorm.DB) *gorm.DB {
	return tx.Table("org_unit").
		Select("id, parent_id, name, unit_type, leader_staff_id").
		Where("status = ?", opStatusActive).
		Order("id ASC")
}

// opQueryStaffCounts 每个 org_unit 的在职直属人数。
func opQueryStaffCounts(tx *gorm.DB) *gorm.DB {
	return tx.Table("internal_staff").
		Select("org_unit_id, count(*) AS cnt").
		Where("status = ?", opStatusActive).
		Group("org_unit_id")
}

// opQueryStaffNames 指定 id 的员工姓名（ownerName 用）。
func opQueryStaffNames(tx *gorm.DB, ids []int64) *gorm.DB {
	return tx.Table("internal_staff").Select("id, name").Where("id IN ?", ids)
}

// opQueryRoles 全部角色，按 id 升序。
func opQueryRoles(tx *gorm.DB) *gorm.DB {
	return tx.Table("role").
		Select("id, code, name, data_scope, field_mask").
		Order("id ASC")
}

// opQueryRolePermissions 全部角色的权限点关联行（一次查完，不按角色循环）。
func opQueryRolePermissions(tx *gorm.DB) *gorm.DB {
	return tx.Table("role_permission AS rp").
		Select("rp.role_id, p.module_code || ':' || p.action_code AS code, p.name AS point_name").
		Joins("JOIN permission_point p ON p.id = rp.permission_point_id")
}

// opQueryGrantCounts 每个角色的未过期授权人数。
func opQueryGrantCounts(tx *gorm.DB) *gorm.DB {
	return tx.Table("role_grant").
		Select("role_id, count(*) AS cnt").
		Where("expires_at IS NULL OR expires_at > now()").
		Group("role_id")
}

// ------------------------------------------------------------
// 组织树
// ------------------------------------------------------------

// ListActiveOrgUnits 返回全部 status='ACTIVE' 的组织节点（按 id 升序，保证输出稳定）。
//
// 只 SELECT 建树需要的 5 列；**不含 path**（库里 path 有脏值，见 service 注释）。
func (r *OrgPermRepo) ListActiveOrgUnits(ctx context.Context) ([]orgperm.OrgUnitRow, error) {
	rows := make([]opOrgUnitRow, 0)
	err := opQueryOrgUnits(r.txOf(ctx)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list active org units: %w", err)
	}
	out := make([]orgperm.OrgUnitRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, orgperm.OrgUnitRow{
			ID:            row.ID,
			ParentID:      row.ParentID,
			Name:          row.Name,
			UnitType:      row.UnitType,
			LeaderStaffID: row.LeaderStaffID,
		})
	}
	return out, nil
}

// CountStaffByOrgUnit 返回每个 org_unit 的**在职直属**人数（一次 group by，不循环）。
//
// 口径：只数 status='ACTIVE' 的员工（离职/停用不计入组织人数）。
// 注意这是**直属**人数，含下级的累计值由 service 在树上累加。
func (r *OrgPermRepo) CountStaffByOrgUnit(ctx context.Context) (map[int64]int64, error) {
	rows := make([]opStaffCountRow, 0)
	err := opQueryStaffCounts(r.txOf(ctx)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("count staff by org unit: %w", err)
	}
	out := make(map[int64]int64, len(rows))
	for _, row := range rows {
		out[row.OrgUnitID] = row.Cnt
	}
	return out, nil
}

// ListStaffNames 返回 id → name（一次 IN 查询，用于组织负责人姓名）。
// ids 为空时不发 SQL，直接返回空 map。
func (r *OrgPermRepo) ListStaffNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows := make([]opStaffNameRow, 0)
	err := opQueryStaffNames(r.txOf(ctx), ids).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list staff names: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}

// ------------------------------------------------------------
// 角色矩阵
// ------------------------------------------------------------

// ListRoles 返回全部角色（按 id 升序），field_mask 已展开成 hide 列表。
//
// 外部门户角色（SUPPLIER / CUSTOMER）不参与 M1–M12 权限点矩阵，
// 其 role_permission 为空是预期行为（CLAUDE.md §五），但角色本身**照常返回**，
// 这样页面上的「外部门户角色」计数才正确。
func (r *OrgPermRepo) ListRoles(ctx context.Context) ([]orgperm.RoleRow, error) {
	rows := make([]opRoleRow, 0)
	err := opQueryRoles(r.txOf(ctx)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	out := make([]orgperm.RoleRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, orgperm.RoleRow{
			ID:         row.ID,
			Code:       row.Code,
			Name:       row.Name,
			DataScope:  row.DataScope,
			HideFields: parseHideFields(row.FieldMask),
		})
	}
	return out, nil
}

// ListRolePermissions 一次性取回**全部**角色的权限点关联行（避免逐角色查询）。
//
// code = permission_point.module_code || ':' || action_code（形如 "M5:E"），
// 与 pkg/perm 的常量同构；point_name 是中文名，交由 service 拆成 module / action。
func (r *OrgPermRepo) ListRolePermissions(ctx context.Context) ([]orgperm.RolePermissionRow, error) {
	rows := make([]opRolePermRow, 0)
	err := opQueryRolePermissions(r.txOf(ctx)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	out := make([]orgperm.RolePermissionRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, orgperm.RolePermissionRow{
			RoleID:    row.RoleID,
			Code:      row.Code,
			PointName: row.PointName,
		})
	}
	return out, nil
}

// CountGrantsByRole 返回每个角色的**有效授权**人数（一次 group by）。
//
// 口径：只数「未过期」的授权（expires_at IS NULL OR expires_at > now()），
// 与登录时 AuthRepo.ListRolesByOperator 的口径一致——已过期的授权拿不到角色，
// 就不该被算作该角色的成员。
func (r *OrgPermRepo) CountGrantsByRole(ctx context.Context) (map[int64]int64, error) {
	rows := make([]opGrantCountRow, 0)
	err := opQueryGrantCounts(r.txOf(ctx)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("count grants by role: %w", err)
	}
	out := make(map[int64]int64, len(rows))
	for _, row := range rows {
		out[row.RoleID] = row.Cnt
	}
	return out, nil
}

// parseHideFields 解析 role.field_mask（jsonb {"hide":[...]}）→ hide 字符串切片。
// NULL / 空 / 非法 JSON 一律返回 nil（service 会兜成 []）。
func parseHideFields(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var doc struct {
		Hide []string `json:"hide"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return doc.Hide
}
