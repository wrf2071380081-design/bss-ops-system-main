// Package api 的 org_permissions.go：M11 组织与权限只读接口（批次 5）。
//
// 两个接口都严格挂 RequirePerm(M11:V)——返回的是**全系统权限配置**
// （组织层级、角色权限矩阵、数据域、字段剔除清单），比一般业务数据更敏感，
// 不能因为「只是查看」而放宽。
//
// 不做字段剔除（fieldmask）：DTO 里只有 id / code / name / type / memberCount /
// ownerName / permissions / fieldRestrictions 等键，与现有 mask 清单
// （unit_cost / margin_* / floor_price …）无任何交集，挂上去是空转。
// 响应里不含 password_hash、token、role_snapshot。
package api

import (
	"github.com/gin-gonic/gin"

	"model_bss/internal/domain/orgperm"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// OrgPermissionHandler 组织与权限只读处理器。
type OrgPermissionHandler struct {
	svc *orgperm.Service
}

// NewOrgPermissionHandler 构造。
func NewOrgPermissionHandler(svc *orgperm.Service) *OrgPermissionHandler {
	return &OrgPermissionHandler{svc: svc}
}

// ListOrganizations 组织树。
//
//	@Summary	组织树（M11:V，只读）
//	@Description	返回 status='ACTIVE' 的组织节点，按 parent_id 递归成树（children 嵌套）。口径：id/code 同为 org_unit.id 转字符串（org_unit 无 code 列，**code 不是业务编码**）；type 取值 COMPANY/DEPARTMENT/TEAM（DB 的 unit_type=DEPT 映射为 DEPARTMENT）；memberCount 是**含全部下级**的在职人数（不是直属人数）；ownerName 来自 leader_staff_id → internal_staff.name，无负责人为空串。parent_id 指向不存在/非 ACTIVE 节点的，降为根节点展示（不丢数据）。
//	@Tags		组织与权限
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse	"code=0；data=[]OrganizationNode{id,code,name,type,memberCount,ownerName,children?}"
//	@Failure	401	{object}	api.APIResponse	"code=10002 未认证"
//	@Failure	403	{object}	api.APIResponse	"code=10003 无 M11:V"
//	@Router		/api/internal/org-permissions/organizations [get]
func (h *OrgPermissionHandler) ListOrganizations(c *gin.Context) {
	nodes, err := h.svc.ListOrganizations(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, nodes)
}

// ListRoles 角色权限矩阵。
//
//	@Summary	角色权限矩阵（M11:V，只读）
//	@Description	返回全部角色及其权限点明细。口径：portal 由 role.code 推导（SUPPLIER→supplier、CUSTOMER→customer、其余→internal，**role 表无 portal 列**）；dataScope 取 role.data_scope 原值（SELF/DEPT/DEPT_SUB/ALL）；permissions[].module/action 由 permission_point.name 按「·」拆分，code 形如 M11:V，排序按 pkg/perm 的 M1..M12 语义顺序；fieldRestrictions 取 role.field_mask 的 hide 数组（NULL 时返回 []）；memberCount 是 role_grant 里**未过期**的授权人数；description 恒为空串（role 表无该列，不编造文案）。permissions 与 fieldRestrictions 恒为非 null 数组。
//	@Tags		组织与权限
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse	"code=0；data=[]RoleSummary{id,name,description,portal,memberCount,dataScope,permissions,fieldRestrictions}"
//	@Failure	401	{object}	api.APIResponse	"code=10002 未认证"
//	@Failure	403	{object}	api.APIResponse	"code=10003 无 M11:V"
//	@Router		/api/internal/org-permissions/roles [get]
func (h *OrgPermissionHandler) ListRoles(c *gin.Context) {
	roles, err := h.svc.ListRoles(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, roles)
}
