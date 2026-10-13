// Package api 的 org_permissions_staff.go：M11 账号管理写接口（2026-10-13 补）。
//
// 权限：全部挂 RequirePerm(perm.M11Edit) + 服务层 perm.AnyRoleCan(CanManageStaff)
// 收敛到 PLATFORM_ADMIN。账号管理能改任何人的权限包，是最高危写操作，两层都不能单删。
//
// 数据域：**不做行级过滤**。账号管理是全系统管理职能，没有「谁的员工」归属概念。
//
// 安全：响应里绝不出现 password_hash / token；审计里只记「口令已重置」这一事实。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/orgperm"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
)

// StaffHandler 账号管理处理器。
type StaffHandler struct {
	svc *orgperm.Service
}

// NewStaffHandler 构造。
func NewStaffHandler(svc *orgperm.Service) *StaffHandler { return &StaffHandler{svc: svc} }

// ------------------------------------------------------------
// 读（账号管理页的员工列表/详情）
// ------------------------------------------------------------

// ListStaff 员工列表。
//
//	@Summary	员工列表（M11:E + PLATFORM_ADMIN）
//	@Description	分页列内部员工，含账号登录名/状态与已授角色。keyword 命中 name/mobile/login_id（模糊）；status 取 ACTIVE/DISABLED，空=全部。**不做数据域过滤**（账号管理无归属概念）。响应不含 password_hash / token。
//	@Tags		账号管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		keyword	query		string	false	"姓名/手机号/登录名模糊搜索"
//	@Param		status	query		string	false	"ACTIVE / DISABLED，空=全部"
//	@Param		page	query		int		false	"页码（默认 1）"
//	@Param		size	query		int		false	"每页大小（默认 20，上限 200）"
//	@Success	200		{object}	api.APIResponse	"code=0；data={list,total,page,size}"
//	@Failure	401		{object}	api.APIResponse	"code=10002 未认证"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Router		/api/internal/org-permissions/staff [get]
func (h *StaffHandler) ListStaff(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	page, size := parsePageQuery(c)
	q := orgperm.StaffQuery{
		Keyword: c.Query("keyword"),
		Status:  c.Query("status"),
		Page:    page,
		Size:    size,
	}
	res, err := h.svc.ListStaff(c.Request.Context(), q)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// GetStaff 员工详情。
//
//	@Summary	员工详情（M11:E + PLATFORM_ADMIN）
//	@Description	按 id 取单个员工，含账号与角色明细。响应不含 password_hash / token。
//	@Tags		账号管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int64	true	"internal_staff.id"
//	@Success	200	{object}	api.APIResponse	"code=0；data=StaffDetail"
//	@Failure	401	{object}	api.APIResponse	"code=10002 未认证"
//	@Failure	403	{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	404	{object}	api.APIResponse	"code=10004 员工不存在"
//	@Router		/api/internal/org-permissions/staff/{id} [get]
func (h *StaffHandler) GetStaff(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	id, ok := parseStaffID(c)
	if !ok {
		return
	}
	res, err := h.svc.GetStaff(c.Request.Context(), id)
	if err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, res)
}

// ------------------------------------------------------------
// 写
// ------------------------------------------------------------

// CreateStaff 新建员工（人 + 账号 + 角色 + 审计，单事务）。
//
//	@Summary	新建员工（M11:E + PLATFORM_ADMIN，必挂幂等）
//	@Description	一体创建 internal_staff + account + role_grant + audit_log。login_id 4-32 位小写字母/数字/下划线且小写开头，INTERNAL 门户内唯一；mobile 11 位且唯一；password 8-64 字符；role_ids 至少 1 个且必须全部存在、全部为内部角色（SUPPLIER/CUSTOMER 必须走各自入驻流程），且相互不互斥（SALES↔PRICING_OP、SALES↔PROCUREMENT）。org_unit_id 必须存在。
//	@Tags		账号管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		orgperm.CreateStaffInput	true	"新建员工请求体"
//	@Success	200		{object}	api.APIResponse	"code=0；data=StaffDetail"
//	@Failure	400		{object}	api.APIResponse	"code=10001 参数或业务规则非法"
//	@Failure	401		{object}	api.APIResponse	"code=10002 未认证"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	409		{object}	api.APIResponse	"code=10005 幂等冲突"
//	@Router		/api/internal/org-permissions/staff [post]
func (h *StaffHandler) CreateStaff(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	op := middleware.OperatorFrom(c)
	var in orgperm.CreateStaffInput
	if err := bindStaffJSON(c, &in); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status))
		return
	}
	res, err := h.svc.CreateStaff(c.Request.Context(), in, orgperm.Operator{
		OperatorID:   op.OperatorID,
		OperatorRole: operatorRoleOf(op),
		RequestID:    requestIDOf(c),
	})
	if err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, res)
}

