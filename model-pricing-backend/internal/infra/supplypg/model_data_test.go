package supplypg

import (
	"encoding/json"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
)

func modelDataRow() ModelDataRow {
	return ModelDataRow{Company: "示例公司", Name: "示例模型", OfficialID: "example", ModelType: "TEXT", InputModalities: []string{"TEXT"}, Capacity: json.RawMessage(`{}`), Abilities: json.RawMessage(`{}`), PriceItems: json.RawMessage(`[]`), APIAccess: json.RawMessage(`{}`), Lifecycle: json.RawMessage(`{}`), Sources: json.RawMessage(`[]`)}
}
func TestModelDataReadsMigrationDefaultsAsIncompleteLists(t *testing.T) {
	row := modelDataRow()
	data, err := row.DomainData()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Capacity) != 0 || len(data.Abilities) != 0 || len(data.PriceItems) != 0 || data.CheckedAt != "" {
		t.Fatalf("invented facts %+v", data)
	}
	data.InputModalities[0] = "IMAGE"
	if row.InputModalities[0] != "TEXT" {
		t.Fatal("aliased array")
	}
}
func TestModelDataRejectsNonemptyLegacyObjectsWithoutDroppingFacts(t *testing.T) {
	for _, raw := range []string{`{"window":128000}`, `null`, `[] {}`, `[{"unknown":"value"}]`} {
		row := modelDataRow()
		row.Capacity = json.RawMessage(raw)
		if _, err := row.DomainData(); err == nil {
			t.Fatalf("accepted incompatible capacity %s", raw)
		}
	}
}
func TestModelDataRoundTripPreservesTypedFactsSourcesAndExactPrice(t *testing.T) {
	data := supply.ModelData{Company: "示例公司", Name: "示例模型", OfficialID: "example", ModelType: "TEXT", InputModalities: []string{"TEXT"}, Capacity: []supply.OfficialFact{{Key: "context", Disclosure: "DISCLOSED", Value: "128000"}}, PriceItems: []supply.PriceItem{}, Sources: []supply.Source{{Label: "官方", URL: "https://example.com/pricing", Kind: "OFFICIAL_PRICING"}}, CheckedAt: "2026-10-20"}
	row, err := EncodeModelData(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := row.DomainData()
	if err != nil {
		t.Fatal(err)
	}
	if result.Capacity[0].Value != "128000" || result.Sources[0].Kind != "OFFICIAL_PRICING" || result.CheckedAt != "2026-10-20" {
		t.Fatalf("lost fields %+v", result)
	}
	if string(row.PriceItems) != "[]" || string(row.Abilities) != "[]" {
		t.Fatal("new writes must use canonical arrays")
	}
	data.PriceItems = []supply.PriceItem{{ItemKey: "input", Amount: "0.00000000", UnitQuantity: "1000000", Dimensions: map[string]string{"tier": "9007199254740993"}}}
	row, err = EncodeModelData(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err = row.DomainData()
	if err != nil || result.PriceItems[0].Amount != "0.00000000" || result.PriceItems[0].Dimensions["tier"] != "9007199254740993" {
		t.Fatalf("lossy price %+v %v", result, err)
	}
}
func TestModelDataWriteRefusesTruncationAndInvalidDate(t *testing.T) {
	for _, data := range []supply.ModelData{{Company: strings.Repeat("界", 65)}, {Name: strings.Repeat("x", 129)}, {OfficialID: strings.Repeat("x", 129)}, {ModelType: strings.Repeat("x", 17)}, {CheckedAt: "2026-02-30"}} {
		if _, err := EncodeModelData(data); err == nil {
			t.Fatalf("accepted lossy/invalid data %+v", data)
		}
	}
}
