// Package orgperm 的 staff.go：M11 组织与权限的**账号管理写侧**（2026-10-13 补）。
//
// 背景：本包原为全只读（批次 5），导致平台**没有任何账号管理入口**——
// 连 PLATFORM_ADMIN 都无法在界面上建人、授权、改口令，只能手工插库。
// 本文件补齐写侧，与只读侧共用同一个 Service。
//
// 硬口径：
//  1. **不做行级数据域过滤**。账号管理是全系统管理职能，没有「谁的员工」这种归属概念，
//     授权只靠路由级 RequirePerm(M11:E) + 服务层角色兜底（见 Service 方法注释）。
//  2. **单事务**。建人 = internal_staff + account + role_grant + audit_log 四连写，
//     事务边界在 repo（红线 8：与业务写同事务）。
//  3. **role_mutex 必须拦**。库里 SALES↔PRICING_OP、SALES↔PROCUREMENT 互斥，
//     授权前查全量已授角色做两两比对，不允许半途提交。
//  4. **不许把最后一个 PLATFORM_ADMIN 收掉/停用**。否则平台无人可管理，属不可逆事故。
//  5. **外部门户角色（SUPPLIER / CUSTOMER）不允许在这里授**——那两个角色的 owner
//     是 subject_operator 而不是 internal_staff，必须走供应商/客户自己的入驻流程。
package orgperm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"model_bss/pkg/perm"
)

// ============================================================
// 错误（全部由 handler 映射成 HTTP 码，见 org_permissions_staff.go）
// ============================================================

var (
	// ErrStaffNotFound 员工不存在（含已软删/查无此 id）。
	ErrStaffNotFound = errors.New("员工不存在")
	// ErrOrgUnitNotFound 组织节点不存在。
	ErrOrgUnitNotFound = errors.New("组织节点不存在")
	// ErrLoginIDInvalid 登录名格式非法。
	ErrLoginIDInvalid = errors.New("登录名必须为 4-32 位小写字母、数字或下划线，且以小写字母开头")
	// ErrLoginIDTaken 登录名在 INTERNAL 门户已被占用。
	ErrLoginIDTaken = errors.New("登录名已被占用")
	// ErrMobileInvalid 手机号格式非法。
	ErrMobileInvalid = errors.New("手机号必须为 11 位且以 1 开头")
	// ErrMobileTaken 手机号已被占用（internal_staff.mobile 有唯一约束）。
	ErrMobileTaken = errors.New("手机号已被占用")
	// ErrPasswordTooWeak 口令长度不足。
	ErrPasswordTooWeak = errors.New("口令长度必须在 8-64 个字符之间")
	// ErrNameRequired 姓名必填。
	ErrNameRequired = errors.New("姓名不能为空")
	// ErrRoleNotFound 角色不存在。
	ErrRoleNotFound = errors.New("角色不存在")
	// ErrRoleNotInternal 试图授予外部门户角色。
	ErrRoleNotInternal = errors.New("供应商/客户角色必须走各自入驻流程，不能在此授予")
	// ErrRoleMutexConflict 与已授角色互斥。
	ErrRoleMutexConflict = errors.New("角色与已授予的角色互斥，不能同时持有")
	// ErrRoleAlreadyGranted 角色已授予。
	ErrRoleAlreadyGranted = errors.New("该角色已授予，无需重复操作")
	// ErrRoleNotGranted 角色未授予。
	ErrRoleNotGranted = errors.New("该角色未授予，无法回收")
	// ErrLastPlatformAdmin 试图收掉/停用最后一个平台管理员。
	ErrLastPlatformAdmin = errors.New("不允许回收最后一个平台管理员角色或停用唯一平台管理员")
	// ErrNoRolesGiven 建人时未授予任何角色。
	ErrNoRolesGiven = errors.New("新建员工必须至少授予一个角色")
)

