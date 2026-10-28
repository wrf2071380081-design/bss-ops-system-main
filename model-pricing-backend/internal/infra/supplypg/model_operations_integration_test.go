package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelOperationReadPostgres(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE operation_log(id bigint PRIMARY KEY,entity_type text,entity_id bigint,action text,operator_name_snapshot text,reason text,created_at timestamptz,"before" jsonb,"after" jsonb) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	model, _ := supply.NewStandardModel("9007199254740995", "7", supply.ModelData{Name: "历史名称"}, now)
	raw, _ := json.Marshal(model)
	var snapshot map[string]any
	json.Unmarshal(raw, &snapshot)
	snapshot["api_key"] = "do-not-return"
	snapshot["data"].(map[string]any)["password"] = "do-not-return"
	raw, _ = json.Marshal(snapshot)
	for _, item := range []struct{ id, kind, model string }{{"9007199254740993", "standard_model", model.ID}, {"9007199254740994", "standard_model", model.ID}, {"9007199254740996", "standard_model", "12"}, {"9007199254740997", "resource", model.ID}} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log VALUES($1,$2,$3,'UPDATE','维护人','核对价格',$4,$5::jsonb,$5::jsonb)`, item.id, item.kind, item.model, now, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	store, _ := NewModelRepository("pg_temp", func(_ context.Context, run func(*sql.Tx) error) error { return run(tx) })
	reader := supply.NewModelReader(store)
	page, err := reader.ListOperations(ctx, supply.ModelOperationQuery{ModelID: model.ID, Page: 1, Size: 1})
	if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != "9007199254740994" || page.List[0].ModelName != "历史名称" {
		t.Fatalf("bad historical page: %+v %v", page, err)
	}
	detail, err := reader.GetOperation(ctx, model.ID, "9007199254740993")
	if err != nil || detail.After == nil || detail.Before == nil {
		t.Fatalf("detail %+v %v", detail, err)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "do-not-return") {
		t.Fatal("raw snapshot leaked")
	}
	if _, err := reader.GetOperation(ctx, "12", "9007199254740993"); !errors.Is(err, supply.ErrNotFound) {
		t.Fatalf("cross-model detail: %v", err)
	}
	global, err := reader.ListOperations(ctx, supply.ModelOperationQuery{})
	if err != nil || global.Total != 3 {
		t.Fatalf("non-model logs mixed: %+v %v", global, err)
	}
}
