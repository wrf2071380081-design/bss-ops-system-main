// Package orgperm 是 M11（组织与权限）只读视图的领域层（批次 5）。
//
// 定位：**只读**。本包不提供任何写能力——角色增删改、授权变更一律不做。
// 数据来源全部是权限体系自身的配置表（org_unit / internal_staff / role /
// role_permission / permission_point / role_grant），因此不参与业务数据域过滤
// （没有「谁的报价」这种归属概念），权限控制只靠路由级 RequirePerm(M11:V)。
//
// DTO 契约以前端 `model-pricing-web/src/api/orgPermissions.ts` 为准，字段名与
// 枚举值必须逐字一致（camelCase）。
package orgperm

// 组织类型枚举（前端 OrganizationNodeDTO.type 的联合类型）。
//
// 注意：DB 的 org_unit.unit_type 当前只有 COMPANY / DEPT 两种值，
// 且 DEPT 在 DTO 里要改叫 DEPARTMENT。映射见 service.unitTypeOf。
const (
	OrgTypeCompany    = "COMPANY"
	OrgTypeDepartment = "DEPARTMENT"
	OrgTypeTeam       = "TEAM"
)

// 门户归属枚举（前端 RoleSummaryDTO.portal 的联合类型）。
const (
	PortalInternal = "internal"
	PortalSupplier = "supplier"
	PortalCustomer = "customer"
)

// OrganizationNode 组织树节点（对应前端 OrganizationNodeDTO）。
//
// 字段口径（与前端 DTO 注释一致）：
//   - ID：org_unit.id 转字符串。DTO 的 id 是 string，不是数字。
//   - Code：**org_unit 没有 code 列**，这里用 id 转字符串填充，
//     即 code 恒等于 id，**不是业务编码**。将来若加了真实编码列，改这里即可。
//   - MemberCount：该节点**含全部下级**的在职人数（不是直属人数）。
//   - OwnerName：org_unit.leader_staff_id → internal_staff.name；无负责人时空串。
//   - Children：叶子节点不输出该键（omitempty），前端 `children?.length || 0`。
type OrganizationNode struct {
	ID          string             `json:"id"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Type        string             `json:"type"` // COMPANY / DEPARTMENT / TEAM
	MemberCount int64              `json:"memberCount"`
	OwnerName   string             `json:"ownerName"`
	Children    []OrganizationNode `json:"children,omitempty"`
}

// RolePermission 角色持有的单个权限点（对应前端 RolePermissionDTO）。
//
// Module / Action 由 permission_point.name 按全角间隔号「·」（U+00B7）拆分得到，
// 例如 name="成本管理·编辑" → Module="成本管理"、Action="编辑"。
// **不在本包硬编码任何模块中文名**——文案的唯一事实源是 permission_point 表，
// 顺序与合法性由 pkg/perm 的 60 点清单（perm.All()）锚定。
type RolePermission struct {
	Code   string `json:"code"`   // 形如 "M5:E"，取自 module_code || ':' || action_code
	Module string `json:"module"` // permission_point.name 的「·」前半
	Action string `json:"action"` // permission_point.name 的「·」后半
}

// RoleSummary 角色矩阵一行（对应前端 RoleSummaryDTO）。
//
// 字段口径：
//   - Portal：role 表没有 portal 列，按 role.code 推导（见 service.portalOf）。
//   - Description：role 表没有 description 列，且**不编造文案**——恒返回空串，
//     前端展示时自行兜底（见前端 DTO 注释）。
//   - DataScope：role.data_scope 原值透传（SELF / DEPT / DEPT_SUB / ALL）。
//   - Permissions / FieldRestrictions：**恒为非 null 数组**（空则 []），
//     前端直接取 .length，null 会炸。
type RoleSummary struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Portal            string           `json:"portal"`
	MemberCount       int64            `json:"memberCount"`
	DataScope         string           `json:"dataScope"`
	Permissions       []RolePermission `json:"permissions"`
	FieldRestrictions []string         `json:"fieldRestrictions"`
}

// ============================================================
// 仓储行模型（repo → service，不出现在 API 响应里）
// ============================================================

// OrgUnitRow 组织节点原始行。只取建树需要的列，不含 path
// （path 是物化路径，库里存在脏值——id=2 的 path 是 '/1/1/' 而非 '/1/2/'，
// 所以本包按 parent_id 建树，不依赖 path）。
type OrgUnitRow struct {
	ID            int64
	ParentID      *int64
	Name          string
	UnitType      string
	LeaderStaffID *int64
}

// RoleRow 角色原始行。HideFields 是 role.field_mask 的 {"hide":[...]} 展开结果。
type RoleRow struct {
	ID         int64
	Code       string
	Name       string
	DataScope  string
	HideFields []string
}

// RolePermissionRow 角色-权限点关联行。
type RolePermissionRow struct {
	RoleID    int64
	Code      string
	PointName string
}
