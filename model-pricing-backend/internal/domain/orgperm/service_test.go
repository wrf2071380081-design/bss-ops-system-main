package orgperm

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"model_bss/pkg/perm"
)

// ============================================================
// fake store
// ============================================================

type fakeStore struct {
	orgUnits []OrgUnitRow
	staffCnt map[int64]int64
	names    map[int64]string
	roles    []RoleRow
	perms    []RolePermissionRow
	grants   map[int64]int64

	err error // 非 nil 时所有方法都返回它

	// 观测点：验证「避免 N+1」——负责人姓名必须一次 IN 查询取回，不能逐节点查。
	nameCalls   int
	nameCallIDs [][]int64
}

func (f *fakeStore) ListActiveOrgUnits(context.Context) ([]OrgUnitRow, error) {
	return f.orgUnits, f.err
}

func (f *fakeStore) CountStaffByOrgUnit(context.Context) (map[int64]int64, error) {
	return f.staffCnt, f.err
}

func (f *fakeStore) ListStaffNames(_ context.Context, ids []int64) (map[int64]string, error) {
	f.nameCalls++
	cp := make([]int64, len(ids))
	copy(cp, ids)
	f.nameCallIDs = append(f.nameCallIDs, cp)
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[int64]string, len(ids))
	for _, id := range ids {
		if f.names != nil {
			out[id] = f.names[id]
		}
	}
	return out, nil
}

func (f *fakeStore) ListRoles(context.Context) ([]RoleRow, error) { return f.roles, f.err }

func (f *fakeStore) ListRolePermissions(context.Context) ([]RolePermissionRow, error) {
	return f.perms, f.err
}

func (f *fakeStore) CountGrantsByRole(context.Context) (map[int64]int64, error) {
	return f.grants, f.err
}

func ptr(v int64) *int64 { return &v }

// ============================================================
// 组织树
// ============================================================

// TestListOrganizations_TreeAndCumulativeMemberCount 覆盖：
// 三层嵌套 + memberCount 含全部下级（不是直属）。
func TestListOrganizations_TreeAndCumulativeMemberCount(t *testing.T) {
	st := &fakeStore{
		orgUnits: []OrgUnitRow{
			{ID: 1, Name: "根公司", UnitType: "COMPANY"},
			{ID: 2, ParentID: ptr(1), Name: "平台部", UnitType: "DEPT"},
			{ID: 3, ParentID: ptr(2), Name: "主数据组", UnitType: "TEAM"},
			{ID: 4, ParentID: ptr(1), Name: "采购部", UnitType: "DEPT"},
		},
		staffCnt: map[int64]int64{1: 1, 2: 2, 3: 3, 4: 4},
	}
	got, err := NewService(st, nil).ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 个根节点，实际 %d", len(got))
	}
	root := got[0]
	if root.ID != "1" || root.Code != "1" || root.Name != "根公司" || root.Type != OrgTypeCompany {
		t.Fatalf("根节点字段不符：%+v", root)
	}
	// 1 + (2+3) + 4 = 10
	if root.MemberCount != 10 {
		t.Fatalf("根 memberCount=10（含全部下级），实际 %d", root.MemberCount)
	}
	if len(root.Children) != 2 {
		t.Fatalf("根应有 2 个直接子节点，实际 %d", len(root.Children))
	}
	plat, purch := root.Children[0], root.Children[1]
	if plat.ID != "2" || plat.Type != OrgTypeDepartment || plat.MemberCount != 5 { // 2+3
		t.Fatalf("平台部不符：%+v", plat)
	}
	if purch.ID != "4" || purch.MemberCount != 4 {
		t.Fatalf("采购部不符：%+v", purch)
	}
	if len(plat.Children) != 1 || plat.Children[0].ID != "3" || plat.Children[0].MemberCount != 3 {
		t.Fatalf("主数据组不符：%+v", plat.Children)
	}
	// unit_type=TEAM 必须映射成 TEAM（不是 DEPARTMENT）——三种枚举都要在本用例里钉住，
	// 不能只靠 TestUnitTypeOf，否则树这条路径上改坏映射没人报警。
	if plat.Children[0].Type != OrgTypeTeam {
		t.Fatalf("TEAM 应映射为 TEAM，实际 %s", plat.Children[0].Type)
	}
	if plat.Children[0].Children != nil {
		t.Fatalf("叶子节点不应有 children：%+v", plat.Children[0].Children)
	}
	if purch.Type != OrgTypeDepartment {
		t.Fatalf("DEPT 应映射为 DEPARTMENT，实际 %s", purch.Type)
	}
}

