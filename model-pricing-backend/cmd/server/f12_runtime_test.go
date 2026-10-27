package main

import (
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/repo"
	"os"
	"testing"
)

func TestDailyRuntimeWiresNativeF12(t *testing.T) {
	if os.Getenv("BSS_F12_V43_TEST") != "1" {
		t.Skip("explicit v43 daily opt-in required")
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
	deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
	if err != nil {
		t.Fatal(err)
	}
	if deps.F12Readiness == nil || deps.F12Writer == nil {
		t.Fatal("native F12 readiness and writer must be wired into daily runtime")
	}
}
