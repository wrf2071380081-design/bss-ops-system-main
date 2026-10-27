package supplypg

import (
	"encoding/json"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func TestModelOperationSnapshotWhitelistsAndScopes(t *testing.T) {
	m, err := supply.NewStandardModel("9007199254740993", "7", supply.ModelData{Name: "历史名称"}, time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(m)
	var payload map[string]any
	json.Unmarshal(raw, &payload)
	payload["api_key"] = "private-secret"
	payload["data"].(map[string]any)["password"] = "private-secret"
	raw, _ = json.Marshal(payload)
	snapshot := modelOperationSnapshot(raw, m.ID)
	if snapshot == nil || snapshot.Data.Name != "历史名称" {
		t.Fatal("valid model history rejected")
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "private-secret") {
		t.Fatal("raw fields leaked")
	}
	if modelOperationSnapshot(raw, "12") != nil {
		t.Fatal("cross-model snapshot leaked")
	}
	if modelOperationSnapshot([]byte(`{"api_key":"private-secret"}`), m.ID) != nil {
		t.Fatal("unknown legacy shape treated as full history")
	}
}
