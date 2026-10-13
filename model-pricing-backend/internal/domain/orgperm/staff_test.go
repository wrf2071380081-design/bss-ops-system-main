package orgperm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"model_bss/pkg/perm"
)

// fakeStaffStore 是 StaffStore 的内存假实现。
// 只实现本测试用到的分支；未用到的方法返回零值 + nil error。
type fakeStaffStore struct {
	staff      map[int64]*StaffRow
	roles      map[int64]RoleRefRow
	orgs       map[int64]OrgUnitRefRow
	loginTaken map[string]bool
	mobTaken   map[string]bool
	adminCount int64

	created  *CreateStaffTxParams
	updated  *UpdateStaffTxParams
	reset    *ResetPasswordTxParams
	granted  *GrantRolesTxParams
	revoked  *RevokeRoleTxParams
	failWith error
}

func newFakeStaffStore() *fakeStaffStore {
	return &fakeStaffStore{
		staff:      map[int64]*StaffRow{},
		roles:      map[int64]RoleRefRow{},
		orgs:       map[int64]OrgUnitRefRow{},
		loginTaken: map[string]bool{},
		mobTaken:   map[string]bool{},
	}
}

func (f *fakeStaffStore) ListStaff(context.Context, StaffQuery) ([]StaffRow, int64, error) {
	return nil, 0, f.failWith
}

func (f *fakeStaffStore) FindStaff(_ context.Context, id int64) (*StaffRow, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	row, ok := f.staff[id]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (f *fakeStaffStore) ExistsLoginID(_ context.Context, id string) (bool, error) {
	return f.loginTaken[id], nil
}

func (f *fakeStaffStore) ExistsMobile(_ context.Context, m string) (bool, error) {
	return f.mobTaken[m], nil
}

func (f *fakeStaffStore) FindOrgUnit(_ context.Context, id int64) (*OrgUnitRefRow, error) {
	o, ok := f.orgs[id]
	if !ok {
		return nil, nil
	}
	return &o, nil
}

func (f *fakeStaffStore) FindRolesByID(_ context.Context, ids []int64) ([]RoleRefRow, error) {
	out := make([]RoleRefRow, 0, len(ids))
	for _, id := range ids {
		if r, ok := f.roles[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStaffStore) CountActivePlatformAdmins(context.Context) (int64, error) {
	return f.adminCount, nil
}

func (f *fakeStaffStore) StaffHasRole(context.Context, int64, int64) (bool, error) { return false, nil }

func (f *fakeStaffStore) CreateStaffTx(_ context.Context, p CreateStaffTxParams) (*StaffRow, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.created = &p
	id := int64(100)
	// 与真实 repo 对齐：建完回查一次，所以 Roles 是查回来的而不是入参回显
	roles := make([]StaffRoleRow, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, StaffRoleRow{ID: r.ID, Code: r.Code, Name: r.Name, DataScope: r.DataScope})
	}
	f.staff[id] = &StaffRow{
		ID: id, Name: p.Name, Mobile: p.Mobile, OrgUnitID: p.OrgID,
		Status: StaffStatusActive, LoginID: p.LoginID, AccountStatus: StaffStatusActive,
		Roles: roles,
	}
	return f.staff[id], nil
}

func (f *fakeStaffStore) UpdateStaffTx(_ context.Context, p UpdateStaffTxParams) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.updated = &p
	if row, ok := f.staff[p.StaffID]; ok {
		row.Name, row.Mobile, row.OrgUnitID, row.Status = p.Name, p.Mobile, p.OrgID, p.Status
	}
	return nil
}

func (f *fakeStaffStore) ResetPasswordTx(_ context.Context, p ResetPasswordTxParams) error {
	f.reset = &p
	return f.failWith
}

func (f *fakeStaffStore) GrantRolesTx(_ context.Context, p GrantRolesTxParams) error {
	f.granted = &p
	return f.failWith
}

func (f *fakeStaffStore) RevokeRoleTx(_ context.Context, p RevokeRoleTxParams) error {
	f.revoked = &p
	return f.failWith
}

// newSvc 造一个只接写侧仓储的 Service（只读侧 Store 用 nil——写方法不碰它）。
func newSvc(fs *fakeStaffStore) *Service {
	return NewService(nil, fs)
}

func seedRoles(fs *fakeStaffStore) {
	fs.roles[1] = RoleRefRow{ID: 1, Code: perm.RolePlatformAdmin, Name: "平台管理员", DataScope: "ALL"}
	fs.roles[2] = RoleRefRow{ID: 2, Code: perm.RoleModelOps, Name: "模型运营", DataScope: "ALL"}
	fs.roles[3] = RoleRefRow{ID: 3, Code: perm.RolePricingOp, Name: "定价运营", DataScope: "ALL"}
	fs.roles[4] = RoleRefRow{ID: 4, Code: perm.RoleSales, Name: "销售", DataScope: "SELF"}
	fs.roles[5] = RoleRefRow{ID: 5, Code: perm.RoleProcurement, Name: "采购", DataScope: "SELF"}
	fs.roles[6] = RoleRefRow{ID: 6, Code: perm.RoleSupplier, Name: "供应商", DataScope: "SELF"}
	fs.roles[7] = RoleRefRow{ID: 7, Code: perm.RoleCustomer, Name: "客户", DataScope: "SELF"}
	fs.orgs[1] = OrgUnitRefRow{ID: 1, Name: "总部"}
}

var testOp = Operator{OperatorID: 1, OperatorRole: perm.RolePlatformAdmin, RequestID: "req-1"}

// ============================================================
// 校验函数：每条分支都要有用例（批次 5 的教训）
// ============================================================

func TestValidateLoginID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"合法小写开头", "sales_op", nil},
		{"合法纯字母", "modelops", nil},
		{"合法带数字下划线", "a_1_b", nil},
		{"合法最长32位", "a" + "1234567890123456789012345678901", nil},
		{"空串", "", ErrLoginIDInvalid},
		{"太短3位", "abc", ErrLoginIDInvalid},
		{"太长33位", "a" + "12345678901234567890123456789012", ErrLoginIDInvalid},
		{"大写开头", "Sales_op", ErrLoginIDInvalid},
		{"数字开头", "1sales", ErrLoginIDInvalid},
		{"下划线开头", "_sales", ErrLoginIDInvalid},
		{"含连字符", "sales-op", ErrLoginIDInvalid},
		{"含中文", "销售", ErrLoginIDInvalid},
		{"含空格", "sales op", ErrLoginIDInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateLoginID(c.in)
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
		})
	}
}