// 口径常量（避免裸字符串在多处漂移）。
const (
	// PortalInternalAccount account.portal_type 的内部门户取值（大写，见库内既有数据）。
	PortalInternalAccount = "INTERNAL"
	// OwnerTypeStaff account.owner_type 的员工取值。
	OwnerTypeStaff = "STAFF"
	// RolePlatformAdmin 平台管理员角色 code——最后一名保护的对象。
	RolePlatformAdmin = "PLATFORM_ADMIN"
	// StaffStatusActive / StaffStatusDisabled internal_staff.status 取值。
	StaffStatusActive   = "ACTIVE"
	StaffStatusDisabled = "DISABLED"
	// PasswordMinLen / PasswordMaxLen 口令长度边界（按 rune 计）。
	PasswordMinLen = 8
	PasswordMaxLen = 64
	// LoginIDMinLen / LoginIDMaxLen 登录名长度边界。
	LoginIDMinLen = 4
	LoginIDMaxLen = 32
)

var (
	loginIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{3,31}$`)
	mobileRe  = regexp.MustCompile(`^1[3-9]\d{9}$`)
)

// ============================================================
// DTO（契约以内部调用方为准，字段名 camelCase 与其它模块一致）
// ============================================================

// StaffRoleRef 是员工持有的一个角色（精简视图，不给全权限点明细）。
type StaffRoleRef struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	DataScope string `json:"dataScope"`
	Portal    string `json:"portal"` // 由 role.code 推导，见 service.portalOf
}

// StaffAccountRef 是员工的账号信息。
//
// ⚠️ **绝不返回 password_hash / token**——响应里只暴露登录名与状态。
type StaffAccountRef struct {
	LoginID string `json:"loginId"`
	Status  string `json:"status"`
}

// StaffSummary 员工列表行。
type StaffSummary struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Mobile    string           `json:"mobile"`
	OrgUnitID string           `json:"orgUnitId"`
	OrgName   string           `json:"orgName"`
	Status    string           `json:"status"`
	Account   *StaffAccountRef `json:"account"` // 无账号时为 null（理论不出现）
	Roles     []StaffRoleRef   `json:"roles"`   // 恒非 null
}

// StaffDetail 员工详情（比列表行多 roles，结构一致，留作扩展点）。
type StaffDetail struct {
	StaffSummary
}

// StaffPage 分页结果。
type StaffPage struct {
	List  []StaffSummary `json:"list"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

// StaffQuery 员工列表查询。
type StaffQuery struct {
	Keyword string // 命中 name / mobile / login_id（模糊）
	Status  string // ACTIVE / DISABLED，空=全部
	Page    int
	Size    int
}

// CreateStaffInput 新建员工（人 + 账号 + 角色一体）。
type CreateStaffInput struct {
	Name      string  `json:"name"`
	Mobile    string  `json:"mobile"`
	OrgUnitID int64   `json:"org_unit_id"`
	LoginID   string  `json:"login_id"`
	Password  string  `json:"password"`
	RoleIDs   []int64 `json:"role_ids"` // 至少 1 个
}

// UpdateStaffInput 修改员工（不含账号与角色——那两条有专门接口）。
type UpdateStaffInput struct {
	Name      string `json:"name"`
	Mobile    string `json:"mobile"`
	OrgUnitID int64  `json:"org_unit_id"`
	Status    string `json:"status"`
}

// ResetPasswordInput 重置口令。
type ResetPasswordInput struct {
	Password string `json:"password"`
}

// GrantRoleInput 授权角色。
type GrantRoleInput struct {
	RoleIDs []int64 `json:"role_ids"`
}

// Operator 是发起写操作的人（只用于审计落库，**绝不参与权限判定**——
// 权限判定在中间件 RequirePerm 与服务层角色兜底，不在这里）。
type Operator struct {
	OperatorID   int64
	OperatorRole string
	RequestID    string
}

// ============================================================
// 校验（纯函数，便于单测覆盖每条分支）
// ============================================================

// ValidateLoginID 登录名：4-32 位，小写字母/数字/下划线，小写字母开头。
// 不允许多语言的空白与中文——login_id 会进 URL 与登录态，宁严勿宽。
func ValidateLoginID(s string) error {
	t := strings.TrimSpace(s)
	if len(t) < LoginIDMinLen || len(t) > LoginIDMaxLen || !loginIDRe.MatchString(t) {
		return ErrLoginIDInvalid
	}
	return nil
}

// ValidateMobile 手机号：11 位、1 开头、第二位 3-9。
func ValidateMobile(s string) error {
	t := strings.TrimSpace(s)
	if !mobileRe.MatchString(t) {
		return ErrMobileInvalid
	}
	return nil
}

// ValidatePassword 口令长度 8-64（按 rune 计，避免中文/emoji 被按字节截断的歧义）。
// 不做强制度规则（大小写/数字/符号）——那是产品策略，不是合法性边界。
func ValidatePassword(s string) error {
	n := utf8.RuneCountInString(s)
	if n < PasswordMinLen || n > PasswordMaxLen {
		return ErrPasswordTooWeak
	}
	return nil
}

// ValidateName 姓名非空（去空白后）。
func ValidateName(s string) error {
	if strings.TrimSpace(s) == "" {
		return ErrNameRequired
	}
	return nil
}

// ValidateStaffStatus 员工状态取值。
func ValidateStaffStatus(s string) error {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case StaffStatusActive, StaffStatusDisabled:
		return nil
	default:
		return fmt.Errorf("员工状态必须是 %s 或 %s", StaffStatusActive, StaffStatusDisabled)
	}
}

