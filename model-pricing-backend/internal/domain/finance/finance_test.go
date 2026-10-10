package finance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"model_bss/internal/domain/auth"
)

// ============================================================
// fakeStore
// ============================================================

type fakeStore struct {
	fxRows []FxLockRow
	fxErr  error

	accountPage *AccountPage
	accountErr  error

	accountRow     *AccountRow
	accountFound   bool
	accountInScope bool
	accountGetErr  error

	txns   []TxnRow
	txnErr error

	// 捕获调用参数
	lastScope         OwnerScope
	lastKeyword       string
	lastStatus        string
	lastPage          int
	lastSize          int
	lastTxnCustomerID int64
	lastTxnLimit      int64
}

func (f *fakeStore) ListFxLocks(_ context.Context) ([]FxLockRow, error) {
	return f.fxRows, f.fxErr
}

func (f *fakeStore) ListAccounts(_ context.Context, scope OwnerScope, keyword, status string, page, size int) (*AccountPage, error) {
	f.lastScope, f.lastKeyword, f.lastStatus, f.lastPage, f.lastSize = scope, keyword, status, page, size
	return f.accountPage, f.accountErr
}

func (f *fakeStore) GetAccountInScope(_ context.Context, _ int64, scope OwnerScope) (*AccountRow, bool, bool, error) {
	f.lastScope = scope
	return f.accountRow, f.accountFound, f.accountInScope, f.accountGetErr
}

func (f *fakeStore) ListRecentTransactions(_ context.Context, customerID int64, limit int) ([]TxnRow, error) {
	f.lastTxnCustomerID, f.lastTxnLimit = customerID, int64(limit)
	return f.txns, f.txnErr
}

// fixedNow 固定当前时间 2026-10-10（Asia/Shanghai），使 currentYM = "2026-10"。
func fixedNow() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }

func newSvc(s *fakeStore) *ReadService { return NewReadService(s, fixedNow) }

// ============================================================
// 汇率快照：三态推导 + 占位行
// ============================================================

// TestListFxRates_ThreeStatesAndPlaceholder: 历史月=EXPIRED、未来月=LOCKED、当月无记录→占位 PENDING_LOCK。
func TestListFxRates_ThreeStatesAndPlaceholder(t *testing.T) {
	aug := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	future := time.Date(2027, 1, 9, 1, 42, 56, 0, time.UTC)
	s := &fakeStore{fxRows: []FxLockRow{
		{ID: 3, Ym: "2026-08", FxTier: "6.800", LockedByName: "财务甲", LockedAt: aug, UpdatedAt: aug},
		{ID: 12, Ym: "2027-01", FxTier: "6.900", LockedByName: "财务冒烟", LockedAt: future, UpdatedAt: future},
	}}
	res, err := newSvc(s).ListFxRates(context.Background(), "", "", "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 3, res.Total) // 2 真实行 + 1 占位行

	byMonth := map[string]FxRateItem{}
	for _, it := range res.List {
		byMonth[it.Month] = it
	}

	past, ok := byMonth["2026-08"]
	require.True(t, ok)
	require.Equal(t, FxStatusExpired, past.Status)
	require.Equal(t, "6.8", past.Rate) // numeric(6,3) "6.800" → 规范形式
	require.Equal(t, BaseCurrency, past.BaseCurrency)
	require.Equal(t, QuoteCurrency, past.QuoteCurrency)
	require.Equal(t, "财务甲", past.LockedBy)

	fut, ok := byMonth["2027-01"]
	require.True(t, ok)
	require.Equal(t, FxStatusLocked, fut.Status) // 未来月已锁定仍是 LOCKED

	ph, ok := byMonth["2026-10"]
	require.True(t, ok, "当月无记录应补占位行")
	require.Equal(t, FxStatusPendingLock, ph.Status)
	require.Equal(t, "2026-10", ph.ID)
	require.Equal(t, "", ph.Rate)
	require.Empty(t, ph.LockedBy)
	require.Nil(t, ph.LockedAt, "占位行不得伪造锁定时间")
	require.Nil(t, ph.UpdatedAt, "占位行不得伪造更新时间")
}

// TestListFxRates_CurrentMonthLocked_NoPlaceholder: 当月已有记录 → LOCKED 且不补占位行。
func TestListFxRates_CurrentMonthLocked_NoPlaceholder(t *testing.T) {
	now := time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)
	s := &fakeStore{fxRows: []FxLockRow{
		{ID: 20, Ym: "2026-10", FxTier: "7.000", LockedByName: "财务冒烟", LockedAt: now, UpdatedAt: now},
	}}
	res, err := newSvc(s).ListFxRates(context.Background(), "", "", "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, res.Total)
	require.Equal(t, FxStatusLocked, res.List[0].Status)
	require.Equal(t, "7.0", res.List[0].Rate)
	require.NotNil(t, res.List[0].LockedAt)
}

// TestListFxRates_OrderDesc: 按 ym 倒序。
func TestListFxRates_OrderDesc(t *testing.T) {
	s := &fakeStore{fxRows: []FxLockRow{
		{ID: 1, Ym: "2026-06", FxTier: "6.500"},
		{ID: 2, Ym: "2026-09", FxTier: "6.700"},
	}}
	res, err := newSvc(s).ListFxRates(context.Background(), "", "", "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"2026-10", "2026-09", "2026-06"}, []string{
		res.List[0].Month, res.List[1].Month, res.List[2].Month,
	})
}