func TestValidateMobile(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"合法", "13900001001", nil},
		{"合法第二位3", "13700000000", nil},
		{"合法第二位9", "19900000000", nil},
		{"空串", "", ErrMobileInvalid},
		{"10位", "1390000100", ErrMobileInvalid},
		{"12位", "139000010011", ErrMobileInvalid},
		{"第二位2", "12900000000", ErrMobileInvalid},
		{"非1开头", "23900000000", ErrMobileInvalid},
		{"含字母", "1390000100a", ErrMobileInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateMobile(c.in)
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"合法8位", "Test@1234", nil},
		{"合法长口令", "a-very-long-passphrase-1234567890", nil},
		{"7位太短", "Test@12", ErrPasswordTooWeak},
		{"空串", "", ErrPasswordTooWeak},
		{"中文按rune计8位合法", "中文口令测试八位", nil},
		{"中文7位太短", "中文口令测试七", ErrPasswordTooWeak},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePassword(c.in)
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
		})
	}
}

func TestValidateNameAndStatus(t *testing.T) {
	require.NoError(t, ValidateName("张三"))
	require.ErrorIs(t, ValidateName(""), ErrNameRequired)
	require.ErrorIs(t, ValidateName("   "), ErrNameRequired)

	require.NoError(t, ValidateStaffStatus("ACTIVE"))
	require.NoError(t, ValidateStaffStatus("disabled"))
	require.NoError(t, ValidateStaffStatus(" ACTIVE "))
	require.Error(t, ValidateStaffStatus("FROZEN"))
	require.Error(t, ValidateStaffStatus(""))
}

// ============================================================
// 互斥：每个互斥对 + 非互斥组合都要覆盖
// ============================================================

func TestIsMutexPair(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{perm.RoleSales, perm.RolePricingOp, true},
		{perm.RolePricingOp, perm.RoleSales, true}, // 无向
		{perm.RoleSales, perm.RoleProcurement, true},
		{perm.RoleProcurement, perm.RoleSales, true}, // 无向
		{perm.RolePricingOp, perm.RoleProcurement, false},
		{perm.RoleModelOps, perm.RolePricingOp, false},
		{perm.RoleModelOps, perm.RoleSales, false},
		{perm.RoleFinance, perm.RolePricingOp, false},
		{perm.RolePlatformAdmin, perm.RoleModelOps, false},
	}
	for _, c := range cases {
		t.Run(c.a+"|"+c.b, func(t *testing.T) {
			require.Equal(t, c.want, isMutexPair(c.a, c.b))
		})
	}
}