// ============================================================
// 服务
// ============================================================

// StaffStore 是账号管理写侧需要的全部数据访问。
// 与只读侧 Store 分开：避免为了加写方法而改动批次 5 的只读接口与既有测试。
//
// Service 结构体与 NewService 构造在 service.go——写侧与只读侧共用同一个 Service，
// staffStore 是 NewService 的必填第二参（漏传编译不过，见 service.go 注释）。
type StaffStore interface {
	// ListStaff 分页列员工（含账号与角色，一次查完不 N+1）。
	ListStaff(ctx context.Context, q StaffQuery) ([]StaffRow, int64, error)
	// FindStaff 按 id 取单个员工（含账号与角色）。
	FindStaff(ctx context.Context, id int64) (*StaffRow, error)
	// ExistsLoginID INTERNAL 门户下登录名是否已存在。
	ExistsLoginID(ctx context.Context, loginID string) (bool, error)
	// ExistsMobile 手机号是否已被占用。
	ExistsMobile(ctx context.Context, mobile string) (bool, error)
	// FindOrgUnit 组织节点（不存在返回 nil）。
	FindOrgUnit(ctx context.Context, id int64) (*OrgUnitRefRow, error)
	// FindRolesByID 批量取角色（不存在的 id 不出现在结果里）。
	FindRolesByID(ctx context.Context, ids []int64) ([]RoleRefRow, error)
	// CountActivePlatformAdmins 未过期授权中持 PLATFORM_ADMIN 的**在职**员工数。
	CountActivePlatformAdmins(ctx context.Context) (int64, error)
	// StaffHasRole 员工是否已持某角色（未过期授权）。
	StaffHasRole(ctx context.Context, staffID, roleID int64) (bool, error)
	// CreateStaffTx 单事务四连写：staff + account + grants + audit。
	CreateStaffTx(ctx context.Context, p CreateStaffTxParams) (*StaffRow, error)
	// UpdateStaffTx 单事务：改 staff + audit。
	UpdateStaffTx(ctx context.Context, p UpdateStaffTxParams) error
	// ResetPasswordTx 单事务：改 account.password_hash + audit。
	ResetPasswordTx(ctx context.Context, p ResetPasswordTxParams) error
	// GrantRolesTx 单事务：逐角色授权 + audit。
	GrantRolesTx(ctx context.Context, p GrantRolesTxParams) error
	// RevokeRoleTx 单事务：回收授权 + audit。
	RevokeRoleTx(ctx context.Context, p RevokeRoleTxParams) error
}

// ListStaff 员工列表。
//
// 不做数据域过滤（账号管理是全系统职能，无归属概念）；授权只靠 M11:E。
func (s *Service) ListStaff(ctx context.Context, q StaffQuery) (*StaffPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 {
		q.Size = 20
	}
	if q.Size > 200 {
		q.Size = 200
	}
	rows, total, err := s.staffStore.ListStaff(ctx, q)
	if err != nil {
		return nil, err
	}
	list := make([]StaffSummary, 0, len(rows))
	for i := range rows {
		list = append(list, toStaffSummary(&rows[i]))
	}
	return &StaffPage{List: list, Total: total, Page: q.Page, Size: q.Size}, nil
}