// UpdateStaff 修改员工（姓名/手机号/组织/状态）。
//
//	@Summary	修改员工（M11:E + PLATFORM_ADMIN，必挂幂等）
//	@Description	只改 internal_staff 四列；账号与角色有专门接口。停用保护：唯一在职持 PLATFORM_ADMIN 的员工不允许置为 DISABLED。手机号变更时会重新校验唯一性。
//	@Tags		账号管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int64						true	"internal_staff.id"
//	@Param		body	body		orgperm.UpdateStaffInput	true	"修改员工请求体"
//	@Success	200		{object}	api.APIResponse	"code=0；data=StaffDetail"
//	@Failure	400		{object}	api.APIResponse	"code=10001 参数非法"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	404		{object}	api.APIResponse	"code=10004 员工不存在"
//	@Router		/api/internal/org-permissions/staff/{id} [put]
func (h *StaffHandler) UpdateStaff(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	id, ok := parseStaffID(c)
	if !ok {
		return
	}
	op := middleware.OperatorFrom(c)
	var in orgperm.UpdateStaffInput
	if err := bindStaffJSON(c, &in); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status))
		return
	}
	res, err := h.svc.UpdateStaff(c.Request.Context(), id, in, orgperm.Operator{
		OperatorID:   op.OperatorID,
		OperatorRole: operatorRoleOf(op),
		RequestID:    requestIDOf(c),
	})
	if err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, res)
}

// ResetStaffPassword 重置员工账号口令。
//
//	@Summary	重置口令（M11:E + PLATFORM_ADMIN，必挂幂等）
//	@Description	管理侧重置，**不校验原口令**（原口令不该到达服务端）。新口令 8-64 字符，bcrypt cost 10 落库。审计只记「已重置」这一事实，不记明文或密文。
//	@Tags		账号管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int64							true	"internal_staff.id"
//	@Param		body	body		orgperm.ResetPasswordInput	true	"重置口令请求体"
//	@Success	200		{object}	api.APIResponse	"code=0；data={reset:true}"
//	@Failure	400		{object}	api.APIResponse	"code=10001 口令长度非法"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	404		{object}	api.APIResponse	"code=10004 员工不存在"
//	@Router		/api/internal/org-permissions/staff/{id}/password [post]
func (h *StaffHandler) ResetStaffPassword(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	id, ok := parseStaffID(c)
	if !ok {
		return
	}
	op := middleware.OperatorFrom(c)
	var in orgperm.ResetPasswordInput
	if err := bindStaffJSON(c, &in); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status))
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), id, in, orgperm.Operator{
		OperatorID:   op.OperatorID,
		OperatorRole: operatorRoleOf(op),
		RequestID:    requestIDOf(c),
	}); err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, gin.H{"reset": true})
}

// GrantStaffRole 给员工追加角色。
//
//	@Summary	授权角色（M11:E + PLATFORM_ADMIN，必挂幂等）
//	@Description	一次可授多个。重复授予已持有的角色 → 400；与外部门户角色 → 400；与已持角色互斥（SALES↔PRICING_OP、SALES↔PROCUREMENT）→ 400。role_grant 无唯一约束，仓储用 NOT EXISTS 兜底防重复行。
//	@Tags		账号管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int64						true	"internal_staff.id"
//	@Param		body	body		orgperm.GrantRoleInput	true	"授权请求体（role_ids 至少 1 个）"
//	@Success	200		{object}	api.APIResponse	"code=0；data=StaffDetail"
//	@Failure	400		{object}	api.APIResponse	"code=10001 角色不存在/外部门户/已授予/互斥"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	404		{object}	api.APIResponse	"code=10004 员工不存在"
//	@Router		/api/internal/org-permissions/staff/{id}/roles [post]
func (h *StaffHandler) GrantStaffRole(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	id, ok := parseStaffID(c)
	if !ok {
		return
	}
	op := middleware.OperatorFrom(c)
	var in orgperm.GrantRoleInput
	if err := bindStaffJSON(c, &in); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status))
		return
	}
	if err := h.svc.GrantRole(c.Request.Context(), id, in, orgperm.Operator{
		OperatorID:   op.OperatorID,
		OperatorRole: operatorRoleOf(op),
		RequestID:    requestIDOf(c),
	}); err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	detail, err := h.svc.GetStaff(c.Request.Context(), id)
	if err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, detail)
}

