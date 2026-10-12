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
	"strconv"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/finance"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// FinanceHandler 是财务中心只读接口处理器。
type FinanceHandler struct {
	svc *finance.ReadService
}

// NewFinanceHandler 构造。
func NewFinanceHandler(svc *finance.ReadService) *FinanceHandler {
	return &FinanceHandler{svc: svc}
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
