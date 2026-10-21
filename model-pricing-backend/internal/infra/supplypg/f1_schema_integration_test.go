package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Opt-in metadata-only test against the dedicated migration validation DB.
// No password/DSN is logged; pgx reads PG* from the process environment.
func TestF1SchemaPostgres34Metadata(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL schema test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal("cannot initialize test database driver")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("test database identity check failed")
	}
	report, err := InspectF1Schema(ctx, db, "public")
	if err != nil {
		t.Fatalf("metadata query failed: %v", err)
	}
	for _, field := range []string{"standard_model.maintainer_id", "standard_model.revision", "standard_model.version_no", "standard_model.verify_status", "standard_model.checked_by"} {
		if !slices.Contains(report.MissingColumns, field) {
			t.Fatalf("expected missing %s", field)
		}
	}
	if report.ColumnsCompatible || report.RepositoryReady || len(report.IndexDefinitions) == 0 {
		t.Fatalf("unexpected readiness %+v", report)
	}
	found := false
	for _, issue := range report.IncompatibleColumns {
		if strings.Contains(issue, "standard_model_version.version_no") && strings.Contains(issue, "integer") {
			found = true
		}
	}
	if !found {
		t.Fatal("version integer incompatibility not detected")
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	t.Log(string(raw))
}
