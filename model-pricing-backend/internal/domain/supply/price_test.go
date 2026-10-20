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
