// Package api 的 finance.go：财务中心只读接口（内部门户 M10，12-finance.md §1/§2/§3）。
//
//	GET /api/internal/finance/fx-rates              月度汇率快照（M10:V）
//	GET /api/internal/finance/accounts              客户财务账户列表（M10:V + 行级过滤）
//	GET /api/internal/finance/accounts/:customerId  客户财务账户详情（M10:V + 行级过滤）
//
// 红线：金额字符串（decimal）；客户财务数据按 owner_sales_operator_id 做 SELF/DEPT/DEPT_SUB/ALL
// 行级过滤（红线 7）；不返回 cost/floor/margin 等成本字段。
package api

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/finance"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
)

// FinanceHandler 是财务中心接口处理器（只读三接口 + 汇率锁定）。
type FinanceHandler struct {
	svc     *finance.ReadService
	lockSvc *finance.LockService
}

// NewFinanceHandler 构造。lockSvc 为 nil 时不提供锁定接口（路由侧也会跳过）。
func NewFinanceHandler(svc *finance.ReadService, lockSvc *finance.LockService) *FinanceHandler {
	return &FinanceHandler{svc: svc, lockSvc: lockSvc}
}

// financeScopeOf 把 middleware.Operator 投影到 finance.OwnerScope（行级过滤）。
func financeScopeOf(op *middleware.Operator) finance.OwnerScope {
	return finance.OwnerScope{
		DataScope:  op.DataScope,
		StaffID:    op.StaffID,
		MyOrgID:    op.MyOrgID,
		ScopePaths: op.ScopePaths,
	}
}

// financeErrToAppErr 领域错误 → HTTP 错误码。
func financeErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, finance.ErrCustomerNotFound):
		return apperr.New(apperr.ErrNotFound.Code, err.Error(), apperr.ErrNotFound.Status) // 404
	case errors.Is(err, finance.ErrCustomerOutOfScope):
		return apperr.New(apperr.ErrForbidden.Code, err.Error(), apperr.ErrForbidden.Status) // 403
	default:
		return apperr.ErrSystem
	}
}

// ListFxRates 月度汇率快照。
//
// @Summary 月度汇率快照（M10:V；美元兑人民币固定按 USD/CNY）
// @Description 返回按月锁定的汇率档位。status 三态：当月已锁定=LOCKED（含未来月）、早于当月=EXPIRED、当月无记录=PENDING_LOCK（补一条占位行，非数据库记录）。金额/汇率为字符串。表内无数据时也会返回当月占位行，不造历史演示数据。
// @Tags 财务中心
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码" default(1)
// @Param size query int false "每页（上限 200）" default(20)
// @Param month query string false "月份（YYYY-MM）精确匹配"
// @Param status query string false "LOCKED / PENDING_LOCK / EXPIRED"
// @Param search query string false "模糊匹配月份或锁定人姓名"
// @Success 200 {object} api.APIResponse "code=0；data={list,total,page,size}"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M10:V"
// @Router /api/internal/finance/fx-rates [get]
func (h *FinanceHandler) ListFxRates(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	res, err := h.svc.ListFxRates(c.Request.Context(),
		c.Query("month"), c.Query("status"), c.Query("search"), page, size)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// ListAccounts 客户财务账户列表（行级过滤）。
//
// @Summary 客户财务账户列表（M10:V；行级过滤：SELF/DEPT/DEPT_SUB/ALL）
// @Description 返回 customer_profile + legal_subject。availableCredit = credit_limit - credit_used（服务层 decimal 计算）。行级过滤按 owner_sales_operator_id（销售归属）。customerCode = 统一社会信用代码（个人主体空），currency 固定 CNY。不返回成本字段。
// @Tags 财务中心
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码" default(1)
// @Param size query int false "每页（上限 200）" default(20)
// @Param search query string false "模糊匹配客户名称或编码（uscc）"
// @Param status query string false "NORMAL / WARNING / FROZEN"
// @Success 200 {object} api.APIResponse "code=0；data={list,total,page,size}"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M10:V"
// @Router /api/internal/finance/accounts [get]
func (h *FinanceHandler) ListAccounts(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	res, err := h.svc.ListAccounts(c.Request.Context(), financeScopeOf(op),
		c.Query("search"), c.Query("status"), page, size)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// GetAccount 客户财务账户详情。
//
// @Summary 客户财务账户详情（M10:V；行级过滤；含近期流水）
// @Description 在账户摘要基础上追加 billingEntity/invoiceTitle/taxNoMasked/settlementCycle/recentTransactions。recentTransactions 合并 credit_txn 与 deposit_txn，按时间倒序取最近 10 条；方向映射：CONSUME→CREDIT_USAGE、REPAY→CREDIT_RELEASE、ADJUST→CREDIT_USAGE；PAY→DEPOSIT_IN、DEDUCT/REFUND→DEPOSIT_OUT。越域 403，不存在 404。
// @Tags 财务中心
// @Produce json
// @Security BearerAuth
// @Param customerId path int64 true "customer_profile.id"
// @Success 200 {object} api.APIResponse "code=0；data={...AccountDetail}"
// @Failure 400 {object} api.APIResponse "code=10001 路径参数非法"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M10:V 或客户超出数据域"
// @Failure 404 {object} api.APIResponse "code=10004 客户不存在"
// @Router /api/internal/finance/accounts/{customerId} [get]
func (h *FinanceHandler) GetAccount(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("customerId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customerId 非法", apperr.ErrInvalidParams.Status))
		return
	}
	res, err := h.svc.GetAccount(c.Request.Context(), id, financeScopeOf(op))
	if err != nil {
		response.Error(c, financeErrToAppErr(err))
		return
	}
	response.Success(c, res)
}

