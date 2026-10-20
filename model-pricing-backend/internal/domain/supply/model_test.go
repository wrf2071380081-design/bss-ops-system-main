package supply

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var modelNow = time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)

func completeModelData() ModelData {
	output := samplePrice()
	output.ItemKey = "text_output_standard"
	output.TaskType = "TEXT_OUTPUT"
	output.Label = "输出"
	output.Amount = "8.00000000"
	return ModelData{Company: "Example", Name: "Example Model", OfficialID: "example-v1", ModelType: "TEXT", InputModalities: []string{"TEXT"}, OutputModalities: []string{"TEXT"},
		Capacity:   []OfficialFact{{Key: "context_window", Disclosure: "DISCLOSED", Value: "128000 tokens"}},
		Abilities:  []Ability{{Key: "tool_call", State: "UNDISCLOSED"}},
		APIAccess:  []OfficialFact{{Key: "protocol", Disclosure: "DISCLOSED", Value: "HTTPS API"}},
		Lifecycle:  []OfficialFact{{Key: "release", Disclosure: "UNDISCLOSED"}},
		PriceItems: []PriceItem{samplePrice(), output}, Sources: []Source{{Label: "官方价格", URL: "https://vendor.example/pricing", Kind: "OFFICIAL_PRICING"}}, CheckedAt: "2026-10-20"}
}

func TestIncompleteModelCanBeSavedButCannotQuote(t *testing.T) {
	m, err := NewStandardModel("9007199254740993", "7", ModelData{Name: "待补齐"}, modelNow)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "9007199254740993" || m.MaintainerID != "7" || m.VerifyStatus != "INCOMPLETE" {
		t.Fatalf("bad draft identity/state: %+v", m)
	}
	if !slices.Contains(m.MissingFields(), "price_items") || !slices.Contains(m.MissingFields(), "sources") {
		t.Fatalf("missing fields not actionable: %v", m.MissingFields())
	}
	if m.Readiness().Ready {
		t.Fatal("incomplete model may quote")
	}
}

func TestCompleteModelRequiresHumanVerification(t *testing.T) {
	m, err := NewStandardModel("1", "7", completeModelData(), modelNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.MissingFields()) != 0 || m.VerifyStatus != "UNVERIFIED" || m.Readiness().Ready {
		t.Fatalf("completeness was treated as verification: %+v", m)
	}
	verified, changed, err := m.verified("7", 1, "2026-10-20", modelNow)
	if err != nil || !changed || verified.VerifyStatus != "VERIFIED" || !verified.Readiness().Ready || verified.CheckedBy != "7" {
		t.Fatalf("human verification failed: %v %+v", err, verified)
	}
	if verified.VersionNo != 2 || verified.Revision != 2 || m.VersionNo != 1 {
		t.Fatal("verification overwrote prior version")
	}
	again, changed, err := verified.verified("7", 2, "2026-10-20", modelNow)
	if err != nil || changed || again.VersionNo != 2 {
		t.Fatal("unchanged verification created another version")
	}
}

func TestModalitySpecificPricesAndUnknownFactsBlockVerification(t *testing.T) {
	cases := []struct {
		name, missing string
		change        func(*ModelData)
	}{
		{"text output", "price_items.TEXT_OUTPUT", func(d *ModelData) { d.PriceItems = d.PriceItems[:1] }},
		{"image generation", "price_items.IMAGE_OUTPUT_OR_GENERATION", func(d *ModelData) { d.ModelType = "IMAGE" }},
		{"video generation", "price_items.VIDEO_OUTPUT_OR_GENERATION", func(d *ModelData) { d.ModelType = "VIDEO" }},
		{"audio task", "price_items.AUDIO_TASK", func(d *ModelData) { d.ModelType = "AUDIO" }},
		{"unverified ability", "abilities[0]", func(d *ModelData) { d.Abilities[0].State = "UNVERIFIED" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := completeModelData()
			tc.change(&data)
			m, err := NewStandardModel("1", "7", data, modelNow)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(m.MissingFields(), tc.missing) {
				t.Fatalf("want %s in %v", tc.missing, m.MissingFields())
			}
			if _, _, err = m.verified("7", 1, "2026-10-20", modelNow); !errors.Is(err, ErrIncomplete) {
				t.Fatalf("incomplete model verified: %v", err)
			}
		})
	}
}

