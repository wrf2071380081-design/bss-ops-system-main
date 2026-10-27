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

func TestCanceledRowCannotRetestDaily44(t *testing.T) {
	if os.Getenv("BSS_SUPPLIER_V44_TEST") != "1" {
		t.Skip("explicit daily fixture required")
	}
	var f struct{ Run, Supplier string }
	b, err := os.ReadFile(os.Getenv("BSS_NATIVE_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &f); err != nil || len(f.Run) < 10 || f.Run[:10] != "QA-NATIVE-" {
		t.Fatal("marked native fixture required", err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal(err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	raw, _ := client.DB.DB()
	tx, err := raw.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRow(`SELECT sr.resource_id::text FROM submission_row sr JOIN submission s ON s.id=sr.submission_id WHERE s.supplier_id=$1 AND sr.row_status='CANCELED' ORDER BY sr.id LIMIT 1`, f.Supplier).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE onboarding_flow SET current_node='CONFIG' WHERE kind='SUPPLIER' AND owner_id=$1`, f.Supplier); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE onboarding_node SET status='IN_PROGRESS' WHERE node_key='CONFIG' AND flow_id IN(SELECT id FROM onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1)`, f.Supplier); err != nil {
		t.Fatal(err)
	}
	wrapped := client.DB.Session(&gorm.Session{NewDB: true})
	wrapped.Statement.ConnPool = tx
	ctx := infradb.WithDB(context.Background(), wrapped)
	store, _ := NewTestPlatformStore(raw, "public", "")
	if _, err = store.InternalRetestResource(ctx, "1", id); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("canceled row must not start a retest even while the other rows are active: %v", err)
	}
}
