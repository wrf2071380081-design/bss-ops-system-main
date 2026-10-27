package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/domain/supply"
	"os"
	"testing"
	"time"
)

// All fixtures use explicitly qualified temporary tables and roll back. This
// exercises the query without inserting into the running business tables.
func TestSourceOperationsTemporaryPostgres(t *testing.T) {
	if os.Getenv("SUPPLY_TEMP_READ_TEST") != "1" {
		t.Skip("explicit temporary-table PG check required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TEMP TABLE standard_model(id bigint PRIMARY KEY) ON COMMIT DROP`,
		`CREATE TEMP TABLE supplier(id bigint PRIMARY KEY,name text,region text) ON COMMIT DROP`,
		`CREATE TEMP TABLE supplier_qualification(id bigint PRIMARY KEY,supplier_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE supplier_formal_config(id bigint PRIMARY KEY,supplier_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE contract(id bigint PRIMARY KEY,subject_type text,subject_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE credit_line(id bigint PRIMARY KEY,subject_type text,subject_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE resource(id bigint PRIMARY KEY,standard_model_id bigint,supplier_id bigint,supplier_model_name text,region text) ON COMMIT DROP`,
		`CREATE TEMP TABLE plan(id bigint PRIMARY KEY,resource_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE operation_log(id bigint PRIMARY KEY,entity_type text,entity_id bigint,action text,operator_name_snapshot text,reason text,created_at timestamptz,"after" jsonb) ON COMMIT DROP`,
		`INSERT INTO pg_temp.standard_model VALUES(1),(2)`,
		`INSERT INTO pg_temp.supplier VALUES(3,'甲','CN'),(4,'乙','OVERSEAS'),(5,'无资源','CN')`,
		`INSERT INTO pg_temp.supplier_qualification VALUES(61,3)`,
		`INSERT INTO pg_temp.supplier_formal_config VALUES(31,3)`,
		`INSERT INTO pg_temp.contract VALUES(71,'SUPPLIER',3),(72,'CUSTOMER',3)`,
		`INSERT INTO pg_temp.credit_line VALUES(91,'SUPPLIER',3),(92,'CUSTOMER',3)`,
		`INSERT INTO pg_temp.resource VALUES(11,1,3,'api-a','CN'),(12,1,4,'api-b','OVERSEAS'),(13,2,3,'other','CN')`,
		`INSERT INTO pg_temp.plan VALUES(81,12)`,
		`INSERT INTO pg_temp.operation_log VALUES(101,'resource',11,'ADMIT','操作人','接入','2026-10-22T01:00:00Z','{"api_key":"private"}'),(102,'plan',81,'PLAN_REVISION','操作人','计划','2026-10-22T01:00:00Z','{"operation":"CREATE","private":"secret"}'),(103,'resource',13,'ADMIT',NULL,NULL,'2026-10-22T02:00:00Z',NULL),(104,'supplier',3,'UPDATE',NULL,NULL,'2026-10-22T02:00:00Z',NULL)`,
		`INSERT INTO pg_temp.operation_log VALUES(105,'supplier_qualification',61,'UPLOAD',NULL,NULL,'2026-10-22T02:00:00Z',NULL),(106,'supplier_formal_config',31,'FORMAL_CONFIG_REVISION',NULL,NULL,'2026-10-22T02:00:00Z','{"operation":"CREATE","api_key":"private"}'),(107,'contract',71,'UPDATE',NULL,NULL,'2026-10-22T02:00:00Z',NULL),(108,'credit_line',91,'UPDATE',NULL,NULL,'2026-10-22T02:00:00Z',NULL),(109,'contract',72,'UPDATE',NULL,NULL,'2026-10-22T02:00:00Z',NULL),(110,'supplier',5,'CREATE',NULL,NULL,'2026-10-22T02:00:00Z',NULL)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewModelReadStore("pg_temp", func(ctx context.Context, run func(*sql.Tx) error) error { return run(tx) })
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewSourceOperationReader(store)
	page, err := reader.List(ctx, supply.SourceOperationQuery{ModelID: "1", Page: 1, Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.List) != 1 || page.List[0].ID != "102" || page.List[0].Action != "CREATE" || page.List[0].ResourceID != "12" {
		t.Fatalf("wrong aggregation: %+v", page)
	}
	page, err = reader.List(ctx, supply.SourceOperationQuery{ModelID: "1", Page: 2, Size: 1})
	if err != nil || len(page.List) != 1 || page.List[0].ID != "101" {
		t.Fatalf("wrong stable page: %+v %v", page, err)
	}
	page, err = reader.List(ctx, supply.SourceOperationQuery{ModelID: "1", ResourceID: "11", Page: 1, Size: 20})
	if err != nil || page.Total != 1 {
		t.Fatalf("wrong resource filter: %+v %v", page, err)
	}
	if _, err := reader.List(ctx, supply.SourceOperationQuery{ModelID: "1", ResourceID: "13", Page: 1, Size: 20}); err != supply.ErrNotFound {
		t.Fatalf("cross-model resource accepted: %v", err)
	}
	page, err = reader.List(ctx, supply.SourceOperationQuery{SupplierID: "3", Page: 1, Size: 20})
	if err != nil || page.Total != 7 || len(page.List) != 7 || page.List[0].ID != "108" || page.List[6].ID != "101" {
		t.Fatalf("wrong supplier aggregation: %+v %v", page, err)
	}
	page, err = reader.List(ctx, supply.SourceOperationQuery{SupplierID: "3", EntityType: "contract", Page: 1, Size: 20})
	if err != nil || page.Total != 1 || page.List[0].ID != "107" {
		t.Fatalf("customer contract leaked into supplier log scope: %+v %v", page, err)
	}
	page, err = reader.List(ctx, supply.SourceOperationQuery{SupplierID: "5", Page: 1, Size: 20})
	if err != nil || page.Total != 1 || page.List[0].ResourceID != "" {
		t.Fatalf("supplier without resources lost its subject history: %+v %v", page, err)
	}
	if _, err := reader.List(ctx, supply.SourceOperationQuery{SupplierID: "3 OR 1=1", Page: 1, Size: 20}); err == nil {
		t.Fatal("invalid supplier scope accepted")
	}
}
