// Package customer 的 resource_quote.go：客户报价的资源口径（RESOURCE）。
//
// 背景：客户报价原先只认 model_sku（定价域口径）。但我们真正卖出去的是
// 「某供应商在某区域提供的某个标准模型」——即供应域的 resource（带采购系数、
// 正式配置、测试证据、可报价状态）。两侧模型集当前不重叠，resource 无法反推 sku，
// 所以报价明细要直接记 resource_id（迁移 000048）。
//
// 成本口径（与定价域 SKU 口径并列，不是替换）：
//   - 资源口径 unit_cost = 官方价项金额 × 采购系数（供应域口径：采购系数即供应商报价折扣）
//   - floor = unit_cost / (1 - min_gross_margin)，复用 cost.Floor 同一份公式
//   - 老 SKU 口径仍读 cost_baseline，不受影响
//
// 边界：
//   - 候选只给供应域判定「可报价」的资源（ADMITTED + 正式配置已确认 + 测试证据有效 +
//     模型就绪），不在这里重算一遍可报价判定，避免两处口径漂移。
//   - 资源明细带 price_item_key：官方价分项（text_input / text_output …）的键。
package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// QuoteTypeResource 按供应资源出单（候选=已接入且可报价的资源）。
const QuoteTypeResource = "RESOURCE"

// 领域错误。
var (
	ErrResourceStoreUnavailable = errors.New("资源口径报价依赖不可用")
	ErrResourceNotQuotable      = errors.New("资源不可报价（未接入、正式配置未确认、测试证据无效或模型未就绪）")
	ErrResourcePriceItem        = errors.New("资源没有该官方价项")
	ErrResourceDuplicate        = errors.New("同一资源不能重复出现在一张报价单里")
)

// ResourceQuoteCandidate 是报价候选资源（一行一条可报价资源）。
type ResourceQuoteCandidate struct {
	ResourceID     int64    `json:"resource_id"`
	ModelName      string   `json:"model_name"`
	OfficialID     string   `json:"official_id"`
	Company        string   `json:"company"`
	ModelType      string   `json:"model_type"`
	SupplierID     int64    `json:"supplier_id"`
	SupplierName   string   `json:"supplier_name"`
	Region         string   `json:"region"`
	SupplierModel  string   `json:"supplier_model_id"`
	Coefficient    string   `json:"coefficient"`
	BlockedReasons []string `json:"blocked_reasons,omitempty"`
	// PriceItems 是该资源的官方价项（键 + 金额 + 单位量），供前端按分项报价。
	PriceItems []ResourcePriceItem `json:"price_items"`
}

