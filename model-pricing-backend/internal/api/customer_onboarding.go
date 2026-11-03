// Package api 的 customer_onboarding.go：客户开通流程（G4-B2）。
//
// 三条路由（C 域，注册在 router_trade.go）：
//
//	POST /internal/customers/:id/onboarding   M8:E + 幂等  发起开通（首单先合同后配置）
//	GET  /internal/customers/:id/onboarding   M8:V         读流程进度（含 deeplink，前端直接用）
//	POST /internal/customer-nodes/:id/complete M8:E + 幂等  完成节点（CONFIG 完成=写客户来源绑定）
//
// 与供应商流程的关系：表结构、唯一索引、审计写法共用；建流程/推进逻辑另写（路线 B），
// 供应商链路一行不动。
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

// CustomerOnboardingHandler 客户开通流程处理器。
type CustomerOnboardingHandler struct {
	svc *customer.OnboardingService
}

// NewCustomerOnboardingHandler 构造。
func NewCustomerOnboardingHandler(svc *customer.OnboardingService) *CustomerOnboardingHandler {
	return &CustomerOnboardingHandler{svc: svc}
}

// customerOnboardingErrToAppErr 领域错误 → HTTP 错误码。
//
// 409 的三种都要能指导操作：
//   - 已有进行中流程 → 先完成再发起
//   - 节点已完成 → 不能重复提交
//   - 前序未完成 → 先去办前一个节点
func customerOnboardingErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, customer.ErrOnboardingInProgress),
		errors.Is(err, customer.ErrOnboardingNodeCompleted),
		errors.Is(err, customer.ErrOnboardingNodeNotReady):
		return apperr.New(apperr.ErrConflict.Code, err.Error(), apperr.ErrConflict.Status) // 409
	case errors.Is(err, customer.ErrOnboardingFlowNotFound),
		errors.Is(err, customer.ErrOnboardingNodeNotFound),
		errors.Is(err, customer.ErrCustomerProfileNotFound),
		errors.Is(err, customer.ErrLinkResourceNotFound):
		return apperr.New(apperr.ErrNotFound.Code, err.Error(), apperr.ErrNotFound.Status) // 404
	case errors.Is(err, customer.ErrOnboardingModelsRequired),
		errors.Is(err, customer.ErrOnboardingModelInvalid),
		errors.Is(err, customer.ErrOnboardingModelDuplicated),
		errors.Is(err, customer.ErrOnboardingResourceNotAdmitted),
		errors.Is(err, customer.ErrOnboardingRegionMismatch),
		errors.Is(err, customer.ErrOnboardingAssigneeRequired),
		errors.Is(err, customer.ErrOnboardingContractAssignee),
		errors.Is(err, customer.ErrOnboardingReasonRequired),
		errors.Is(err, customer.ErrLinkModelMismatch),
		errors.Is(err, customer.ErrLinkResourceNotAdmitted):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status) // 400
	case errors.Is(err, customer.ErrLinkModelAlreadyActive),
		errors.Is(err, customer.ErrLinkDuplicateResource):
		// 写绑定时撞唯一索引：属于可解决的业务冲突（先归档旧绑定 / 该资源已绑定）。
		return apperr.New(apperr.ErrConflict.Code, err.Error(), apperr.ErrConflict.Status) // 409
	default:
		return apperr.ErrSystem
	}
}

// startOnboardingBody 发起开通请求体。
type startOnboardingBody struct {
	FirstOrder         bool                            `json:"first_order"`
	ContractAssigneeID int64                           `json:"contract_assignee_id"`
	ConfigAssigneeID   int64                           `json:"config_assignee_id"`
	Models             []customer.OnboardingModelInput `json:"models"`
	Reason             string                          `json:"reason"`
}

