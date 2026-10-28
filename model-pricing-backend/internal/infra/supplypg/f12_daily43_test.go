package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"testing"
)

func TestF12Daily43SchemaCapability(t *testing.T) {
	if os.Getenv("BSS_F12_V43_TEST") != "1" {
		t.Skip("explicit daily v43 opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("daily model_bss required", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	raw, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	ready, err := f12RecordSchemaReady(context.Background(), raw, "public")
	if err != nil || !ready {
		t.Fatalf("F12 fields and version uniqueness are not ready: ready=%v err=%v", ready, err)
	}
	var version int
	var dirty bool
	if err := raw.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil || version < 44 || dirty {
		t.Fatalf("expected at least canonical 44/clean, got %d/%v err=%v", version, dirty, err)
	}
}

// The download path intentionally uses a read-only transaction. Authorization
// must not issue row locks, while state-changing saves keep their locks.
func TestNativeF12ReadOnlyAuthorization(t *testing.T) {
	if os.Getenv("BSS_F12_V43_TEST") != "1" {
		t.Skip("explicit daily v43 opt-in required")
	}
	var fixture struct{ Run, Supplier string }
	b, err := os.ReadFile(os.Getenv("BSS_NATIVE_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &fixture); err != nil || len(fixture.Run) < 10 || fixture.Run[:10] != "QA-NATIVE-" {
		t.Fatal("marked native fixture required", err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("daily model_bss required", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	raw, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	n, err := NewNativeSupplierF12(raw, "public")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := raw.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var flow, actor string
	if err = tx.QueryRow(`SELECT f.id::text,nd.assignee_id::text FROM onboarding_flow f JOIN onboarding_node nd ON nd.flow_id=f.id WHERE f.kind='SUPPLIER' AND f.owner_id=$1 AND nd.node_key='CONTRACT'`, fixture.Supplier).Scan(&flow, &actor); err != nil {
		t.Fatal(err)
	}
	if err = n.AuthorizeF12FlowRead(context.Background(), tx, actor, "SUPPLIER", fixture.Supplier, flow, "CONTRACT"); err != nil {
		t.Fatalf("assigned handler read-only authorization: %v", err)
	}
	if err = n.AuthorizeF12FlowRead(context.Background(), tx, "3", "SUPPLIER", fixture.Supplier, flow, "CONTRACT"); err != nil {
		t.Fatalf("actual procurement owner must also be able to download: %v", err)
	}
	if err = n.AuthorizeF12FlowRead(context.Background(), tx, "2", "SUPPLIER", fixture.Supplier, flow, "CONTRACT"); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("unassigned reader: %v", err)
	}
}
