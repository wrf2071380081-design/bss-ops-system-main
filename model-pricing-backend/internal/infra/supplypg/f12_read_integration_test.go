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
	"model_bss/internal/domain/supply"
)

// Only temporary tables are created; this test remains deferred while Docker is maintained.
func TestF12LatestContractTemporaryPostgres(t *testing.T) {
	if os.Getenv("SUPPLY_TEMP_READ_TEST") != "1" { t.Skip("explicit temporary-table PG check required") }
	db, err := sql.Open("pgx", "")
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { t.Fatal(err) }
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TEMP TABLE supplier(id bigint PRIMARY KEY) ON COMMIT DROP`,
		`CREATE TEMP TABLE customer_profile(id bigint PRIMARY KEY) ON COMMIT DROP`,
		`CREATE TEMP TABLE contract(id bigint PRIMARY KEY,contract_key text,subject_type text,subject_id bigint,status text,version integer,file_ref text,signed_at timestamptz,created_at timestamptz,updated_at timestamptz) ON COMMIT DROP`,
		`INSERT INTO pg_temp.supplier VALUES(21)`,
		`INSERT INTO pg_temp.customer_profile VALUES(21)`,
		`INSERT INTO pg_temp.contract(id,contract_key,subject_type,subject_id,status,version,created_at,updated_at) VALUES(8,'8','SUPPLIER',21,'PROCESSING',1,'2026-01-01','2026-01-01'),(18,'8','SUPPLIER',21,'PROCESSING',9,'2026-01-02','2026-01-02'),(28,'8','CUSTOMER',21,'ARCHIVED',99,'2026-01-03','2026-01-03')`,
		`INSERT INTO pg_temp.contract(id,contract_key,subject_type,subject_id,status,version,created_at,updated_at) SELECT n,n::text,'SUPPLIER',21,'PROCESSING',1,'2026-10-25'::timestamptz,'2026-10-25'::timestamptz FROM generate_series(100,124) n`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil { t.Fatal(err) }
	}
	var schema string
	if err := tx.QueryRowContext(ctx, `SELECT nspname FROM pg_namespace WHERE oid=pg_my_temp_schema()`).Scan(&schema); err != nil { t.Fatal(err) }
	store, err := NewF12ReadStore(schema, func(ctx context.Context, run func(*sql.Tx) error) error { return run(tx) })
	if err != nil { t.Fatal(err) }
	reader := supply.NewF12Reader(store)
	history, err := reader.ListContracts(ctx, supply.F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 20})
	if err != nil { t.Fatal(err) }
	for _, row := range history.List { if row.ContractKey == "8" { t.Fatal("fixture must put target archive outside visible page") } }
	q := supply.F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: "21", ContractKey: "8", LatestOnly: true, Page: 1, Size: 2}
	latest, err := reader.ListContracts(ctx, q)
	if err != nil || latest.Total != 1 || latest.List[0].Version != 9 || latest.List[0].ID != "18" {
		t.Fatalf("archive lookup must ignore pagination and same-id customer data: %+v %v", latest, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.contract SELECT 19,contract_key,subject_type,subject_id,status,version,file_ref,signed_at,created_at,updated_at FROM pg_temp.contract WHERE id=18`); err != nil { t.Fatal(err) }
	if _, err := reader.ListContracts(ctx, q); err != supply.ErrRevisionConflict { t.Fatalf("duplicate latest version accepted: %v", err) }
	if _, err := tx.ExecContext(ctx, `ALTER TABLE pg_temp.contract DROP COLUMN contract_key`); err != nil { t.Fatal(err) }
	if _, err := reader.ListContracts(ctx, q); err != supply.ErrDependencyUnavailable { t.Fatalf("missing archive structure misreported as no record: %v", err) }
}

