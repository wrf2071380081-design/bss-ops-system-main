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
//
// Partial（部分批准）是对齐原型的关键状态：原型里「批的进办理、驳的可重报」，
// 不能像 000057 那样「任一驳回就整单退回」，否则一个模型有问题会把其他模型一起卡住。
const (
	QuoteApprovalPending   = "PENDING"
	QuoteApprovalApproved  = "APPROVED"
	QuoteApprovalPartial   = "PARTIAL"
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
	// 原型「处理意见必填」：整单一句结论说明，写进审批摘要。
	ErrQuoteApprovalNoteRequired = errors.New("请填写处理意见")
	// 原型「有批准模型时的技术办理人」必填：批准即派发，必须知道派给谁。
	ErrQuoteApprovalOpsAssignee = errors.New("请选择技术办理人（有批准模型时必填）")
	// 原型「必要商务办理人」：客户合同未归档时必须指定，否则批完没人办合同。
	ErrQuoteApprovalBizAssignee  = errors.New("请选择商务办理人（客户合同与授信尚未归档）")
	ErrQuoteApprovalWithdrawNote = errors.New("请填写撤回原因")
	// ErrQuoteApprovalNotApprover 对齐业务口径：报价审批只有「超管」（can_approve_customer_quote）能做。
	// 与权限点 M9:A 分开——销售也持着 M9:A，但批价不该由销售角色顺带获得。
	ErrQuoteApprovalNotApprover = errors.New("只有超管可以审批客户报价")
	// 关联重报（原型「关联驳回模型新申请」）：新一版报价只对被驳回的模型再走一次审批。
	ErrQuoteApprovalReapplyStatus = errors.New("只有部分批准或已驳回的审批才能作为重报来源")
	ErrQuoteApprovalReapplyOther  = errors.New("重报来源不属于同一个客户")
	ErrQuoteApprovalReapplyModel  = errors.New("关联重报至少要选一个被驳回的模型")
	ErrQuoteApprovalReapplyScope  = errors.New("关联重报只能选择上一张审批里被驳回的模型")
	ErrQuoteApprovalReapplyNoRows = errors.New("新报价单里没有上一轮被驳回的模型，请先为这些模型配置来源并定价")
	// ErrApprovalStoreUnavailable 能力未接线（进程装配漏挂），属服务端错误。
	ErrApprovalStoreUnavailable = errors.New("报价审批能力不可用")
)

// ApprovalPriceItem 是一条价目快照（原型审批详情右侧「报价明细」的一行）。
//
// 带官方价与计价单位：审批人看到「输入 2.2667 USD」并不知道贵不贵，
// 要能对上「官方价 3 USD 的 0.7556」才知道这次批的是几折。
type ApprovalPriceItem struct {
	Key      string `json:"key"`
	Price    string `json:"price"`
	Currency string `json:"currency"`
	// Amount 是官方价（与 Price 同 unit_quantity 口径）；官方价缺失时为空。
	Amount string `json:"amount"`
	// UnitCode / UnitQuantity 用于把「/ 百万 tokens」这类量纲说清楚。
	UnitCode     string `json:"unit_code"`
	UnitQuantity string `json:"unit_quantity"`
	// Coefficient 是客户报价系数 = Price ÷ Amount，保留 4 位；算不出时为空。
	Coefficient string `json:"coefficient"`
}

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
	// PriceDetail 是提交时的价目快照（原型审批详情右侧「报价明细」）。
	PriceDetail []ApprovalPriceItem `json:"price_detail"`
}

// ApprovalDraft 是「保存进度」存下来的审批人填写状态（原型弹窗的草稿）。
//
// 与 ApprovalDecision 的区别：草稿允许不完整——可以只填几个模型、意见空着、
// 办理人还没选。校验只在提交结论时做，草稿的目的是"别让填了一半的内容丢掉"。
type ApprovalDraft struct {
	Rows               []ApprovalDecision `json:"rows"`
	Note               string             `json:"note"`
	OpsAssigneeID      int64              `json:"ops_assignee_id"`
	BusinessAssigneeID int64              `json:"business_assignee_id"`
	SavedBy            int64              `json:"saved_by"`
	SavedAt            *time.Time         `json:"saved_at"`
}

