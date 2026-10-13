// Package orgperm 的 service.go：组织树与角色矩阵装配（批次 5，全只读）。
//
// 两条硬要求（任务书 §数据来源）：
//  1. **避免 N+1**：所有关联数据（直属人数、负责人姓名、权限点、授权人数）
//     一律一次性 group by / IN 查出来再在内存里挂回，绝不按节点或按角色循环查库。
//  2. **不编造**：role 表没有的字段（description / portal）要么留空，
//     要么给出可验证的推导规则并写进注释，不写臆测文案。
package orgperm

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"model_bss/pkg/perm"
)

// permPointSep 是 permission_point.name 里模块名与操作名的分隔符——
// 全角间隔号「·」（U+00B7，UTF-8 = c2 b7），例如 "成本管理·编辑"。
const permPointSep = "·"

// Store 是 orgperm 需要的全部只读数据访问。
// 全部方法都是「批量取、不带分页」，由 service 在内存里组装。
type Store interface {
	// ListActiveOrgUnits 返回全部 status='ACTIVE' 的组织节点（顺序由 SQL 保证）。
	ListActiveOrgUnits(ctx context.Context) ([]OrgUnitRow, error)
	// CountStaffByOrgUnit 返回每个 org_unit 的**在职直属**人数（key = org_unit_id）。
	CountStaffByOrgUnit(ctx context.Context) (map[int64]int64, error)
	// ListStaffNames 返回 id → name（只查给定 id，用于 ownerName）。
	ListStaffNames(ctx context.Context, ids []int64) (map[int64]string, error)
	// ListRoles 返回全部角色（含 field_mask 展开）。
	ListRoles(ctx context.Context) ([]RoleRow, error)
	// ListRolePermissions 返回全部角色的权限点关联行（一次查完，不按角色循环）。
	ListRolePermissions(ctx context.Context) ([]RolePermissionRow, error)
	// CountGrantsByRole 返回每个角色的**有效授权**人数（key = role_id）。
	CountGrantsByRole(ctx context.Context) (map[int64]int64, error)
}

// Service 组织与权限服务（只读侧 + 账号管理写侧共用）。
type Service struct {
	store Store
	// staffStore 是账号管理写侧的仓储。2026-10-13 补写接口时加入；
	// nil 时写方法会 panic——router 里用 deps.StaffStore != nil 守住了不挂载，
	// 这里再加一层显式检查，避免将来有人绕过 router 直接调 service。
	staffStore StaffStore
}

// NewService 构造。
//
// staffStore 是**必填**参数（不是可选）——漏传编译不过，防止写接口在没接仓储的
// 环境里被静默启用。这与批次 2b「service 签名加首位 scope」是同一条纪律。
func NewService(store Store, staffStore StaffStore) *Service {
	return &Service{store: store, staffStore: staffStore}
}

// staffStoreOrErr 返回写侧仓储；未注入时返回系统错误而不是 panic。
// 正常路径下 router 已经拦住（deps.StaffStore == nil 就不挂载写路由）。
func (s *Service) staffStoreOrErr() (StaffStore, error) {
	if s.staffStore == nil {
		return nil, fmt.Errorf("orgperm: StaffStore 未注入，账号管理写接口不可用")
	}
	return s.staffStore, nil
}

// ============================================================
// 组织树
// ============================================================

// ListOrganizations 返回组织树（根节点数组，children 递归嵌套）。
//
// 口径：
//   - 只含 status='ACTIVE' 的节点（由 Store 保证）；
//   - **按 parent_id 建树，不用 path**：库里 path 存在脏值（id=2 的 path='/1/1/'），
//     按 path 解析会挂错父节点；parent_id 是外键，可信；
//   - parent_id 为 NULL、或指向一个不存在/非 ACTIVE 的节点时，该节点**降为根节点**，
//     保证数据不会因为父节点缺失而整棵消失；
//   - memberCount = 直属在职人数 + 全部下级节点的 memberCount（后序遍历累加）。
func (s *Service) ListOrganizations(ctx context.Context) ([]OrganizationNode, error) {
	rows, err := s.store.ListActiveOrgUnits(ctx)
	if err != nil {
		return nil, err
	}
	direct, err := s.store.CountStaffByOrgUnit(ctx)
	if err != nil {
		return nil, err
	}
	ownerNames, err := s.listOwnerNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	return buildTree(rows, direct, ownerNames), nil
}

