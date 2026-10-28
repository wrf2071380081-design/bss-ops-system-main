package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSupplierReadWorksBeforeAliasStructureDelivery(t *testing.T) {
	dsn := os.Getenv("SUPPLY_SUPPLIER_ALIASLESS_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_SUPPLIER_ALIASLESS_DSN to run the isolated alias-less v36/v37 read check")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	ctx := context.Background()

	var databaseName string
	var version int64
	var dirty, aliasTableExists bool
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "model_bss_a_") {
		t.Fatalf("supplier alias-less read test refuses non-isolated database %q", databaseName)
	}
	if err := db.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version < 36 || dirty {
		t.Fatalf("supplier read compatibility requires a clean v36+ database, got version=%d dirty=%t", version, dirty)
	}
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.supplier_alias') IS NOT NULL`).Scan(&aliasTableExists); err != nil {
		t.Fatal(err)
	}
	if aliasTableExists {
		t.Skip("test database includes the not-yet-delivered supplier_alias structure")
	}
	var buyerID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM public.internal_staff ORDER BY id LIMIT 1`).Scan(&buyerID); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	name := os.Getenv("SUPPLY_SUPPLIER_ALIASLESS_NAME")
	if name == "" {
		name = fmt.Sprintf("QA-Supplier-Aliasless-%d", createdAt.UnixNano())
	}
	var supplierID string
	if err := db.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,contact,phone,established,settlement_currency,status,created_at,updated_at,created_by,updated_by)
VALUES($1,'CN','','',false,'CNY','ACTIVE',$2,$2,$3,$3) RETURNING id::text`, name, createdAt, buyerID).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	keepFixture := os.Getenv("SUPPLY_SUPPLIER_ALIASLESS_KEEP_FIXTURE") == "1"
	defer func() {
		if keepFixture {
			t.Logf("retained supplier alias-less fixture %s (id %s) in the isolated database", name, supplierID)
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM public.operation_log WHERE entity_type='supplier' AND entity_id=$1`, supplierID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1`, supplierID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id=$1`, supplierID)
	}()
	profile, err := supply.NewSupplier(supplierID, fmt.Sprint(buyerID), supply.SupplierData{Name: name, Region: "CN", SettlementCurrency: "CNY"}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	profile.Status = "ACTIVE"
	profile.Revision = 1
	after, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.assignment_log(entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by,changed_at)
VALUES('supplier',$1,'buyer_id',NULL,$2,$2,$3)`, supplierID, buyerID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"after",reason,request_id,created_at)
VALUES('supplier',$1,'CREATE',$2,$3,'alias-less read fixture','908070',$4)`, supplierID, buyerID, after, createdAt); err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context) (*sql.Tx, error) { return db.BeginTx(ctx, nil) }
	logger := ResourceStateLogAppender(func(context.Context, *sql.Tx, ResourceStateLog) error { return nil })
	store, err := NewSupplierStore("public", db, resolve, logger)
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewSupplierReader(store)
	aliasesSupported, capabilityErr := reader.AliasesSupported(ctx)
	if capabilityErr != nil || aliasesSupported {
		t.Fatalf("alias capability must be false without the required alias table columns, got %t, %v", aliasesSupported, capabilityErr)
	}
	got, getErr := reader.Get(ctx, supplierID)
	page, listErr := reader.List(ctx, supply.SupplierListQuery{Page: 1, Size: 20, Keyword: name})
	if getErr != nil || got == nil || got.ID != supplierID || len(got.Aliases) != 0 {
		t.Fatalf("supplier detail must remain readable without the optional alias table: %+v, %v", got, getErr)
	}
	if listErr != nil || page == nil || page.Total != 1 || len(page.List) != 1 || page.List[0].ID != supplierID {
		t.Fatalf("supplier list must remain readable without the optional alias table: %+v, %v", page, listErr)
	}
}

func TestSupplierAliasCapabilityRequiresEveryReadWriteColumn(t *testing.T) {
	dsn := os.Getenv("SUPPLY_SUPPLIER_ALIASLESS_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_SUPPLIER_ALIASLESS_DSN to run the isolated supplier alias capability check")
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
		t.Fatalf("supplier alias capability test refuses non-isolated database %q", databaseName)
	}
	schema := fmt.Sprintf("supplier_alias_capability_%d", time.Now().UnixNano())
	quotedSchema := `"` + schema + `"`
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`) }()
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`.supplier_alias (id bigint)`); err != nil {
		t.Fatal(err)
	}
	store, err := NewSupplierStore(schema, db, func(ctx context.Context) (*sql.Tx, error) { return db.BeginTx(ctx, nil) }, ResourceStateLogAppender(func(context.Context, *sql.Tx, ResourceStateLog) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	supported, err := store.SupplierAliasesSupported(ctx)
	if err != nil || supported {
		t.Fatalf("partial alias table must be reported unsupported, got %t, %v", supported, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE `+quotedSchema+`.supplier_alias`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+quotedSchema+`.supplier_alias (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, supplier_id bigint NOT NULL,
 alias_name varchar(128) NOT NULL, normalized_name varchar(128) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatal(err)
	}
	supported, err = store.SupplierAliasesSupported(ctx)
	if err != nil || !supported {
		t.Fatalf("complete alias column set should be reported supported, got %t, %v", supported, err)
	}
}