// RevokeStaffRole 回收员工的一个角色。
//
//	@Summary	回收角色（M11:E + PLATFORM_ADMIN，必挂幂等）
//	@Description	按 role_id 回收该员工的未过期授权。保护：**不允许收掉最后一个 PLATFORM_ADMIN 授权**，否则平台无人可管理。未授予的角色 → 400。
//	@Tags		账号管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int64	true	"internal_staff.id"
//	@Param		roleId	path		int64	true	"role.id"
//	@Success	200		{object}	api.APIResponse	"code=0；data=StaffDetail"
//	@Failure	400		{object}	api.APIResponse	"code=10001 角色未授予 / 试图收掉唯一平台管理员"
//	@Failure	403		{object}	api.APIResponse	"code=10003 无 M11:E 或角色不含 PLATFORM_ADMIN"
//	@Failure	404		{object}	api.APIResponse	"code=10004 员工不存在"
//	@Router		/api/internal/org-permissions/staff/{id}/roles/{roleId} [delete]
func (h *StaffHandler) RevokeStaffRole(c *gin.Context) {
	if !h.canManage(c) {
		return
	}
	id, ok := parseStaffID(c)
	if !ok {
		return
	}
	roleID, err := strconv.ParseInt(c.Param("roleId"), 10, 64)
	if err != nil || roleID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	op := middleware.OperatorFrom(c)
	if err := h.svc.RevokeRole(c.Request.Context(), id, roleID, orgperm.Operator{
		OperatorID:   op.OperatorID,
		OperatorRole: operatorRoleOf(op),
		RequestID:    requestIDOf(c),
	}); err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	detail, err := h.svc.GetStaff(c.Request.Context(), id)
	if err != nil {
		response.Error(c, staffErrToAppErr(err))
		return
	}
	response.Success(c, detail)
}

// ------------------------------------------------------------
// 内部工具
// ------------------------------------------------------------

// canManage 服务层二次鉴权：角色必须含 PLATFORM_ADMIN。
//
// 第一层是中间件 RequirePerm(M11:E)（000031 后仅 PLATFORM_ADMIN 持该点）。
// 本层是防御性冗余：将来若把 M11:E 授给别的角色，这里仍按角色 code 拦住。
// 与 UpdateCostParams 的 AnyRoleCan(CanEditCostParam) 同一条纪律。
func (h *StaffHandler) canManage(c *gin.Context) bool {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return false
	}
	if !perm.AnyRoleCan(op.Roles, perm.CanManageStaff) {
		response.Error(c, apperr.New(
			apperr.ErrForbidden.Code,
			fmt.Sprintf("角色 %v 不含 PLATFORM_ADMIN，不允许管理账号（M11:E 服务层角色收敛）", op.Roles),
			apperr.ErrForbidden.Status,
		))
		return false
	}
	return true
}

// parseStaffID 解析路径参数 id；非法时已写好 400 响应并返回 false。
func parseStaffID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return 0, false
	}
	return id, true
}

// bindStaffJSON 读 body 并 JSON 解码；空 body / 非法 JSON 一律 400（带原因）。
func bindStaffJSON(c *gin.Context, v any) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return errors.New("请求体读取失败")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return errors.New("请求体不能为空")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("JSON 解析失败：%s", err.Error())
	}
	return nil
}

// staffErrToAppErr 把领域错误映射为 HTTP 错误码。
//
// 归类：不存在 → 404；业务规则（格式/唯一性/互斥/已授予/唯一管理员）→ 400；
// 其余 → 500（不把内部错误细节透给前端）。
func staffErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, orgperm.ErrStaffNotFound):
		return apperr.ErrNotFound
	case errors.Is(err, orgperm.ErrNameRequired),
		errors.Is(err, orgperm.ErrLoginIDInvalid),
		errors.Is(err, orgperm.ErrLoginIDTaken),
		errors.Is(err, orgperm.ErrMobileInvalid),
		errors.Is(err, orgperm.ErrMobileTaken),
		errors.Is(err, orgperm.ErrPasswordTooWeak),
		errors.Is(err, orgperm.ErrOrgUnitNotFound),
		errors.Is(err, orgperm.ErrRoleNotFound),
		errors.Is(err, orgperm.ErrRoleNotInternal),
		errors.Is(err, orgperm.ErrRoleMutexConflict),
		errors.Is(err, orgperm.ErrRoleAlreadyGranted),
		errors.Is(err, orgperm.ErrRoleNotGranted),
		errors.Is(err, orgperm.ErrLastPlatformAdmin),
		errors.Is(err, orgperm.ErrNoRolesGiven):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status)
	default:
		return apperr.ErrSystem
	}
}
