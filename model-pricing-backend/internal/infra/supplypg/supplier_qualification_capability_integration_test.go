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

func TestSupplierQualificationMetadataCapabilityChecksTypesAndNullability(t *testing.T) {
	dsn := os.Getenv("SUPPLY_SUPPLIER_QUALIFICATION_CAPABILITY_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_SUPPLIER_QUALIFICATION_CAPABILITY_DSN to run the isolated supplier qualification schema check")
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
		t.Fatalf("supplier qualification capability test refuses non-isolated database %q", databaseName)
	}
	schema := fmt.Sprintf("supplier_qualification_capability_%d", time.Now().UnixNano())
	quotedSchema := `"` + schema + `"`
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`) }()
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`.supplier_qualification (
 qualification_type varchar(128), valid_from text, valid_to date,
 review_status varchar(24), revision integer, created_by text
)`); err != nil {
		t.Fatal(err)
	}
	supported, err := qualificationMetadataColumns(ctx, db, schema)
	if err != nil || supported {
		t.Fatalf("incompatible metadata field types/nullability must be reported unsupported, got %t, %v", supported, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE `+quotedSchema+`.supplier_qualification`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`.supplier_qualification (
 qualification_type varchar(128), valid_from date, valid_to date,
 review_status varchar(24), revision integer NOT NULL, created_by bigint
)`); err != nil {
		t.Fatal(err)
	}
	supported, err = qualificationMetadataColumns(ctx, db, schema)
	if err != nil || !supported {
		t.Fatalf("compatible qualification metadata structure should be supported, got %t, %v", supported, err)
	}
}