// fxLockErrToAppErr 汇率锁定领域错误 → HTTP 错误码。
func fxLockErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, finance.ErrFxTierInvalid),
		errors.Is(err, finance.ErrFxAdjustReasonRequired):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status) // 400
	default:
		return apperr.ErrSystem
	}
}

// lockBody 是 POST /fx-lock 请求体。
type lockBody struct {
	FxTier       string `json:"fx_tier"`
	AdjustReason string `json:"adjust_reason"`
}

// GetFxLock 当月汇率锁定状态。
//
// @Summary 当月汇率锁定状态（M10:V；含 8 档可选值）
// @Description 返回当月锁定状态（LOCKED/PENDING_LOCK）与 8 档可选档位。已锁定时回填 fxTier/lockedAt/lockedBy/adjustReason；未锁定时 fxTier 为空串。
// @Tags 财务中心
// @Produce json
// @Security BearerAuth
// @Success 200 {object} api.APIResponse "code=0；data={month,status,fxTier,lockedAt,lockedBy,adjustReason,tiers}"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M10:V"
// @Router /api/internal/finance/fx-lock [get]
func (h *FinanceHandler) GetFxLock(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	if h.lockSvc == nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	res, err := h.lockSvc.GetCurrent(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, res)
}

// PostFxLock 锁定/调整当月汇率档位。
//
// @Summary 锁定/调整当月汇率档位（M10:E + 服务层仅 FINANCE；幂等必填 Idempotency-Key）
// @Description fx_tier 必须 ∈ 固定 8 档（6.5/6.7/6.75/6.8/6.85/6.9/6.95/7.0），否则 400。当月无记录 → 首次锁定（adjust_reason 可空）；当月已有记录 → 视为调整，adjust_reason 必填否则 400。服务层二道校验 perm.CanLockFx 只放行 FINANCE（即使非财务角色被配了 M10:E 也锁不了）。写操作同事务落 audit_log。月中调整本批为简化版（直接写 + 强制理由 + 审计，不含审批链）。
// @Tags 财务中心
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "幂等键"
// @Param body body lockBody true "fx_tier 必填（8 档之一）；adjust_reason 调整时必填"
// @Success 200 {object} api.APIResponse "code=0；data={month,fxTier,status,adjusted,lockedAt,lockedBy}"
// @Failure 400 {object} api.APIResponse "code=10001 档位非法 / 调整缺理由"
// @Failure 401 {object} api.APIResponse "code=10002 未认证"
// @Failure 403 {object} api.APIResponse "code=10003 无 M10:E 或角色非 FINANCE"
// @Failure 409 {object} api.APIResponse "code=10005 幂等冲突"
// @Router /api/internal/finance/fx-lock [post]
func (h *FinanceHandler) PostFxLock(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	// 服务层二道校验：仅 FINANCE 可锁汇率（即使被误配 M10:E 也不放行）。
	if !perm.AnyRoleCan(op.Roles, perm.CanLockFx) {
		response.Error(c, apperr.New(
			apperr.ErrForbidden.Code,
			fmt.Sprintf("角色 %v 不含 FINANCE，不允许锁定汇率（M10:E 服务层角色收敛）", op.Roles),
			apperr.ErrForbidden.Status,
		))
		return
	}
	var body lockBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, fmt.Sprintf("JSON 解析失败：%s", err.Error()), apperr.ErrInvalidParams.Status))
		return
	}
	if h.lockSvc == nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	res, err := h.lockSvc.Lock(c.Request.Context(),
		finance.LockInput{FxTier: body.FxTier, AdjustReason: body.AdjustReason},
		op.OperatorID, operatorRoleOf(op), requestIDOf(c))
	if err != nil {
		response.Error(c, fxLockErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}
