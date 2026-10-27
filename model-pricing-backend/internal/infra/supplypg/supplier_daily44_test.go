package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"testing"
)

func TestSupplierDaily44Capabilities(t *testing.T) {
	if os.Getenv("BSS_SUPPLIER_V44_TEST") != "1" {
		t.Skip("explicit daily schema opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("daily model_bss required", err)
	}
	c, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := raw.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	aliases, err := (&SupplierStore{schema: "public"}).supplierAliasStructureAvailable(context.Background(), tx)
	if err != nil || !aliases {
		t.Fatalf("supplier aliases unavailable: %v %v", aliases, err)
	}
	metadata, err := qualificationMetadataColumns(context.Background(), tx, "public")
	if err != nil || !metadata {
		t.Fatalf("qualification metadata unavailable: %v %v", metadata, err)
	}
	var version int
	var dirty bool
	if err = tx.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil || version < 45 || dirty {
		t.Fatalf("expected at least canonical 45/clean, got %d/%v: %v", version, dirty, err)
	}
}
