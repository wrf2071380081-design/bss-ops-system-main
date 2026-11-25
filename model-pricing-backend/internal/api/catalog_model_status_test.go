package api

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogModelStateIsNotVendorSuccess(t *testing.T) {
	for _, tc := range []struct {
		job   string
		model map[string]any
		want  string
	}{
		{"SUCCEEDED", map[string]any{"coverage": "PARTIAL"}, "PARTIAL"},
		{"SUCCEEDED", map[string]any{"coverage": "COMPLETE"}, "COMPLETE"},
		{"SUCCEEDED", map[string]any{}, "PARTIAL"},
		{"SUCCEEDED", nil, "NOT_OBSERVED"},
		{"FAILED", nil, "UNAVAILABLE"},
		{"RUNNING", nil, "RUNNING"},
		{"SUCCEEDED", map[string]any{"coverage": "COMPLETE", "status": "NOT_OBSERVED"}, "NOT_OBSERVED"},
	} {
		if got := catalogModelState(tc.job, tc.model); got != tc.want {
			t.Fatalf("%+v: got %s", tc, got)
		}
	}
}

func TestCatalogModelStatusPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated catalog DB required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err = db.QueryRow("SELECT current_database()").Scan(&name); err != nil || !strings.HasSuffix(name, "_catalog_test") {
		t.Fatal("isolated test DB required", err)
	}
	ctx := context.Background()
	ddl, _ := os.ReadFile("../../migrations/000079_catalog_business.up.sql")
	if _, err = db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	ddl, _ = os.ReadFile("../../migrations/000082_catalog_model_coverage_index.up.sql")
	if _, err = db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS public.standard_model (id bigint PRIMARY KEY,company text,official_id text); INSERT INTO public.standard_model VALUES(999999,'Google','status-fixture')`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM public.standard_model WHERE id=999999")
	defer db.Exec("DELETE FROM model_catalog.runs WHERE origin_key='status-fixture'")
	const complete = `{"models":[{"id":"status-fixture","coverage":"COMPLETE"}]}`
	const partial = `{"models":[{"id":"status-fixture","coverage":"PARTIAL"}]}`
	for _, row := range []struct {
		id, scope, at, payload string
		owner                  int
	}{
		{"99999999-0000-0000-0000-000000000001", "manual", "2026-11-01", complete, 3},
		{"99999999-0000-0000-0000-000000000002", "platform", "2026-11-02", partial, 0},
		{"99999999-0000-0000-0000-000000000003", "personal", "2026-11-03", complete, 4},
	} {
		if _, err = db.Exec(`INSERT INTO model_catalog.runs(id,vendor,trigger,scope,owner_staff_id,origin_key,started_at,finished_at,status,result) VALUES($1,'google','MANUAL',$2,$3,'status-fixture',$4::timestamptz,$4::timestamptz,'SUCCEEDED',$5::jsonb)`, row.id, row.scope, row.owner, row.at, row.payload); err != nil {
			t.Fatal(err)
		}
	}
	store := NewCatalogBusinessStore(db)
	first, err := store.modelStatuses(ctx, []string{"999999"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	value := first["999999"]
	if value.State != "PARTIAL" || value.LastCompleteAt == nil || value.LastCompleteAt.Format(time.DateOnly) != "2026-11-01" {
		t.Fatalf("partial erased prior complete: %+v", value)
	}
	other, err := store.modelStatuses(ctx, []string{"999999"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if other["999999"].State != "PARTIAL" || other["999999"].LastCompleteAt != nil {
		t.Fatalf("private history leaked: %+v", other)
	}
}
