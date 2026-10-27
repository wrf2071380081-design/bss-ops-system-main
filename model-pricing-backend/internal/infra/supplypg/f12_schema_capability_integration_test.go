package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestF12RecordSchemaCapabilityRejectsIncompatibleTypes(t *testing.T) {
	dsn := os.Getenv("SUPPLY_F12_SCHEMA_CAPABILITY_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_F12_SCHEMA_CAPABILITY_DSN to run the isolated F12 schema compatibility check")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)

	ctx := context.Background()
	var databaseName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "model_bss_a_") {
		t.Fatalf("F12 schema capability test refuses non-isolated database %q", databaseName)
	}

	schema := fmt.Sprintf("f12_schema_capability_%d", time.Now().UnixNano())
	quotedSchema := `"` + schema + `"`
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`) }()

	for _, table := range []string{"contract", "credit_line"} {
		base := `document_name varchar(160), document_no text, file_id text,
			source_flow_id text, source_module_key varchar(80), source_flow_revision text,
			reason text, created_by bigint`
		if table == "contract" {
			base += `, contract_key text, version integer NOT NULL DEFAULT 1`
		} else {
			base += `, subject_type text NOT NULL, subject_id bigint NOT NULL,
				direction text NOT NULL, currency text NOT NULL, version integer NOT NULL DEFAULT 1`
		}
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`."`+table+`" (`+base+`)`); err != nil {
			t.Fatal(err)
		}
	}
	supported, err := f12RecordSchemaReady(ctx, db, schema)
	if err != nil || supported {
		t.Fatalf("text source_flow_revision must be rejected, got supported=%t err=%v", supported, err)
	}
	for _, table := range []string{"contract", "credit_line"} {
		if _, err := db.ExecContext(ctx, `ALTER TABLE `+quotedSchema+`."`+table+`" ALTER COLUMN source_flow_revision TYPE integer USING NULL::integer`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX contract_version_unique ON `+quotedSchema+`.contract(contract_key,version) WHERE contract_key IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX credit_line_version_nonunique ON `+quotedSchema+`.credit_line(subject_type,subject_id,direction,currency,version)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX credit_line_version_partial ON `+quotedSchema+`.credit_line(subject_type,subject_id,direction,currency,version) WHERE version > 0`); err != nil {
		t.Fatal(err)
	}
	supported, err = f12RecordSchemaReady(ctx, db, schema)
	if err != nil || supported {
		t.Fatalf("compatible columns without version uniqueness must be rejected, got supported=%t err=%v", supported, err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX credit_line_version_unique ON `+quotedSchema+`.credit_line(subject_type,subject_id,direction,currency,version)`); err != nil {
		t.Fatal(err)
	}
	supported, err = f12RecordSchemaReady(ctx, db, schema)
	if err != nil || !supported {
		t.Fatalf("compatible text and integer columns should be accepted, got supported=%t err=%v", supported, err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+quotedSchema+`.contract ALTER COLUMN created_by SET NOT NULL`); err != nil {
		t.Fatal(err)
	}
	supported, err = f12RecordSchemaReady(ctx, db, schema)
	if err != nil || supported {
		t.Fatalf("non-null legacy-compatible field must be rejected, got supported=%t err=%v", supported, err)
	}
}
