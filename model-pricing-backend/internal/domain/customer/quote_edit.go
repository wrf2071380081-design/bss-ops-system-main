package customer

import (
	"context"
	"fmt"
	"time"
)

// QuoteForEdit 是「继续编辑」回填报价编辑器所需的数据。
//
// 为什么需要它：报价单落库后，编辑器手里没有明细，点「继续编辑」只能开一张空白单，
// 销售看到的就是「模型 0 个」。原型里继续编辑会把上次的模型、来源、折扣都带出来。
//
// 口径：明细按「供应来源（resource）」分组，一组就是编辑器里的一行；
// 行内价项带官方价（amount）与已存单价（unit_price），前端据此反算报价系数与毛利率。
type QuoteForEdit struct {
	ID           int64      `json:"id"`
	CustomerID   int64      `json:"customer_id"`
	CustomerName string     `json:"customer_name"`
	VersionNo    int        `json:"version_no"`
	Status       string     `json:"status"`
	OurCompany   string     `json:"our_company"`
	ValidUntil   *time.Time `json:"valid_until"`
	// Lines 只含资源口径明细；SKU 口径的老单没有 resource_id，无法回填到资源口径编辑器，
	// 会进 SkippedItems（前端据此提示「有 N 条老口径明细未带出」）。
	Lines        []QuoteForEditLine `json:"lines"`
	SkippedItems int                `json:"skipped_items"`
}

// QuoteForEditLine 是编辑器里的一行：一个模型 + 一个供应来源。
type QuoteForEditLine struct {
	ResourceID      int64  `json:"resource_id"`
	StandardModelID int64  `json:"standard_model_id"`
	ModelName       string `json:"model_name"`
	OfficialID      string `json:"official_id"`
	Company         string `json:"company"`
	ModelType       string `json:"model_type"`
	SupplierID      int64  `json:"supplier_id"`
	SupplierName    string `json:"supplier_name"`
	SupplierModel   string `json:"supplier_model"`
	Region          string `json:"region"`
	// Coefficient 是采购系数（资源的成本折扣），不是客户报价系数。
	Coefficient string `json:"coefficient"`
	// QuotationCoefficient 是对外报价系数（000068 起落库，编辑器填的原值）。
	// 为空表示历史单据没记录——前端只能用「单价 ÷ 官方价」兜一个初值，仅用于编辑。
	QuotationCoefficient string `json:"quotation_coefficient"`
	// 来源最近一次测试结果：报价行里直接摆连通/RPS/TPS/缓存命中率。
	// 回填老单也要看得到「这个来源当初跑得怎么样」，不能因为是从库里读的就空着。
	TestConnectionMS string                  `json:"test_connection_ms"`
	TestRPS          string                  `json:"test_rps"`
	TestTPS          string                  `json:"test_tps"`
	TestCacheHitRate string                  `json:"test_cache_hit_rate"`
	PriceItems       []QuoteForEditPriceItem `json:"price_items"`
}

// QuoteForEditPriceItem 是行内的一个价项：官方价 + 这张单已经报的单价。
type QuoteForEditPriceItem struct {
	ItemKey      string `json:"item_key"`
	Label        string `json:"label"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	UnitCode     string `json:"unit_code"`
	UnitQuantity string `json:"unit_quantity"`
	// UnitPrice / FloorPrice 是这张报价单落库时的值（快照），回填时原样带回。
	UnitPrice  string `json:"unit_price"`
	FloorPrice string `json:"floor_price"`
}

// GetQuoteForEdit 读一张报价单用于继续编辑（M9:V + 行级数据域）。
func (s *Service) GetQuoteForEdit(ctx context.Context, quoteID int64, scope OwnerScope) (*QuoteForEdit, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("客户仓储不可用")
	}
	if quoteID <= 0 {
		return nil, ErrQuoteNotFound
	}
	quote, err := s.store.LoadQuoteForEdit(ctx, quoteID)
	if err != nil {
		return nil, fmt.Errorf("load quote for edit: %w", err)
	}
	if quote == nil {
		return nil, ErrQuoteNotFound
	}
	// 行级数据域：和审批/特价一样，越域按 403 处理，不能靠前端藏按钮。
	found, inScope, err := s.store.CheckCustomerScope(ctx, quote.CustomerID, scope)
	if err != nil {
		return nil, fmt.Errorf("check customer scope: %w", err)
	}
	if !found {
		return nil, ErrCustomerNotFound
	}
	if !inScope {
		return nil, ErrCustomerOutOfScope
	}
	return quote, nil
}