func TestCheckMutex(t *testing.T) {
	t.Run("候选内部互斥", func(t *testing.T) {
		err := checkMutex([]string{perm.RoleSales, perm.RolePricingOp}, nil)
		require.ErrorIs(t, err, ErrRoleMutexConflict)
	})
	t.Run("候选与已持互斥", func(t *testing.T) {
		err := checkMutex([]string{perm.RoleSales}, []string{perm.RoleProcurement})
		require.ErrorIs(t, err, ErrRoleMutexConflict)
	})
	t.Run("无冲突", func(t *testing.T) {
		err := checkMutex([]string{perm.RoleModelOps, perm.RolePricingOp}, []string{perm.RoleFinance})
		require.NoError(t, err)
	})
	t.Run("空输入合法", func(t *testing.T) {
		require.NoError(t, checkMutex(nil, nil))
	})
}

// ============================================================
// CreateStaff
// ============================================================

func TestCreateStaff_Success(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	got, err := newSvc(fs).CreateStaff(context.Background(), CreateStaffInput{
		Name: "新销售", Mobile: "13900002001", OrgUnitID: 1,
		LoginID: "new_sales", Password: "Test@1234", RoleIDs: []int64{4},
	}, testOp)
	require.NoError(t, err)
	require.Equal(t, "new_sales", got.Account.LoginID)
	require.Equal(t, "SALES", got.Roles[0].Code)
	require.Equal(t, "internal", got.Roles[0].Portal)
	require.NotNil(t, fs.created)
	require.Equal(t, "new_sales", fs.created.LoginID)
}

func TestCreateStaff_Validation(t *testing.T) {
	base := func() CreateStaffInput {
		return CreateStaffInput{
			Name: "新销售", Mobile: "13900002001", OrgUnitID: 1,
			LoginID: "new_sales", Password: "Test@1234", RoleIDs: []int64{4},
		}
	}
	cases := []struct {
		name   string
		mutate func(*CreateStaffInput)
		want   error
	}{
		{"名称为空", func(in *CreateStaffInput) { in.Name = " " }, ErrNameRequired},
		{"手机号非法", func(in *CreateStaffInput) { in.Mobile = "123" }, ErrMobileInvalid},
		{"登录名非法", func(in *CreateStaffInput) { in.LoginID = "AB" }, ErrLoginIDInvalid},
		{"口令太短", func(in *CreateStaffInput) { in.Password = "short" }, ErrPasswordTooWeak},
		{"无角色", func(in *CreateStaffInput) { in.RoleIDs = nil }, ErrNoRolesGiven},
		{"组织不存在", func(in *CreateStaffInput) { in.OrgUnitID = 99 }, ErrOrgUnitNotFound},
		{"角色不存在", func(in *CreateStaffInput) { in.RoleIDs = []int64{999} }, ErrRoleNotFound},
		{"外部门户角色", func(in *CreateStaffInput) { in.RoleIDs = []int64{6} }, ErrRoleNotInternal},
		{"客户角色", func(in *CreateStaffInput) { in.RoleIDs = []int64{7} }, ErrRoleNotInternal},
		{"角色互斥", func(in *CreateStaffInput) { in.RoleIDs = []int64{4, 3} }, ErrRoleMutexConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newFakeStaffStore()
			seedRoles(fs)
			in := base()
			c.mutate(&in)
			_, err := newSvc(fs).CreateStaff(context.Background(), in, testOp)
			require.ErrorIs(t, err, c.want)
			require.Nil(t, fs.created, "校验失败时绝不应触达仓储写")
		})
	}
}

func TestCreateStaff_DuplicateLoginAndMobile(t *testing.T) {
	t.Run("登录名占用", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.loginTaken["new_sales"] = true
		_, err := newSvc(fs).CreateStaff(context.Background(), CreateStaffInput{
			Name: "x", Mobile: "13900002001", OrgUnitID: 1,
			LoginID: "new_sales", Password: "Test@1234", RoleIDs: []int64{4},
		}, testOp)
		require.ErrorIs(t, err, ErrLoginIDTaken)
	})
	t.Run("手机号占用", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.mobTaken["13900002001"] = true
		_, err := newSvc(fs).CreateStaff(context.Background(), CreateStaffInput{
			Name: "x", Mobile: "13900002001", OrgUnitID: 1,
			LoginID: "new_sales", Password: "Test@1234", RoleIDs: []int64{4},
		}, testOp)
		require.ErrorIs(t, err, ErrMobileTaken)
	})
}

// ============================================================
// GrantRole / RevokeRole
// ============================================================

