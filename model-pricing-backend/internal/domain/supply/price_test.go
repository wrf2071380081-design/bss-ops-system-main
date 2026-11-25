package supply

import (
	"errors"
	"testing"
)

func samplePrice() PriceItem {
	return PriceItem{ItemKey: "text_input_standard", Label: "输入", TaskType: "TEXT_INPUT", Amount: "2.00000000", Currency: "USD", UnitCode: "TOKEN", UnitQuantity: "1000000", Condition: "标准 API 计费", SourceURL: "https://vendor.example/pricing"}
}

// A04: a zero charge is valid, not missing; a boundary change must break this test.
func TestPriceItemsPreserveZeroAndConditionalTiers(t *testing.T) {
	first := samplePrice()
	first.Amount = "0.00000000"
	second := samplePrice()
	second.ItemKey = "text_input_long"
	second.Condition = "上下文超过 200k"
	second.Amount = "999999999999.99999999"
	if err := ValidatePriceItems([]PriceItem{first, second}); err != nil {
		t.Fatal(err)
	}
	if first.Amount != "0.00000000" || second.Condition != "上下文超过 200k" {
		t.Fatal("validation rewrote official price evidence")
	}
}

func TestPriceItemsRejectInvalidValuesWithFieldPath(t *testing.T) {
	cases := []struct {
		name, path string
		change     func(*PriceItem)
	}{
		{"negative", "price_items[0].amount", func(p *PriceItem) { p.Amount = "-0.1" }},
		{"too precise", "price_items[0].amount", func(p *PriceItem) { p.Amount = "0.000000001" }},
		{"overflow", "price_items[0].amount", func(p *PriceItem) { p.Amount = "1000000000000" }},
		{"exponent", "price_items[0].amount", func(p *PriceItem) { p.Amount = "2e3" }},
		{"missing is not zero", "price_items[0].amount", func(p *PriceItem) { p.Amount = "" }},
		{"currency", "price_items[0].currency", func(p *PriceItem) { p.Currency = "usd" }},
		{"zero unit", "price_items[0].unit_quantity", func(p *PriceItem) { p.UnitQuantity = "0" }},
		{"negative unit", "price_items[0].unit_quantity", func(p *PriceItem) { p.UnitQuantity = "-1" }},
		{"unknown task", "price_items[0].task_type", func(p *PriceItem) { p.TaskType = "UNKNOWN" }},
		{"unknown unit", "price_items[0].unit_code", func(p *PriceItem) { p.UnitCode = "UNKNOWN" }},
		{"condition", "price_items[0].condition", func(p *PriceItem) { p.Condition = " " }},
		{"unknown dimension", "price_items[0].dimensions.secret", func(p *PriceItem) { p.Dimensions = map[string]string{"secret": "value"} }},
		{"credential URL", "price_items[0].source_url", func(p *PriceItem) { p.SourceURL = "https://user:password@vendor.example/pricing" }},
		{"secret query URL", "price_items[0].source_url", func(p *PriceItem) { p.SourceURL = "https://vendor.example/pricing?api_key=private" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := samplePrice()
			tc.change(&p)
			err := ValidatePriceItems([]PriceItem{p})
			var field *FieldError
			if !errors.As(err, &field) || field.Path != tc.path {
				t.Fatalf("want field %s, got %v", tc.path, err)
			}
		})
	}
}

func TestPriceItemsRejectDuplicateStableKey(t *testing.T) {
	p := samplePrice()
	other := p
	other.Label = "另一个名称"
	other.Condition = "不同条件"
	var field *FieldError
	if err := ValidatePriceItems([]PriceItem{p, other}); !errors.As(err, &field) || field.Path != "price_items[1].item_key" {
		t.Fatalf("duplicate key accepted: %v", err)
	}
}

// 同一 task_type 的多档计费项是合法数据：Claude 的缓存写入分 5 分钟 / 1 小时，缓存读取另计，
// 三者 task_type 都是 CACHE_INPUT，只有计费项名称与单价不同。判重若不带 label，这些行会被判成
// 「同一条件两个价」，写入被拒、已存的行连列表都读不出来（2026-11-25 标准模型库整页 500）。
func TestPriceItemsAllowSameTaskTypeWithDistinctLabels(t *testing.T) {
	tiers := []struct{ key, label, amount string }{
		{"cache_write_5m", "5 分钟缓存写入", "12.50000000"},
		{"cache_write_1h", "1 小时缓存写入", "20.00000000"},
		{"cache_read", "缓存读取", "1.00000000"},
	}
	items := make([]PriceItem, 0, len(tiers))
	for _, tier := range tiers {
		price := samplePrice()
		price.ItemKey, price.Label, price.TaskType, price.Amount = tier.key, tier.label, "CACHE_INPUT", tier.amount
		items = append(items, price)
	}
	if err := ValidatePriceItems(items); err != nil {
		t.Fatalf("legal cache tiers rejected: %v", err)
	}
	// 同一计费项名称 + 同一计费条件却给出两个价，仍然要拦。
	same := items[0]
	same.ItemKey, same.Amount = "cache_write_5m_alt", "13.00000000"
	var field *FieldError
	if err := ValidatePriceItems([]PriceItem{items[0], same}); !errors.As(err, &field) || field.Path != "price_items[1].amount" {
		t.Fatalf("conflicting amount for one tier accepted: %v", err)
	}
}