// GetStaff 员工详情。
func (s *Service) GetStaff(ctx context.Context, id int64) (*StaffDetail, error) {
	row, err := s.staffStore.FindStaff(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrStaffNotFound
	}
	return &StaffDetail{StaffSummary: toStaffSummary(row)}, nil
}

// CreateStaff 新建员工（人 + 账号 + 角色 + 审计，单事务）。
//
// 服务层二次鉴权由 handler 做（perm.AnyRoleCan 收窄到 PLATFORM_ADMIN），
// 这里只做业务规则校验。
func (s *Service) CreateStaff(ctx context.Context, in CreateStaffInput, op Operator) (*StaffDetail, error) {
	if err := ValidateName(in.Name); err != nil {
		return nil, err
	}
	if err := ValidateMobile(in.Mobile); err != nil {
		return nil, err
	}
	if err := ValidateLoginID(in.LoginID); err != nil {
		return nil, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	if len(in.RoleIDs) == 0 {
		return nil, ErrNoRolesGiven
	}
	if in.OrgUnitID <= 0 {
		return nil, ErrOrgUnitNotFound
	}

	org, err := s.staffStore.FindOrgUnit(ctx, in.OrgUnitID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrOrgUnitNotFound
	}

	taken, err := s.staffStore.ExistsLoginID(ctx, strings.TrimSpace(in.LoginID))
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrLoginIDTaken
	}
	taken, err = s.staffStore.ExistsMobile(ctx, strings.TrimSpace(in.Mobile))
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrMobileTaken
	}

	roles, err := s.loadInternalRoles(ctx, in.RoleIDs)
	if err != nil {
		return nil, err
	}
	if err := checkMutex(roleCodes(roles), nil); err != nil {
		return nil, err
	}

	row, err := s.staffStore.CreateStaffTx(ctx, CreateStaffTxParams{
		Name:     strings.TrimSpace(in.Name),
		Mobile:   strings.TrimSpace(in.Mobile),
		OrgID:    in.OrgUnitID,
		OrgName:  org.Name,
		LoginID:  strings.TrimSpace(in.LoginID),
		Password: in.Password,
		Roles:    roles,
		Operator: op,
	})
	if err != nil {
		return nil, err
	}
	return &StaffDetail{StaffSummary: toStaffSummary(row)}, nil
}

// UpdateStaff 修改员工基础信息与状态。
//
// 停用保护：若该员工是**唯一**的持 PLATFORM_ADMIN 在职员工，不允许置为 DISABLED。
func (s *Service) UpdateStaff(ctx context.Context, id int64, in UpdateStaffInput, op Operator) (*StaffDetail, error) {
	if err := ValidateName(in.Name); err != nil {
		return nil, err
	}
	if err := ValidateMobile(in.Mobile); err != nil {
		return nil, err
	}
	if err := ValidateStaffStatus(in.Status); err != nil {
		return nil, err
	}
	if in.OrgUnitID <= 0 {
		return nil, ErrOrgUnitNotFound
	}

	cur, err := s.staffStore.FindStaff(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrStaffNotFound
	}

	org, err := s.staffStore.FindOrgUnit(ctx, in.OrgUnitID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrOrgUnitNotFound
	}

	mobile := strings.TrimSpace(in.Mobile)
	if mobile != cur.Mobile {
		taken, err := s.staffStore.ExistsMobile(ctx, mobile)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrMobileTaken
		}
	}

	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == StaffStatusDisabled && cur.Status != StaffStatusDisabled {
		if err := s.ensureNotLastAdmin(ctx, cur); err != nil {
			return nil, err
		}
	}

	if err := s.staffStore.UpdateStaffTx(ctx, UpdateStaffTxParams{
		StaffID:  id,
		Name:     strings.TrimSpace(in.Name),
		Mobile:   mobile,
		OrgID:    in.OrgUnitID,
		Status:   status,
		Before:   staffBeforeValue(cur),
		Operator: op,
	}); err != nil {
		return nil, err
	}
	return s.GetStaff(ctx, id)
}

