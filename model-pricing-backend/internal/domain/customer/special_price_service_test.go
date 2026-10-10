package customer

import (
	"context"
	"errors"
	"testing"
)

// ============================================================
// fake store （特价用）
// ============================================================

type fakeSpecialStore struct {
	quote    *QuoteRow
	quoteErr error
	items    []QuoteItemDetail
	itemsErr error

	reqRes *SpecialPriceResult
	reqErr error

	// 数据域校验（红线 7 / 批次 2b）：默认在域内；越域用例显式置 scopeOutOfScope=true。
	scopeOutOfScope  bool
	scopeCalls       int
	scopeLastQuoteID int64

	reqTxCalls int // 护栏：越域时 RequestSpecialPriceTx 必须一次都不被调用
	lastQuote  QuoteRow
	lastImpact *MarginImpact
	lastReason string
	lastEM     string
	lastOpID   int64
	lastReqID  string
}

func (f *fakeSpecialStore) CheckQuoteOwnerScope(_ context.Context, quoteID int64, _ OwnerScope) (bool, bool, error) {
	f.scopeCalls++
	f.scopeLastQuoteID = quoteID
	if f.scopeOutOfScope {
		return true, false, nil // 存在但不在数据域内
	}
	return true, true, nil
}

func (f *fakeSpecialStore) LoadQuote(_ context.Context, _ int64) (*QuoteRow, error) {
	return f.quote, f.quoteErr
}
func (f *fakeSpecialStore) LoadQuoteItemsDetail(_ context.Context, _ int64) ([]QuoteItemDetail, error) {
	return f.items, f.itemsErr
}
func (f *fakeSpecialStore) RequestSpecialPriceTx(_ context.Context, quote QuoteRow, impact *MarginImpact, reason, em string, opID int64, reqID string) (*SpecialPriceResult, error) {
	f.reqTxCalls++
	f.lastQuote = quote
	f.lastImpact = impact
	f.lastReason = reason
	f.lastEM = em
	f.lastOpID = opID
	f.lastReqID = reqID
	return f.reqRes, f.reqErr
}

// ============================================================
// 基础 fixture
// ============================================================

func baseQuote() *QuoteRow {
	return &QuoteRow{
		ID: 1, CustomerID: 1, VersionNo: 1, Status: QuoteStatusDraft,
		QuoteType: QuoteTypeTemp, PriceBookVersion: 0, OwnerSalesID: 2,
	}
}

func baseItems() []QuoteItemDetail {
	return []QuoteItemDetail{
		{SKUID: 40, Currency: "CNY", UnitPrice: dec("3.00000000"), FloorPrice: dec("3.52941176")}, // 破线 -0.5294
		{SKUID: 41, Currency: "CNY", UnitPrice: dec("12.00000000"), FloorPrice: dec("10.58823529")},
	}
}

// ============================================================
// 测试
// ============================================================

func TestSpecialPriceRequest_Success(t *testing.T) {
	f := &fakeSpecialStore{
		quote: baseQuote(),
		items: baseItems(),
		reqRes: &SpecialPriceResult{
			QuoteID: 1, ChangeRequestID: 42, StepCount: 2,
			Status: SpecialPricePending,
		},
	}
	svc := NewSpecialPriceService(f)
	res, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 1, Reason: "战略客户", ExpectedMargin: "0.08",
	}, 7, "req-1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Status != SpecialPricePending || res.ChangeRequestID != 42 {
		t.Fatalf("res=%+v", res)
	}
	// 验证 store 收到了 pre-computed impact
	if f.lastImpact == nil {
		t.Fatalf("impact nil")
	}
	// min(items.unit_price - floor_price) = -0.52941176
	if f.lastImpact.DeltaGapDistance != "-0.52941176" {
		t.Fatalf("gap=%s", f.lastImpact.DeltaGapDistance)
	}
	// margin 取最小：(3.0-3.52941176)/3.0 ≈ -0.17647059
	if f.lastImpact.CurrentMargin != "-0.17647059" {
		t.Fatalf("margin=%s", f.lastImpact.CurrentMargin)
	}
}

func TestSpecialPriceRequest_ReasonRequired(t *testing.T) {
	f := &fakeSpecialStore{quote: baseQuote()}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 1, Reason: "", ExpectedMargin: "0.08",
	}, 7, "req-1")
	if !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("err=%v, want ErrReasonRequired", err)
	}
}

func TestSpecialPriceRequest_ExpectedMarginInvalid(t *testing.T) {
	f := &fakeSpecialStore{quote: baseQuote()}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 1, Reason: "r", ExpectedMargin: "not-a-decimal",
	}, 7, "req-1")
	if !errors.Is(err, ErrExpectedMarginInvalid) {
		t.Fatalf("err=%v, want ErrExpectedMarginInvalid", err)
	}
}

func TestSpecialPriceRequest_QuoteNotFound(t *testing.T) {
	f := &fakeSpecialStore{quote: nil, quoteErr: nil}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 999, Reason: "r", ExpectedMargin: "0.08",
	}, 7, "req-1")
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestSpecialPriceRequest_QuoteNotDraft(t *testing.T) {
	q := baseQuote()
	q.Status = QuoteStatusEffective
	f := &fakeSpecialStore{quote: q}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 1, Reason: "r", ExpectedMargin: "0.08",
	}, 7, "req-1")
	if !errors.Is(err, ErrQuoteNotDraft) {
		t.Fatalf("err=%v", err)
	}
}

