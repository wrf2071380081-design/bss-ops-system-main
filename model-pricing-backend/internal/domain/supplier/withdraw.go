// Package supplier 的 withdraw.go：供应商自助撤回报价（产品反馈 2 的裁决）。
//
// 裁决口径：**允许审批完成前撤回**——供应商提交后若发现报错，在平台审批人做出结论之前
// 可以自己撤回，撤回后该单不再阻塞新提交（FindNonTerminalQuote 不含 VOIDED）。
//
// 为什么不做「已审批单也能撤回」：APPROVED_PENDING/EFFECTIVE 已产生审批结论或已生效，
// 撤回等于推翻他人决策，属另一条业务流（另需撤销审批/下线流程），本批不实现。
package supplier

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// ---- 领域错误 ----

var (
	// ErrQuoteNotWithdrawable 撤回前置状态非法（非 SUBMITTED/APPROVING）。
	//
	// 注意：库里实际落盘几乎恒为 APPROVING（SUBMITTED→APPROVING 同事务瞬时完成，
	// 见 repo.CreateQuoteWithTodo），SUBMITTED 保留在白名单里是为了兼容历史数据与导入链路。
	ErrQuoteNotWithdrawable = errors.New("报价单状态不允许撤回（仅审批完成前可撤回）")
	// ErrQuoteWithdrawReasonTooLong 撤回原因超长（按 rune 计，上限 500）。
	ErrQuoteWithdrawReasonTooLong = errors.New("撤回原因不超过500字")
)

// ---- 状态常量 ----

// 报价单状态中与撤回相关的三个值。
// 其它状态（APPROVED_PENDING / EFFECTIVE / EXPIRED / REJECTED）一律不可撤回。
const (
	QuoteStatusSubmitted = "SUBMITTED"
	QuoteStatusApproving = "APPROVING"
	QuoteStatusVoided    = "VOIDED"
)

// maxWithdrawReasonRunes 是撤回原因的长度上限（与驳回原因 500 字同口径）。
const maxWithdrawReasonRunes = 500

// IsWithdrawableStatus 报告该状态是否允许撤回。
//
// 与 approve.go 的 ErrQuoteNotApprovable 同构：白名单而不是黑名单——
// 新增状态默认不可撤回，避免"新状态忘了加判断"直接放行。
func IsWithdrawableStatus(status string) bool {
	return status == QuoteStatusSubmitted || status == QuoteStatusApproving
}

// ---- DTO ----

// WithdrawInput 是撤回报价的请求体。Reason 选填（供应商可只点撤回不写原因），
// 但一旦填写就写入 audit_log.reason 留痕。
type WithdrawInput struct {
	Reason string `json:"reason"`
}

// WithdrawResult 是撤回报价的响应。
type WithdrawResult struct {
	ID          int64     `json:"id"`
	Status      string    `json:"status"`
	WithdrawnAt time.Time `json:"withdrawn_at"`
}

// WithdrawParams 是 WithdrawQuote 的落库参数。
//
// SupplierID 是**必须**的：行级过滤由 UPDATE ... WHERE supplier_id = ? 保证，
// 供应商门户绝不能靠"先查后改"两段式判断归属（TOCTOU + 漏一处即越权）。
type WithdrawParams struct {
	QuoteID      int64
	SupplierID   int64
	Reason       string
	OperatorID   int64
	OperatorRole string
	RequestID    string
	Now          time.Time
}

// ---- 领域服务 ----

// WithdrawQuote 撤回报价（供应商自助）。
//
// 校验顺序：原因长度（廉价校验先做）→ 仓储条件更新（状态 + 归属一次判完）。
// 归属不符与状态非法都返回 ErrQuoteNotWithdrawable，不区分 403/404——
// 与 GetQuote 同口径（"不是自己的单"按不存在处理，避免探测他人 ID）。
func (s *QuoteService) WithdrawQuote(ctx context.Context, supplierID, quoteID int64, in WithdrawInput, operatorID int64, requestID string) (*WithdrawResult, error) {
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > maxWithdrawReasonRunes {
		return nil, ErrQuoteWithdrawReasonTooLong
	}
	return s.store.WithdrawQuote(ctx, WithdrawParams{
		QuoteID:      quoteID,
		SupplierID:   supplierID,
		Reason:       reason,
		OperatorID:   operatorID,
		OperatorRole: "SUPPLIER",
		RequestID:    requestID,
		Now:          s.now(),
	})
}
