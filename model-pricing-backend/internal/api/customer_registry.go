// Package api 的 customer_registry.go：客户创建与基础档案（G4-B1）。
//
// 为什么要有这两个接口：`customer_profile` / `legal_subject` 之前只有读路径，
// 「完整开通」流程没有对象可挂。这里补的是**最小可用**的创建口 + 详情档案口。
//
// 权限（与客户域其它接口同口径，不新造权限码）：
//   - POST /customers      → M8:E（客户管理·编辑）+ 幂等
//   - GET  /customers/:id  → M8:V（客户管理·查看）+ 数据域过滤
//
// 红线：写接口必挂 Idempotency（红线 5）；比例/金额一律字符串传输（红线 1），
// 所以 tax_rate 用字符串接收，避免 JSON float 把 0.06 变成 0.059999999999999998。
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

// CustomerRegistryHandler 客户创建与基础档案处理器。
type CustomerRegistryHandler struct {
	svc *customer.RegistryService
}

// NewCustomerRegistryHandler 构造。
func NewCustomerRegistryHandler(svc *customer.RegistryService) *CustomerRegistryHandler {
	return &CustomerRegistryHandler{svc: svc}
}

// customerRegistryErrToAppErr 领域错误 → HTTP 错误码。
//
// 409 只留给「用户能自己解决」的冲突（改名 / 换一张执照），
// 校验类一律 400，数据域越权 403，能力未接线 500。
func customerRegistryErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, customer.ErrCustomerNameDuplicate),
		errors.Is(err, customer.ErrCustomerUSCCDuplicate):
		return apperr.New(apperr.ErrConflict.Code, err.Error(), apperr.ErrConflict.Status) // 409
	case errors.Is(err, customer.ErrCustomerProfileNotFound):
		return apperr.New(apperr.ErrNotFound.Code, err.Error(), apperr.ErrNotFound.Status) // 404
	case errors.Is(err, customer.ErrCustomerOutOfScope):
		return apperr.New(apperr.ErrForbidden.Code, err.Error(), apperr.ErrForbidden.Status) // 403
	case errors.Is(err, customer.ErrCustomerNameRequired),
		errors.Is(err, customer.ErrCustomerNameTooLong),
		errors.Is(err, customer.ErrCustomerSubjectType),
		errors.Is(err, customer.ErrCustomerIndividualUnsupported),
		errors.Is(err, customer.ErrCustomerUSCCInvalid),
		errors.Is(err, customer.ErrCustomerRegionInvalid),
		errors.Is(err, customer.ErrCustomerCurrencyInvalid),
		errors.Is(err, customer.ErrCustomerTaxRateInvalid),
		errors.Is(err, customer.ErrCustomerOwnerRequired),
		errors.Is(err, customer.ErrCustomerOwnerNotFound),
		errors.Is(err, customer.ErrCustomerPaymentType),
		errors.Is(err, customer.ErrCustomerBillingCycle):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status) // 400
	default:
		return apperr.ErrSystem
	}
}

// createCustomerBody 创建客户请求体。
//
// 指针用于区分「没传」与「传了零值」：tax_rate 传空串与不传都算未填写；
// billing_cycle 不传走默认 30，传 0 则视为非法（0 天账期没有业务含义）。
type createCustomerBody struct {
	LegalName            string `json:"legal_name"`
	SubjectType          string `json:"subject_type"`
	USCC                 string `json:"uscc"`
	Region               string `json:"region"`
	SettlementCurrency   string `json:"settlement_currency"`
	TaxRate              string `json:"tax_rate"`
	OwnerSalesOperatorID int64  `json:"owner_sales_operator_id"`
	PaymentType          string `json:"payment_type"`
	// 指针：区分「不传（默认 30）」与「传 0（非法）」。
	BillingCycle *int   `json:"billing_cycle"`
	Reason       string `json:"reason"`
}

// CreateCustomer 新建客户（主体 + 客户档案，单事务）。
//
// @Summary 新建客户（M8:E）
// @Description 一个事务里创建 legal_subject（verification_status 直接 VERIFIED，人工录入已核验）与 customer_profile。同名客户（忽略大小写与首尾空格）返回 409；同一 USCC 已被占用也返回 409。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "幂等键"
// @Param body body createCustomerBody true "legal_name / region / settlement_currency / owner_sales_operator_id 必填"
// @Success 200 {object} api.APIResponse "code=0；data=CustomerProfile"
// @Failure 400 {object} api.APIResponse "code=10001 校验失败（名称/区域/币种/税率/账期/销售）"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:E"
// @Failure 409 {object} api.APIResponse "code=10008 客户名称或统一社会信用代码已存在"
// @Router /api/internal/customers [post]
func (h *CustomerRegistryHandler) CreateCustomer(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	var body createCustomerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	profile, err := h.svc.Create(c.Request.Context(), customer.CreateCustomerInput{
		LegalName:            body.LegalName,
		SubjectType:          body.SubjectType,
		USCC:                 body.USCC,
		Region:               body.Region,
		SettlementCurrency:   body.SettlementCurrency,
		TaxRate:              body.TaxRate,
		OwnerSalesOperatorID: body.OwnerSalesOperatorID,
		PaymentType:          body.PaymentType,
		BillingCycle:         body.BillingCycle,
		Reason:               body.Reason,
	}, op.StaffID, requestIDOf(c))
	if err != nil {
		response.Error(c, customerRegistryErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, profile)
	response.Success(c, profile)
}

// GetCustomerProfile 客户基础档案（M8:V）。
//
// @Summary 客户基础档案（M8:V）
// @Description 返回客户主体与档案基础字段（名称/USCC/主体类型/区域/币种/税率/合同状态/销售归属/账期/授信/状态）。数据域与客户列表一致：SELF / DEPT_SUB 只能看自己归属的客户，越权返回 403。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_profile.id"
// @Success 200 {object} api.APIResponse "code=0；data=CustomerProfile"
// @Failure 400 {object} api.APIResponse "code=10001 客户 ID 非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M8:V 或客户超出数据域"
// @Failure 404 {object} api.APIResponse "code=10004 客户不存在"
// @Router /api/internal/customers/{id} [get]
func (h *CustomerRegistryHandler) GetCustomerProfile(c *gin.Context) {
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
	profile, err := h.svc.Profile(c.Request.Context(), id, scopeOf(op))
	if err != nil {
		response.Error(c, customerRegistryErrToAppErr(err))
		return
	}
	response.Success(c, profile)
}
