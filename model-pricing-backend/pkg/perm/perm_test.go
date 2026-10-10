package perm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePermStore struct {
	points []string
	err    error
}

func (f fakePermStore) AllPoints() ([]string, error) {
	return f.points, f.err
}

func TestValidateInDB_Consistent(t *testing.T) {
	store := fakePermStore{points: All()}
	require.NoError(t, ValidateInDB(store))
}

func TestValidateInDB_MissingCodeConstant(t *testing.T) {
	// DB 缺少代码中的一个常量 → 应报错并指出缺少项
	points := All()
	store := fakePermStore{points: points[1:]} // 去掉第一个 M1:V
	err := ValidateInDB(store)
	require.Error(t, err)
	require.Contains(t, err.Error(), "M1:V")
}

func TestValidateInDB_UnknownDBPoint(t *testing.T) {
	// DB 多出一个代码未声明的权限点 → 应报错并指出多余项
	points := append(All(), "M99:X")
	store := fakePermStore{points: points}
	err := ValidateInDB(store)
	require.Error(t, err)
	require.Contains(t, err.Error(), "M99:X")
}

func TestValidateInDB_StoreError(t *testing.T) {
	store := fakePermStore{err: errBoom}
	err := ValidateInDB(store)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load permission points")
}

var errBoom = fakeError("boom")

type fakeError string

func (e fakeError) Error() string { return string(e) }

// ---- 6b-1：CanEditCostParam / CanLockPrimary / CanLockFx 按角色 code 收敛 ----
//
// 背景（06-cost §11-1）：M5:E 权限点在 000007_seed 里被 6 个角色持有，
// 但 MODEL_OPS / RETRO_OP 是历史越权（补录专员能改成本参数就是越权）。
// 必须锁在 pkg/perm 一处，并用表驱动测试把允许/禁止集合钉死。

// allSeedRoles 列出 role 表里的全部 11 个角色 code（用于穷举断言，防漏判）。
// 批次 3a 补齐了此前漏掉的 OPS_ADMIN / AUDIT_READONLY——穷举不全等于漏判。
var allSeedRoles = []string{
	RolePlatformAdmin,
	RoleModelOps,
	RoleProcurement,
	RolePricingOp,
	RoleSales,
	RoleFinance,
	RoleOpsAdmin,
	RoleRetroOp,
	RoleAuditReadOnly,
	RoleSupplier,
	RoleCustomer,
}

func TestCanEditCostParam(t *testing.T) {
	for _, role := range allSeedRoles {
		got := CanEditCostParam(role)
		want := role == RolePricingOp
		require.Equalf(t, want, got, "CanEditCostParam(%s)", role)
	}
	// 显式断言越权角色（红线条款）
	require.False(t, CanEditCostParam(RoleModelOps), "MODEL_OPS 不能改成本参数（越权持有 M5:E）")
	require.False(t, CanEditCostParam(RoleRetroOp), "RETRO_OP 不能改成本参数（越权持有 M5:E）")
	require.False(t, CanEditCostParam(""), "空 role 必须拒绝")
}

func TestCanLockPrimary(t *testing.T) {
	for _, role := range allSeedRoles {
		got := CanLockPrimary(role)
		want := role == RoleProcurement
		require.Equalf(t, want, got, "CanLockPrimary(%s)", role)
	}
	require.False(t, CanLockPrimary(RoleModelOps), "MODEL_OPS 不能锁主供应商")
	require.False(t, CanLockPrimary(RoleRetroOp), "RETRO_OP 不能锁主供应商")
	require.False(t, CanLockPrimary(""), "空 role 必须拒绝")
}

func TestCanLockFx(t *testing.T) {
	for _, role := range allSeedRoles {
		got := CanLockFx(role)
		want := role == RoleFinance
		require.Equalf(t, want, got, "CanLockFx(%s)", role)
	}
	require.False(t, CanLockFx(RoleModelOps), "MODEL_OPS 不能锁汇率")
	require.False(t, CanLockFx(RoleRetroOp), "RETRO_OP 不能锁汇率")
	require.False(t, CanLockFx(""), "空 role 必须拒绝")
}

// ---- 批次 3a：成本域三个动作的角色矩阵 ----
//
// 背景：三个动作（成本参数配置 / 主供应商锁定 / 汇率锁定）曾全部挤在 M5:E 一个权限点上，
// 而 M5:E 被 6 个角色共享——权限点层面完全无法区分谁该做哪个动作。
// 000030_split_m5_permission 已按动作把权限点拆到 M5:C / M5:E / M10:E，
// 但权限点只解决「模块 × 操作码」维度，**角色维度仍由本包三个 Can* 函数收敛**。
//
// 下面三个测试把「角色 → 允许哪些成本动作」钉死：
//   1. 每个动作恰好允许一个角色（单一职责，不多不少）
//   2. 每个角色最多允许一个成本动作（互斥，防一个角色通吃）
//   3. 多角色账号走 AnyRoleCan 后的实际放行结果（对应批次 3a 验收口径）
// 若有人把某个 Can* 放宽成多角色（如给 CanEditCostParam 加上 MODEL_OPS），此处立刻红。