// TestListFxRates_Filters: month / status / search 过滤与分页 total。
func TestListFxRates_Filters(t *testing.T) {
	s := &fakeStore{fxRows: []FxLockRow{
		{ID: 1, Ym: "2026-08", FxTier: "6.800", LockedByName: "张财务"},
		{ID: 2, Ym: "2026-09", FxTier: "6.900", LockedByName: "李财务"},
	}}
	svc := newSvc(s)

	pending, err := svc.ListFxRates(context.Background(), "", FxStatusPendingLock, "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, pending.Total)
	require.Equal(t, "2026-10", pending.List[0].Month)

	aug, err := svc.ListFxRates(context.Background(), "2026-08", "", "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, aug.Total)
	require.Equal(t, "2026-08", aug.List[0].Month)

	byName, err := svc.ListFxRates(context.Background(), "", "", "李财务", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, byName.Total)
	require.Equal(t, "2026-09", byName.List[0].Month)
}

// TestListFxRates_Pagination: 分页切片 + total 为过滤后总数。
func TestListFxRates_Pagination(t *testing.T) {
	rows := []FxLockRow{}
	for _, ym := range []string{"2026-01", "2026-02", "2026-03", "2026-04"} {
		rows = append(rows, FxLockRow{ID: 1, Ym: ym, FxTier: "6.800"})
	}
	// 另加两行凑够 6 条（含当月占位后 total=6）。
	rows = append(rows, FxLockRow{ID: 2, Ym: "2026-05", FxTier: "6.800"})
	s := &fakeStore{fxRows: rows}

	res, err := newSvc(s).ListFxRates(context.Background(), "", "", "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, 6, res.Total) // 5 真实 + 1 占位
	require.Len(t, res.List, 2)
	require.Equal(t, 2, res.Page)
	require.Equal(t, 2, res.Size)
}

// ============================================================
// 账户列表 / 详情
// ============================================================

// TestListAccounts_AvailableCreditDecimal: availableCredit = credit_limit − credit_used（手算锚点）。
func TestListAccounts_AvailableCreditDecimal(t *testing.T) {
	s := &fakeStore{accountPage: &AccountPage{
		Total: 1,
		Rows: []AccountRow{{
			ID: 1, LegalName: "示例科技有限公司", Uscc: "E2E000000000000001",
			LevelCode: "PRO", CreditStatus: "WARNING",
			CreditLimit: "100000.00", CreditUsed: "85000.00", DepositAmount: "20000.00",
			PaymentType: "MONTHLY", BillingCycle: 30, UpdatedAt: fixedNow(),
		}},
	}}
	scope := OwnerScope{DataScope: "SELF", StaffID: 9}
	res, err := newSvc(s).ListAccounts(context.Background(), scope, " 关键词 ", " WARNING ", 0, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, res.Total)
	require.Len(t, res.List, 1)
	it := res.List[0]
	require.Equal(t, "15000.00", it.AvailableCredit) // 100000.00 - 85000.00
	require.Equal(t, "100000.00", it.CreditLimit)
	require.Equal(t, "85000.00", it.UsedCredit)
	require.Equal(t, "20000.00", it.DepositBalance)
	require.Equal(t, "1", it.CustomerID)
	require.Equal(t, "E2E000000000000001", it.CustomerCode)
	require.Equal(t, AccountCurrency, it.Currency)
	require.Equal(t, "MONTHLY", it.PaymentTerms)
	// 分页默认值（page<=0 → 1，size<=0 → 20）。
	require.Equal(t, 1, res.Page)
	require.Equal(t, 20, res.Size)
	// keyword/status 已 trim 后透传。
	require.Equal(t, "关键词", s.lastKeyword)
	require.Equal(t, "WARNING", s.lastStatus)
	require.Equal(t, scope, s.lastScope)
}

// TestGetAccount_NotFoundAndOutOfScope: 404 / 403 语义。
func TestGetAccount_NotFoundAndOutOfScope(t *testing.T) {
	s := &fakeStore{accountFound: false}
	_, err := newSvc(s).GetAccount(context.Background(), 999, OwnerScope{})
	require.ErrorIs(t, err, ErrCustomerNotFound)

	s2 := &fakeStore{accountFound: true, accountInScope: false}
	_, err2 := newSvc(s2).GetAccount(context.Background(), 3, OwnerScope{DataScope: "SELF", StaffID: 9})
	require.ErrorIs(t, err2, ErrCustomerOutOfScope)
}

// TestGetAccount_DetailFieldsAndTransactions: 详情字段映射 + 合并流水 + 取最近 10 条。
func TestGetAccount_DetailFieldsAndTransactions(t *testing.T) {
	row := &AccountRow{
		ID: 1, LegalName: "示例科技有限公司", Uscc: "E2E000000000000001",
		LevelCode: "PRO", CreditStatus: "WARNING",
		CreditLimit: "100000.00", CreditUsed: "85000.00", DepositAmount: "20000.00",
		PaymentType: "MONTHLY", BillingCycle: 30, UpdatedAt: fixedNow(),
	}
	creditAt := time.Date(2026, 10, 8, 10, 3, 27, 0, time.UTC)
	depositAt := time.Date(2026, 10, 7, 22, 3, 27, 0, time.UTC)
	s := &fakeStore{
		accountRow: row, accountFound: true, accountInScope: true,
		txns: []TxnRow{
			{ID: 3, Source: "CREDIT", OccurredAt: creditAt, Direction: "ADJUST", Amount: "500.00", RefType: "MANUAL"},
			{ID: 1, Source: "DEPOSIT", OccurredAt: depositAt, Direction: "PAY", Amount: "20000.00", Reason: "预付押金"},
		},
	}
	detail, err := newSvc(s).GetAccount(context.Background(), 1, OwnerScope{DataScope: "ALL"})
	require.NoError(t, err)
	require.Equal(t, "示例科技有限公司", detail.BillingEntity)
	require.Equal(t, "示例科技有限公司", detail.InvoiceTitle)
	require.Equal(t, "E2E0****0001", detail.TaxNoMasked)
	require.Equal(t, "30 天", detail.SettlementCycle)
	require.Equal(t, "15000.00", detail.AvailableCredit)
	require.Equal(t, int64(RecentTxnLimit), s.lastTxnLimit)
	require.Len(t, detail.RecentTransactions, 2)
	require.Equal(t, "C3", detail.RecentTransactions[0].ID)
	require.Equal(t, TxnCreditUsage, detail.RecentTransactions[0].Type) // ADJUST→CREDIT_USAGE
	require.Equal(t, "D1", detail.RecentTransactions[1].ID)
	require.Equal(t, TxnDepositIn, detail.RecentTransactions[1].Type)
	require.Equal(t, "预付押金", detail.RecentTransactions[1].Note)
}

// ============================================================
// 纯函数
// ============================================================

// TestDeriveFxStatus: 边界（等于当月 → LOCKED；早一月 → EXPIRED）。
func TestDeriveFxStatus(t *testing.T) {
	require.Equal(t, FxStatusLocked, deriveFxStatus("2026-10", "2026-10"))
	require.Equal(t, FxStatusLocked, deriveFxStatus("2027-01", "2026-10"))
	require.Equal(t, FxStatusExpired, deriveFxStatus("2026-09", "2026-10"))
}

// TestMapTransactions_Directions: 方向映射锚点（含 ADJUST / REFUND）。
func TestMapTransactions_Directions(t *testing.T) {
	now := fixedNow()
	out := mapTransactions([]TxnRow{
		{ID: 1, Source: "CREDIT", OccurredAt: now, Direction: "CONSUME", Amount: "85000.00", RefType: "QUOTE", RefID: "1001"},
		{ID: 2, Source: "CREDIT", OccurredAt: now, Direction: "REPAY", Amount: "10000.00", RefType: "MANUAL"},
		{ID: 3, Source: "CREDIT", OccurredAt: now, Direction: "ADJUST", Amount: "500.00"},
		{ID: 4, Source: "DEPOSIT", OccurredAt: now, Direction: "PAY", Amount: "20000.00", Reason: "预付押金"},
		{ID: 5, Source: "DEPOSIT", OccurredAt: now, Direction: "DEDUCT", Amount: "500.00", Reason: "超量扣减"},
		{ID: 6, Source: "DEPOSIT", OccurredAt: now, Direction: "REFUND", Amount: "100.00"},
		{ID: 7, Source: "CREDIT", OccurredAt: now, Direction: "UNKNOWN", Amount: "1.00"}, // 未知方向跳过
	})
	require.Len(t, out, 6)

	want := map[string]string{
		"C1": TxnCreditUsage, "C2": TxnCreditRelease, "C3": TxnCreditUsage,
		"D4": TxnDepositIn, "D5": TxnDepositOut, "D6": TxnDepositOut,
	}
	for _, tx := range out {
		require.Equalf(t, want[tx.ID], tx.Type, "id=%s", tx.ID)
		require.Equal(t, AccountCurrency, tx.Currency)
	}
	// referenceNo：credit 有 ref_id 时拼 ref_type:ref_id；无 ref_id 时仅 ref_type。
	for _, tx := range out {
		if tx.ID == "C1" {
			require.Equal(t, "QUOTE:1001", tx.ReferenceNo)
		}
		if tx.ID == "C2" {
			require.Equal(t, "MANUAL", tx.ReferenceNo)
		}
	}
}

// TestMaskUscc: 脱敏规则。
func TestMaskUscc(t *testing.T) {
	require.Equal(t, "", maskUscc(""))
	require.Equal(t, "****", maskUscc("1234"))
	require.Equal(t, "12****78", maskUscc("12345678"))
	require.Equal(t, "E2E0****0001", maskUscc("E2E000000000000001"))
}

// TestCanonicalTier: numeric(6,3) 文本规范化（复用 fxtier）。
func TestCanonicalTier(t *testing.T) {
	require.Equal(t, "6.9", canonicalTier("6.900"))
	require.Equal(t, "7.0", canonicalTier("7.000"))
	require.Equal(t, "6.75", canonicalTier("6.750"))
	require.Equal(t, "6.86", canonicalTier("6.86")) // 非 8 档历史值原样返回
	require.Equal(t, "", canonicalTier(""))
}

// TestBuildAccountSummary_BadAmount: 金额无法解析 → 报错（不静默）。
func TestBuildAccountSummary_BadAmount(t *testing.T) {
	_, err := buildAccountSummary(AccountRow{CreditLimit: "not-a-number", CreditUsed: "0"})
	require.Error(t, err)
}

// TestListAccounts_StoreError: 仓储错误向上传播。
func TestListAccounts_StoreError(t *testing.T) {
	s := &fakeStore{accountErr: errors.New("boom")}
	_, err := newSvc(s).ListAccounts(context.Background(), OwnerScope{}, "", "", 1, 20)
	require.Error(t, err)
}

// 确保 auth 包被引用（OwnerScope.DataScope 为 auth.DataScope）。
var _ auth.DataScope = auth.ScopeSELF