// QuoteApproval 是一张审批单（含逐模型行）。
type QuoteApproval struct {
	ID           int64      `json:"id"`
	QuoteID      int64      `json:"quote_id"`
	Status       string     `json:"status"`
	ModelCount   int        `json:"model_count"`
	Note         string     `json:"note"`
	SubmittedBy  int64      `json:"submitted_by"`
	SubmittedAt  time.Time  `json:"submitted_at"`
	DecidedBy    *int64     `json:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at"`
	CustomerID   int64      `json:"customer_id"`
	CustomerName string     `json:"customer_name"`
	QuoteVersion int        `json:"quote_version"`
	QuoteStatus  string     `json:"quote_status"`
	// 原型「技术办理人 / 必要商务办理人」：审批通过后「客户模型开通」「合同与授信」待办的负责人。
	OpsAssigneeID      *int64 `json:"ops_assignee_id"`
	BusinessAssigneeID *int64 `json:"business_assignee_id"`
	// WithdrawReason 是撤回原因（原型撤回弹窗必填）。
	WithdrawReason string `json:"withdraw_reason"`
	// PreviousApprovalID 是重报来源（关联驳回模型重报），普通提交为 0。
	PreviousApprovalID int64 `json:"previous_approval_id"`
	// Draft 是审批人「保存进度」留下的草稿；提交结论后为 nil。
	Draft *ApprovalDraft     `json:"draft,omitempty"`
	Rows  []QuoteApprovalRow `json:"rows"`
}

// QuoteApprovalSummary 是列表行（不带逐模型明细）。
//
// 原型「报价审批」列表的列是「报价单 ID / 客户 / 销售 / 提交时间 / 模型数 / 状态」，
// 所以这里带上客户名与销售名，避免前端显示成「员工 #12」。
type QuoteApprovalSummary struct {
	ID            int64      `json:"id"`
	QuoteID       int64      `json:"quote_id"`
	CustomerID    int64      `json:"customer_id"`
	CustomerName  string     `json:"customer_name"`
	Status        string     `json:"status"`
	ModelCount    int        `json:"model_count"`
	SubmittedBy   int64      `json:"submitted_by"`
	SubmittedName string     `json:"submitted_name"`
	SubmittedAt   time.Time  `json:"submitted_at"`
	DecidedBy     *int64     `json:"decided_by"`
	DecidedName   string     `json:"decided_name"`
	DecidedAt     *time.Time `json:"decided_at"`
}

// DecideInput 是「超管逐模型审批」的完整入参（对齐原型弹窗的四个必填）。
type DecideInput struct {
	Rows []ApprovalDecision `json:"rows"`
	// Note 是处理意见（原型必填，进审批摘要）。
	Note string `json:"note"`
	// OpsAssigneeID 是技术办理人（有批准模型时必填）。
	OpsAssigneeID int64 `json:"ops_assignee_id"`
	// BusinessAssigneeID 是必要商务办理人（客户合同未归档时必填）。
	BusinessAssigneeID int64 `json:"business_assignee_id"`
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
	SubmitQuoteApprovalTx(ctx context.Context, quoteID, previousApprovalID int64, modelIDs []int64, operatorID int64, requestID string) (*QuoteApproval, error)
	DecideQuoteApprovalTx(ctx context.Context, approvalID int64, in DecideRecord, operatorID int64, requestID string) (*QuoteApproval, error)
	WithdrawQuoteApprovalTx(ctx context.Context, approvalID int64, reason string, operatorID int64, requestID string) (*QuoteApproval, error)
	LoadQuoteApproval(ctx context.Context, approvalID int64) (*QuoteApproval, error)
	ListQuoteApprovals(ctx context.Context, q QuoteApprovalQuery) ([]QuoteApprovalSummary, error)
	// SaveQuoteApprovalDraft 存审批草稿；传 nil 表示清空（提交结论时用）。
	SaveQuoteApprovalDraft(ctx context.Context, approvalID int64, draft *ApprovalDraft, operatorID int64, requestID string) error
	// StaffCanApproveCustomerQuote 读审批标记位（迁移 000066）。
	// 有权限点 M9:A 但没这个标记位的人（销售）不能看到/处理报价审批。
	StaffCanApproveCustomerQuote(ctx context.Context, staffID int64) (bool, error)
	// CustomerNeedsBusiness 判断这次批准是否必须同时派商务：客户合同/授信未归档 → true。
	// 对应原型 cuNeedsBusiness：合同与授信都齐了才不用再派商务。
	CustomerNeedsBusiness(ctx context.Context, quoteID int64) (bool, error)
}

// DecideRecord 是落库参数（领域层已做完校验，仓储只管写）。
type DecideRecord struct {
	Decisions          []ApprovalDecision
	Note               string
	OpsAssigneeID      int64
	BusinessAssigneeID int64
}

// QuoteApprovalService 报价单审批服务。
type QuoteApprovalService struct {
	store QuoteApprovalStore
}

// NewQuoteApprovalService 构造。
func NewQuoteApprovalService(store QuoteApprovalStore) *QuoteApprovalService {
	return &QuoteApprovalService{store: store}
}

