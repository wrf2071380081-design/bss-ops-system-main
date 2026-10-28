package main

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/repo"
	"os"
	"testing"
)

func TestDailyRuntimeAssemblesBindingAndAdmission(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily development database required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("daily configuration required")
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	t.Setenv("MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR", "")
	t.Setenv("MODEL_BSS_F12_ATTACHMENT_STORE_DIR", "")
	deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
	if err != nil {
		t.Fatal(err)
	}
	if deps.ResourceAdmitter == nil || deps.ResourceConfigBinder == nil {
		t.Fatal("runtime must construct A binding/admission services even while B providers are unavailable")
	}
	_, err = deps.ResourceWorkflow.GetResourceWorkflow(context.Background(), "3", "9223372036854775807")
	if !errors.Is(err, supply.ErrNotFound) {
		t.Fatalf("daily runtime must resolve native workflow facts, got %v", err)
	}
}
