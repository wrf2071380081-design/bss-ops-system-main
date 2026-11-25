package supply

import (
	"encoding/json"
	"testing"
)

func TestOfficialProfileRoundTripPreservesScopeAndEvidence(t *testing.T) {
	raw := `{"capacity":[{"key":"max_output","disclosure":"DISCLOSED","value":"8192","label":"最大输出","unit":"TOKEN","semantics":"SHARED_WINDOW","condition":"标准模式","scope":"API","source_url":"https://example.com/limits","quote":"Max output 8192"}],"abilities":[{"key":"vision","state":"SUPPORTED","description":"仅标准模式","condition":"标准模式","source_url":"https://example.com/vision","quote":"vision supported"}]}`
	var data ModelData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	capacity := decoded["capacity"].([]any)[0].(map[string]any)
	for _, key := range []string{"label", "unit", "semantics", "condition", "scope", "source_url", "quote"} {
		if capacity[key] == nil || capacity[key] == "" {
			t.Fatalf("lost %s: %s", key, encoded)
		}
	}
	ability := decoded["abilities"].([]any)[0].(map[string]any)
	if ability["source_url"] != "https://example.com/vision" {
		t.Fatal("ability evidence lost")
	}
}

func TestPriceDimensionsPreserveRegionDeploymentAndEffectiveDates(t *testing.T) {
	p := samplePrice()
	p.Dimensions = map[string]string{"region": "中国内地", "deployment": "标准API", "context_tier": "0-128K", "valid_from": "2026-01-01", "valid_to": "2026-12-31", "period": "PEAK", "account_tier": "enterprise"}
	if err := ValidatePriceItems([]PriceItem{p}); err != nil {
		t.Fatal(err)
	}
}

func TestConflictingScopedPricesRequireSelection(t *testing.T) {
	first, second := samplePrice(), samplePrice()
	second.ItemKey = "other_source"
	second.Amount = "3"
	if ValidatePriceItems([]PriceItem{first, second}) == nil {
		t.Fatal("conflicting scoped prices accepted")
	}
	second.Dimensions = map[string]string{"region": "海外"}
	if err := ValidatePriceItems([]PriceItem{first, second}); err != nil {
		t.Fatal(err)
	}
}
