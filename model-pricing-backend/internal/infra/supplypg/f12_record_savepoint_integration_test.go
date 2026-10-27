package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"model_bss/internal/domain/supply"
)

func TestF12RecordStoreRollsBackFailedUnitOfWorkToSavepoint(t *testing.T) {
	dsn := os.Getenv("SUPPLY_F12_SAVEPOINT_ISOLATED_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_F12_SAVEPOINT_ISOLATED_DSN to run the isolated F12 transaction check")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)

	ctx := context.Background()
	var databaseName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "model_bss_a_") {
		t.Fatalf("F12 savepoint test refuses non-isolated database %q", databaseName)
	}

	schema := fmt.Sprintf("f12_savepoint_%d", time.Now().UnixNano())
	quotedSchema := `"` + schema + `"`
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`) }()

	for _, table := range []string{"contract", "credit_line"} {
		_, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`."`+table+`" (
			document_name varchar(160), document_no text, file_id text,
			source_flow_id text, source_module_key varchar(80), source_flow_revision integer,
			reason text, created_by bigint
		)`)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+quotedSchema+`.contract ADD COLUMN contract_key text`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`.rollback_probe (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	outer, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Rollback()
	store, err := NewF12RecordStore(schema, func(context.Context) (*sql.Tx, error) { return outer, nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("fail after a write, before the unit of work completes")
	err = store.Within(ctx, func(raw supply.F12RecordTransaction) error {
		tx, ok := raw.(*f12RecordTransaction)
		if !ok {
			return errors.New("unexpected F12 transaction adapter")
		}
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO `+quotedSchema+`.rollback_probe(id) VALUES(1)`); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("failed F12 operation error changed: got %v, want %v", err, wantErr)
	}
	var count int
	if err := outer.QueryRowContext(ctx, `SELECT count(*) FROM `+quotedSchema+`.rollback_probe`).Scan(&count); err != nil {
		t.Fatalf("outer idempotency transaction should remain usable after savepoint rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed F12 unit of work left %d partial rows in the outer transaction", count)
	}
	var one int
	if err := outer.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("outer transaction should remain usable for the idempotency result: value=%d err=%v", one, err)
	}
}
