// Package api 的 supplier_portal_profile.go：供应商门户「主体档案」只读接口（批次 7a）。
//
// ⚠️ 别和 internal/api/supplier_profile.go 搞混：那个是 `/api/internal/suppliers`
// （内部门户 M3），本文件是 `/api/supplier/profile`（供应商门户）。
//
// 鉴权：供应商门户**不挂 RequirePerm**（SUPPLIER 角色零内部权限点，挂了必然 403），
// 只走 AuthN；隔离靠「供应商身份只从登录态解析」——
// subject_operator.id → subject_id → supplier_profile.id，绝不接受请求参数指定 supplier_id。
//
// 字段剔除：DTO 物理不含成本/毛利/平台售价键（也没有 settle_type 原值，
// 只暴露由它推导的 paymentTerms），因此无需运行时 fieldmask.Apply。
//
// 本批**只实现 GET**；PUT /api/supplier/profile 未注册（返回 404）。
package api

import (
	"errors"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supplier"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// SupplierPortalProfileHandler 供应商门户档案处理器。
type SupplierPortalProfileHandler struct {
	svc         *supplier.PortalProfileService
	supplierSvc *supplier.Service
}

// NewSupplierPortalProfileHandler 构造。
func NewSupplierPortalProfileHandler(svc *supplier.PortalProfileService, supplierSvc *supplier.Service) *SupplierPortalProfileHandler {
	return &SupplierPortalProfileHandler{svc: svc, supplierSvc: supplierSvc}
}

// GetPortalProfile 供应商门户「主体档案」。
//
//	@Summary	供应商主体档案（供应商门户，只读）
//	@Description	返回**登录供应商自己**的主体档案。身份只从登录态解析（subject_operator.id → subject_id → supplier_profile.id），不接受任何请求参数指定主体。脱敏：registrationNoMasked 保留 USCC 前 4 后 2 中间打星；contactPhoneMasked 保留前 3 后 4 中间打星。paymentTerms 由 settle_type + billing_cycle 推导（PREPAID→「预付」、MONTHLY→「月结 N 天」），不是独立字段。响应不含成本/毛利/平台售价，也不含 credit_line 等商务敏感字段。本批只实现 GET，PUT 未开放。
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse	"code=0；data=PortalProfile{id,legalName,registrationNoMasked,status,qualificationStatus,contactName,contactPhoneMasked,settlementCurrency,paymentTerms,updatedAt}"
//	@Failure	401	{object}	api.APIResponse	"code=10002 未认证（非 SUPPLIER 身份）"
//	@Failure	404	{object}	api.APIResponse	"code=10004 档案不存在"
//	@Router		/api/supplier/profile [get]
func (h *SupplierPortalProfileHandler) GetPortalProfile(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.OperatorType != "SUPPLIER" {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	sup, err := h.supplierSvc.ResolveByOperator(c.Request.Context(), op.OperatorID)
	if err != nil {
		if errors.Is(err, supplier.ErrNotFound) {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		response.Error(c, apperr.ErrSystem)
		return
	}

	profile, err := h.svc.Get(c.Request.Context(), sup.ID)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	if profile == nil {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	response.Success(c, profile)
}
