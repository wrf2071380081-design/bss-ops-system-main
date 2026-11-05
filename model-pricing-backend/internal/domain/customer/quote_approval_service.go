// Package customer 的 quote_approval_service.go：客户报价单审批（原型「提交审批 → 超管逐模型批准」）。
//
// 业务链路（与 000057 迁移头注释一致）：
//
//	DRAFT --submit--> PENDING --全部批准--> APPROVED
//	                          --任一驳回--> DRAFT（改价后可重新提交）
//	                          --撤回-----> DRAFT
//
// 与「特价审批」（change_request[SPECIAL_PRICE]）区分：
//   - 特价审批批的是「允许破线」，两步 PRICING_OP → FINANCE，走 approval_step；
//   - 这里批的是「这张报价单这批模型可以对外」，一步、超管（M9:A）、逐模型。
//
// 为什么不用 approval_step：那套是「一步一个决定」，而这里要「一个模型一个决定 + 驳回原因」，
// 粒度对不上；硬塞会把逐模型结论压成一个字符串。
package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 报价单审批状态。
const (
	QuoteApprovalPending   = "PENDING"
	QuoteApprovalApproved  = "APPROVED"
	QuoteApprovalRejected  = "REJECTED"
	QuoteApprovalWithdrawn = "WITHDRAWN"
	// 逐模型决定
	ApprovalRowPending  = "PENDING"
	ApprovalRowApproved = "APPROVED"
	ApprovalRowRejected = "REJECTED"
)

// 领域错误。
var (
	ErrQuoteApprovalNotFound    = errors.New("报价单审批不存在")
	ErrQuoteApprovalNotPending  = errors.New("该审批单已结束，不能重复处理")
	ErrQuoteApprovalSelfReview  = errors.New("不能审批自己提交的报价单")
	ErrQuoteApprovalNotOwner    = errors.New("只有提交人本人可以撤回")
	ErrQuoteApprovalQuoteStatus = errors.New("只有草稿状态的报价单可以提交审批")
	ErrQuoteApprovalNoModels    = errors.New("报价单还没有可审批的模型")
	ErrQuoteApprovalRows        = errors.New("请逐模型给出结论（每个模型都要批准或驳回）")
	ErrQuoteApprovalReason      = errors.New("驳回必须填写原因")
	// ErrApprovalStoreUnavailable 能力未接线（进程装配漏挂），属服务端错误。
	ErrApprovalStoreUnavailable = errors.New("报价审批能力不可用")
)

// QuoteApprovalRow 是审批单里的一个模型。
type QuoteApprovalRow struct {
	ID              int64      `json:"id"`
	StandardModelID int64      `json:"standard_model_id"`
	ModelName       string     `json:"model_name"`
	OfficialID      string     `json:"official_id"`
	ItemCount       int        `json:"item_count"`
	PriceMin        string     `json:"price_min"`
	PriceMax        string     `json:"price_max"`
	Result          string     `json:"result"`
	Reason          string     `json:"reason"`
	DecidedBy       *int64     `json:"decided_by"`
	DecidedAt       *time.Time `json:"decided_at"`
}

// QuoteApproval 是一张审批单（含逐模型行）。
type QuoteApproval struct {
	ID           int64     `json:"id"`
	QuoteID      int64     `json:"quote_id"`
	Status       string    `json:"status"`
	ModelCount   int       `json:"model_count"`
	Note         string    `json:"note"`
	SubmittedBy  int64     `json:"submitted_by"`
	SubmittedAt  time.Time `json:"submitted_at"`
	DecidedBy    *int64    `json:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at"`
	CustomerID   int64     `json:"customer_id"`
	QuoteVersion int       `json:"quote_version"`
	QuoteStatus  string    `json:"quote_status"`
	Rows         []QuoteApprovalRow `json:"rows"`
}

// QuoteApprovalSummary 是列表行（不带逐模型明细）。
type QuoteApprovalSummary struct {
	ID          int64      `json:"id"`
	QuoteID     int64      `json:"quote_id"`
	CustomerID  int64      `json:"customer_id"`
	Status      string     `json:"status"`
	ModelCount  int        `json:"model_count"`
	SubmittedBy int64      `json:"submitted_by"`
	SubmittedAt time.Time  `json:"submitted_at"`
	DecidedBy   *int64     `json:"decided_by"`
	DecidedAt   *time.Time `json:"decided_at"`
}

// ApprovalDecision 是「某个模型批 / 驳」。
type ApprovalDecision struct {
	RowID  int64  `json:"row_id"`
	Result string `json:"result"`
	Reason string `json:"reason"`
}

// QuoteApprovalStore 是审批所需的存储能力（实现见 repo/customer_quote_approval.go）。
type QuoteApprovalStore interface {
	// CheckQuoteOwnerScope 行级数据域校验（红线 7）：越域 → inScope=false。
	CheckQuoteOwnerScope(ctx context.Context, quoteID int64, scope OwnerScope) (found, inScope bool, err error)
	SubmitQuoteApprovalTx(ctx context.Context, quoteID int64, operatorID int64, requestID string) (*QuoteApproval, error)
	DecideQuoteApprovalTx(ctx context.Context, approvalID int64, decisions []ApprovalDecision, operatorID int64, requestID string) (*QuoteApproval, error)
	WithdrawQuoteApprovalTx(ctx context.Context, approvalID int64, operatorID int64, requestID string) (*QuoteApproval, error)
	LoadQuoteApproval(ctx context.Context, approvalID int64) (*QuoteApproval, error)
	ListQuoteApprovals(ctx context.Context, customerID int64, status string, limit int) ([]QuoteApprovalSummary, error)
}