func TestF12ReadSeparatesSameNumericIDAcrossSubjectTypes(t *testing.T) {
	dsn := os.Getenv("SUPPLY_F12_ISOLATED_DSN")
	if dsn == "" {
		t.Skip("set SUPPLY_F12_ISOLATED_DSN to run the isolated v36 subject-collision check")
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
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "model_bss_a_") {
		t.Fatalf("F12 subject-collision test refuses non-isolated database %q", databaseName)
	}
	if err := db.QueryRowContext(ctx, `SELECT version,dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 36 || dirty {
		t.Fatalf("F12 compatibility read requires clean v36, got version=%d dirty=%t", version, dirty)
	}

	var subjectID int64
	base := int64(1_000_000_000 + time.Now().UnixNano()%1_000_000_000)
	for offset := int64(0); offset < 100; offset++ {
		candidate := base + offset
		var occupied bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.supplier WHERE id=$1) OR EXISTS(SELECT 1 FROM public.customer_profile WHERE id=$1)`, candidate).Scan(&occupied); err != nil {
			t.Fatal(err)
		}
		if !occupied {
			subjectID = candidate
			break
		}
	}
	if subjectID == 0 {
		t.Fatal("could not allocate an unused shared supplier/customer ID in the isolated database")
	}
	stamp := fmt.Sprintf("%d", subjectID)
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM public.contract WHERE subject_type IN ('SUPPLIER','CUSTOMER') AND subject_id=$1`, subjectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.credit_line WHERE subject_type IN ('SUPPLIER','CUSTOMER') AND subject_id=$1`, subjectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.supplier WHERE id=$1`, subjectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.customer_profile WHERE id=$1`, subjectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM public.legal_subject WHERE legal_name=$1`, "QA-F12-Customer-"+stamp)
	}
	defer cleanup()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.supplier(id,name,region,settlement_currency) OVERRIDING SYSTEM VALUE VALUES($1,$2,'CN','CNY')`, subjectID, "QA-F12-Supplier-"+stamp); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	var legalSubjectID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.legal_subject(subject_type,legal_name) VALUES('COMPANY',$1) RETURNING id`, "QA-F12-Customer-"+stamp).Scan(&legalSubjectID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	var ownerID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM public.internal_staff ORDER BY id LIMIT 1`).Scan(&ownerID); err != nil {
		tx.Rollback()
		t.Fatalf("need a staff owner for customer_profile fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.customer_profile(id,subject_id,owner_sales_operator_id) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3)`, subjectID, legalSubjectID, ownerID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.contract(subject_type,subject_id,status,version) VALUES('SUPPLIER',$1,'ARCHIVED',1),('CUSTOMER',$1,'PROCESSING',2)`, subjectID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.credit_line(subject_type,subject_id,direction,amount,currency,version) VALUES
('SUPPLIER',$1,'SUPPLIER_TO_US',100,'CNY',1),('SUPPLIER',$1,'SUPPLIER_TO_US',125,'CNY',2),('CUSTOMER',$1,'US_TO_CUSTOMER',500,'CNY',1)`, subjectID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	store, err := NewF12ReadStore("public", ModelReadTransactions(db))
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewF12Reader(store)
	customerContracts, err := reader.ListContracts(ctx, supply.F12ContractQuery{SubjectType: "CUSTOMER", SubjectID: stamp})
	if err != nil || customerContracts.Total != 1 || customerContracts.List[0].Status != "PROCESSING" || customerContracts.List[0].Version != 2 {
		t.Fatalf("customer contract read mixed with supplier contract: %+v %v", customerContracts, err)
	}
	supplierContracts, err := reader.ListContracts(ctx, supply.F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: stamp})
	if err != nil || supplierContracts.Total != 1 || supplierContracts.List[0].Status != "ARCHIVED" || supplierContracts.List[0].Version != 1 {
		t.Fatalf("supplier contract read mixed with customer contract: %+v %v", supplierContracts, err)
	}
	customerCredits, err := reader.ListCredits(ctx, supply.F12CreditQuery{SubjectType: "CUSTOMER", SubjectID: stamp, Direction: "US_TO_CUSTOMER", Currency: "CNY"})
	if err != nil || customerCredits.Total != 1 || len(customerCredits.Current) != 1 || customerCredits.Current[0].Record == nil || customerCredits.Current[0].Record.Amount != "500.00" {
		t.Fatalf("customer current credit was not isolated: %+v %v", customerCredits, err)
	}
	supplierCredits, err := reader.ListCredits(ctx, supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: stamp, Direction: "SUPPLIER_TO_US", Currency: "CNY"})
	if err != nil || supplierCredits.Total != 2 || len(supplierCredits.Current) != 1 || supplierCredits.Current[0].Record == nil || supplierCredits.Current[0].Record.Amount != "125.00" {
		t.Fatalf("supplier current credit was not isolated or selected by version: %+v %v", supplierCredits, err)
	}
}
