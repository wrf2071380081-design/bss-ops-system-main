package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"model_bss/internal/domain/customer"
)

// trimTrailingZero 把 numeric 直接 cast 成 text 得到的 "0.700000" 去尾零成 "0.7"。
// 只用于回填/展示，不做数值校验；空串原样返回。
func trimTrailingZero(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// quoteEditItemRow 是「继续编辑」回填的明细读侧：一条价项 + 它的来源与官方价项。
type quoteEditItemRow struct {
	ResourceID          *int64  `gorm:"column:resource_id"`
	PriceItemKey        *string `gorm:"column:price_item_key"`
	UnitPrice           string  `gorm:"column:unit_price"`
	FloorPrice          string  `gorm:"column:floor_price"`
	// QuotationCoefficient 落库的对外报价系数（000068 起）；历史单据为空串。
	QuotationCoefficient string `gorm:"column:quotation_coefficient"`
	Currency            string  `gorm:"column:currency"`
	StandardModelID     int64   `gorm:"column:standard_model_id"`
	SupplierID          int64   `gorm:"column:supplier_id"`
	Region              string  `gorm:"column:region"`
	ResourceCoefficient string  `gorm:"column:resource_coefficient"`
	SupplierModelName   string  `gorm:"column:supplier_model_name"`
	ModelName           string  `gorm:"column:model_name"`
	OfficialID          string  `gorm:"column:official_id"`
	Company             string  `gorm:"column:company"`
	ModelType           string  `gorm:"column:model_type"`
	PriceItemsJSON      string  `gorm:"column:price_items"`
	SupplierName        string  `gorm:"column:supplier_name"`
	TestConnectionMS    string  `gorm:"column:test_connection_ms"`
	TestRPS             string  `gorm:"column:test_rps"`
	TestTPS             string  `gorm:"column:test_tps"`
	TestHitTokens       string  `gorm:"column:test_hit_tokens"`
	TestInputTokens     string  `gorm:"column:test_input_tokens"`
}

// officialPriceItem 是 standard_model.price_items 里的一项。
type officialPriceItem struct {
	ItemKey      string `json:"item_key"`
	Label        string `json:"label"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	UnitCode     string `json:"unit_code"`
	UnitQuantity string `json:"unit_quantity"`
}

// LoadQuoteForEdit 实现 customer.Store.LoadQuoteForEdit。
//
// 为什么要一次读完：报价编辑器要「模型 + 来源 + 全部价项 + 已报价」四样东西，
// 分三次查容易读到半新半旧的中间态。这里一条 SQL 出全部，
// 官方价从 standard_model.price_items 按 item_key 匹配。
func (r *CustomerRepo) LoadQuoteForEdit(ctx context.Context, quoteID int64) (*customer.QuoteForEdit, error) {
	// 头部一条查完（含客户名）：回填编辑器要显示「关联客户」，只给 id 没法填。
	const headQuery = `
		SELECT q.id, q.customer_id, q.version_no, q.status, q.our_company, q.valid_until,
		       COALESCE(ls.legal_name, '') AS customer_name
		  FROM customer_quote q
		  LEFT JOIN customer_profile cp ON cp.id = q.customer_id
		  LEFT JOIN legal_subject ls ON ls.id = cp.subject_id
		 WHERE q.id = ?`
	var head struct {
		ID           int64      `gorm:"column:id"`
		CustomerID   int64      `gorm:"column:customer_id"`
		VersionNo    int        `gorm:"column:version_no"`
		Status       string     `gorm:"column:status"`
		OurCompany   string     `gorm:"column:our_company"`
		ValidUntil   *time.Time `gorm:"column:valid_until"`
		CustomerName string     `gorm:"column:customer_name"`
	}
	if err := r.txOf(ctx).Raw(headQuery, quoteID).Scan(&head).Error; err != nil {
		return nil, fmt.Errorf("load quote head: %w", err)
	}
	if head.ID == 0 {
		return nil, nil
	}
	out := &customer.QuoteForEdit{
		ID:           head.ID,
		CustomerID:   head.CustomerID,
		CustomerName: head.CustomerName,
		VersionNo:    head.VersionNo,
		Status:       head.Status,
		OurCompany:   head.OurCompany,
		ValidUntil:   head.ValidUntil,
	}

	const query = `
		SELECT i.resource_id,
		       i.price_item_key,
		       i.unit_price::text                              AS unit_price,
		       i.floor_price::text                             AS floor_price,
		       COALESCE(i.quotation_coefficient::text, '')     AS quotation_coefficient,
		       i.currency,
		       COALESCE(r.standard_model_id, 0)                AS standard_model_id,
		       COALESCE(r.supplier_id, 0)                      AS supplier_id,
		       COALESCE(r.region, '')                          AS region,
		       COALESCE(r.coefficient::text, '')               AS resource_coefficient,
		       COALESCE(r.supplier_model_name, '')             AS supplier_model_name,
		       COALESCE(sm.name, '')                           AS model_name,
		       COALESCE(sm.official_id, '')                    AS official_id,
		       COALESCE(sm.company, '')                        AS company,
		       COALESCE(sm.model_type, '')                     AS model_type,
		       COALESCE(sm.price_items, '[]'::jsonb)::text     AS price_items,
		       COALESCE(sp.name, '')                           AS supplier_name,
		       COALESCE(conn.connection_ms::text, '')          AS test_connection_ms,
		       COALESCE(perf.rps::text, '')                    AS test_rps,
		       COALESCE(perf.tps::text, '')                    AS test_tps,
		       COALESCE(cache.cache_hit_tokens::text, '')      AS test_hit_tokens,
		       COALESCE(cache.cache_input_tokens::text, '')    AS test_input_tokens
		  FROM customer_quote_item i
		  LEFT JOIN resource r ON r.id = i.resource_id
		  LEFT JOIN standard_model sm ON sm.id = r.standard_model_id
		  LEFT JOIN supplier sp ON sp.id = r.supplier_id
		  -- 实测数据分属不同报告：连通性测验只写 connection_ms，压测只写 rps/tps，
		  -- 缓存命中又只在部分报告里。所以不能"取最新一条"，那样后一次压测会把
		  -- 上一次的连通延迟覆盖成空，界面上就成了「未测试」。按指标各取最近非空。
		  LEFT JOIN LATERAL (
		        SELECT p.connection_ms
		          FROM test_report p
		         WHERE p.resource_id = i.resource_id AND p.connection_ms IS NOT NULL
		         ORDER BY p.tested_at DESC NULLS LAST, p.id DESC
		         LIMIT 1
		       ) conn ON TRUE
		  LEFT JOIN LATERAL (
		        SELECT p.rps, p.tps
		          FROM test_report p
		         WHERE p.resource_id = i.resource_id AND (p.rps IS NOT NULL OR p.tps IS NOT NULL)
		         ORDER BY p.tested_at DESC NULLS LAST, p.id DESC
		         LIMIT 1
		       ) perf ON TRUE
		  LEFT JOIN LATERAL (
		        SELECT p.cache_hit_tokens, p.cache_input_tokens
		          FROM test_report p
		         WHERE p.resource_id = i.resource_id AND p.cache_input_tokens IS NOT NULL
		         ORDER BY p.tested_at DESC NULLS LAST, p.id DESC
		         LIMIT 1
		       ) cache ON TRUE
		 WHERE i.customer_quote_id = ?
		 ORDER BY i.resource_id ASC, i.price_item_key ASC`
	var rows []quoteEditItemRow
	if err := r.txOf(ctx).Raw(query, quoteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load quote items for edit: %w", err)
	}

	lines := make([]customer.QuoteForEditLine, 0, len(rows))
	index := map[int64]int{}
	for _, row := range rows {
		// SKU 口径的老明细没有 resource_id：资源口径编辑器表达不了，计数后跳过。
		if row.ResourceID == nil || *row.ResourceID <= 0 {
			out.SkippedItems++
			continue
		}
		resourceID := *row.ResourceID
		pos, ok := index[resourceID]
		if !ok {
			lines = append(lines, customer.QuoteForEditLine{
				ResourceID:       resourceID,
				StandardModelID:  row.StandardModelID,
				ModelName:        row.ModelName,
				OfficialID:       row.OfficialID,
				Company:          row.Company,
				ModelType:        row.ModelType,
				SupplierID:       row.SupplierID,
				SupplierName:     row.SupplierName,
				SupplierModel:    row.SupplierModelName,
				Region:           row.Region,
				Coefficient:      row.ResourceCoefficient,
				// 落库的对外报价系数（000068 起）。为空表示历史单据，编辑器会自行兜底。
				QuotationCoefficient: trimTrailingZero(row.QuotationCoefficient),
				TestConnectionMS: row.TestConnectionMS,
				TestRPS:          row.TestRPS,
				TestTPS:          row.TestTPS,
				TestCacheHitRate: cacheHitPercent(row.TestHitTokens, row.TestInputTokens),
				PriceItems:       []customer.QuoteForEditPriceItem{},
			})
			pos = len(lines) - 1
			index[resourceID] = pos
		}
		key := ""
		if row.PriceItemKey != nil {
			key = *row.PriceItemKey
		}
		official := findOfficialItem(row.PriceItemsJSON, key)
		item := customer.QuoteForEditPriceItem{
			ItemKey:    key,
			Currency:   row.Currency,
			UnitPrice:  trimDecimal(row.UnitPrice),
			FloorPrice: trimDecimal(row.FloorPrice),
		}
		if official != nil {
			item.Amount = trimDecimal(official.Amount)
			item.Label = official.Label
			item.UnitCode = official.UnitCode
			item.UnitQuantity = official.UnitQuantity
			if item.Currency == "" {
				item.Currency = official.Currency
			}
		}
		lines[pos].PriceItems = append(lines[pos].PriceItems, item)
	}
	// 价项顺序跟官方价目表一致（输入 → 输出 → 缓存…），而不是按 item_key 字典序。
	for pos := range lines {
		sort.SliceStable(lines[pos].PriceItems, func(i, j int) bool {
			return itemRank(lines[pos].PriceItems[i].ItemKey) < itemRank(lines[pos].PriceItems[j].ItemKey)
		})
	}
	out.Lines = lines
	return out, nil
}

// itemRank 让价项按「输入 → 输出 → 缓存」的常规顺序展示。
//
// 不用官方价项数组下标：老单的价项可能已经不在官方价目表里（官方下架了某个价项），
// 用固定次序兜底，至少不会因为「找不到」就排到最前面。
func itemRank(key string) int {
	switch key {
	case "input", "text_input":
		return 0
	case "output", "text_output":
		return 1
	case "cache_input", "text_cache_input":
		return 2
	case "cache_write", "text_cache_write":
		return 3
	case "cache_storage", "text_cache_storage":
		return 4
	case "image_input":
		return 5
	case "image_output":
		return 6
	default:
		return 9
	}
}

// findOfficialItem 在 standard_model.price_items 里按 item_key 找一项。
func findOfficialItem(raw, key string) *officialPriceItem {
	if raw == "" {
		return nil
	}
	var items []officialPriceItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	for _, item := range items {
		if item.ItemKey == key {
			matched := item
			return &matched
		}
	}
	return nil
}

// cacheHitPercent 缓存命中率 = 命中 token ÷ 输入 token（百分数，保留 2 位）。
//
// 与接入模型投影（supplypg 的 cacheHitRate）同口径，避免两处对"命中率"算法不一致。
func cacheHitPercent(hitTokens, inputTokens string) string {
	hit, errHit := strconv.ParseFloat(strings.TrimSpace(hitTokens), 64)
	input, errInput := strconv.ParseFloat(strings.TrimSpace(inputTokens), 64)
	if errHit != nil || errInput != nil || input <= 0 {
		return ""
	}
	return strconv.FormatFloat(hit/input*100, 'f', 2, 64)
}
