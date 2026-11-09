// Package api 的 customer_quote_approval.go：客户报价单审批（原型「提交审批 → 超管逐模型批准」）。
//
// 权限：
//   - POST /customer-quotes/:id/submit-approval        M9:E + 幂等（销售提交）
//   - GET  /customer-quote-approvals/:id               M9:V
//   - POST /customer-quote-approvals/:id/decision      M9:A + 幂等（超管逐模型批准/驳回）
//   - POST /customer-quote-approvals/:id/withdraw      M9:E + 幂等（提交人撤回）
//   - GET  /customer-workspace/quote-approvals         M9:V（客户详情「报价审批」列表）
package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/customer"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// QuoteApprovalHandler 报价单审批。
type QuoteApprovalHandler struct {
	svc *customer.QuoteApprovalService
}

// NewQuoteApprovalHandler 构造。
func NewQuoteApprovalHandler(svc *customer.QuoteApprovalService) *QuoteApprovalHandler {
	return &QuoteApprovalHandler{svc: svc}
}

// quoteApprovalErrToAppErr 领域错误 → HTTP 错误码。
//
// 409 与 403 刻意分开：
//   - 自审 / 非提交人撤回 → 403（没这个权限，换谁来都不行）
//   - 已结束 / 报价单状态不对 / 已有待审批单 → 409（换个时机可能就行）
func quoteApprovalErrToAppErr(err error) apperr.Error {
	switch {
	case errors.Is(err, customer.ErrQuoteApprovalNotFound), errors.Is(err, customer.ErrQuoteNotFound):
		return apperr.New(apperr.ErrNotFound.Code, err.Error(), apperr.ErrNotFound.Status)
	case errors.Is(err, customer.ErrCustomerOutOfScope),
		errors.Is(err, customer.ErrQuoteApprovalSelfReview),
		errors.Is(err, customer.ErrQuoteApprovalNotApprover),
		errors.Is(err, customer.ErrQuoteApprovalNotOwner):
		return apperr.New(apperr.ErrForbidden.Code, err.Error(), apperr.ErrForbidden.Status)
	case errors.Is(err, customer.ErrQuoteApprovalNotPending),
		errors.Is(err, customer.ErrQuoteAlreadyPending),
		errors.Is(err, customer.ErrQuoteApprovalQuoteStatus):
		return apperr.New(apperr.ErrIdempotency.Code, err.Error(), apperr.ErrIdempotency.Status)
	case errors.Is(err, customer.ErrQuoteApprovalNoModels),
		errors.Is(err, customer.ErrQuoteApprovalRows),
		errors.Is(err, customer.ErrQuoteApprovalReason),
		errors.Is(err, customer.ErrQuoteApprovalNoteRequired),
		errors.Is(err, customer.ErrQuoteApprovalOpsAssignee),
		errors.Is(err, customer.ErrQuoteApprovalBizAssignee),
		errors.Is(err, customer.ErrQuoteApprovalWithdrawNote),
		errors.Is(err, customer.ErrQuoteApprovalReapplyModel),
		errors.Is(err, customer.ErrQuoteApprovalReapplyScope),
		errors.Is(err, customer.ErrQuoteApprovalReapplyStatus),
		errors.Is(err, customer.ErrQuoteApprovalReapplyOther),
		errors.Is(err, customer.ErrQuoteApprovalReapplyNoRows),
		errors.Is(err, customer.ErrInvalidQuoteType):
		return apperr.New(apperr.ErrInvalidParams.Code, err.Error(), apperr.ErrInvalidParams.Status)
	default:
		return apperr.ErrSystem
	}
}

// submitQuoteApprovalBody 提交审批请求体。
//
// previous_approval_id + model_ids 是「关联驳回模型重报」：只对被驳回的模型再走一次审批。
type submitQuoteApprovalBody struct {
	PreviousApprovalID int64   `json:"previous_approval_id"`
	ModelIDs           []int64 `json:"model_ids"`
}

