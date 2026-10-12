// Package finance 财务中心（内部门户 M10）。
//
// 本文件覆盖 12-finance.md §1/§2/§3 三个**只读**接口的领域模型与服务：
//   - GET /api/internal/finance/fx-rates          月度汇率快照
//   - GET /api/internal/finance/accounts          客户财务账户列表（行级过滤）
//   - GET /api/internal/finance/accounts/{id}     客户财务账户详情（含近期流水）
//
// 汇率锁定（读写）属批次 6，见 lock.go。
//
// 红线：
//   - 金额一律 decimal 字符串，禁止 float64（availableCredit 在服务层用 decimal 计算）。
//   - 客户财务数据按 owner_sales_operator_id 做 SELF/DEPT/DEPT_SUB/ALL 行级过滤（红线 7）。
//   - 不返回 cost / floor / margin 等成本字段。
package finance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"model_bss/internal/domain/auth"
	"model_bss/pkg/fxtier"
)

// ---- 常量 ----

// 汇率币种对固定为 USD/CNY：本表按业务定义只承载「USD 折算 CNY 的月度档位」，
// 不存在多币种行，也没有 source（来源）列（12-finance.md §0.1）。
const (
	BaseCurrency  = "USD"
	QuoteCurrency = "CNY"
)

// AccountCurrency 客户财务账户币种常量：customer_profile 金额列无币种，按人民币计。
const AccountCurrency = "CNY"

// 汇率快照状态三态（12-finance.md §1.1）。
const (
	FxStatusLocked      = "LOCKED"       // 当月（或未来月）已锁定
	FxStatusPendingLock = "PENDING_LOCK" // 当月无记录 → 占位
	FxStatusExpired     = "EXPIRED"      // 早于当月的历史锁定
)

// 交易类型（12-finance.md §0.3 的方向映射目标）。
const (
	TxnCreditUsage   = "CREDIT_USAGE"
	TxnCreditRelease = "CREDIT_RELEASE"
	TxnDepositIn     = "DEPOSIT_IN"
	TxnDepositOut    = "DEPOSIT_OUT"

	// txnSourceCredit / txnSourceDeposit 是流水来源标识（用于合并排序与 id 前缀）。
	txnSourceCredit  = "CREDIT"
	txnSourceDeposit = "DEPOSIT"
)

// 分页默认值与上限（README.md §4）。
const (
	defaultPageSize = 20
	maxPageSize     = 200
	// RecentTxnLimit 是账户详情「近期变动」合并后的最大条数。
	RecentTxnLimit = 10
)

// ---- 领域错误 ----

var (
	// ErrCustomerNotFound 客户不存在 → 404。
	ErrCustomerNotFound = errors.New("客户不存在")
	// ErrCustomerOutOfScope 客户不在当前操作员的数据域内 → 403。
	ErrCustomerOutOfScope = errors.New("客户不在当前操作员的数据域内")
)

// ---- 数据域 ----

// OwnerScope 是行级过滤所需的操作员数据域视图（与 customer.OwnerScope 对齐）。
// ScopePaths 元素形如 "/1/3/"（与 org_unit.path 一致，含首尾斜杠）。
type OwnerScope struct {
	DataScope  auth.DataScope
	StaffID    int64
	MyOrgID    int64
	ScopePaths []string
}

// ---- 响应 DTO ----

