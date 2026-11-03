// Package api 的 customer_resource_link.go：客户「接入模型」绑定（G4 地基，迁移 000049）。
//
// 权限口径与客户域其它接口保持一致（不做新权限码）：
//   - GET  /customers/:id/resource-links  M8:V（客户管理·查看）
//   - POST /resource-links                M8:E（客户管理·编辑）
//   - POST /resource-links/:id/actions    M8:E（客户管理·编辑）
//
// 红线：
//   - 写接口必挂 Idempotency（红线 5），并把结果写回幂等回放（SetIdempotencyResult）。
//   - 乐观锁：动作接口必须带 expected_revision，冲突映射 409 而不是 500。
//   - 唯一索引冲突不得把 SQL 原文抛给前端：由仓储翻译成领域错误，这里再映射成业务文案。
package api

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// CustomerResourceLinkHandler 客户接入模型绑定处理器。
type CustomerResourceLinkHandler struct {
	svc *customer.ResourceLinkService
}

// NewCustomerResourceLinkHandler 构造。
func NewCustomerResourceLinkHandler(svc *customer.ResourceLinkService) *CustomerResourceLinkHandler {
	return &CustomerResourceLinkHandler{svc: svc}
}

// resourceLinkErrToAppErr 领域错误 → HTTP 错误码。
//
// 409 的两类刻意分开：
//   - ErrLinkRevisionConflict → 10005 幂等/状态冲突（前端提示「刷新后重试」）
//   - ErrLinkDuplicateResource / ErrLinkModelAlreadyActive → 10008 数据已存在（前端提示具体该怎么做）
func resourceLinkErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, customer.ErrLinkNotFound),
		errors.Is(err, customer.ErrLinkCustomerNotFound),
		errors.Is(err, customer.ErrLinkStandardModelNotFound),
		errors.Is(err, customer.ErrLinkResourceNotFound),
		errors.Is(err, customer.ErrLinkReferenceMissing):
		return apperr.New(apperr.ErrNotFound.Code, err.Error(), apperr.ErrNotFound.Status) // 404
	case errors.Is(err, customer.ErrLinkDuplicateResource),
		errors.Is(err, customer.ErrLinkModelAlreadyActive):
		return apperr.New(apperr.ErrConflict.Code, err.Error(), apperr.ErrConflict.Status) // 409
	case errors.Is(err, customer.ErrLinkRevisionConflict),
		errors.Is(err, customer.ErrLinkArchived):
		return apperr.New(apperr.ErrIdempotency.Code, err.Error(), apperr.ErrIdempotency.Status) // 409
	case errors.Is(err, customer.ErrLinkTransitionInvalid),
		errors.Is(err, customer.ErrLinkActionUnsupported),
		errors.Is(err, customer.ErrLinkResourceNotAdmitted),
		errors.Is(err, customer.ErrLinkModelMismatch),
		errors.Is(err, customer.ErrLinkRebindOnlyActive),
		errors.Is(err, customer.ErrLinkCustomerRequired),
		errors.Is(err, customer.ErrLinkModelRequired),
		errors.Is(err, customer.ErrLinkResourceRequired),
		errors.Is(err, customer.ErrLinkRevisionRequired),
		errors.Is(err, customer.ErrLinkQuoteInvalid),
		errors.Is(err, customer.ErrLinkCustomerIDOutOfRange):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status) // 400
	case errors.Is(err, customer.ErrLinkStoreUnavailable):
		return apperr.ErrSystem // 500（能力未接线，不是用户错误）
	default:
		return apperr.ErrSystem
	}
}

// ListCustomerResourceLinks 客户「接入模型」投影（M8:V）。
//
// @Summary 客户接入模型投影（M8:V）
// @Description 按客户返回每个标准模型的服务状态、已绑定资源数、同模型同部署节点可用资源数与风险标记。service_state ∈ 服务中/接入中/已禁用/来源待处理/无可用来源；available_count 只统计同 standard_model_id + 同 region 且 resource.state='ADMITTED' 的资源。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_profile.id"
// @Success 200 {object} api.APIResponse "code=0；data={list:[CustomerModelService]}"
// @Failure 400 {object} api.APIResponse "code=10001 客户 ID 非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:V"
// @Router /api/internal/customers/{id}/resource-links [get]
func (h *CustomerResourceLinkHandler) ListCustomerResourceLinks(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customer id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	list, err := h.svc.ListCustomerModels(c.Request.Context(), id)
	if err != nil {
		response.Error(c, resourceLinkErrToAppErr(err))
		return
	}
	response.Success(c, gin.H{"list": list})
}