// SubmitInput 是「提交审批」的入参。
//
// PreviousApprovalID 是关联重报（原型「关联驳回模型新申请」）用的：销售拿新一版报价单
// 重新提交，只对上一张审批里被驳回的模型再走一次审批。不传就是普通首次提交（整单全模型）。
//
// ModelIDs 可选：不传时由仓储按来源审批的驳回行自动收窄（前端拿不到标准模型 id，
// 硬要它传只会逼出一个错的口径）。
type SubmitInput struct {
	QuoteID            int64
	PreviousApprovalID int64
	ModelIDs           []int64
}

// Submit 提交审批（M9:E）。只有 DRAFT 且有模型的报价单能提交。
func (s *QuoteApprovalService) Submit(ctx context.Context, in SubmitInput, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	if in.QuoteID <= 0 {
		return nil, ErrQuoteNotFound
	}
	found, inScope, err := s.store.CheckQuoteOwnerScope(ctx, in.QuoteID, scope)
	if err != nil {
		return nil, fmt.Errorf("check quote scope: %w", err)
	}
	if !found {
		return nil, ErrQuoteNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	return s.store.SubmitQuoteApprovalTx(ctx, in.QuoteID, in.PreviousApprovalID, in.ModelIDs, operatorID, requestID)
}

// Decide 逐模型审批（M9:A）。
//
// 对齐原型的四条硬规则，全部在服务层挡一次（前端也挡，但服务端才是准绳）：
//  1. 只有超管能审，且不能审自己提交的单；
//  2. 每个模型都要给结论，驳回必须写原因；
//  3. 处理意见必填；
//  4. 有批准模型时必须指定技术办理人；客户合同未归档时还要指定商务办理人。
//
// 结论规则：全部批准 → APPROVED（已分派）；全部驳回 → REJECTED；两者都有 → PARTIAL。
func (s *QuoteApprovalService) Decide(ctx context.Context, approvalID int64, in DecideInput, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	// 先看有没有审批权，再读单据：没权限的人不该从错误信息里猜到"这张单在不在"。
	allowed, err := s.store.StaffCanApproveCustomerQuote(ctx, operatorID)
	if err != nil {
		return nil, fmt.Errorf("check approver: %w", err)
	}
	if !allowed {
		return nil, ErrQuoteApprovalNotApprover
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
	normalized, err := NormalizeDecisions(in.Rows, current.Rows)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		return nil, ErrQuoteApprovalNoteRequired
	}
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	if CountApproved(normalized) > 0 {
		if in.OpsAssigneeID <= 0 {
			return nil, ErrQuoteApprovalOpsAssignee
		}
		needsBusiness, err := s.store.CustomerNeedsBusiness(ctx, current.QuoteID)
		if err != nil {
			return nil, fmt.Errorf("check customer business: %w", err)
		}
		if needsBusiness && in.BusinessAssigneeID <= 0 {
			return nil, ErrQuoteApprovalBizAssignee
		}
	}
	record := DecideRecord{
		Decisions:          normalized,
		Note:               note,
		OpsAssigneeID:      in.OpsAssigneeID,
		BusinessAssigneeID: in.BusinessAssigneeID,
	}
	return s.store.DecideQuoteApprovalTx(ctx, approvalID, record, operatorID, requestID)
}

// Withdraw 撤回（M9:E）：只有提交人本人、且仍在待审批；原因必填（对齐原型撤回弹窗）。
func (s *QuoteApprovalService) Withdraw(ctx context.Context, approvalID int64, reason string, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
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
	note := strings.TrimSpace(reason)
	if note == "" {
		return nil, ErrQuoteApprovalWithdrawNote
	}
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	return s.store.WithdrawQuoteApprovalTx(ctx, approvalID, note, operatorID, requestID)
}

// Get 读审批单（M9:V），同样做数据域校验。
func (s *QuoteApprovalService) Get(ctx context.Context, approvalID int64, scope OwnerScope) (*QuoteApproval, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	return s.loadInScope(ctx, approvalID, scope)
}

// QuoteApprovalQuery 是「报价审批」列表的筛选条件（对齐原型 3.10 的四项筛选）。
//
// 原型：销售 / 状态 / 时间（待审批看提交时间、审批记录看处理时间）/ 关键字（报价编号、客户）。
type QuoteApprovalQuery struct {
	CustomerID  int64
	Status      string
	SubmittedBy int64
	Keyword     string
	DateFrom    *time.Time
	DateTo      *time.Time
	// Stage：pending 用 submitted_at 过滤，history 用 decided_at 过滤。
	Stage string
	Limit int
}

// 列表阶段取值。
const (
	ApprovalStagePending = "pending"
	ApprovalStageHistory = "history"
)

// List 列审批单（M9:V）。
//
// customerID=0 表示跨客户：客户报价单页签的「报价审批」是全局队列（原型 3.10，
// 分「待审批 / 审批记录」两个子标签）。仍受 limit（默认 200）约束，不是无界翻页。
func (s *QuoteApprovalService) List(ctx context.Context, q QuoteApprovalQuery) ([]QuoteApprovalSummary, error) {
	if s == nil || s.store == nil {
		return nil, ErrApprovalStoreUnavailable
	}
	if q.CustomerID < 0 {
		return nil, ErrCustomerNotFound
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 200
	}
	switch q.Status {
	case "", QuoteApprovalPending, QuoteApprovalApproved, QuoteApprovalPartial, QuoteApprovalRejected, QuoteApprovalWithdrawn:
	default:
		return nil, fmt.Errorf("%w：status 取值非法", ErrInvalidQuoteType)
	}
	if q.Stage != "" && q.Stage != ApprovalStagePending && q.Stage != ApprovalStageHistory {
		return nil, fmt.Errorf("%w：stage 取值非法", ErrInvalidQuoteType)
	}
	if q.SubmittedBy < 0 {
		return nil, fmt.Errorf("%w：submitted_by 取值非法", ErrInvalidQuoteType)
	}
	return s.store.ListQuoteApprovals(ctx, q)
}

// SaveDraft 保存审批进度（M9:A）。
//
// 与 Decide 的区别：允许不完整。这里只做「只能存自己的、审批单还在待处理」两条护栏，
// 逐模型结论的完整性留到提交时再说——否则「保存进度」就没意义了。
func (s *QuoteApprovalService) SaveDraft(ctx context.Context, approvalID int64, in DecideInput, scope OwnerScope, operatorID int64, requestID string) (*QuoteApproval, error) {
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
	// 只保留属于本单的待决行，避免前端带着别的单的 row_id 把草稿写脏。
	valid := make(map[int64]bool, len(current.Rows))
	for _, row := range current.Rows {
		if row.Result == ApprovalRowPending {
			valid[row.ID] = true
		}
	}
	rows := make([]ApprovalDecision, 0, len(in.Rows))
	for _, decision := range in.Rows {
		if !valid[decision.RowID] {
			continue
		}
		result := strings.ToUpper(strings.TrimSpace(decision.Result))
		if result != ApprovalRowApproved && result != ApprovalRowRejected {
			continue
		}
		reason := strings.TrimSpace(decision.Reason)
		if len([]rune(reason)) > 200 {
			reason = string([]rune(reason)[:200])
		}
		rows = append(rows, ApprovalDecision{RowID: decision.RowID, Result: result, Reason: reason})
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	now := time.Now()
	draft := &ApprovalDraft{
		Rows:               rows,
		Note:               note,
		OpsAssigneeID:      in.OpsAssigneeID,
		BusinessAssigneeID: in.BusinessAssigneeID,
		SavedBy:            operatorID,
		SavedAt:            &now,
	}
	if err := s.store.SaveQuoteApprovalDraft(ctx, approvalID, draft, operatorID, requestID); err != nil {
		return nil, err
	}
	return s.store.LoadQuoteApproval(ctx, approvalID)
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

// CountApproved 数「批准」的模型个数。
func CountApproved(decisions []ApprovalDecision) int {
	approved := 0
	for _, decision := range decisions {
		if decision.Result == ApprovalRowApproved {
			approved++
		}
	}
	return approved
}

// ConclusionOf 按逐模型结果给出整单结论（对齐原型的三分法）。
//
//	全部批准 → APPROVED（已分派）
//	全部驳回 → REJECTED
//	两者都有 → PARTIAL（部分批准）
func ConclusionOf(approved, rejected int) string {
	switch {
	case approved > 0 && rejected == 0:
		return QuoteApprovalApproved
	case approved == 0 && rejected > 0:
		return QuoteApprovalRejected
	case approved > 0 && rejected > 0:
		return QuoteApprovalPartial
	default:
		return QuoteApprovalPending
	}
}

// CanApproveCustomerQuote 当前登录人是否能处理客户报价审批（超管标记位）。
//
// 列表接口用它决定「给不给看队列」：没有这个标记位的人（销售）看到空列表，
// 而不是 403——他们仍要能看自己报价单的审批状态（那走报价记录，不走这里）。
func (s *QuoteApprovalService) CanApproveCustomerQuote(ctx context.Context, staffID int64) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrApprovalStoreUnavailable
	}
	if staffID <= 0 {
		return false, nil
	}
	return s.store.StaffCanApproveCustomerQuote(ctx, staffID)
}