// SubmitQuoteApproval 提交报价单审批（M9:E + 幂等）。
//
// @Summary 提交报价单审批（M9:E + 幂等必填 Idempotency-Key）
// @Description 把 DRAFT 报价单改成 PENDING，并按标准模型生成逐模型待决行（价项只做数量与价格区间快照）。同一报价单同时只能有一个待审批单。带 previous_approval_id + model_ids 时是关联驳回模型重报：模型必须在来源审批的驳回行里。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_quote.id"
// @Param Idempotency-Key header string true "幂等键"
// @Success 200 {object} api.APIResponse "code=0；data=QuoteApproval（含 rows）"
// @Failure 400 {object} api.APIResponse "code=10001 报价单没有可审批的模型 / 重报范围非法"
// @Failure 403 {object} api.APIResponse "code=10003 无 M9:E / 报价不在数据域内"
// @Failure 404 {object} api.APIResponse "code=10004 报价不存在"
// @Failure 409 {object} api.APIResponse "code=10005 报价单不是草稿 / 已存在待审批单"
// @Router /api/internal/customer-quotes/{id}/submit-approval [post]
func (h *QuoteApprovalHandler) SubmitQuoteApproval(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	// body 可为空（老前端只发 lifecycle），解析失败不阻断，走普通首次提交。
	var body submitQuoteApprovalBody
	_ = c.ShouldBindJSON(&body)
	res, err := h.svc.Submit(c.Request.Context(), customer.SubmitInput{
		QuoteID:            id,
		PreviousApprovalID: body.PreviousApprovalID,
		ModelIDs:           body.ModelIDs,
	}, scopeOf(op), op.OperatorID, requestIDOf(c))
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}

// GetQuoteApproval 读审批单（M9:V）。
//
// @Summary 报价单审批详情（M9:V）
// @Description 返回审批单、逐模型决定行与报价单当前状态。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_quote_approval.id"
// @Success 200 {object} api.APIResponse "code=0；data=QuoteApproval"
// @Failure 403 {object} api.APIResponse "code=10003 报价不在数据域内"
// @Failure 404 {object} api.APIResponse "code=10004 审批单不存在"
// @Router /api/internal/customer-quote-approvals/{id} [get]
func (h *QuoteApprovalHandler) GetQuoteApproval(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	res, err := h.svc.Get(c.Request.Context(), id, scopeOf(op))
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	response.Success(c, res)
}

// decideQuoteApprovalBody 逐模型审批请求体（对齐原型弹窗的四个必填）。
type decideQuoteApprovalBody struct {
	Rows []customer.ApprovalDecision `json:"rows"`
	// Note 处理意见（必填，进审批摘要）。
	Note string `json:"note"`
	// OpsAssigneeID 技术办理人（有批准模型时必填）。
	OpsAssigneeID int64 `json:"ops_assignee_id"`
	// BusinessAssigneeID 必要商务办理人（客户合同未归档时必填）。
	BusinessAssigneeID int64 `json:"business_assignee_id"`
}

// withdrawQuoteApprovalBody 撤回请求体：原因必填（对齐原型撤回弹窗）。
type withdrawQuoteApprovalBody struct {
	Reason string `json:"reason"`
}