// createResourceLinkBody 建绑定请求体。
type createResourceLinkBody struct {
	CustomerProfileID int64  `json:"customer_profile_id"`
	StandardModelID   int64  `json:"standard_model_id"`
	ResourceID        int64  `json:"resource_id"`
	QuoteID           *int64 `json:"quote_id"`
	Note              string `json:"note"`
}

// CreateResourceLink 建一条客户来源绑定（M8:E + 幂等）。
//
// 本轮由开通流程内部调用，同时开放手工登记（排障/补录场景）。
//
// @Summary 新建客户来源绑定（M8:E）
// @Description 客户开通完成后建立「客户 + 标准模型 + 供应资源」绑定，初始状态 ACTIVE。同一客户同一供应资源只能有 1 条未归档绑定；同一客户同一标准模型同时只能有 1 条 ACTIVE（第二条返回 409，请先归档旧绑定）。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "幂等键"
// @Param body body createResourceLinkBody true "customer_profile_id / standard_model_id / resource_id 必填；quote_id、note 可选"
// @Success 200 {object} api.APIResponse "code=0；data=ResourceLink"
// @Failure 400 {object} api.APIResponse "code=10001 参数非法 / 资源未准入 / 模型与资源不匹配"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:E"
// @Failure 404 {object} api.APIResponse "code=10004 客户或资源不存在"
// @Failure 409 {object} api.APIResponse "code=10008 已绑定该资源 / 该模型已有生效来源"
// @Router /api/internal/resource-links [post]
func (h *CustomerResourceLinkHandler) CreateResourceLink(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	var body createResourceLinkBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	link, err := h.svc.Create(c.Request.Context(), customer.CreateLinkInput{
		CustomerProfileID: body.CustomerProfileID,
		StandardModelID:   body.StandardModelID,
		ResourceID:        body.ResourceID,
		QuoteID:           body.QuoteID,
		Note:              body.Note,
	}, op.StaffID, requestIDOf(c))
	if err != nil {
		response.Error(c, resourceLinkErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, link)
	response.Success(c, link)
}

// resourceLinkActionBody 状态动作请求体。
type resourceLinkActionBody struct {
	Action           string `json:"action"`
	Reason           string `json:"reason"`
	ExpectedRevision int    `json:"expected_revision"`
}

// ActResourceLink 执行绑定状态动作（M8:E + 幂等 + 乐观锁）。
//
// action ∈ DISABLE_REQUEST | DISABLE_CONFIRM | RESTORE_REQUEST | RESTORE_CONFIRM | OFFLINE_CONFIRM。
// 系统动作（SUPPLY_OFFLINE / SUPPLY_RESTORED）不开放 HTTP 入口，只由供应域事件驱动。
//
// @Summary 客户来源绑定状态动作（M8:E）
// @Description 禁用/恢复/确认停用。SUPPLY_OFFLINE_PENDING 下的 OFFLINE_CONFIRM 会把绑定归档（客户不再可用该来源）。必须回传 expected_revision，冲突返回 409。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_resource_link.id"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body resourceLinkActionBody true "action / expected_revision 必填"
// @Success 200 {object} api.APIResponse "code=0；data=ResourceLink"
// @Failure 400 {object} api.APIResponse "code=10001 动作不支持 / 迁移非法 / 缺 expected_revision"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:E"
// @Failure 404 {object} api.APIResponse "code=10004 绑定不存在"
// @Failure 409 {object} api.APIResponse "code=10005 版本冲突或已归档"
// @Router /api/internal/resource-links/{id}/actions [post]
func (h *CustomerResourceLinkHandler) ActResourceLink(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "resource link id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	var body resourceLinkActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	link, err := h.svc.Act(c.Request.Context(), customer.LinkActionInput{
		ID:               id,
		Action:           body.Action,
		Reason:           body.Reason,
		ExpectedRevision: body.ExpectedRevision,
	}, op.StaffID, requestIDOf(c))
	if err != nil {
		response.Error(c, resourceLinkErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, link)
	response.Success(c, link)
}