// costActions 成本域三个动作，perm 字段记录批次 3a 后该动作挂载的权限点
// （router.go 里 RequirePerm 用的就是这三个常量，不是新写的字面值）。
var costActions = []struct {
	name string
	perm string
	can  func(string) bool
}{
	{"成本参数配置", M5Config, CanEditCostParam},
	{"主供应商锁定", M5Edit, CanLockPrimary},
	{"汇率锁定", M10Edit, CanLockFx},
}

func TestCostActions_ExactlyOneRoleEach(t *testing.T) {
	wantOwner := map[string]string{
		"成本参数配置": RolePricingOp,
		"主供应商锁定": RoleProcurement,
		"汇率锁定":   RoleFinance,
	}
	for _, a := range costActions {
		var allowed []string
		for _, role := range allSeedRoles {
			if a.can(role) {
				allowed = append(allowed, role)
			}
		}
		require.Equalf(t, []string{wantOwner[a.name]}, allowed,
			"动作「%s」（权限点 %s）的允许角色集合必须恰好是 [%s]，多一个少一个都是越权/漏权",
			a.name, a.perm, wantOwner[a.name])
	}
}

func TestCostActions_MutuallyExclusivePerRole(t *testing.T) {
	for _, role := range allSeedRoles {
		var granted []string
		for _, a := range costActions {
			if a.can(role) {
				granted = append(granted, a.name)
			}
		}
		require.LessOrEqualf(t, len(granted), 2,
			"角色 %s 同时被允许 %d 个成本动作 %v——职责分离要求一个角色最多一个", role, len(granted), granted)
	}
}

func TestCostActions_MultiRoleOperator(t *testing.T) {
	cases := []struct {
		name            string
		roles           []string
		wantParam       bool // 成本参数配置
		wantLockPrimary bool // 主供应商锁定
		wantLockFx      bool // 汇率锁定
	}{
		// 批次 3a 验收口径：smoke_admin 持 PRICING_OP → 能改参数，但锁不了主供应商
		{"smoke_admin 真值形状", []string{RolePlatformAdmin, RoleModelOps, RolePricingOp}, true, false, false},
		// 纯平台管理员：全权限角色也持 M5:C / M5:E，但角色不符，服务层必须拦住
		{"纯 PLATFORM_ADMIN", []string{RolePlatformAdmin}, false, false, false},
		// 历史越权组合（000030 已从这两个角色收回 M5:E）：三个动作全禁
		{"MODEL_OPS + RETRO_OP", []string{RoleModelOps, RoleRetroOp}, false, false, false},
		// buyer_a：采购只能锁主供应商，改不了成本参数
		{"buyer_a 采购", []string{RoleProcurement}, false, true, false},
		{"财务：只能锁汇率", []string{RoleFinance}, false, false, true},
		// PRICING_OP × PROCUREMENT 有 role_mutex，但若真被同授，AnyRoleCan 会各放行各的
		{"PRICING_OP + PROCUREMENT", []string{RolePricingOp, RoleProcurement}, true, true, false},
		{"空角色", nil, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantParam, AnyRoleCan(tc.roles, CanEditCostParam), "成本参数配置")
			require.Equal(t, tc.wantLockPrimary, AnyRoleCan(tc.roles, CanLockPrimary), "主供应商锁定")
			require.Equal(t, tc.wantLockFx, AnyRoleCan(tc.roles, CanLockFx), "汇率锁定")
		})
	}
}

func TestCanReadSupplierCommercial(t *testing.T) {
	cases := []struct {
		name    string
		roles   []string
		staffID int64
		ownerID int64
		want    bool
	}{
		{"归属采购", []string{RoleProcurement}, 7, 7, true},
		{"归属员工无需角色特例", []string{RoleSales}, 7, 7, true},
		{"财务在第二角色", []string{RoleSales, RoleFinance}, 8, 7, true},
		{"非归属采购", []string{RoleProcurement}, 8, 7, false},
		{"管理员不自动放行", []string{RolePlatformAdmin}, 8, 7, false},
		{"空角色", nil, 8, 7, false},
		{"空 ID 不算归属", nil, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CanReadSupplierCommercial(tc.roles, tc.staffID, tc.ownerID))
		})
	}
}

func TestAnyRoleCan(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		want  bool
	}{
		{"smoke_admin 真值形状：多角色命中一个即放行", []string{RolePlatformAdmin, RoleModelOps, RolePricingOp}, true},
		{"只有越权角色：拒绝", []string{RoleModelOps, RoleRetroOp}, false},
		{"空列表：拒绝", nil, false},
		{"全部角色都不命中：拒绝", []string{RoleSales, RoleFinance, RoleProcurement}, false},
		{"单角色命中：放行", []string{RolePricingOp}, true},
		{"空串角色：拒绝", []string{""}, false},
		{"命中不在首位也要看到", []string{RoleSales, RolePricingOp}, true},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, AnyRoleCan(tc.roles, CanEditCostParam),
			"AnyRoleCan(%v)", tc.roles)
	}
	// single 为 nil 绝不调用（防御：不能把 nil 函数当判定器）
	require.False(t, AnyRoleCan([]string{RolePricingOp}, nil))
}