// listOwnerNames 一次性取回全部负责人姓名（id → name），避免逐节点查库。
func (s *Service) listOwnerNames(ctx context.Context, rows []OrgUnitRow) (map[int64]string, error) {
	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, r := range rows {
		if r.LeaderStaffID == nil {
			continue
		}
		if _, dup := seen[*r.LeaderStaffID]; dup {
			continue
		}
		seen[*r.LeaderStaffID] = struct{}{}
		ids = append(ids, *r.LeaderStaffID)
	}
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	return s.store.ListStaffNames(ctx, ids)
}

// buildTree 按 parent_id 建树，并在同一趟后序遍历里累加 memberCount。
//
// 实现要点：**先建立「父 → 子 id 列表」的索引，再递归物化**。
// 不能边遍历边把节点值拷进结果数组——父节点先被值拷贝、子节点后挂上去的话，
// 拷进结果的那份是挂载前的快照，子树会整棵丢失。
// 递归物化天然保证「子树的 memberCount 先算完再加到父节点」。
func buildTree(rows []OrgUnitRow, direct map[int64]int64, ownerNames map[int64]string) []OrganizationNode {
	nodes := make(map[int64]*OrganizationNode, len(rows))
	parentOf := make(map[int64]*int64, len(rows))
	order := make([]int64, 0, len(rows))
	for _, r := range rows {
		id := strconv.FormatInt(r.ID, 10)
		name := ""
		if r.LeaderStaffID != nil {
			name = ownerNames[*r.LeaderStaffID]
		}
		parentOf[r.ID] = r.ParentID
		nodes[r.ID] = &OrganizationNode{
			ID:          id,
			Code:        id, // org_unit 无 code 列：用 id 填充，见 model.go 注释
			Name:        r.Name,
			Type:        unitTypeOf(r.UnitType),
			MemberCount: direct[r.ID],
			OwnerName:   name,
		}
		order = append(order, r.ID)
	}

	// 父 → 子 id 列表；parent_id 为 NULL / 指向不存在的节点 / 自引用 → 降为根。
	rootIDs := make([]int64, 0, len(rows))
	childrenOf := make(map[int64][]int64, len(rows))
	for _, id := range order {
		parentID := int64(0)
		if p := parentOf[id]; p != nil {
			parentID = *p
		}
		if _, ok := nodes[parentID]; ok && parentID != id {
			childrenOf[parentID] = append(childrenOf[parentID], id)
			continue
		}
		rootIDs = append(rootIDs, id)
	}

	// 环防护：parent_id 理论上不可能成环（外键 + 业务写入约束），
	// 但一旦成环递归会栈溢出，这里用 visiting 集合直接断开。
	visiting := make(map[int64]bool, len(rows))
	var materialize func(id int64) OrganizationNode
	materialize = func(id int64) OrganizationNode {
		node := *nodes[id]
		node.Children = nil
		if visiting[id] {
			return node
		}
		visiting[id] = true
		var sub int64
		for _, childID := range childrenOf[id] {
			child := materialize(childID)
			sub += child.MemberCount
			node.Children = append(node.Children, child)
		}
		delete(visiting, id)
		node.MemberCount += sub
		return node
	}

	roots := make([]OrganizationNode, 0, len(rootIDs))
	for _, id := range rootIDs {
		roots = append(roots, materialize(id))
	}
	return roots
}

// unitTypeOf 把 DB 的 org_unit.unit_type 映射成 DTO 枚举。
//
// DB 当前只有 COMPANY / DEPT 两种值（TEAM 是设计预留）。
// 未知值兜底成 DEPARTMENT——DTO 的 type 是三值联合类型，不能把 DB 原值透出去，
// 否则前端渲染成空；将来新增组织类型必须同步扩展本函数并补单测。
func unitTypeOf(unitType string) string {
	switch strings.ToUpper(strings.TrimSpace(unitType)) {
	case "COMPANY":
		return OrgTypeCompany
	case "TEAM":
		return OrgTypeTeam
	default:
		return OrgTypeDepartment
	}
}

// ============================================================
// 角色矩阵
// ============================================================