func TestGrantRole(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Name: "x", Roles: []StaffRoleRow{{ID: 1, Code: perm.RolePlatformAdmin}}}
		err := newSvc(fs).GrantRole(context.Background(), 100, GrantRoleInput{RoleIDs: []int64{2}}, testOp)
		require.NoError(t, err)
		require.Len(t, fs.granted.Roles, 1)
		require.Equal(t, perm.RoleModelOps, fs.granted.Roles[0].Code)
	})
	t.Run("重复授予已持有的角色", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 2, Code: perm.RoleModelOps}}}
		err := newSvc(fs).GrantRole(context.Background(), 100, GrantRoleInput{RoleIDs: []int64{2}}, testOp)
		require.ErrorIs(t, err, ErrRoleAlreadyGranted)
		require.Nil(t, fs.granted)
	})
	t.Run("与已持角色互斥", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 4, Code: perm.RoleSales}}}
		err := newSvc(fs).GrantRole(context.Background(), 100, GrantRoleInput{RoleIDs: []int64{3}}, testOp)
		require.ErrorIs(t, err, ErrRoleMutexConflict)
		require.Nil(t, fs.granted)
	})
	t.Run("员工不存在", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		err := newSvc(fs).GrantRole(context.Background(), 999, GrantRoleInput{RoleIDs: []int64{2}}, testOp)
		require.ErrorIs(t, err, ErrStaffNotFound)
	})
	t.Run("空角色列表", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100}
		err := newSvc(fs).GrantRole(context.Background(), 100, GrantRoleInput{}, testOp)
		require.ErrorIs(t, err, ErrNoRolesGiven)
	})
}

func TestRevokeRole(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{
			{ID: 1, Code: perm.RolePlatformAdmin}, {ID: 2, Code: perm.RoleModelOps},
		}}
		fs.adminCount = 2
		err := newSvc(fs).RevokeRole(context.Background(), 100, 2, testOp)
		require.NoError(t, err)
		require.Equal(t, int64(2), fs.revoked.RoleID)
		require.Equal(t, perm.RoleModelOps, fs.revoked.RoleCode)
	})
	t.Run("角色未授予", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 2, Code: perm.RoleModelOps}}}
		err := newSvc(fs).RevokeRole(context.Background(), 100, 3, testOp)
		require.ErrorIs(t, err, ErrRoleNotGranted)
		require.Nil(t, fs.revoked)
	})
	t.Run("收掉唯一平台管理员被拦", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 1, Code: perm.RolePlatformAdmin}}}
		fs.adminCount = 1
		err := newSvc(fs).RevokeRole(context.Background(), 100, 1, testOp)
		require.ErrorIs(t, err, ErrLastPlatformAdmin)
		require.Nil(t, fs.revoked, "被拦时绝不应触达仓储")
	})
	t.Run("平台管理员有2人时可收", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 1, Code: perm.RolePlatformAdmin}}}
		fs.adminCount = 2
		err := newSvc(fs).RevokeRole(context.Background(), 100, 1, testOp)
		require.NoError(t, err)
	})
	t.Run("非管理员角色不受最后一名保护影响", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, Roles: []StaffRoleRow{{ID: 2, Code: perm.RoleModelOps}}}
		fs.adminCount = 1 // 全局只有 1 个管理员，但收的不是他的管理员角色
		err := newSvc(fs).RevokeRole(context.Background(), 100, 2, testOp)
		require.NoError(t, err)
	})
}

// ============================================================
// UpdateStaff / ResetPassword
// ============================================================

func TestUpdateStaff_DisableLastAdminBlocked(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	fs.staff[100] = &StaffRow{
		ID: 100, Name: "管理员", Mobile: "13900002001", OrgUnitID: 1,
		Status: StaffStatusActive, LoginID: "the_admin",
		Roles: []StaffRoleRow{{ID: 1, Code: perm.RolePlatformAdmin}},
	}
	fs.adminCount = 1
	_, err := newSvc(fs).UpdateStaff(context.Background(), 100, UpdateStaffInput{
		Name: "管理员", Mobile: "13900002001", OrgUnitID: 1, Status: StaffStatusDisabled,
	}, testOp)
	require.ErrorIs(t, err, ErrLastPlatformAdmin)
	require.Nil(t, fs.updated, "被拦时绝不应触达仓储")
}

func TestUpdateStaff_MobileUniquenessChecked(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	fs.staff[100] = &StaffRow{ID: 100, Name: "x", Mobile: "13900002001", OrgUnitID: 1, Status: StaffStatusActive}
	fs.mobTaken["13900002999"] = true
	_, err := newSvc(fs).UpdateStaff(context.Background(), 100, UpdateStaffInput{
		Name: "x", Mobile: "13900002999", OrgUnitID: 1, Status: StaffStatusActive,
	}, testOp)
	require.ErrorIs(t, err, ErrMobileTaken)
}