// QuoteApprovalService 报价单审批服务。
type QuoteApprovalService struct {
	store QuoteApprovalStore
}

// NewQuoteApprovalService 构造。
func NewQuoteApprovalService(store QuoteApprovalStore) *QuoteApprovalService {
	return &QuoteApprovalService{store: store}
}

// Submit 提交审批（M9:E）。只有 DRAFT 且有模型的报价单能提交。
func (s *QuoteApprovalService) Submit(ctx context.Context, quoteID int64, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	if quoteID <= 0 {
		return nil, ErrQuoteNotFound
	}
	found, inScope, err := s.store.CheckQuoteOwnerScope(ctx, quoteID, scope)
	if err != nil {
		return nil, fmt.Errorf("check quote scope: %w", err)
	}
	if !found {
		return nil, ErrQuoteNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	return s.store.SubmitQuoteApprovalTx(ctx, quoteID, operatorID, requestID)
}

// Decide 逐模型审批（M9:A）。同一单不能由提交人自己批。
func (s *QuoteApprovalService) Decide(ctx context.Context, approvalID int64, decisions []ApprovalDecision, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	current, err := s.loadInScope(ctx, approvalID, scope)
	if err != nil {
		return nil, err
	}
	if current.Status != QuoteApprovalPending {
		return nil, ErrQuoteApprovalNotPending
	}
	if current.SubmittedBy == operatorID {
		return nil, ErrQuoteApprovalSelfReview
	}
	normalized, err := NormalizeDecisions(decisions, current.Rows)
	if err != nil {
		return nil, err
	}
	return s.store.DecideQuoteApprovalTx(ctx, approvalID, normalized, operatorID, requestID)
}

// Withdraw 撤回（M9:E）：只有提交人本人、且仍在待审批。
func (s *QuoteApprovalService) Withdraw(ctx context.Context, approvalID int64, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	current, err := s.loadInScope(ctx, approvalID, scope)
	if err != nil {
		return nil, err
	}
	if current.Status != QuoteApprovalPending {
		return nil, ErrQuoteApprovalNotPending
	}
	if current.SubmittedBy != operatorID {
		return nil, ErrQuoteApprovalNotOwner
	}
	return s.store.WithdrawQuoteApprovalTx(ctx, approvalID, operatorID, requestID)
}

// Get 读审批单（M9:V），同样做数据域校验。
func (s *QuoteApprovalService) Get(ctx context.Context, approvalID int64, scope OwnerScope) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	return s.loadInScope(ctx, approvalID, scope)
}

// List 列审批单（M9:V）。
//
// customerID=0 表示跨客户：客户报价单页签的「报价审批」是全局队列（原型 3.10，
// 分「待审批 / 审批记录」两个子标签）。仍受 limit（默认 200）约束，不是无界翻页。
func (s *QuoteApprovalService) List(ctx context.Context, customerID int64, status string, limit int) ([]QuoteApprovalSummary, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	if customerID < 0 {
		return nil, ErrCustomerNotFound
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	switch status {
	case "", QuoteApprovalPending, QuoteApprovalApproved, QuoteApprovalRejected, QuoteApprovalWithdrawn:
	default:
		return nil, fmt.Errorf("%w：status 取值非法", ErrInvalidQuoteType)
	}
	return s.store.ListQuoteApprovals(ctx, customerID, status, limit)
}

func (s *QuoteApprovalService) loadInScope(ctx context.Context, approvalID int64, scope OwnerScope) (*QuoteApproval, error) {
	if approvalID <= 0 {
		return nil, ErrQuoteApprovalNotFound
	}
	current, err := s.store.LoadQuoteApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrQuoteApprovalNotFound
	}
	found, inScope, err := s.store.CheckQuoteOwnerScope(ctx, current.QuoteID, scope)
	if err != nil {
		return nil, fmt.Errorf("check quote scope: %w", err)
	}
	if !found {
		return nil, ErrQuoteApprovalNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	return current, nil
}

// NormalizeDecisions 纯函数：校验并归一化逐模型决定。
//
// 口径：本次必须覆盖**当前所有待决定**的模型，一个都不能漏——漏一个就落单，
// 再补一次会变成"分批审批"，和原型「一次把每个模型批完」不一致，也会让并发很难讲清。
func NormalizeDecisions(input []ApprovalDecision, rows []QuoteApprovalRow) ([]ApprovalDecision, error) {
	pending := make(map[int64]bool, len(rows))
	for _, row := range rows {
		if row.Result == ApprovalRowPending {
			pending[row.ID] = true
		}
	}
	if len(pending) == 0 {
		return nil, ErrQuoteApprovalNotPending
	}
	seen := make(map[int64]bool, len(input))
	out := make([]ApprovalDecision, 0, len(input))
	for _, decision := range input {
		if !pending[decision.RowID] || seen[decision.RowID] {
			return nil, ErrQuoteApprovalRows
		}
		result := strings.ToUpper(strings.TrimSpace(decision.Result))
		if result != ApprovalRowApproved && result != ApprovalRowRejected {
			return nil, ErrQuoteApprovalRows
		}
		reason := strings.TrimSpace(decision.Reason)
		if result == ApprovalRowRejected && reason == "" {
			return nil, ErrQuoteApprovalReason
		}
		if len([]rune(reason)) > 200 {
			reason = string([]rune(reason)[:200])
		}
		seen[decision.RowID] = true
		out = append(out, ApprovalDecision{RowID: decision.RowID, Result: result, Reason: reason})
	}
	if len(out) != len(pending) {
		return nil, ErrQuoteApprovalRows
	}
	return out, nil
}