// ListRoles 返回角色权限矩阵（每行含权限点明细、数据域、字段限制、成员数）。
func (s *Service) ListRoles(ctx context.Context) ([]RoleSummary, error) {
	roles, err := s.store.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	permRows, err := s.store.ListRolePermissions(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := s.store.CountGrantsByRole(ctx)
	if err != nil {
		return nil, err
	}

	byRole := make(map[int64][]RolePermissionRow, len(roles))
	for _, p := range permRows {
		byRole[p.RoleID] = append(byRole[p.RoleID], p)
	}

	out := make([]RoleSummary, 0, len(roles))
	for _, r := range roles {
		perms := make([]RolePermission, 0, len(byRole[r.ID]))
		for _, p := range byRole[r.ID] {
			module, action := splitPointName(p.PointName, p.Code)
			perms = append(perms, RolePermission{Code: p.Code, Module: module, Action: action})
		}
		sortPermissions(perms)

		restrictions := r.HideFields
		if restrictions == nil {
			restrictions = []string{}
		}
		out = append(out, RoleSummary{
			ID:                strconv.FormatInt(r.ID, 10),
			Name:              r.Name,
			Description:       "", // role 表无该列，不编造；前端按空串兜底
			Portal:            portalOf(r.Code),
			MemberCount:       grants[r.ID],
			DataScope:         r.DataScope,
			Permissions:       perms,
			FieldRestrictions: restrictions,
		})
	}
	return out, nil
}

// portalOf 由 role.code 推导门户归属。
//
// role 表**没有 portal 列**。规则（与登录态 portal_type 同口径）：
//   - code = SUPPLIER → supplier（供应商门户）
//   - code = CUSTOMER → customer（客户门户）
//   - 其余全部 → internal（内部运营）
//
// 角色 code 常量取 pkg/perm（唯一事实源），不在这里写裸字符串。
// 将来新增外部门户角色必须同步扩展本函数。
func portalOf(roleCode string) string {
	switch roleCode {
	case perm.RoleSupplier:
		return PortalSupplier
	case perm.RoleCustomer:
		return PortalCustomer
	default:
		return PortalInternal
	}
}

// splitPointName 把 permission_point.name 拆成 module / action。
// name 形如 "成本管理·编辑" → ("成本管理", "编辑")。
// 没有分隔符（数据异常或将来改格式）时：module 取整个 name，action 退化为
// 操作码本身（如 "E"），保证前端两列都不为空。
func splitPointName(pointName, code string) (module, action string) {
	pointName = strings.TrimSpace(pointName)
	if pointName == "" {
		return code, code
	}
	if i := strings.Index(pointName, permPointSep); i >= 0 {
		return strings.TrimSpace(pointName[:i]), strings.TrimSpace(pointName[i+len(permPointSep):])
	}
	return pointName, actionCodeOf(code)
}

// actionCodeOf 从 "M5:E" 里取冒号后的操作码。
func actionCodeOf(code string) string {
	if i := strings.Index(code, ":"); i >= 0 && i+1 < len(code) {
		return code[i+1:]
	}
	return code
}

// permOrder 是 pkg/perm.All() 的下标映射。
//
// 用途：权限点的**展示顺序**以 perm.go 声明的语义顺序（M1..M12 × V/E/A/C/P）为准。
// 若按 DB 的 module_code 字典序排，会排出 M1, M10, M11, M12, M2… 这种反直觉顺序。
// 同时它也保证：本包输出的所有 code 都在 perm.All() 里（不在则排到末尾，见 sortPermissions）。
var permOrder = buildPermOrder()

func buildPermOrder() map[string]int {
	all := perm.All()
	m := make(map[string]int, len(all))
	for i, code := range all {
		m[code] = i
	}
	return m
}

// sortPermissions 按 perm.go 的语义顺序排序；不在 perm.All() 里的未知 code 排最后
// （不可能出现——启动校验 ValidateInDB 会拒绝启动，此处只是兜底），
// 同序时按 code 字典序，保证输出稳定可测。
func sortPermissions(items []RolePermission) {
	rank := func(code string) int {
		if i, ok := permOrder[code]; ok {
			return i
		}
		return len(permOrder)
	}
	sort.Slice(items, func(i, j int) bool {
		ri, rj := rank(items[i].Code), rank(items[j].Code)
		if ri != rj {
			return ri < rj
		}
		return items[i].Code < items[j].Code
	})
}
