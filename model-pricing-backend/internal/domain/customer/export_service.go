// Package customer 的 export_service.go：报价单导出。
//
// 模板口径（2026-11-10 PM 确认，对齐 TokenOps 报价单模板）：
//   - 一个文件两个 sheet：海外（在前）/ 国内（在后）
//   - 每个 sheet 只有两列：模型名 / 对外报价（系数）
//   - 不加抬头：客户名、我方公司、报价单号、状态、导出时间、有效期一律不出现
//   - TEMP 的红字水印已按 PM 要求取消
//
// 安全约束：底价（floor_price）与破线标记（below_floor）属于内部风控数据，
// 任何情况下都不得出现在导出文件里。改动本文件时请先读这条。
//
// 范围（裁决 4）：只做 XLSX；PDF 仍是 9b-② 遗留。
package customer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// 领域错误。
var (
	ErrExportFormatUnsupported = errors.New("导出格式暂不支持（本批仅支持 xlsx；pdf 登记为 9b-② 遗留）")
	ErrQuoteExpired            = errors.New("临时报价已过期（valid_until < now），不能导出")
	ErrQuoteForExportNotFound  = errors.New("客户报价不存在")
)

// 导出模板常量：两张表，顺序与 PM 模板一致（海外在前）。
const (
	// SheetOverseas / SheetDomestic 是导出文件里两张表的名字，也是分组键。
	SheetOverseas = "海外"
	SheetDomestic = "国内"

	colModelName = "模型名"
	colQuotation = "对外报价"

	// 列宽照 PM 模板设置，只影响可读性，不影响内容。
	widthModelName = 34.83
	widthQuotation = 14.83
)

// ExportService 客户报价导出。
type ExportService struct {
	store ExportStore
	now   func() time.Time
	loc   *time.Location
}

// NewExportService 构造；now=nil 用 time.Now；loc=nil 用 Asia/Shanghai。
func NewExportService(store ExportStore, now func() time.Time, loc *time.Location) *ExportService {
	if now == nil {
		now = time.Now
	}
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Shanghai")
	}
	return &ExportService{store: store, now: now, loc: loc}
}

// ExportStore 是导出所需只读视图。
type ExportStore interface {
	// CheckQuoteOwnerScope 行级数据域校验（红线 7）：越域 → inScope=false。
	// service 必须在 LoadQuote 之后、渲染之前调用，越域返回 ErrCustomerOutOfScope。
	CheckQuoteOwnerScope(ctx context.Context, quoteID int64, scope OwnerScope) (found, inScope bool, err error)
	// LoadQuote 同 SpecialPriceStore 的读取口径。
	LoadQuote(ctx context.Context, quoteID int64) (*QuoteRow, error)
	// LoadQuoteItemsDetail 读明细（含模型名、区域、官方价，导出算系数要用）。
	LoadQuoteItemsDetail(ctx context.Context, quoteID int64) ([]QuoteItemDetail, error)
	// LoadCustomerLegalName 读 legal_subject.legal_name。
	// 模板取消抬头与水印后导出已不再调用，保留给后续 PDF 版（需要抬头）使用。
	LoadCustomerLegalName(ctx context.Context, customerID int64) (string, error)
}

// GenerateXLSX 生成 XLSX 文件流（直返 []byte）。
//
// scope 是当前操作员的数据域（红线 7）。越域一律 ErrCustomerOutOfScope（→403），
// 不得因"只是读"而放行——导出文件含对外报价，属越权读。
func (s *ExportService) GenerateXLSX(ctx context.Context, scope OwnerScope, quoteID int64, format string) ([]byte, error) {
	if format != "xlsx" {
		return nil, ErrExportFormatUnsupported
	}
	quote, err := s.store.LoadQuote(ctx, quoteID)
	if err != nil {
		return nil, fmt.Errorf("load quote %d: %w", quoteID, err)
	}
	if quote == nil {
		return nil, ErrQuoteForExportNotFound
	}
	// 数据域校验：紧跟 LoadQuote，早于任何后续取数（含 items）。
	if err := checkQuoteOwnerScope(ctx, s.store, scope, quoteID, ErrQuoteForExportNotFound); err != nil {
		return nil, err
	}
	// TEMP 才校验 valid_until（其余类型是长期价格条目，不该因临时校验被拒）。
	if quote.QuoteType == QuoteTypeTemp {
		if quote.ValidUntil != nil && s.now().After(*quote.ValidUntil) {
			return nil, ErrQuoteExpired
		}
	}
	items, err := s.store.LoadQuoteItemsDetail(ctx, quoteID)
	if err != nil {
		return nil, fmt.Errorf("load items: %w", err)
	}
	return s.renderXLSX(items)
}

