package supplypg

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"testing"
)

// Native authorization only: uncommitted QA workflow facts are rolled back.
// This does not test B approval creation or call an Inspector/Gateway.
func TestInternalRetestNativeCurrentAssignee(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" || os.Getenv("BSS_WORKBENCH_FIXTURE") == "" {
		t.Skip("explicit daily QA fixture required")
	}
	var fixture struct {
		Run       string   `json:"run"`
		Resources []string `json:"resource_ids"`
		Actor     string   `json:"actor_id"`
		Other     string   `json:"other_actor_id"`
	}
	raw, err := os.ReadFile(os.Getenv("BSS_WORKBENCH_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Resources) != 2 {
		t.Fatal("fixture required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("wrong database")
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	db, _ := client.DB.DB()
	var name string
	var version int
	var dirty bool
	if err = db.QueryRow("select current_database(),version,dirty from schema_migrations").Scan(&name, &version, &dirty); err != nil || name != "model_bss" || version != 42 || dirty {
		t.Fatal("expected model_bss 42 clean")
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	wrapped := client.DB.Session(&gorm.Session{NewDB: true})
	wrapped.Statement.ConnPool = tx
	ctx := infradb.WithDB(context.Background(), wrapped)
	store, _ := NewTestPlatformStore(db, "public", "")
	state, _ := NewResourceStateStore("public", nil)
	r, err := state.Current(ctx, tx, fixture.Resources[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.InternalRetestResource(ctx, fixture.Actor, r.ID); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("buyer/admin without active handling must be forbidden: %v", err)
	}
	var flow string
	if _, err = tx.ExecContext(ctx, "UPDATE submission SET status='PROCESSING' WHERE id=(SELECT submission_id FROM submission_row WHERE id=$1)", r.OriginRowID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, "INSERT INTO onboarding_flow(kind,owner_id,status,current_node) VALUES('SUPPLIER',$1,'PROCESSING','CONFIG') RETURNING id::text", r.SupplierID).Scan(&flow); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO onboarding_node(flow_id,node_key,enabled,status,assignee_id) VALUES($1,'CONFIG',true,'IN_PROGRESS',$2)", flow, fixture.Other); err != nil {
		t.Fatal(err)
	}
	target, err := store.InternalRetestResource(ctx, fixture.Other, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Model == "" || target.BaseURL != "" || target.SealedAPIKey != "" {
		t.Fatal("authority lookup must return identity, never supplier credentials")
	}
	if _, err = store.InternalRetestResource(ctx, fixture.Actor, r.ID); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("administrator cannot replace designated handler", err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE onboarding_node SET status='COMPLETED' WHERE flow_id=$1", flow); err != nil {
		t.Fatal(err)
	}
	if _, err = store.InternalRetestResource(ctx, fixture.Other, r.ID); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("completed handling node cannot start tests", err)
	}
}
