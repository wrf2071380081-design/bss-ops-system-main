package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/api"
	"model_bss/internal/domain/supply"
	tp "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/repo"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPortalDraftResourceDaily44(t *testing.T) {
	mode, path := os.Getenv("BSS_PORTAL_MODE"), os.Getenv("BSS_NATIVE_MANIFEST")
	if mode == "" || path == "" {
		t.Skip("explicit marked daily fixture required")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var f struct{ Run, Supplier, Leader, PortalDraft, PortalRow, Resource string }
	b, err := os.ReadFile(path)
	must(err)
	must(json.Unmarshal(b, &f))
	if !strings.HasPrefix(f.Run, "QA-NATIVE-") || f.PortalDraft == "" {
		t.Fatal("marked draft required")
	}
	cfg, err := config.Load()
	must(err)
	if cfg.Database.DBName != "model_bss" {
		t.Fatal("daily database required")
	}
	client, err := infradb.New(cfg.Database)
	must(err)
	defer client.Close()
	db, err := client.DB.DB()
	must(err)
	ctx := context.Background()
	var version int
	var dirty bool
	must(db.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
	if version != 45 || dirty {
		t.Fatal("45/clean required")
	}
	var owner, name string
	must(db.QueryRow(`SELECT s.supplier_id::text,sp.name FROM submission s JOIN supplier sp ON sp.id=s.supplier_id WHERE s.id=$1`, f.PortalDraft).Scan(&owner, &name))
	if owner != f.Supplier || name != f.Run {
		t.Fatal("QA identity mismatch")
	}
	must(db.QueryRow(`SELECT id::text FROM submission_row WHERE submission_id=$1 ORDER BY id LIMIT 1`, f.PortalDraft).Scan(&f.PortalRow))
	submissions, err := supplypg.NewSubmissionStore(db, "public")
	must(err)
	row, err := submissions.TestRow(ctx, mustInt(f.Supplier), mustInt(f.PortalDraft), mustInt(f.PortalRow))
	must(err)
	deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
	must(err)
	query := supply.ResourceListQuery{View: "flat", SupplierID: f.Supplier, Page: 1, Size: 20, SortBy: "id", Order: "asc", NullsLast: true, TieBreakBy: "id"}
	if mode == "reset-submit" {
		result, err := db.Exec(`UPDATE submission SET status='DRAFT',submitted_at=NULL,revision=revision+1 WHERE id=$1 AND supplier_id=$2 AND status='PENDING_APPROVAL' AND NOT EXISTS(SELECT 1 FROM operation_log l WHERE l.entity_type='submission' AND l.entity_id=submission.id AND l.action='APPROVE')`, f.PortalDraft, f.Supplier)
		must(err)
		count, err := result.RowsAffected()
		must(err)
		if count != 1 {
			t.Fatal("only unapproved marked QA submission may be reset")
		}
		return
	}
	if mode == "evidence" {
		id, err := submissions.EnsureTestResource(ctx, row)
		must(err)
		again, err := submissions.EnsureTestResource(ctx, row)
		must(err)
		if id != again {
			t.Fatal("test allocation created duplicate resources")
		}
		f.Resource = fmt.Sprint(id)
		// Persist actual identity even if the following assertion fails.
		var document map[string]any
		must(json.Unmarshal(b, &document))
		document["Resource"], document["PortalRow"] = f.Resource, f.PortalRow
		encoded, err := json.Marshal(document)
		must(err)
		must(os.WriteFile(path, encoded, 0600))
		listing, err := deps.Resources.List(ctx, query)
		must(err)
		if listing.Total != 0 {
			t.Fatalf("draft test allocation leaked into formal resource list: total=%d", listing.Total)
		}
		_, err = deps.Resources.Get(ctx, f.Resource, "CN")
		if !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("test-only resource detail must be hidden, got %v", err)
		}
		store, err := supplypg.NewTestPlatformStore(db, "public", t.TempDir())
		must(err)
		job, _, err := store.CreateJob(ctx, tp.NewJobRecord{ResourceID: f.Resource, Category: tp.CategoryBaseline, ActorID: "1", TargetFingerprint: row.TestFingerprint, IdempotencyKey: f.Run + "-portal-baseline"})
		must(err)
		must(submissions.AttachTestJob(ctx, mustInt(f.PortalRow), mustInt(job), "baseline"))
		zero := "0.00"
		_, err = store.CompleteJob(ctx, tp.Completion{JobID: job, ResourceID: f.Resource, Category: tp.CategoryBaseline, FinalStatus: "SUCCEEDED", Verdict: "pass", Outcome: tp.Outcome{ConnectivityPassed: true, ConnectionMS: &zero}, TestedBy: "1", ReportJSON: json.RawMessage(`{"purpose":"QA portal baseline only; no Inspector call"}`), FinishedAt: time.Now().UTC()})
		must(err)
		return
	}
	if mode != "adopt" {
		t.Fatal("unknown portal mode")
	}
	runtime := api.NewSupplyChainRuntime(db, nil, deps.F12Readiness)
	var status string
	must(db.QueryRow(`SELECT status FROM submission WHERE id=$1`, f.PortalDraft).Scan(&status))
	if status == "PENDING_APPROVAL" {
		must(runtime.Approve(ctx, mustInt(f.PortalDraft), mustInt(f.Leader), 1, 1, f.Run))
	} else if status != "PROCESSING" {
		t.Fatal("expected actual approval or processing state")
	}
	current, err := deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if current.Revision != 1 || current.QuoteReadiness.Eligible {
		t.Fatalf("invalid formal adoption %+v", current)
	}
	listing, err := deps.Resources.List(ctx, query)
	must(err)
	if listing.Total != 1 {
		t.Fatalf("formal resource count=%d", listing.Total)
	}
	if err = runtime.Approve(ctx, mustInt(f.PortalDraft), mustInt(f.Leader), 1, 1, f.Run); err == nil {
		t.Fatal("duplicate decision accepted")
	}
	var count int
	must(db.QueryRow(`SELECT count(*) FROM resource WHERE supplier_id=$1`, f.Supplier).Scan(&count))
	if count != 1 {
		t.Fatalf("duplicate physical resources=%d", count)
	}
	must(db.QueryRow(`SELECT count(*) FROM operation_log WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION'`, f.Resource).Scan(&count))
	if count != 1 {
		t.Fatalf("adoption control count=%d", count)
	}
	var origin string
	must(db.QueryRow(`SELECT "after"->'resource'->>'origin_row_id' FROM operation_log WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION'`, f.Resource).Scan(&origin))
	if origin != f.PortalRow {
		t.Fatalf("wrong adopted source row: %s", origin)
	}
}