// ResetPassword 重置员工账号口令。
//
// 不校验原口令——这是管理侧重置，不是用户改密；原口令不该到达服务端。
func (s *Service) ResetPassword(ctx context.Context, id int64, in ResetPasswordInput, op Operator) error {
	if err := ValidatePassword(in.Password); err != nil {
		return err
	}
	cur, err := s.staffStore.FindStaff(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return ErrStaffNotFound
	}
	if cur.LoginID == "" {
		// 没有 INTERNAL 账号的员工（理论上不出现）不给重置口令
		return ErrStaffNotFound
	}
	return s.staffStore.ResetPasswordTx(ctx, ResetPasswordTxParams{
		StaffID:  id,
		Password: in.Password,
		Operator: op,
	})
}

// GrantRole 给员工追加角色（可一次多个）。
func (s *Service) GrantRole(ctx context.Context, id int64, in GrantRoleInput, op Operator) error {
	if len(in.RoleIDs) == 0 {
		return ErrNoRolesGiven
	}
	cur, err := s.staffStore.FindStaff(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return ErrStaffNotFound
	}

	roles, err := s.loadInternalRoles(ctx, in.RoleIDs)
	if err != nil {
		return err
	}
	// 已持有的角色报「已授予」——调用方传重复 id 属于用法错误，不该静默跳过。
	for _, want := range roles {
		for _, have := range cur.Roles {
			if have.ID == want.ID {
				return ErrRoleAlreadyGranted
			}
		}
	}
	// 与已持角色做互斥比对
	heldCodes := make([]string, 0, len(cur.Roles))
	for _, h := range cur.Roles {
		heldCodes = append(heldCodes, h.Code)
	}
	if err := checkMutex(roleCodes(roles), heldCodes); err != nil {
		return err
	}

	return s.staffStore.GrantRolesTx(ctx, GrantRolesTxParams{
		StaffID:  id,
		Roles:    roles,
		Operator: op,
	})
}

// RevokeRole 回收员工的一个角色。
//
// 保护：不允许收掉最后一个 PLATFORM_ADMIN 授权。
func (s *Service) RevokeRole(ctx context.Context, id, roleID int64, op Operator) error {
	cur, err := s.staffStore.FindStaff(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return ErrStaffNotFound
	}

	var target *StaffRoleRow
	for i := range cur.Roles {
		if cur.Roles[i].ID == roleID {
			target = &cur.Roles[i]
			break
		}
	}
	if target == nil {
		return ErrRoleNotGranted
	}
	if target.Code == RolePlatformAdmin {
		if err := s.ensureNotLastAdmin(ctx, cur); err != nil {
			return err
		}
	}

	return s.staffStore.RevokeRoleTx(ctx, RevokeRoleTxParams{
		StaffID:  id,
		RoleID:   roleID,
		RoleCode: target.Code,
		Operator: op,
	})
}

// ============================================================
// 内部工具
// ============================================================

// loadInternalRoles 按 id 批量取角色，并拒绝外部门户角色与不存在的 id。
func (s *Service) loadInternalRoles(ctx context.Context, ids []int64) ([]RoleRefRow, error) {
	seen := make(map[int64]struct{}, len(ids))
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrRoleNotFound
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}

	rows, err := s.staffStore.FindRolesByID(ctx, uniq)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(uniq) {
		// 有 id 查不到——调用方传了不存在的角色
		return nil, ErrRoleNotFound
	}
	for _, r := range rows {
		if portalOf(r.Code) != PortalInternal {
			return nil, ErrRoleNotInternal
		}
	}
	// 稳定顺序，便于审计与测试断言
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

// checkMutex 校验候选角色与已持角色之间无互斥。
//
// 互斥表来自 role_mutex（当前库内互斥对：SALES↔PRICING_OP、SALES↔PROCUREMENT）。
// 互斥是**无向**的，两边都要比。
func checkMutex(candidateCodes, heldCodes []string) error {
	for _, c := range candidateCodes {
		for _, other := range candidateCodes {
			if c == other {
				continue
			}
			if isMutexPair(c, other) {
				return ErrRoleMutexConflict
			}
		}
		for _, held := range heldCodes {
			if isMutexPair(c, held) {
				return ErrRoleMutexConflict
			}
		}
	}
	return nil
}

// isMutexPair 判断两个角色 code 是否互斥。
// 与 000007_seed 的 role_mutex 种子保持同步；新增互斥对必须同步此处并补单测。
func isMutexPair(a, b string) bool {
	pairs := [][2]string{
		{perm.RoleSales, perm.RolePricingOp},
		{perm.RoleSales, perm.RoleProcurement},
	}
	for _, p := range pairs {
		if (a == p[0] && b == p[1]) || (a == p[1] && b == p[0]) {
			return true
		}
	}
	return false
}