// DecideQuoteApproval 逐模型批准/驳回（M9:A + 幂等）。
//
// @Summary 报价单逐模型审批（M9:A + 幂等必填 Idempotency-Key）
// @Description 必须覆盖当前全部待决模型，一个都不能漏；驳回必须填原因、处理意见必填；有批准模型时 ops_assignee_id 必填，客户合同未归档时 business_assignee_id 必填。全部批准 → 审批单 APPROVED、报价单 APPROVED 并派发两条待办；部分批准 → PARTIAL、只对批准模型派发；全部驳回 → REJECTED、报价单回 DRAFT（改价后可重提）。提交人不能自审。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_quote_approval.id"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body decideQuoteApprovalBody true "rows[{row_id,result,reason}]"
// @Success 200 {object} api.APIResponse "code=0；data=QuoteApproval"
// @Failure 400 {object} api.APIResponse "code=10001 逐模型结论不完整 / 驳回缺原因"
// @Failure 403 {object} api.APIResponse "code=10003 无 M9:A / 自审 / 越域"
// @Failure 404 {object} api.APIResponse "code=10004 审批单不存在"
// @Failure 409 {object} api.APIResponse "code=10005 审批单已结束或已被他人处理"
// @Router /api/internal/customer-quote-approvals/{id}/decision [post]
func (h *QuoteApprovalHandler) DecideQuoteApproval(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	var body decideQuoteApprovalBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "JSON 解析失败：请提供 rows[{row_id,result,reason}] 与 note/ops_assignee_id", apperr.ErrInvalidParams.Status))
		return
	}
	res, err := h.svc.Decide(c.Request.Context(), id, customer.DecideInput{
		Rows:               body.Rows,
		Note:               body.Note,
		OpsAssigneeID:      body.OpsAssigneeID,
		BusinessAssigneeID: body.BusinessAssigneeID,
	}, scopeOf(op), op.OperatorID, requestIDOf(c))
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}

// SaveQuoteApprovalDraft 保存逐模型审批进度（M9:A + 幂等）。
//
// @Summary 保存报价审批草稿（M9:A + 幂等必填 Idempotency-Key）
// @Description 允许不完整：只存已填的逐模型结论、处理意见与办理人，不改变审批状态；提交结论后草稿自动清空。
// @Tags 客户报价
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_quote_approval.id"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body decideQuoteApprovalBody true "rows[{row_id,result,reason}] + note + 两个办理人（都可空）"
// @Success 200 {object} api.APIResponse "code=0；data=QuoteApproval"
// @Failure 403 {object} api.APIResponse "code=10003 无 M9:A / 自审 / 越域"
// @Failure 404 {object} api.APIResponse "code=10004 审批单不存在"
// @Failure 409 {object} api.APIResponse "code=10005 审批单已结束"
// @Router /api/internal/customer-quote-approvals/{id}/draft [post]
func (h *QuoteApprovalHandler) SaveQuoteApprovalDraft(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	var body decideQuoteApprovalBody
	_ = c.ShouldBindJSON(&body)
	res, err := h.svc.SaveDraft(c.Request.Context(), id, customer.DecideInput{
		Rows:               body.Rows,
		Note:               body.Note,
		OpsAssigneeID:      body.OpsAssigneeID,
		BusinessAssigneeID: body.BusinessAssigneeID,
	}, scopeOf(op), op.OperatorID, requestIDOf(c))
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}

// WithdrawQuoteApproval 撤回审批（M9:E + 幂等，仅提交人）。
//
// @Summary 撤回报价单审批（M9:E + 幂等必填 Idempotency-Key）
// @Description 仅提交人本人、且审批单仍在 PENDING 时可撤回；撤回原因必填（body.reason）；撤回后报价单回 DRAFT。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param id path int64 true "customer_quote_approval.id"
// @Param Idempotency-Key header string true "幂等键"
// @Success 200 {object} api.APIResponse "code=0；data=QuoteApproval"
// @Failure 403 {object} api.APIResponse "code=10003 无 M9:E / 非提交人 / 越域"
// @Failure 404 {object} api.APIResponse "code=10004 审批单不存在"
// @Failure 409 {object} api.APIResponse "code=10005 审批单已结束"
// @Router /api/internal/customer-quote-approvals/{id}/withdraw [post]
func (h *QuoteApprovalHandler) WithdrawQuoteApproval(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "id 非法", apperr.ErrInvalidParams.Status))
		return
	}
	// body 允许为空对象：老前端还在只发 lifecycle，服务端会给「请填写撤回原因」的业务提示。
	var body withdrawQuoteApprovalBody
	_ = c.ShouldBindJSON(&body)
	res, err := h.svc.Withdraw(c.Request.Context(), id, body.Reason, scopeOf(op), op.OperatorID, requestIDOf(c))
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	middleware.SetIdempotencyResult(c, res)
	response.Success(c, res)
}

