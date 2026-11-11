package customer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"model_bss/internal/domain/auth"
)

// ============================================================
// fake store
// ============================================================

type fakeExportStore struct {
	quote     *QuoteRow
	quoteErr  error
	items     []QuoteItemDetail
	itemsErr  error
	legalName string
	legalErr  error

	// 数据域校验（红线 7 / 批次 2b）：默认在域内；越域用例显式置 scopeOutOfScope=true。
	scopeOutOfScope  bool
	scopeCalls       int
	scopeLastQuoteID int64
	itemsCalls       int
	legalNameCalls   int
}

func (f *fakeExportStore) CheckQuoteOwnerScope(_ context.Context, quoteID int64, _ OwnerScope) (bool, bool, error) {
	f.scopeCalls++
	f.scopeLastQuoteID = quoteID
	if f.scopeOutOfScope {
		return true, false, nil // 存在但不在数据域内
	}
	return true, true, nil
}

func (f *fakeExportStore) LoadQuote(_ context.Context, _ int64) (*QuoteRow, error) {
	return f.quote, f.quoteErr
}
func (f *fakeExportStore) LoadQuoteItemsDetail(_ context.Context, _ int64) ([]QuoteItemDetail, error) {
	f.itemsCalls++
	return f.items, f.itemsErr
}
func (f *fakeExportStore) LoadCustomerLegalName(_ context.Context, _ int64) (string, error) {
	f.legalNameCalls++
	return f.legalName, f.legalErr
}

// selfScope 构造 SELF 数据域（批次 2b 用例用）。
func selfScope(staffID int64) OwnerScope {
	return OwnerScope{DataScope: auth.ScopeSELF, StaffID: staffID}
}

// ============================================================
// fixture
// ============================================================

func baseExportQuote() *QuoteRow {
	v := time.Now().Add(24 * time.Hour)
	return &QuoteRow{
		ID: 1, CustomerID: 1, VersionNo: 1, Status: QuoteStatusDraft,
		QuoteType: QuoteTypeTemp, ValidUntil: &v, OurCompany: "根公司",
	}
}

// baseExportItems 资源口径明细：一个海外模型 + 一个国内模型。
// 系数取落库值（与单价 ÷ 官方价恰好一致，便于早期数据核对）。
func baseExportItems() []QuoteItemDetail {
	return []QuoteItemDetail{
		{ResourceID: 40, PriceItemKey: "input", Currency: "USD", ModelName: "gpt-4o",
			Region: "OVERSEAS", QuotationCoefficient: "0.65",
			UnitPrice: dec("1.30000000"), OfficialAmount: "2.00000000",
			FloorPrice: dec("1.00000000")},
		{ResourceID: 41, PriceItemKey: "input", Currency: "CNY", ModelName: "混元 全系（含混元3）",
			Region: "CN", QuotationCoefficient: "0.61",
			UnitPrice: dec("1.22000000"), OfficialAmount: "2.00000000",
			FloorPrice: dec("0.90000000")},
	}
}

func openXLSX(t *testing.T, buf []byte) *excelize.File {
	t.Helper()
	xls, err := excelize.OpenReader(io.NopCloser(bytes.NewReader(buf)))
	if err != nil {
		t.Fatalf("重开 XLSX 失败：%v", err)
	}
	return xls
}

// ============================================================
// 模板结构
// ============================================================