// TestListOrganizations_OrphanAndSelfParentBecomeRoot 覆盖：
// parent_id 指向不存在的节点 / 自引用 → 降为根节点，数据不丢、不死循环。
func TestListOrganizations_OrphanAndSelfParentBecomeRoot(t *testing.T) {
	st := &fakeStore{
		orgUnits: []OrgUnitRow{
			{ID: 5, ParentID: ptr(99), Name: "孤儿部门", UnitType: "DEPT"},
			{ID: 7, ParentID: ptr(7), Name: "自引用部门", UnitType: "DEPT"},
			{ID: 9, Name: "正常根", UnitType: "COMPANY"},
		},
		staffCnt: map[int64]int64{5: 2, 7: 1, 9: 3},
	}
	got, err := NewService(st, nil).ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("3 个节点全部应降为根，实际 %d：%+v", len(got), got)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"5", "7", "9"}) {
		t.Fatalf("根节点顺序应为输入顺序 5,7,9，实际 %v", ids)
	}
}

// TestListOrganizations_ParentAppearsAfterChildIsStillAttached 防回归：
// 早期实现「边遍历边把节点值拷进结果数组」，父节点先入数组、子节点后挂，
// 子树会整棵丢失。本用例把子节点排在父节点之前（违反 id 升序的输入顺序）。
func TestListOrganizations_ParentAppearsAfterChildIsStillAttached(t *testing.T) {
	st := &fakeStore{
		orgUnits: []OrgUnitRow{
			{ID: 20, ParentID: ptr(10), Name: "子", UnitType: "DEPT"},
			{ID: 10, Name: "父", UnitType: "COMPANY"},
		},
		staffCnt: map[int64]int64{10: 1, 20: 2},
	}
	got, err := NewService(st, nil).ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "10" {
		t.Fatalf("只应有一个根节点 10，实际 %+v", got)
	}
	if len(got[0].Children) != 1 || got[0].Children[0].ID != "20" {
		t.Fatalf("子节点 20 未挂到父节点 10：%+v", got[0])
	}
	if got[0].MemberCount != 3 {
		t.Fatalf("父 memberCount 应为 1+2=3，实际 %d", got[0].MemberCount)
	}
}

// TestListOrganizations_OwnerNameSingleQuery 覆盖 ownerName 且验证「不逐节点查库」。
func TestListOrganizations_OwnerNameSingleQuery(t *testing.T) {
	st := &fakeStore{
		orgUnits: []OrgUnitRow{
			{ID: 1, LeaderStaffID: ptr(11), Name: "公司", UnitType: "COMPANY"},
			{ID: 2, ParentID: ptr(1), LeaderStaffID: ptr(11), Name: "部门", UnitType: "DEPT"},
			{ID: 3, ParentID: ptr(1), Name: "无负责人部门", UnitType: "DEPT"},
		},
		names: map[int64]string{11: "顾言"},
	}
	got, err := NewService(st, nil).ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].OwnerName != "顾言" || got[0].Children[0].OwnerName != "顾言" {
		t.Fatalf("ownerName 未取到：%+v", got[0])
	}
	if got[0].Children[1].OwnerName != "" {
		t.Fatalf("无负责人应为空串，实际 %q", got[0].Children[1].OwnerName)
	}
	// 关键：3 个节点、其中 2 个指向同一个负责人 → 只能发 1 次查询、去重成 1 个 id。
	if st.nameCalls != 1 {
		t.Fatalf("负责人姓名必须一次查回（防 N+1），实际调用 %d 次", st.nameCalls)
	}
	if !reflect.DeepEqual(st.nameCallIDs[0], []int64{11}) {
		t.Fatalf("传入的 id 应去重为 [11]，实际 %v", st.nameCallIDs[0])
	}
}

// TestListOrganizations_NoLeaderSkipsQuery 无负责人时不发 SQL。
func TestListOrganizations_NoLeaderSkipsQuery(t *testing.T) {
	st := &fakeStore{
		orgUnits: []OrgUnitRow{{ID: 1, Name: "公司", UnitType: "COMPANY"}},
	}
	if _, err := NewService(st, nil).ListOrganizations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.nameCalls != 0 {
		t.Fatalf("无 leader_staff_id 时不应查 internal_staff，实际 %d 次", st.nameCalls)
	}
}

// TestListOrganizations_EmptyIsEmptySliceNotNil 空数据必须序列化成 []，不是 null。
func TestListOrganizations_EmptyIsEmptySliceNotNil(t *testing.T) {
	got, err := NewService(&fakeStore{}, nil).ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("空数据必须返回非 nil 空切片（JSON 才能是 [] 而不是 null）")
	}
	if len(got) != 0 {
		t.Fatalf("应为空，实际 %d", len(got))
	}
}