// ListQuoteApprovals 审批单列表（M9:V）。
//
// @Summary 客户报价审批列表（M9:V）
// @Description 按客户列审批单，可选 status=PENDING/APPROVED/REJECTED/WITHDRAWN。
// @Description 不传 customer_id 表示跨客户（客户报价单页签的全局审批队列），仍受 200 条上限约束。
// @Tags 客户报价
// @Produce json
// @Security BearerAuth
// @Param customer_id query int64 false "customer_profile.id；不传=全部客户"
// @Param status query string false "PENDING / APPROVED / REJECTED / WITHDRAWN"
// @Success 200 {object} api.APIResponse "code=0；data={list:[QuoteApprovalSummary]}"
// @Failure 400 {object} api.APIResponse "code=10001 customer_id 非法"
// @Router /api/internal/customer-workspace/quote-approvals [get]
func (h *QuoteApprovalHandler) ListQuoteApprovals(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	var customerID int64
	if raw := c.Query("customer_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "customer_id 必须是正整数", apperr.ErrInvalidParams.Status))
			return
		}
		customerID = parsed
	}
	var submittedBy int64
	if raw := c.Query("submitted_by"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "submitted_by 必须是正整数", apperr.ErrInvalidParams.Status))
			return
		}
		submittedBy = parsed
	}
	// 时间区间：前端传 ISO 日期或日期时间，按「含首含尾」处理（日期只给到天时补到当天 23:59:59）。
	dateFrom, err := parseApprovalDate(c.Query("date_from"), false)
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "date_from 不是合法时间", apperr.ErrInvalidParams.Status))
		return
	}
	dateTo, err := parseApprovalDate(c.Query("date_to"), true)
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "date_to 不是合法时间", apperr.ErrInvalidParams.Status))
		return
	}
	q := customer.QuoteApprovalQuery{
		CustomerID:  customerID,
		Status:      c.Query("status"),
		SubmittedBy: submittedBy,
		Keyword:     c.Query("keyword"),
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		Stage:       c.Query("stage"),
		Limit:       200,
	}
	// 只有超管（can_approve_customer_quote）能看整条审批队列。销售也持 M9:A，
	// 但报价审批不该由销售角色顺带获得。对销售不返回 403 也不返回空：
	// 强制收窄成「他自己提交的」，这样客户详情里仍能看到自己的单走到哪一步，
	// 又不会看到别人的报价与批价结论。
	allowed, err := h.svc.CanApproveCustomerQuote(c.Request.Context(), op.OperatorID)
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	if !allowed {
		q.SubmittedBy = op.OperatorID
		if customerID <= 0 {
			// 跨客户的全局队列对销售没有意义：他自己的单在哪个客户下，他自己清楚。
			response.Success(c, gin.H{"list": []customer.QuoteApprovalSummary{}})
			return
		}
	}
	list, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Error(c, quoteApprovalErrToAppErr(err))
		return
	}
	response.Success(c, gin.H{"list": list})
}

// parseApprovalDate 解析列表筛选的时间。
//
// 只给日期（2026-11-07）时：起始按当天 00:00:00，结束按当天 23:59:59——
// 否则「截止=今天」会把今天的单全筛掉，用户会以为数据丢了。
func parseApprovalDate(raw string, endOfDay bool) (*time.Time, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			if layout == "2006-01-02" && endOfDay {
				parsed = parsed.Add(24*time.Hour - time.Second)
			}
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("unsupported date: %s", raw)
}