func TestExport_TwoSheetsModelAndCoefficient(t *testing.T) {
	f := &fakeExportStore{quote: baseExportQuote(), items: baseExportItems()}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(buf) == 0 {
		t.Fatalf("buffer empty")
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()

	sheets := xls.GetSheetList()
	if len(sheets) != 2 || sheets[0] != SheetOverseas || sheets[1] != SheetDomestic {
		t.Fatalf("sheet 列表=%v，应为 [海外 国内]", sheets)
	}

	// 两张表都是两列表头。
	for _, sheet := range []string{SheetOverseas, SheetDomestic} {
		if v, _ := xls.GetCellValue(sheet, "A1"); v != "模型名" {
			t.Fatalf("%s!A1=%q", sheet, v)
		}
		if v, _ := xls.GetCellValue(sheet, "B1"); v != "对外报价" {
			t.Fatalf("%s!B1=%q", sheet, v)
		}
		// 只有两列：C 列必须为空。
		if v, _ := xls.GetCellValue(sheet, "C1"); v != "" {
			t.Fatalf("%s!C1=%q，模板只有两列", sheet, v)
		}
	}

	// 海外：gpt-4o / 0.65
	if v, _ := xls.GetCellValue(SheetOverseas, "A2"); v != "gpt-4o" {
		t.Fatalf("海外!A2=%q", v)
	}
	if v, _ := xls.GetCellValue(SheetOverseas, "B2"); v != "0.65" {
		t.Fatalf("海外!B2=%q，应为 0.65", v)
	}
	// 国内：混元 / 0.61
	if v, _ := xls.GetCellValue(SheetDomestic, "A2"); v != "混元 全系（含混元3）" {
		t.Fatalf("国内!A2=%q", v)
	}
	if v, _ := xls.GetCellValue(SheetDomestic, "B2"); v != "0.61" {
		t.Fatalf("国内!B2=%q，应为 0.61", v)
	}
}

// TestExport_NoInternalFields 守住"内部数据不外泄"：底价、破线、SKU、币种、
// 单价、报价类型、水印文案都不得出现在文件里。任何人把这些加回模板都会红。
func TestExport_NoInternalFields(t *testing.T) {
	f := &fakeExportStore{quote: baseExportQuote(), items: baseExportItems()}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()

	forbidden := []string{
		"floor_price", "below_floor", "unit_price", "sku_id", "quote_type",
		"status", "exported_at", "valid_until", "临时报价",
		"1.00000000", "0.90000000", // fixture 里的底价，不能出现在导出中
		"USD", "CNY",
	}
	for _, sheet := range xls.GetSheetList() {
		rows, err := xls.GetRows(sheet)
		if err != nil {
			t.Fatalf("%s GetRows: %v", sheet, err)
		}
		for _, row := range rows {
			for _, cell := range row {
				low := strings.ToLower(cell)
				for _, bad := range forbidden {
					if strings.Contains(low, strings.ToLower(bad)) {
						t.Fatalf("%s 出现内部字段 %q（单元格=%q）", sheet, bad, cell)
					}
				}
			}
		}
	}
}

// TestExport_CoefficientIsStoredValueNotDerived 守核心口径：导出的系数必须是
// 报价时存下来的值，**不能**用 单价 ÷ 官方价 反推。
//
// 构造一条"反推会算错"的数据：单价 1.50 / 官方价 2.00 反推得 0.75，
// 但当初存的是 0.60（例如官方价后来涨过）。导出必须显示 0.60。
func TestExport_CoefficientIsStoredValueNotDerived(t *testing.T) {
	items := []QuoteItemDetail{
		{ResourceID: 40, PriceItemKey: "input", Currency: "USD", ModelName: "gpt-4o",
			Region: "OVERSEAS", QuotationCoefficient: "0.60",
			UnitPrice: dec("1.50000000"), OfficialAmount: "2.00000000"},
	}
	f := &fakeExportStore{quote: baseExportQuote(), items: items}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()
	if v, _ := xls.GetCellValue(SheetOverseas, "B2"); v != "0.60" {
		t.Fatalf("B2=%q，应为落库的 0.60（反推会得到 0.75）", v)
	}
}

// TestExport_SameModelMultipleItemsCollapsed 一个模型 input/output 两行价项，
// 导出只出一行（一模型一系数）。
func TestExport_SameModelMultipleItemsCollapsed(t *testing.T) {
	items := []QuoteItemDetail{
		{ResourceID: 40, PriceItemKey: "input", Currency: "USD", ModelName: "gpt-4o",
			Region: "OVERSEAS", QuotationCoefficient: "0.65",
			UnitPrice: dec("1.30000000"), OfficialAmount: "2.00000000"},
		{ResourceID: 40, PriceItemKey: "output", Currency: "USD", ModelName: "gpt-4o",
			Region: "OVERSEAS", QuotationCoefficient: "0.65",
			UnitPrice: dec("1.95000000"), OfficialAmount: "3.00000000"},
	}
	f := &fakeExportStore{quote: baseExportQuote(), items: items}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()

	if v, _ := xls.GetCellValue(SheetOverseas, "A3"); v != "" {
		t.Fatalf("A3=%q，同一模型应折叠成一行", v)
	}
}

// TestExport_SkipRowWhenCoefficientMissing 历史单据没有系数（000068 之前）时跳过，
// 绝不从单价倒推一个数出来。
func TestExport_SkipRowWhenCoefficientMissing(t *testing.T) {
	items := []QuoteItemDetail{
		{ResourceID: 40, PriceItemKey: "input", Currency: "USD", ModelName: "历史单里的模型",
			Region: "OVERSEAS", QuotationCoefficient: "",
			UnitPrice: dec("1.30000000"), OfficialAmount: "2.00000000"},
	}
	f := &fakeExportStore{quote: baseExportQuote(), items: items}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()
	if v, _ := xls.GetCellValue(SheetOverseas, "A2"); v != "" {
		t.Fatalf("没有系数的行不应输出，A2=%q", v)
	}
}

// TestExport_SkipUnknownRegion 区域未知（SKU 口径老数据）不塞进任何一张表。
func TestExport_SkipUnknownRegion(t *testing.T) {
	items := []QuoteItemDetail{
		{SKUID: 9, Currency: "CNY", ModelName: "老口径模型", QuotationCoefficient: "0.8",
			UnitPrice: dec("1.00000000"), OfficialAmount: "1.00000000"},
	}
	f := &fakeExportStore{quote: baseExportQuote(), items: items}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()
	for _, sheet := range []string{SheetOverseas, SheetDomestic} {
		if v, _ := xls.GetCellValue(sheet, "A2"); v != "" {
			t.Fatalf("%s!A2=%q，region 未知时不应输出", sheet, v)
		}
	}
}

// TestExport_EmptyItemsStillHasTwoSheets 没有明细时仍保留两张空表，
// 客户看到的是"这份报价没有模型"，而不是缺表。
func TestExport_EmptyItemsStillHasTwoSheets(t *testing.T) {
	f := &fakeExportStore{quote: baseExportQuote(), items: nil}
	svc := NewExportService(f, fixedNow, nil)
	buf, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	xls := openXLSX(t, buf)
	defer func() { _ = xls.Close() }()
	sheets := xls.GetSheetList()
	if len(sheets) != 2 {
		t.Fatalf("sheet=%v，应为两张", sheets)
	}
	if v, _ := xls.GetCellValue(SheetOverseas, "A1"); v != "模型名" {
		t.Fatalf("空表也要有表头，A1=%q", v)
	}
}

// ============================================================
// 校验与错误路径
// ============================================================

func TestExport_QuoteNotFound(t *testing.T) {
	f := &fakeExportStore{quote: nil}
	svc := NewExportService(f, fixedNow, nil)
	_, err := svc.GenerateXLSX(context.Background(), selfScope(7), 999, "xlsx")
	if !errors.Is(err, ErrQuoteForExportNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestExport_FormatUnsupported(t *testing.T) {
	f := &fakeExportStore{}
	svc := NewExportService(f, fixedNow, nil)
	_, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "pdf")
	if !errors.Is(err, ErrExportFormatUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestExport_Expired(t *testing.T) {
	q := baseExportQuote()
	// valid_until 必须相对 fixedNow 派生：service 用 svc.now()（= fixedNow）比较。
	past := fixedNow().Add(-24 * time.Hour)
	q.ValidUntil = &past
	f := &fakeExportStore{quote: q, items: baseExportItems()}
	svc := NewExportService(f, fixedNow, nil)
	_, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx")
	if !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("err=%v", err)
	}
}

func TestExport_ApplyNotExpiredEvenIfPastValidUntil(t *testing.T) {
	// 非 TEMP 不受 valid_until 拦截（APPLY/CLONE 长期价格）
	q := baseExportQuote()
	q.QuoteType = QuoteTypeApply
	past := time.Now().Add(-24 * time.Hour)
	q.ValidUntil = &past
	f := &fakeExportStore{quote: q, items: baseExportItems()}
	svc := NewExportService(f, fixedNow, nil)
	if _, err := svc.GenerateXLSX(context.Background(), selfScope(7), 1, "xlsx"); err != nil {
		t.Fatalf("APPLY 即使有过期 valid_until 也不应 409：%v", err)
	}
}

// ============================================================
// 行级数据域（红线 7 / 批次 2b：修 V1 导出越权读）
// ============================================================

func TestExport_OutOfScope(t *testing.T) {
	// 越域导出必须 ErrCustomerOutOfScope（→403），且不得继续取明细。
	f := &fakeExportStore{
		quote:           baseExportQuote(),
		items:           baseExportItems(),
		legalName:       "东信公司",
		scopeOutOfScope: true,
	}
	svc := NewExportService(f, fixedNow, nil)
	_, err := svc.GenerateXLSX(context.Background(), selfScope(2), 2, "xlsx")
	if !errors.Is(err, ErrCustomerOutOfScope) {
		t.Fatalf("err=%v, want ErrCustomerOutOfScope", err)
	}
	if f.scopeCalls != 1 {
		t.Fatalf("数据域校验未被调用（scopeCalls=%d）——防漏拼护栏失效", f.scopeCalls)
	}
	if f.scopeLastQuoteID != 2 {
		t.Fatalf("scope 校验传错 quoteID=%d，want 2", f.scopeLastQuoteID)
	}
	if f.itemsCalls != 0 || f.legalNameCalls != 0 {
		t.Fatalf("越域后不得继续取数：itemsCalls=%d legalNameCalls=%d", f.itemsCalls, f.legalNameCalls)
	}
}

func TestExport_InScopeStillChecksScope(t *testing.T) {
	// 在域内也必须真的走过校验——防有人整段删掉校验后测试仍绿。
	f := &fakeExportStore{quote: baseExportQuote(), items: baseExportItems()}
	svc := NewExportService(f, fixedNow, nil)
	if _, err := svc.GenerateXLSX(context.Background(), selfScope(7), 2, "xlsx"); err != nil {
		t.Fatalf("err=%v", err)
	}
	if f.scopeCalls != 1 {
		t.Fatalf("数据域校验未被调用（scopeCalls=%d）", f.scopeCalls)
	}
	if f.scopeLastQuoteID != 2 {
		t.Fatalf("scopeLastQuoteID=%d，want 2", f.scopeLastQuoteID)
	}
}
