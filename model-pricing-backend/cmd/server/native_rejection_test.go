package main

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"model_bss/internal/api"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRejectionDaily42SharesTransaction(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	path := filepath.Join(t.TempDir(), "rejection.json")
	t.Setenv("BSS_NATIVE_MANIFEST", path)
	t.Setenv("BSS_NATIVE_MODE", "leadership")
	t.Setenv("BSS_TEST_PASSWORD", "temporary QA account")
	TestNativeHandlingDaily42Success(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f nativeFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	db, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	defer func() {
		if err := cleanupNativeFixture(ctx, db, f); err != nil {
			t.Error(err)
		}
	}()
	runtime := api.NewSupplyChainRuntime(db, nil)
	id := mustInt(f.Submissions[0])
	actor := mustInt(f.Leader)
	sentinel := errors.New("outer idempotency transaction must roll back")
	err = client.DB.Transaction(func(tx *gorm.DB) error {
		if err := runtime.Reject(infradb.WithDB(ctx, tx), id, 1, actor, f.Run, f.Run+"-rollback"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var status string
	var count int
	if err := db.QueryRowContext(ctx, `SELECT status FROM submission WHERE id=$1`, id).Scan(&status); err != nil || status != "PENDING_APPROVAL" {
		t.Fatalf("outer rollback lost: %s %v", status, err)
	}
	if err := runtime.Reject(ctx, id, 1, 1, f.Run, f.Run+"-forbidden"); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("nonapprover: %v", err)
	}
	if err := runtime.Reject(ctx, id, 2, actor, f.Run, f.Run+"-stale"); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("stale: %v", err)
	}
	if err := runtime.Reject(ctx, id, 1, actor, f.Run, f.Run+"-success"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM operation_log WHERE entity_type='submission' AND entity_id=$1 AND action='REJECT' AND operator_id=$2 AND operator_name_snapshot=$3`, id, actor, f.Run).Scan(&count); err != nil || count != 1 {
		t.Fatalf("actual operator log: %d %v", count, err)
	}
	if err := runtime.Reject(ctx, id, 1, actor, f.Run, f.Run+"-again"); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("repeat decision: %v", err)
	}
}