// TestListOrganizations_StoreErrorPropagates 仓储错误必须上抛，不能被吞成空结果。
func TestListOrganizations_StoreErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	st := &fakeStore{err: want, orgUnits: []OrgUnitRow{{ID: 1}}}
	if _, err := NewService(st, nil).ListOrganizations(context.Background()); !errors.Is(err, want) {
		t.Fatalf("错误应原样上抛，实际 %v", err)
	}
}

func TestUnitTypeOf(t *testing.T) {
	cases := map[string]string{
		"COMPANY": OrgTypeCompany,
		"DEPT":    OrgTypeDepartment,
		"TEAM":    OrgTypeTeam,
		"dept":    OrgTypeDepartment,
		" DEPT ":  OrgTypeDepartment,
		"GROUP":   OrgTypeDepartment, // 未知兜底，保证 DTO 三值联合类型闭合
	}
	for in, want := range cases {
		if got := unitTypeOf(in); got != want {
			t.Fatalf("unitTypeOf(%q)=%s，期望 %s", in, got, want)
		}
	}
}

// ============================================================
// 角色矩阵
// ============================================================

func TestListRoles_AssemblesAllFields(t *testing.T) {
	st := &fakeStore{
		roles: []RoleRow{
			{ID: 23, Code: perm.RolePlatformAdmin, Name: "平台管理员", DataScope: "ALL", HideFields: nil},
			{ID: 30, Code: perm.RoleSales, Name: "销售", DataScope: "SELF", HideFields: []string{"unit_cost", "margin_before"}},
			{ID: 31, Code: perm.RoleSupplier, Name: "供应商操作员", DataScope: "SELF"},
			{ID: 32, Code: perm.RoleCustomer, Name: "客户操作员", DataScope: "SELF"},
		},
		perms: []RolePermissionRow{
			{RoleID: 23, Code: perm.M1View, PointName: "模型管理·查看"},
			{RoleID: 30, Code: perm.M9Edit, PointName: "客户报价·编辑"},
		},
		grants: map[int64]int64{23: 1, 30: 2},
	}
	got, err := NewService(st, nil).ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("期望 4 个角色，实际 %d", len(got))
	}

	admin := got[0]
	if admin.ID != "23" || admin.Name != "平台管理员" || admin.DataScope != "ALL" {
		t.Fatalf("角色基础字段不符：%+v", admin)
	}
	if admin.Portal != PortalInternal {
		t.Fatalf("PLATFORM_ADMIN 应 internal，实际 %s", admin.Portal)
	}
	if admin.MemberCount != 1 {
		t.Fatalf("memberCount 应取 role_grant 计数=1，实际 %d", admin.MemberCount)
	}
	if admin.Description != "" {
		t.Fatalf("role 表无 description 列，必须返回空串而不是编造，实际 %q", admin.Description)
	}
	if len(admin.Permissions) != 1 || admin.Permissions[0].Code != perm.M1View {
		t.Fatalf("permissions 不符：%+v", admin.Permissions)
	}
	if admin.Permissions[0].Module != "模型管理" || admin.Permissions[0].Action != "查看" {
		t.Fatalf("module/action 未按「·」拆分：%+v", admin.Permissions[0])
	}
	if admin.FieldRestrictions == nil || len(admin.FieldRestrictions) != 0 {
		t.Fatalf("field_mask 为 NULL 时必须返回 []（非 null），实际 %v", admin.FieldRestrictions)
	}

	sales := got[1]
	if !reflect.DeepEqual(sales.FieldRestrictions, []string{"unit_cost", "margin_before"}) {
		t.Fatalf("fieldRestrictions 应取 hide 数组，实际 %v", sales.FieldRestrictions)
	}
	if sales.MemberCount != 2 {
		t.Fatalf("销售 memberCount 应为 2，实际 %d", sales.MemberCount)
	}

	if got[2].Portal != PortalSupplier || got[3].Portal != PortalCustomer {
		t.Fatalf("portal 推导错误：%s / %s", got[2].Portal, got[3].Portal)
	}
	// 无 role_grant 计数的角色 → 0，不是缺键。
	if got[2].MemberCount != 0 || got[3].MemberCount != 0 {
		t.Fatalf("无授权的角色 memberCount 应为 0：%d / %d", got[2].MemberCount, got[3].MemberCount)
	}
	// 外部门户角色 role_permission 为空是预期行为，permissions 必须是 [] 而非 null。
	if got[2].Permissions == nil || len(got[2].Permissions) != 0 {
		t.Fatalf("空权限必须返回 []，实际 %v", got[2].Permissions)
	}
}