// FxRateItem 是汇率快照的一行。
//
// 注意：PENDING_LOCK 占位行不是数据库记录——id 取当月 ym，rate 为空串，
// LockedAt/LockedBy/UpdatedAt 省略（不伪造时间），见 12-finance.md §1.2。
type FxRateItem struct {
	ID            string     `json:"id"`
	Month         string     `json:"month"`
	BaseCurrency  string     `json:"baseCurrency"`
	QuoteCurrency string     `json:"quoteCurrency"`
	Rate          string     `json:"rate"`
	Status        string     `json:"status"`
	LockedAt      *time.Time `json:"lockedAt,omitempty"`
	LockedBy      string     `json:"lockedBy,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// FxRateListResult 是汇率快照分页结果。
type FxRateListResult struct {
	List  []FxRateItem `json:"list"`
	Total int          `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

// AccountSummary 是客户财务账户列表的一行（12-finance.md §2.1）。
type AccountSummary struct {
	CustomerID      string    `json:"customerId"`
	CustomerCode    string    `json:"customerCode"`
	CustomerName    string    `json:"customerName"`
	LevelCode       string    `json:"levelCode"`
	Status          string    `json:"status"`
	Currency        string    `json:"currency"`
	CreditLimit     string    `json:"creditLimit"`
	UsedCredit      string    `json:"usedCredit"`
	AvailableCredit string    `json:"availableCredit"`
	DepositBalance  string    `json:"depositBalance"`
	PaymentTerms    string    `json:"paymentTerms"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Transaction 是账户详情「近期变动」的一行（合并 credit_txn 与 deposit_txn）。
type Transaction struct {
	ID          string    `json:"id"`
	OccurredAt  time.Time `json:"occurredAt"`
	Type        string    `json:"type"`
	Amount      string    `json:"amount"`
	Currency    string    `json:"currency"`
	ReferenceNo string    `json:"referenceNo"`
	Note        string    `json:"note"`
}

// AccountDetail 是客户财务账户详情（12-finance.md §3.1）。
type AccountDetail struct {
	AccountSummary
	BillingEntity      string        `json:"billingEntity"`
	InvoiceTitle       string        `json:"invoiceTitle"`
	TaxNoMasked        string        `json:"taxNoMasked"`
	SettlementCycle    string        `json:"settlementCycle"`
	RecentTransactions []Transaction `json:"recentTransactions"`
}

// AccountListResult 是客户财务账户分页结果。
type AccountListResult struct {
	List  []AccountSummary `json:"list"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`
}

// ---- 仓储行模型（repo 扫描用） ----

// FxLockRow 是 fx_rate_lock 的一行（联 internal_staff.name 回显锁定人）。
type FxLockRow struct {
	ID           int64
	Ym           string
	FxTier       string // numeric(6,3) 文本，如 "6.900"
	LockedByName string
	LockedAt     time.Time
	UpdatedAt    time.Time
}

// AccountRow 是 customer_profile JOIN legal_subject 的一行。
type AccountRow struct {
	ID            int64
	LegalName     string
	Uscc          string
	LevelCode     string
	CreditStatus  string
	CreditLimit   string
	CreditUsed    string
	DepositAmount string
	PaymentType   string
	BillingCycle  int
	UpdatedAt     time.Time
}

// TxnRow 是合并流水的一行（Source 区分 credit_txn / deposit_txn）。
type TxnRow struct {
	ID         int64
	Source     string
	OccurredAt time.Time
	Direction  string
	Amount     string
	RefType    string
	RefID      string
	Reason     string
}

// AccountPage 是账户列表的仓储返回（行 + 总数）。
type AccountPage struct {
	Rows  []AccountRow
	Total int64
}

// ReadStore 是财务只读域的仓储接口（GORM 实现见 internal/repo/finance.go）。
type ReadStore interface {
	// ListFxLocks 返回全部汇率锁定行（表很小，按月一条）。
	ListFxLocks(ctx context.Context) ([]FxLockRow, error)
	// ListAccounts 行级过滤 + 关键词/状态过滤 + 分页。
	ListAccounts(ctx context.Context, scope OwnerScope, keyword, status string, page, size int) (*AccountPage, error)
	// GetAccountInScope 取单账户；found/inScope 区分 404 与 403。
	GetAccountInScope(ctx context.Context, customerID int64, scope OwnerScope) (row *AccountRow, found bool, inScope bool, err error)
	// ListRecentTransactions 合并 credit_txn 与 deposit_txn，按时间倒序取前 limit 条。
	ListRecentTransactions(ctx context.Context, customerID int64, limit int) ([]TxnRow, error)
}

// ---- Service ----

// ReadService 是财务只读域服务（无状态，可并发）。
type ReadService struct {
	store ReadStore
	now   func() time.Time
	loc   *time.Location
}

// NewReadService 构造。now 为 nil 时用 time.Now；时区固定 Asia/Shanghai（生效判定口径）。
func NewReadService(store ReadStore, now func() time.Time) *ReadService {
	if now == nil {
		now = time.Now
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &ReadService{store: store, now: now, loc: loc}
}

// currentYM 返回当前月（Asia/Shanghai），格式 "2006-01"。
func (s *ReadService) currentYM() string {
	return s.now().In(s.loc).Format("2006-01")
}

// ListFxRates 汇率快照列表（12-finance.md §1）。
//
// 步骤：
//  1. 取全表行，按 cur（当前月）推导状态：ym >= cur → LOCKED；ym < cur → EXPIRED。
//  2. 当月无记录时补一条 PENDING_LOCK 占位行。
//  3. 按 month / status / search 过滤 → ym DESC 排序 → 分页。
func (s *ReadService) ListFxRates(ctx context.Context, month, status, keyword string, page, size int) (*FxRateListResult, error) {
	page, size = normalizePage(page, size)
	cur := s.currentYM()

	rows, err := s.store.ListFxLocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fx locks: %w", err)
	}

	items := make([]FxRateItem, 0, len(rows)+1)
	seenCur := false
	for _, r := range rows {
		item := FxRateItem{
			ID:            fmt.Sprintf("%d", r.ID),
			Month:         r.Ym,
			BaseCurrency:  BaseCurrency,
			QuoteCurrency: QuoteCurrency,
			Rate:          canonicalTier(r.FxTier),
			Status:        deriveFxStatus(r.Ym, cur),
		}
		lockedAt := r.LockedAt
		updatedAt := r.UpdatedAt
		item.LockedAt = &lockedAt
		item.LockedBy = r.LockedByName
		item.UpdatedAt = &updatedAt
		if r.Ym == cur {
			seenCur = true
		}
		items = append(items, item)
	}
	// 当月无记录 → 补占位行（不写库，仅读响应；见 §1.2）。
	if !seenCur {
		items = append(items, FxRateItem{
			ID:            cur,
			Month:         cur,
			BaseCurrency:  BaseCurrency,
			QuoteCurrency: QuoteCurrency,
			Rate:          "",
			Status:        FxStatusPendingLock,
		})
	}

	// 过滤。
	filtered := make([]FxRateItem, 0, len(items))
	for _, it := range items {
		if month != "" && it.Month != month {
			continue
		}
		if status != "" && it.Status != status {
			continue
		}
		if keyword != "" && !fxKeywordMatch(it, keyword) {
			continue
		}
		filtered = append(filtered, it)
	}
	// ym DESC（字符串比较对 "YYYY-MM" 等价于时间倒序）。
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Month > filtered[j].Month })

	total := len(filtered)
	list := paginateItems(filtered, page, size)
	return &FxRateListResult{List: list, Total: total, Page: page, Size: size}, nil
}

// ListAccounts 客户财务账户列表（12-finance.md §2）。
func (s *ReadService) ListAccounts(ctx context.Context, scope OwnerScope, keyword, status string, page, size int) (*AccountListResult, error) {
	page, size = normalizePage(page, size)
	pg, err := s.store.ListAccounts(ctx, scope, strings.TrimSpace(keyword), strings.TrimSpace(status), page, size)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	res := &AccountListResult{List: []AccountSummary{}, Page: page, Size: size}
	if pg == nil {
		return res, nil
	}
	res.Total = pg.Total
	for _, row := range pg.Rows {
		summary, cerr := buildAccountSummary(row)
		if cerr != nil {
			return nil, cerr
		}
		res.List = append(res.List, summary)
	}
	return res, nil
}

// GetAccount 客户财务账户详情（12-finance.md §3）。
func (s *ReadService) GetAccount(ctx context.Context, customerID int64, scope OwnerScope) (*AccountDetail, error) {
	row, found, inScope, err := s.store.GetAccountInScope(ctx, customerID, scope)
	if err != nil {
		return nil, fmt.Errorf("get account %d: %w", customerID, err)
	}
	if !found {
		return nil, ErrCustomerNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	summary, err := buildAccountSummary(*row)
	if err != nil {
		return nil, err
	}
	txns, err := s.store.ListRecentTransactions(ctx, customerID, RecentTxnLimit)
	if err != nil {
		return nil, fmt.Errorf("list recent transactions %d: %w", customerID, err)
	}
	detail := &AccountDetail{
		AccountSummary:     summary,
		BillingEntity:      row.LegalName,
		InvoiceTitle:       row.LegalName,
		TaxNoMasked:        maskUscc(row.Uscc),
		SettlementCycle:    fmt.Sprintf("%d 天", row.BillingCycle),
		RecentTransactions: mapTransactions(txns),
	}
	return detail, nil
}

// ---- 纯函数 ----

// deriveFxStatus 推导汇率快照状态：ym >= cur → LOCKED（含未来月）；ym < cur → EXPIRED。
// "YYYY-MM" 的字符串比较等价于时间比较。
func deriveFxStatus(ym, cur string) string {
	if ym >= cur {
		return FxStatusLocked
	}
	return FxStatusExpired
}

// canonicalTier 把库中档位文本（如 "6.900"）规范化为档位回显形式（如 "6.9"）。
// 复用 pkg/fxtier（与报价侧同一 8 档白名单）；历史非 8 档值原样返回（不静默改值）。
func canonicalTier(raw string) string {
	if canonical, ok := fxtier.Normalize(raw); ok {
		return canonical
	}
	return strings.TrimSpace(raw)
}

// buildAccountSummary 由仓储行构造账户摘要，availableCredit 用 decimal 计算。
func buildAccountSummary(row AccountRow) (AccountSummary, error) {
	limit, err := decimal.NewFromString(row.CreditLimit)
	if err != nil {
		return AccountSummary{}, fmt.Errorf("parse credit_limit %q: %w", row.CreditLimit, err)
	}
	used, err := decimal.NewFromString(row.CreditUsed)
	if err != nil {
		return AccountSummary{}, fmt.Errorf("parse credit_used %q: %w", row.CreditUsed, err)
	}
	available := limit.Sub(used)
	return AccountSummary{
		CustomerID:      fmt.Sprintf("%d", row.ID),
		CustomerCode:    row.Uscc,
		CustomerName:    row.LegalName,
		LevelCode:       row.LevelCode,
		Status:          row.CreditStatus,
		Currency:        AccountCurrency,
		CreditLimit:     limit.StringFixed(2),
		UsedCredit:      used.StringFixed(2),
		AvailableCredit: available.StringFixed(2),
		DepositBalance:  normalizeAmount(row.DepositAmount),
		PaymentTerms:    row.PaymentType,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}

// normalizeAmount 把 decimal 文本规整为两位小数；无法解析时原样返回（避免掩盖数据事故）。
func normalizeAmount(raw string) string {
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return raw
	}
	return d.StringFixed(2)
}

// maskUscc 脱敏统一社会信用代码：保留前 4 + 后 4，中间 "*"（长度不足时降级处理）。
// 空值返回空串（个人主体无 uscc）。
func maskUscc(uscc string) string {
	r := []rune(strings.TrimSpace(uscc))
	switch {
	case len(r) == 0:
		return ""
	case len(r) <= 4:
		return strings.Repeat("*", len(r))
	case len(r) <= 8:
		return string(r[:2]) + strings.Repeat("*", len(r)-4) + string(r[len(r)-2:])
	default:
		return string(r[:4]) + "****" + string(r[len(r)-4:])
	}
}

// mapTransactions 把合并流水映射为契约 DTO：
//   - credit_txn：CONSUME→CREDIT_USAGE，REPAY→CREDIT_RELEASE，ADJUST→CREDIT_USAGE（口径待业务复核）
//   - deposit_txn：PAY→DEPOSIT_IN，DEDUCT→DEPOSIT_OUT，REFUND→DEPOSIT_OUT
//
// 不是字段直通，故映射规则在此显式集中（12-finance.md §0.3）。
// 未知方向跳过（不静默造类型）。id 加来源前缀避免两表 id 撞号。
func mapTransactions(rows []TxnRow) []Transaction {
	out := make([]Transaction, 0, len(rows))
	for _, r := range rows {
		var typ, refNo, note string
		switch r.Source {
		case txnSourceCredit:
			switch r.Direction {
			case "CONSUME":
				typ = TxnCreditUsage
			case "REPAY":
				typ = TxnCreditRelease
			case "ADJUST":
				typ = TxnCreditUsage
			default:
				continue
			}
			if r.RefType != "" && r.RefID != "" {
				refNo = r.RefType + ":" + r.RefID
			} else {
				refNo = r.RefType
			}
		case txnSourceDeposit:
			switch r.Direction {
			case "PAY":
				typ = TxnDepositIn
			case "DEDUCT", "REFUND":
				typ = TxnDepositOut
			default:
				continue
			}
			note = r.Reason
		default:
			continue
		}
		out = append(out, Transaction{
			ID:          txnID(r.Source, r.ID),
			OccurredAt:  r.OccurredAt,
			Type:        typ,
			Amount:      normalizeAmount(r.Amount),
			Currency:    AccountCurrency,
			ReferenceNo: refNo,
			Note:        note,
		})
	}
	return out
}

// txnID 给合并流水加来源前缀（credit → "C"，deposit → "D"），避免两表 id 撞号。
func txnID(source string, id int64) string {
	if source == txnSourceCredit {
		return fmt.Sprintf("C%d", id)
	}
	return fmt.Sprintf("D%d", id)
}

// fxKeywordMatch 按月份或锁定人姓名做不区分大小写的模糊匹配。
func fxKeywordMatch(item FxRateItem, keyword string) bool {
	k := strings.ToLower(strings.TrimSpace(keyword))
	return strings.Contains(strings.ToLower(item.Month), k) ||
		strings.Contains(strings.ToLower(item.LockedBy), k)
}

// normalizePage 规整分页参数（README.md §4：page>=1，size∈[1,200]，默认 20）。
func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}

// paginateItems 对已过滤列表做分页切片。
func paginateItems(items []FxRateItem, page, size int) []FxRateItem {
	start := (page - 1) * size
	if start >= len(items) {
		return []FxRateItem{}
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