// quotationLine 是导出一行：一个模型 + 一个对外报价系数。
type quotationLine struct {
	ModelName   string
	Coefficient string
}

// renderXLSX 按模板渲染：固定两张表，只写「模型名 / 对外报价」。
func (s *ExportService) renderXLSX(items []QuoteItemDetail) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	lines := groupQuotationLines(items)

	first := true
	for _, sheet := range []string{SheetOverseas, SheetDomestic} {
		if first {
			// excelize 新建文件自带 "Sheet1"，改名为第一张表，避免残留空表。
			if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
				return nil, fmt.Errorf("rename sheet: %w", err)
			}
			first = false
		} else if _, err := f.NewSheet(sheet); err != nil {
			return nil, fmt.Errorf("create sheet %s: %w", sheet, err)
		}
		_ = f.SetColWidth(sheet, "A", "A", widthModelName)
		_ = f.SetColWidth(sheet, "B", "B", widthQuotation)
		_ = f.SetCellValue(sheet, "A1", colModelName)
		_ = f.SetCellValue(sheet, "B1", colQuotation)
		for i, line := range lines[sheet] {
			row := i + 2
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), line.ModelName)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), line.Coefficient)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

// groupQuotationLines 把明细折成两张表的行：一个模型一行、只留对外报价系数。
//
// 折叠规则（每一条都是为了不给客户错数）：
//   - 一个模型有多个价项（input/output）时只出一行：业务上一模型一个系数，
//     两个价项算出的系数本应相同，取排序后的第一行以保证输出确定性；
//   - 算不出系数的行跳过（官方价缺失、非正数、币种不一致），不猜一个数；
//   - region 为空或未知（SKU 口径老数据）跳过：宁可少一行，也不能把模型
//     塞进错误的区域报出去。
func groupQuotationLines(items []QuoteItemDetail) map[string][]quotationLine {
	out := map[string][]quotationLine{SheetOverseas: {}, SheetDomestic: {}}
	seen := map[string]map[string]bool{SheetOverseas: {}, SheetDomestic: {}}

	for _, it := range items {
		sheet := sheetOfRegion(it.Region)
		if sheet == "" {
			continue
		}
		name := strings.TrimSpace(it.ModelName)
		if name == "" || seen[sheet][name] {
			continue
		}
		coef := strings.TrimSpace(it.QuotationCoefficient)
		if coef == "" {
			// 历史单据（000068 之前）没有记录系数。不猜、不从单价倒推——官方价一变
			// 倒推值就失真。这类行直接跳过，重新保存一次报价单即可补上。
			continue
		}
		seen[sheet][name] = true
		out[sheet] = append(out[sheet], quotationLine{ModelName: name, Coefficient: coef})
	}

	for _, sheet := range []string{SheetOverseas, SheetDomestic} {
		rows := out[sheet]
		sort.Slice(rows, func(i, j int) bool { return rows[i].ModelName < rows[j].ModelName })
	}
	return out
}

// sheetOfRegion 区域 → 目标表；未知区域返回空串（不导出）。
func sheetOfRegion(region string) string {
	switch strings.ToUpper(strings.TrimSpace(region)) {
	case "OVERSEAS":
		return SheetOverseas
	case "CN":
		return SheetDomestic
	default:
		return ""
	}
}