// TestListRoles_PermissionOrderFollowsPermAll 权限点展示顺序必须按 perm.go 的
// M1..M12 语义顺序，不能按 DB 字典序（字典序会排出 M1,M10,M11,M12,M2…）。
func TestListRoles_PermissionOrderFollowsPermAll(t *testing.T) {
	st := &fakeStore{
		roles: []RoleRow{{ID: 1, Code: perm.RolePlatformAdmin, Name: "平台管理员", DataScope: "ALL"}},
		perms: []RolePermissionRow{
			{RoleID: 1, Code: perm.M12Privilege, PointName: "审计·特权"},
			{RoleID: 1, Code: perm.M10View, PointName: "财务结算·查看"},
			{RoleID: 1, Code: perm.M1Edit, PointName: "模型管理·编辑"},
			{RoleID: 1, Code: perm.M2View, PointName: "官方价格同步·查看"},
			{RoleID: 1, Code: perm.M1View, PointName: "模型管理·查看"},
		},
	}
	got, err := NewService(st, nil).ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, p := range got[0].Permissions {
		codes = append(codes, p.Code)
	}
	want := []string{perm.M1View, perm.M1Edit, perm.M2View, perm.M10View, perm.M12Privilege}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("权限点顺序应为 %v，实际 %v", want, codes)
	}
}

// TestListRoles_AllPermCodesAreDeclaredInPerm 输出的每个 code 都必须在 perm.All() 里，
// 保证 DTO 的权限编码词汇与 pkg/perm 唯一事实源一致（不出现野编码）。
func TestListRoles_AllPermCodesAreDeclaredInPerm(t *testing.T) {
	var rows []RolePermissionRow
	for _, code := range perm.All() {
		rows = append(rows, RolePermissionRow{RoleID: 1, Code: code, PointName: "模块·操作"})
	}
	st := &fakeStore{
		roles: []RoleRow{{ID: 1, Code: perm.RolePlatformAdmin, Name: "全权限", DataScope: "ALL"}},
		perms: rows,
	}
	got, err := NewService(st, nil).ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Permissions) != len(perm.All()) {
		t.Fatalf("应有 %d 个权限点，实际 %d", len(perm.All()), len(got[0].Permissions))
	}
	known := make(map[string]struct{}, len(perm.All()))
	for _, c := range perm.All() {
		known[c] = struct{}{}
	}
	for _, p := range got[0].Permissions {
		if _, ok := known[p.Code]; !ok {
			t.Fatalf("输出了 perm.All() 之外的编码 %q", p.Code)
		}
	}
}

func TestListRoles_EmptyIsEmptySliceNotNil(t *testing.T) {
	got, err := NewService(&fakeStore{}, nil).ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("空数据必须返回非 nil 空切片")
	}
}

func TestListRoles_StoreErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	if _, err := NewService(&fakeStore{err: want}, nil).ListRoles(context.Background()); !errors.Is(err, want) {
		t.Fatalf("错误应原样上抛，实际 %v", err)
	}
}

func TestPortalOf(t *testing.T) {
	if got := portalOf(perm.RoleSupplier); got != PortalSupplier {
		t.Fatalf("SUPPLIER→supplier，实际 %s", got)
	}
	if got := portalOf(perm.RoleCustomer); got != PortalCustomer {
		t.Fatalf("CUSTOMER→customer，实际 %s", got)
	}
	for _, code := range []string{perm.RolePlatformAdmin, perm.RoleSales, perm.RoleFinance, perm.RoleOpsAdmin, "UNKNOWN_NEW_ROLE"} {
		if got := portalOf(code); got != PortalInternal {
			t.Fatalf("%s 应 internal，实际 %s", code, got)
		}
	}
}

func TestSplitPointName(t *testing.T) {
	if m, a := splitPointName("成本管理·编辑", "M5:E"); m != "成本管理" || a != "编辑" {
		t.Fatalf("拆分错误：%q / %q", m, a)
	}
	// 无分隔符：module 取整个 name，action 退化为操作码。
	if m, a := splitPointName("成本管理", "M5:E"); m != "成本管理" || a != "E" {
		t.Fatalf("无分隔符兜底错误：%q / %q", m, a)
	}
	// 空 name：两列都退化为 code，不留空白。
	if m, a := splitPointName("", "M5:E"); m != "M5:E" || a != "M5:E" {
		t.Fatalf("空 name 兜底错误：%q / %q", m, a)
	}
}