// StartOnboarding 发起客户开通（M8:E + 幂等）。
//
// @Summary 发起客户开通流程（M8:E）
// @Description 首单顺序为 CONTRACT（合同与授信）→ CONFIG（资源配置）；非首单只有 CONFIG。一个客户同时只允许一条进行中流程（部分唯一索引兜底）。模型必须满足：资源存在、与所选标准模型一致、与客户同一部署节点、资源已准入。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_profile.id"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body startOnboardingBody true "config_assignee_id / models 必填；first_order=true 时 contract_assignee_id 必填"
// @Success 200 {object} api.APIResponse "code=0；data=OnboardingFlow"
// @Failure 400 {object} api.APIResponse "code=10001 模型或办理人参数非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:E"
// @Failure 404 {object} api.APIResponse "code=10004 客户或资源不存在"
// @Failure 409 {object} api.APIResponse "code=10008 该客户已有进行中的开通流程"
// @Router /api/internal/customers/{id}/onboarding [post]
func (h *CustomerOnboardingHandler) StartOnboarding(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customer id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	var body startOnboardingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	flow, err := h.svc.Start(c.Request.Context(), customer.StartOnboardingInput{
		CustomerProfileID:  id,
		FirstOrder:         body.FirstOrder,
		ContractAssigneeID: body.ContractAssigneeID,
		ConfigAssigneeID:   body.ConfigAssigneeID,
		Models:             body.Models,
		Reason:             body.Reason,
	}, op.StaffID, requestIDOf(c))
	if err != nil {
		response.Error(c, customerOnboardingErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, flow)
	response.Success(c, flow)
}

// GetOnboarding 客户开通进度（M8:V）。
//
// @Summary 客户开通流程进度（M8:V）
// @Description 返回最近一条流程的状态、当前节点、节点列表、本次模型与已写入的客户来源绑定，并附带 deeplink（/internal/customers/{id}?tab=onboarding）供前端直接跳转。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_profile.id"
// @Success 200 {object} api.APIResponse "code=0；data=OnboardingFlow"
// @Failure 400 {object} api.APIResponse "code=10001 客户 ID 非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:V"
// @Failure 404 {object} api.APIResponse "code=10004 该客户还没有开通流程"
// @Router /api/internal/customers/{id}/onboarding [get]
func (h *CustomerOnboardingHandler) GetOnboarding(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customer id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	flow, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Error(c, customerOnboardingErrToAppErr(err))
		return
	}
	response.Success(c, flow)
}

// completeNodeBody 节点完成请求体。
//
// 这里**没有** expected_flow_revision，是刻意的：onboarding_flow 没有 revision 列，
// 没有东西可校验；并发保护由 CompleteContract / CompleteConfig 第一句的
// `SELECT ... FROM onboarding_flow WHERE id=$1 FOR UPDATE` 行锁保证
// （后到的请求读到节点已 COMPLETED，直接返回「该节点已完成」，审计也不会重复写）。
// 留一个"收但不校验"的字段比删掉更危险：前端会以为传了有用，后人可能据此去写
// "乐观锁失败重试"逻辑，纯属对着空气。
type completeNodeBody struct {
	Reason string `json:"reason"`
	// F12Reference：可选的外部凭据（合同号 / F12 记录号），原样写进审计。
	// 客户侧不走 F12 就绪校验，所以凭据由办理人手填以提高可追溯性。
	F12Reference string `json:"f12_reference"`
}

// CompleteCustomerNode 完成开通节点（M8:E + 幂等）。
//
// @Summary 完成客户开通节点（M8:E）
// @Description CONFIG 节点完成 = 按流程里的模型逐条写 customer_resource_link（全部成功才提交，任何冲突整批回滚；冲突返回 409 可读文案）；随后流程 COMPLETED。CONTRACT 节点完成由办理人填写说明确认「合同与授信已归档」（客户侧不走 F12 就绪快照），完成后把 CONFIG 推到 IN_PROGRESS。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "onboarding_node.id"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body completeNodeBody true "reason 必填；f12_reference 可选（合同号/F12 记录号，落审计）。无 expected_flow_revision：并发由 flow 行锁保证"
// @Success 200 {object} api.APIResponse "code=0；data=OnboardingFlow"
// @Failure 400 {object} api.APIResponse "code=10001 缺少说明"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:E"
// @Failure 404 {object} api.APIResponse "code=10004 节点不存在"
// @Failure 409 {object} api.APIResponse "code=10008 节点已完成 / 前序未完成 / 绑定冲突"
// @Router /api/internal/customer-nodes/{id}/complete [post]
func (h *CustomerOnboardingHandler) CompleteCustomerNode(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "node id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	var body completeNodeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	flow, err := h.svc.CompleteNode(c.Request.Context(), customer.CompleteNodeInput{
		NodeID:       id,
		Reason:       body.Reason,
		F12Reference: body.F12Reference,
		Operator:     op.StaffID,
		Request:      requestIDOf(c),
	})
	if err != nil {
		response.Error(c, customerOnboardingErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, flow)
	response.Success(c, flow)
}