func TestModelRejectsIllegalSuppliedFieldsAndFutureDates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*ModelData)
	}{
		{"calendar date", func(d *ModelData) { d.CheckedAt = "2026-02-30" }},
		{"future in Shanghai", func(d *ModelData) { d.CheckedAt = "2026-10-21" }},
		{"unknown modality", func(d *ModelData) { d.InputModalities = []string{"UNKNOWN"} }},
		{"unknown fact state", func(d *ModelData) { d.Capacity[0].Disclosure = "MAYBE" }},
		{"disclosed without value", func(d *ModelData) { d.Capacity[0].Value = "" }},
		{"duplicate capability", func(d *ModelData) { d.Abilities = append(d.Abilities, d.Abilities[0]) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := completeModelData()
			tc.change(&d)
			if _, err := NewStandardModel("1", "7", d, modelNow); err == nil {
				t.Fatal("invalid supplied data accepted")
			}
		})
	}
}

func TestModelUpdateProtectsOwnerRevisionAndSnapshots(t *testing.T) {
	data := completeModelData()
	data.PriceItems[0].Dimensions = map[string]string{"context_tier": "standard"}
	m, err := NewStandardModel("1", "7", data, modelNow)
	if err != nil {
		t.Fatal(err)
	}
	data.PriceItems[0].Dimensions["context_tier"] = "caller mutation"
	data.Capacity[0].Value = "caller mutation"
	if m.Data.PriceItems[0].Dimensions["context_tier"] != "standard" || m.Data.Capacity[0].Value != "128000 tokens" {
		t.Fatal("model aliases caller input")
	}
	if _, _, err = m.Updated("8", 1, m.Data, modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-maintainer accepted: %v", err)
	}
	if _, _, err = m.Updated("7", 0, m.Data, modelNow); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	unchanged, changed, err := m.Updated("7", 1, m.Data, modelNow)
	if err != nil || changed || unchanged.Revision != 1 {
		t.Fatal("no-op increased version")
	}
	verified, _, err := m.verified("7", 1, "2026-10-20", modelNow)
	if err != nil {
		t.Fatal(err)
	}
	replacement := cloneData(verified.Data)
	replacement.PriceItems[0].Amount = "3.00000000"
	next, changed, err := verified.Updated("7", 2, replacement, modelNow)
	if err != nil || !changed || next.VerifyStatus != "UNVERIFIED" || next.Revision != 3 || next.VersionNo != 3 || next.CheckedBy != "" {
		t.Fatalf("price change not invalidating verification: %v %+v", err, next)
	}
	if verified.Data.PriceItems[0].Amount != "2.00000000" || verified.VerifyStatus != "VERIFIED" {
		t.Fatal("historical snapshot overwritten")
	}
	replacement.PriceItems[0].Amount = "99"
	if next.Data.PriceItems[0].Amount != "3.00000000" {
		t.Fatal("updated model aliases command")
	}
}

func TestInactiveModelCannotQuoteOrVerify(t *testing.T) {
	m, _ := NewStandardModel("1", "7", completeModelData(), modelNow)
	m.Status = "INACTIVE"
	if _, _, err := m.verified("7", 1, "2026-10-20", modelNow); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive verified: %v", err)
	}
	m.VerifyStatus = "VERIFIED"
	if m.Readiness().Ready {
		t.Fatal("inactive model can quote")
	}
}

func TestNonTextModelsVerifyWithTheirOwnBillingTasks(t *testing.T) {
	cases := []struct{ modelType, task, unit, quantity, output string }{
		{"IMAGE", "IMAGE_GENERATION", "IMAGE", "1", "IMAGE"},
		{"VIDEO", "VIDEO_OUTPUT", "SECOND", "1", "VIDEO"},
		{"AUDIO", "AUDIO_RECOGNITION", "MINUTE", "1", "TEXT"},
	}
	for _, tc := range cases {
		t.Run(tc.modelType, func(t *testing.T) {
			data := completeModelData()
			data.ModelType = tc.modelType
			data.OutputModalities = []string{tc.output}
			price := samplePrice()
			price.ItemKey = "generation_or_recognition"
			price.TaskType = tc.task
			price.UnitCode = tc.unit
			price.UnitQuantity = tc.quantity
			price.Amount = "0.00000000"
			data.PriceItems = []PriceItem{price}
			m, err := NewStandardModel("1", "7", data, modelNow)
			if err != nil {
				t.Fatal(err)
			}
			verified, _, err := m.verified("7", 1, "2026-10-20", modelNow)
			if err != nil || !verified.Readiness().Ready || verified.Data.PriceItems[0].Amount != "0.00000000" || verified.Data.PriceItems[0].UnitCode != tc.unit {
				t.Fatalf("valid modality-specific free price rejected: %v", err)
			}
		})
	}
}