// ResourcePriceItem 是候选资源上的一个官方价项。
type ResourcePriceItem struct {
	ItemKey      string `json:"item_key"`
	Label        string `json:"label"`
	TaskType     string `json:"task_type"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	UnitCode     string `json:"unit_code"`
	UnitQuantity string `json:"unit_quantity"`
	// UnitCost 是采购成本 = Amount × Coefficient（同 unit_quantity 口径）。
	UnitCost string `json:"unit_cost"`
}

// ResourceQuoteCandidatePage 是候选资源分页。
type ResourceQuoteCandidatePage struct {
	List  []ResourceQuoteCandidate `json:"list"`
	Total int64                    `json:"total"`
	Page  int                      `json:"page"`
	Size  int                      `json:"size"`
}

// ResourceQuoteItemInput 是一条资源口径报价明细的输入。
type ResourceQuoteItemInput struct {
	ResourceID   int64  `json:"resource_id"`
	PriceItemKey string `json:"price_item_key"`
	UnitPrice    string `json:"unit_price"` // decimal 字符串
}

// ResourceQuoteStore 是资源口径报价需要的只读端口。
//
// 候选与可报价判定归供应域所有；这里只声明客户域需要的最小形状，
// 由供应域适配器实现（见 internal/api/supply_resource_quote.go）。
type ResourceQuoteStore interface {
	// ListResourceQuoteCandidates 列出可报价资源候选（供应域已判可报价的）。
	ListResourceQuoteCandidates(ctx context.Context, keyword string, page, size int) (*ResourceQuoteCandidatePage, error)
	// LoadResourceQuoteItems 按 (resource_id, price_item_key) 取采购成本口径。
	// 找不到 → ErrResourceNotQuotable / ErrResourcePriceItem。
	LoadResourceQuoteItems(ctx context.Context, refs []ResourceQuoteItemInput) (map[string]ResourceQuoteItem, error)
}

// ResourceQuoteItem 是资源口径明细的成本口径快照。
type ResourceQuoteItem struct {
	ResourceID   int64  `json:"resource_id"`
	PriceItemKey string `json:"price_item_key"`
	Amount       string `json:"amount"`      // 官方价
	Coefficient  string `json:"coefficient"` // 采购系数
	UnitCost     string `json:"unit_cost"`   // Amount × Coefficient
	Currency     string `json:"currency"`
	ModelName    string `json:"model_name"`
	SupplierName string `json:"supplier_name"`
}

// resourceQuoteKey 是明细去重与回查的稳定键。
func resourceQuoteKey(resourceID int64, priceItemKey string) string {
	return fmt.Sprintf("%d:%s", resourceID, priceItemKey)
}

// ResourceUnitCost 纯函数：采购成本 = 官方价项金额 × 采购系数。
//
// 系数与官方价项金额必须同 unit_quantity 口径（供应域已保证）。金额一律 decimal，
// 不用 float64；乘法结果截到 8 位，与 customer_quote_item 的 numeric(20,8) 落库一致。
func ResourceUnitCost(amount, coefficient string) (decimal.Decimal, error) {
	price, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil {
		return decimal.Zero, fmt.Errorf("解析官方价 %q: %w", amount, err)
	}
	factor, err := decimal.NewFromString(strings.TrimSpace(coefficient))
	if err != nil {
		return decimal.Zero, fmt.Errorf("解析采购系数 %q: %w", coefficient, err)
	}
	if price.IsNegative() || factor.IsNegative() {
		return decimal.Zero, errors.New("官方价与采购系数不能为负")
	}
	return price.Mul(factor).Truncate(8).Round(8), nil
}

// BuildResourceQuoteFloor 纯函数：资源口径 floor = 采购成本 / (1 - 最低毛利)。
// 与 SKU 口径共用 cost.Floor 同一份公式，只是 unit_cost 来源不同。
func BuildResourceQuoteFloor(unitCost decimal.Decimal, minGrossMargin decimal.Decimal) (decimal.Decimal, error) {
	return CalcFloor(unitCost, minGrossMargin)
}

// ResourceFloorViolation 是资源口径的 floor 违规明细（409 响应体）。
type ResourceFloorViolation struct {
	ResourceID   int64  `json:"resource_id"`
	PriceItemKey string `json:"price_item_key"`
	ModelName    string `json:"model_name"`
	SupplierName string `json:"supplier_name"`
	UnitPrice    string `json:"unit_price"`
	FloorPrice   string `json:"floor_price"`
}

// ResourceQuoteFloorError 是资源口径 floor 校验失败（携带违规明细给 409 响应）。
type ResourceQuoteFloorError struct {
	Violations []ResourceFloorViolation
}

// Error 实现 error 接口（与 SKU 口径同一句话）。
func (e *ResourceQuoteFloorError) Error() string {
	return fmt.Sprintf("%s（违规 %d 项）", ErrBelowFloor.Error(), len(e.Violations))
}

// Unwrap 让 errors.Is(err, ErrBelowFloor) 命中。
func (e *ResourceQuoteFloorError) Unwrap() error { return ErrBelowFloor }

// GenerateResourceQuote 生成资源口径客户报价（M9:E + 幂等）。
//
// 步骤：
//  1. 归属校验：客户必须在操作员数据域内（与 SKU 口径同一位置）。
//  2. items：每条是 (resource_id, price_item_key, unit_price)；同资源不同价项算两条，
//     同一资源同一价项重复 → ErrResourceDuplicate。
//  3. 取成本口径：供应域按 (resource_id, price_item_key) 给官方价 × 采购系数；
//     资源不可报价 → ErrResourceNotQuotable；没有该价项 → ErrResourcePriceItem。
//  4. floor 校验：unit_price < floor → ResourceQuoteFloorError（errors.Is 命中 ErrBelowFloor）。
//  5. 落库：customer_quote_item 写 resource_id + price_item_key，sku_id 留 NULL。
//
// 与 SKU 口径的关系：两者并存，不是替换。APPLY/CLONE/TEMP 仍是 SKU 口径，
// RESOURCE 是新增的第四条路径，用于「按供应资源出单」。
func (s *Service) GenerateResourceQuote(ctx context.Context, in GenerateResourceQuoteInput, scope OwnerScope, operatorID int64, requestID string) (*GeneratedQuote, error) {
	if s == nil || s.store == nil {
		return nil, ErrResourceStoreUnavailable
	}
	if len(in.Items) == 0 {
		return nil, fmt.Errorf("%w：items 不能为空", ErrInvalidQuoteType)
	}
	found, inScope, err := s.store.CheckCustomerScope(ctx, in.CustomerID, scope)
	if err != nil {
		return nil, fmt.Errorf("check customer scope: %w", err)
	}
	if !found {
		return nil, ErrCustomerNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	levelCode, ownerSalesID, err := s.store.LoadCustomerLevel(ctx, in.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("load customer level: %w", err)
	}
	_ = levelCode

	// 去重：同一 (resource_id, price_item_key) 只能出现一次。
	seen := map[string]bool{}
	refs := make([]ResourceQuoteItemInput, 0, len(in.Items))
	for _, it := range in.Items {
		if it.ResourceID <= 0 || strings.TrimSpace(it.PriceItemKey) == "" {
			return nil, fmt.Errorf("%w：resource_id 与 price_item_key 必填", ErrInvalidQuoteType)
		}
		key := resourceQuoteKey(it.ResourceID, it.PriceItemKey)
		if seen[key] {
			return nil, ErrResourceDuplicate
		}
		seen[key] = true
		refs = append(refs, it)
	}

	bases, err := s.resources.LoadResourceQuoteItems(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("load resource quote items: %w", err)
	}

	margin, err := s.store.LoadMinGrossMargin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load min_gross_margin: %w", err)
	}

	rows := make([]GenerateQuoteItemRow, 0, len(refs))
	violations := make([]ResourceFloorViolation, 0)
	for _, it := range refs {
		base, ok := bases[resourceQuoteKey(it.ResourceID, it.PriceItemKey)]
		if !ok {
			return nil, ErrResourceNotQuotable
		}
		unitPrice, perr := decimal.NewFromString(strings.TrimSpace(it.UnitPrice))
		if perr != nil {
			return nil, fmt.Errorf("解析 resource=%d 的 unit_price: %w", it.ResourceID, perr)
		}
		cost, cerr := decimal.NewFromString(base.UnitCost)
		if cerr != nil {
			return nil, fmt.Errorf("解析 resource=%d 的 unit_cost: %w", it.ResourceID, cerr)
		}
		floor, ferr := BuildResourceQuoteFloor(cost, margin)
		if ferr != nil {
			return nil, fmt.Errorf("floor resource=%d: %w", it.ResourceID, ferr)
		}
		if unitPrice.LessThan(floor) {
			violations = append(violations, ResourceFloorViolation{
				ResourceID: it.ResourceID, PriceItemKey: it.PriceItemKey,
				ModelName: base.ModelName, SupplierName: base.SupplierName,
				UnitPrice: unitPrice.StringFixed(8), FloorPrice: floor.StringFixed(8),
			})
		}
		rows = append(rows, GenerateQuoteItemRow{
			ResourceID: it.ResourceID, PriceItemKey: it.PriceItemKey,
			Currency: base.Currency, UnitPrice: unitPrice, FloorPrice: floor,
		})
	}
	if len(violations) > 0 {
		return nil, &ResourceQuoteFloorError{Violations: violations}
	}

	res, err := s.store.GenerateQuoteTx(ctx, GenerateQuoteTxInput{
		CustomerID:   in.CustomerID,
		QuoteType:    QuoteTypeResource,
		ValidUntil:   in.ValidTo,
		OwnerSalesID: ownerSalesID,
		Items:        rows,
	}, operatorID, requestID)
	if err != nil {
		return nil, fmt.Errorf("generate quote tx: %w", err)
	}
	return res, nil
}

// GenerateResourceQuoteInput 是资源口径报价的输入。
type GenerateResourceQuoteInput struct {
	CustomerID int64                    `json:"customer_id"`
	ValidTo    *time.Time               `json:"valid_to,omitempty"`
	Items      []ResourceQuoteItemInput `json:"items"`
}

// ListResourceQuoteCandidates 列出报价候选资源（可报价的资源）。
func (s *Service) ListResourceQuoteCandidates(ctx context.Context, keyword string, page, size int) (*ResourceQuoteCandidatePage, error) {
	if s == nil || s.resources == nil {
		return nil, ErrResourceStoreUnavailable
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return s.resources.ListResourceQuoteCandidates(ctx, keyword, page, size)
}