func TestUpdateStaff_MobileUnchangedSkipsCheck(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	fs.staff[100] = &StaffRow{ID: 100, Name: "x", Mobile: "13900002001", OrgUnitID: 1, Status: StaffStatusActive}
	fs.mobTaken["13900002001"] = true // 自己的号也标为占用，但值未变不应报错
	_, err := newSvc(fs).UpdateStaff(context.Background(), 100, UpdateStaffInput{
		Name: "y", Mobile: "13900002001", OrgUnitID: 1, Status: StaffStatusActive,
	}, testOp)
	require.NoError(t, err)
	require.NotNil(t, fs.updated)
}

func TestUpdateStaff_NotFound(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	_, err := newSvc(fs).UpdateStaff(context.Background(), 999, UpdateStaffInput{
		Name: "x", Mobile: "13900002001", OrgUnitID: 1, Status: StaffStatusActive,
	}, testOp)
	require.ErrorIs(t, err, ErrStaffNotFound)
}

func TestResetPassword(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, LoginID: "someone"}
		require.NoError(t, newSvc(fs).ResetPassword(context.Background(), 100,
			ResetPasswordInput{Password: "NewPass@123"}, testOp))
		require.Equal(t, "NewPass@123", fs.reset.Password)
	})
	t.Run("口令太短", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		fs.staff[100] = &StaffRow{ID: 100, LoginID: "someone"}
		err := newSvc(fs).ResetPassword(context.Background(), 100,
			ResetPasswordInput{Password: "short"}, testOp)
		require.ErrorIs(t, err, ErrPasswordTooWeak)
		require.Nil(t, fs.reset)
	})
	t.Run("员工不存在", func(t *testing.T) {
		fs := newFakeStaffStore()
		seedRoles(fs)
		err := newSvc(fs).ResetPassword(context.Background(), 999,
			ResetPasswordInput{Password: "NewPass@123"}, testOp)
		require.ErrorIs(t, err, ErrStaffNotFound)
	})
}

// TestGetStaff_NotFound GetStaff 必须把「仓储返回 nil」翻成 ErrStaffNotFound，
// 不能让 handler 收到 nil,nil 然后空指针。
func TestGetStaff_NotFound(t *testing.T) {
	fs := newFakeStaffStore()
	_, err := newSvc(fs).GetStaff(context.Background(), 999)
	require.ErrorIs(t, err, ErrStaffNotFound)
}

// TestStaffStoreNotInjected 未注入写侧仓储时必须返回错误而不是 panic。
// router 用 deps.StaffStore != nil 守住了不挂载，这里是第二道防线。
func TestStaffStoreNotInjected(t *testing.T) {
	svc := NewService(nil, nil)
	_, err := svc.staffStoreOrErr()
	require.Error(t, err)
	require.Contains(t, err.Error(), "StaffStore 未注入")
}

// TestToStaffSummary_RolesNeverNil DTO 的 roles 必须恒为非 null 数组
// （前端直接取 .length，null 会炸）。
func TestToStaffSummary_RolesNeverNil(t *testing.T) {
	got := toStaffSummary(&StaffRow{ID: 1, Name: "x", OrgUnitID: 1})
	require.NotNil(t, got.Roles)
	require.Len(t, got.Roles, 0)
	require.Nil(t, got.Account) // 无账号时为 null
}

// TestLoadInternalRoles_RejectsExternal 外部门户角色一律拒绝，且错误优先于其它校验。
func TestLoadInternalRoles_RejectsExternal(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	_, err := newSvc(fs).loadInternalRoles(context.Background(), []int64{2, 6})
	require.ErrorIs(t, err, ErrRoleNotInternal)

	_, err = newSvc(fs).loadInternalRoles(context.Background(), []int64{0})
	require.ErrorIs(t, err, ErrRoleNotFound)

	_, err = newSvc(fs).loadInternalRoles(context.Background(), []int64{999})
	require.ErrorIs(t, err, ErrRoleNotFound)
}

// TestCreateStaff_StoreErrorPropagates 仓储错误必须上抛，不能被吞成空结果。
func TestCreateStaff_StoreErrorPropagates(t *testing.T) {
	fs := newFakeStaffStore()
	seedRoles(fs)
	fs.failWith = errors.New("db down")
	_, err := newSvc(fs).CreateStaff(context.Background(), CreateStaffInput{
		Name: "x", Mobile: "13900002001", OrgUnitID: 1,
		LoginID: "new_sales", Password: "Test@1234", RoleIDs: []int64{4},
	}, testOp)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStaffNotFound)
}
