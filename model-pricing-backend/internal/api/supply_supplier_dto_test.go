package api

import (
	"encoding/json"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
)

// 内部「档案与办理」走的是 snake_case 契约（前端 supplierEntities.contract.ts）。
// 与门户的 camelCase DTO 是两套键名，错位不会编译报错、只会让页面显示空，
// 所以把 9 个新字段的键名钉在这里。
func TestSupplierEntityDTOExposesCommercialDefaultsAsSnakeCase(t *testing.T) {
	rate := "0.06000000"
	profile := &supply.Supplier{
		ID: "1001", BuyerID: "7", Status: "ACTIVE", Revision: 3,
		CreatedAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
		Data: supply.SupplierData{
			Name: "澄海智算", Region: "CN", RegistrationNo: "DEMO", Contact: "顾宁", Phone: "13800000001",
			TaxRate: &rate, SettlementCurrency: "CNY", Remark: "演示主体",
			ContactTitle: "商务负责人", ContactEmail: "ops@example.com",
			DefaultSettlement: "月结 30 天", DefaultPayment: "银行转账", InvoicePreference: "专用发票",
			MinConsumption: "1000 CNY / 月", DataTrainingPolicy: "不用于训练",
			LogRetentionPolicy: "留存 90 天", DefaultSLA: "99.9%",
		},
	}
	raw, err := json.Marshal(supplierDTO(profile, "7", true, true))
	if err != nil {
		t.Fatalf("marshal dto: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal dto: %v", err)
	}
	want := map[string]string{
		"contact_title": "商务负责人", "contact_email": "ops@example.com",
		"default_settlement": "月结 30 天", "default_payment": "银行转账", "invoice_preference": "专用发票",
		"min_consumption": "1000 CNY / 月", "data_training_policy": "不用于训练",
		"log_retention_policy": "留存 90 天", "default_sla": "99.9%",
	}
	for key, expected := range want {
		got, ok := payload[key]
		if !ok {
			t.Fatalf("entity DTO missing key %q: %s", key, string(raw))
		}
		if got != expected {
			t.Fatalf("entity DTO key %q = %v, want %q", key, got, expected)
		}
	}
	// 空值必须序列化成空串而不是 null，前端契约按 string 校验。
	profile.Data.DefaultSLA = ""
	raw, err = json.Marshal(supplierDTO(profile, "7", true, true))
	if err != nil {
		t.Fatalf("marshal dto: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal dto: %v", err)
	}
	if value, ok := payload["default_sla"]; !ok || value != "" {
		t.Fatalf("empty default_sla should serialize as \"\", got %v (%T)", value, value)
	}
}
