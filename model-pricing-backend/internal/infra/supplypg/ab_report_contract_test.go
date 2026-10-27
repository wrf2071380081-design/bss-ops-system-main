package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
)

func TestABReportProjectionPreservesMissingTestDateAndCategory(t *testing.T) {
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	for _, status := range []string{"QUEUED", "RUNNING", "FAILED"} {
		view, err := buildReportView(reportRow{jobID: "21", status: status, category: "stress", createdAt: now}, "11", nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view)
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		if wire["tested_at"] != nil {
			t.Errorf("%s: unrecorded tested_at must be null, got %v", status, wire["tested_at"])
		}
		if view.TestItem != "stress" {
			t.Errorf("%s: lost test category: %q", status, view.TestItem)
		}
		if view.FinishedAt != nil {
			t.Errorf("%s: fabricated finish time", status)
		}
		if view.TargetMatches == nil || *view.TargetMatches {
			t.Errorf("missing fingerprint cannot assert current target")
		}
	}
}

func TestABReportProjectionDoesNotBorrowOtherJobVerdicts(t *testing.T) {
	now := time.Now().UTC()
	row := reportRow{jobID: "21", status: "SUCCEEDED", category: "stress", createdAt: now, finishedAt: sql.NullTime{Time: now, Valid: true}, stressPass: sql.NullBool{Bool: true, Valid: true}}
	view, err := buildReportView(row, "11", map[string]string{"baseline": "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if view.ConnectivityPassed != nil {
		t.Fatal("unrelated baseline job was merged into this report")
	}
	if view.PressurePassed == nil || !*view.PressurePassed {
		t.Fatal("own stress result lost")
	}
}

// Opt-in, read-only probe against the user's one local development database.
// This test creates no database, schema, tables or business rows.
func TestABDaily42ReportQueries(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily development database required")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal("load database configuration")
	}
	if cfg.Database.DBName != "model_bss" {
		t.Fatal("unexpected configured database")
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal("connect daily database")
	}
	defer client.Close()
	db, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var name string
	var version int
	var dirty bool
	if err := db.QueryRowContext(ctx, "SELECT current_database(),version,dirty FROM schema_migrations").Scan(&name, &version, &dirty); err != nil || name != "model_bss" || version != 42 || dirty {
		t.Fatal("expected model_bss version42 clean")
	}
	const absent = "9223372036854775807"
	t.Run("admission_native_report_column", func(t *testing.T) {
		gate, _ := NewAdmissionReportGate(db, "public")
		if err := gate.Check(ctx, absent); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("native query: %v", err)
		}
	})
	t.Run("empty_report_list_is_array", func(t *testing.T) {
		reader, _ := NewTestReportReader(db, "public", "")
		page, err := reader.List(ctx, "1", absent, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if page.List == nil || len(page.List) != 0 || page.Total != 0 {
			t.Fatal("empty report list must be []")
		}
	})
	if run := os.Getenv("BSS_AB_REPORT_RUN"); strings.HasPrefix(run, "QA-AB-") {
		t.Run("B_create_job_keeps_fingerprint", func(t *testing.T) {
			var total, missing int
			err := db.QueryRowContext(ctx, "SELECT count(*), count(*) FILTER (WHERE target_fingerprint IS NULL OR target_fingerprint='') FROM test_job WHERE idempotency_key LIKE $1", run+"-job-%").Scan(&total, &missing)
			if err != nil || total != 12 || missing != 0 {
				t.Fatalf("B task fingerprints: total=%d missing=%d err=%v", total, missing, err)
			}
		})
		t.Run("report_job_and_file_namespaces", func(t *testing.T) {
			var job, report, resource string
			err := db.QueryRowContext(ctx, "SELECT j.id::text,r.id::text,j.resource_id::text FROM test_job j JOIN test_report r ON r.test_job_id=j.id WHERE j.idempotency_key LIKE $1 ORDER BY j.id DESC LIMIT 1", run+"-job-%").Scan(&job, &report, &resource)
			if err != nil {
				t.Fatal(err)
			}
			reader, _ := NewTestReportReader(db, "public", "")
			detail, err := reader.Get(ctx, "1", resource, job)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(detail)
			var wire map[string]any
			_ = json.Unmarshal(raw, &wire)
			if wire["test_job_id"] != job || wire["test_report_id"] != report || len(detail.Files) != 1 || detail.Files[0].ID != report {
				t.Fatalf("job/report/file IDs must retain their namespaces (job=%s report=%s)", job, report)
			}
		})
	}
}