func TestSpecialPriceRequest_AlreadyPending(t *testing.T) {
	q := baseQuote()
	s := SpecialPricePending
	q.SpecialPriceStatus = &s
	f := &fakeSpecialStore{quote: q}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 1, Reason: "r", ExpectedMargin: "0.08",
	}, 7, "req-1")
	if !errors.Is(err, ErrQuoteAlreadyPending) {
		t.Fatalf("err=%v, want ErrQuoteAlreadyPending", err)
	}
}

// ============================================================
// 纯函数：margin_impact 边界
// ============================================================

func TestComputeMarginImpact_Empty(t *testing.T) {
	got := ComputeMarginImpact(nil, "0.08")
	if got.CurrentPrice != "0" || got.TargetMargin != "0.08" {
		t.Fatalf("got=%+v", got)
	}
}

func TestComputeMarginImpact_AllAboveFloor(t *testing.T) {
	// 全部正毛利——min margin = (5-3.53)/5 = 0.29411764
	items := []QuoteItemDetail{
		{SKUID: 40, Currency: "CNY", UnitPrice: dec("5.00000000"), FloorPrice: dec("3.52941176")},
		{SKUID: 41, Currency: "CNY", UnitPrice: dec("12.00000000"), FloorPrice: dec("10.58823529")},
	}
	got := ComputeMarginImpact(items, "0.08")
	if got.DeltaGapDistance != "1.41176471" { // 12-10.58823529 = 1.41176471；5-3.52941176=1.47058824；min
		t.Fatalf("gap=%s", got.DeltaGapDistance)
	}
	// margin_40 = (5-3.52941176)/5 = 0.29411764 ..
	// margin_41 = (12-10.58823529)/12 = 0.11764706 .. <- 更小
	if got.CurrentMargin != "0.11764706" {
		t.Fatalf("margin=%s", got.CurrentMargin)
	}
	if got.CurrentPrice != "5.00000000" { // min(unit_price)=5.0
		t.Fatalf("cur=%s", got.CurrentPrice)
	}
	if got.TargetPrice != got.CurrentPrice {
		t.Fatalf("target=%s vs cur=%s（应等）", got.TargetPrice, got.CurrentPrice)
	}
}

func TestComputeMarginImpact_ZeroPrice(t *testing.T) {
	items := []QuoteItemDetail{
		{SKUID: 40, Currency: "CNY", UnitPrice: dec("0"), FloorPrice: dec("3.5")},
	}
	got := ComputeMarginImpact(items, "0.0")
	if got.DeltaGapDistance != "-3.50000000" {
		t.Fatalf("gap=%s", got.DeltaGapDistance)
	}
	// unit=0 时 margin 视为 0（防除零）
	if got.CurrentMargin != "0.00000000" {
		t.Fatalf("margin=%s", got.CurrentMargin)
	}
}

// ============================================================
// SpecialPriceStatus 状态机（裁决 5）
// ============================================================

func TestSpecialPriceStatus_StringEnum(t *testing.T) {
	// 显式断言三个常量值——防误改
	if SpecialPricePending != "PENDING" || SpecialPriceApproved != "APPROVED" || SpecialPriceRejected != "REJECTED" {
		t.Fatalf("const P=%s A=%s R=%s", SpecialPricePending, SpecialPriceApproved, SpecialPriceRejected)
	}
	if ChangeSpecialPrice != "SPECIAL_PRICE" {
		t.Fatalf("change_type=%s", ChangeSpecialPrice)
	}
}

// ============================================================
// 行级数据域（红线 7 / 批次 2b：修 V3 特价越权写）
// ============================================================

func TestSpecialPriceRequest_OutOfScope_NoWrite(t *testing.T) {
	// 关键护栏：越域申请特价必须 ErrCustomerOutOfScope，且 RequestSpecialPriceTx 一次都不能调。
	// 修前：为他人报价落了 change_request + approval_step（稽核报告 §3.1 V3）。
	f := &fakeSpecialStore{
		quote:           baseQuote(),
		items:           baseItems(),
		scopeOutOfScope: true,
	}
	svc := NewSpecialPriceService(f)
	_, err := svc.Request(context.Background(), selfScope(2), SpecialPriceInput{
		QuoteID: 4, Reason: "x", ExpectedMargin: "0.20",
	}, 2, "req-1")
	if !errors.Is(err, ErrCustomerOutOfScope) {
		t.Fatalf("err=%v, want ErrCustomerOutOfScope", err)
	}
	if f.scopeCalls != 1 {
		t.Fatalf("数据域校验未被调用（scopeCalls=%d）", f.scopeCalls)
	}
	if f.scopeLastQuoteID != 4 {
		t.Fatalf("scopeLastQuoteID=%d，want 4", f.scopeLastQuoteID)
	}
	if f.reqTxCalls != 0 {
		t.Fatalf("越域后 RequestSpecialPriceTx 被调用了 %d 次——写操作必须早于它拦截", f.reqTxCalls)
	}
}

func TestSpecialPriceRequest_InScopeStillChecksScope(t *testing.T) {
	f := &fakeSpecialStore{
		quote: baseQuote(),
		items: baseItems(),
		reqRes: &SpecialPriceResult{
			QuoteID: 1, ChangeRequestID: 42, StepCount: 2, Status: SpecialPricePending,
		},
	}
	svc := NewSpecialPriceService(f)
	if _, err := svc.Request(context.Background(), selfScope(7), SpecialPriceInput{
		QuoteID: 4, Reason: "战略客户", ExpectedMargin: "0.08",
	}, 7, "req-1"); err != nil {
		t.Fatalf("err=%v", err)
	}
	if f.scopeCalls != 1 {
		t.Fatalf("数据域校验未被调用（scopeCalls=%d）", f.scopeCalls)
	}
}