// ensureNotLastAdmin 确认该员工不是最后一个在职持 PLATFORM_ADMIN 的人。
func (s *Service) ensureNotLastAdmin(ctx context.Context, row *StaffRow) error {
	isAdmin := false
	for _, r := range row.Roles {
		if r.Code == RolePlatformAdmin {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return nil
	}
	n, err := s.staffStore.CountActivePlatformAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastPlatformAdmin
	}
	return nil
}

// toStaffSummary 把仓储行转成 DTO。
// Roles / Account 恒为非 null（前端直接取 .length，null 会炸）。
func toStaffSummary(r *StaffRow) StaffSummary {
	roles := make([]StaffRoleRef, 0, len(r.Roles))
	for i := range r.Roles {
		roles = append(roles, StaffRoleRef{
			ID:        strconv.FormatInt(r.Roles[i].ID, 10),
			Code:      r.Roles[i].Code,
			Name:      r.Roles[i].Name,
			DataScope: r.Roles[i].DataScope,
			Portal:    portalOf(r.Roles[i].Code),
		})
	}
	var acct *StaffAccountRef
	if r.LoginID != "" {
		acct = &StaffAccountRef{LoginID: r.LoginID, Status: r.AccountStatus}
	}
	return StaffSummary{
		ID:        strconv.FormatInt(r.ID, 10),
		Name:      r.Name,
		Mobile:    r.Mobile,
		OrgUnitID: strconv.FormatInt(r.OrgUnitID, 10),
		OrgName:   r.OrgName,
		Status:    r.Status,
		Account:   acct,
		Roles:     roles,
	}
}

// staffBeforeValue 审计用的前置值快照（只留与本次变更相关的字段）。
func staffBeforeValue(r *StaffRow) map[string]any {
	return map[string]any{
		"name":        r.Name,
		"mobile":      r.Mobile,
		"org_unit_id": r.OrgUnitID,
		"status":      r.Status,
	}
}

// roleCodes 取角色 code 列表（互斥比对与审计留痕用）。
func roleCodes(roles []RoleRefRow) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Code)
	}
	return out
}

// ============================================================
// 仓储行模型与事务参数（repo → service，不出现在 API 响应里）
// ============================================================

// StaffRow 员工聚合行（staff + account + roles + org 名）。
type StaffRow struct {
	ID            int64
	Name          string
	Mobile        string
	OrgUnitID     int64
	OrgName       string
	Status        string
	LoginID       string         // 无账号时为空串
	AccountStatus string         // 无账号时为空串
	Roles         []StaffRoleRow // 恒非 null
}

// StaffRoleRow 员工持有的角色行。
type StaffRoleRow struct {
	ID        int64
	Code      string
	Name      string
	DataScope string
}

// RoleRefRow 角色引用行（授权用）。
type RoleRefRow struct {
	ID        int64
	Code      string
	Name      string
	DataScope string
}

// OrgUnitRefRow 组织节点引用行。
type OrgUnitRefRow struct {
	ID   int64
	Name string
}

// CreateStaffTxParams 建人事务参数。
type CreateStaffTxParams struct {
	Name     string
	Mobile   string
	OrgID    int64
	OrgName  string // 用于内存组装回显（避免提交后二次回查）
	LoginID  string
	Password string
	Roles    []RoleRefRow
	Operator Operator
}

// UpdateStaffTxParams 改人事务参数。
type UpdateStaffTxParams struct {
	StaffID  int64
	Name     string
	Mobile   string
	OrgID    int64
	Status   string
	Before   map[string]any
	Operator Operator
}

// ResetPasswordTxParams 重置口令事务参数。
type ResetPasswordTxParams struct {
	StaffID  int64
	Password string
	Operator Operator
}

// GrantRolesTxParams 授权事务参数。
type GrantRolesTxParams struct {
	StaffID  int64
	Roles    []RoleRefRow
	Operator Operator
}

// RevokeRoleTxParams 回收授权事务参数。
type RevokeRoleTxParams struct {
	StaffID  int64
	RoleID   int64
	RoleCode string // 仅用于审计留痕
	Operator Operator
}
