package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResourceStateStorePostgres34Native(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "resource", "operation_log", "test_job", "test_report"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id),ADD UNIQUE(supplier_id,standard_model_id,region)`,
		`INSERT INTO pg_temp.supplier(id,name,region) OVERRIDING SYSTEM VALUE VALUES(21,'真实控制头供应商','CN')`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	work, err := NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 4, 0, 0, 0, time.UTC)
	model, err := supply.NewModelService(work, nil, func() time.Time { return now }).Create(ctx, supply.WriteMeta{ActorID: "7", Reason: "控制头夹具", RequestID: "state-fixture"}, supply.ModelData{Name: "控制头模型", Company: "Example", OfficialID: "control-model", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	failLog, dropLog := false, false
	writes := 0
	logger := func(c context.Context, current *sql.Tx, entry ResourceStateLog) error {
		if current != tx {
			t.Fatal("logger escaped shared transaction")
		}
		writes++
		if dropLog {
			return nil
		}
		if failLog {
			entry.RequestID = strings.Repeat("x", 65)
		}
		var before any
		if len(entry.Before) > 0 {
			before = string(entry.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, entry.EntityType, entry.EntityID, entry.Action, entry.ActorID, before, string(entry.After), entry.Reason, entry.RequestID, entry.OccurredAt)
		return err
	}
	store, err := NewResourceStateStore("pg_temp", logger)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.NextID(ctx, tx)
	if err != nil || !modelIdentity(id) {
		t.Fatalf("identity allocation %q %v", id, err)
	}
	resource := &supply.Resource{ID: id, SupplierID: "21", StandardModelID: model.ID, Region: "CN", SupplierModelID: "vendor-real", Coefficient: "0.7000", BuyerID: "7", State: "PENDING_TEST", Revision: 1, OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, CreatedAt: now, UpdatedAt: now}
	meta := supply.WriteMeta{ActorID: "8", Reason: "已审批进入办理", RequestID: "resource-control-create"}
	created, err := store.Insert(ctx, tx, resource, meta, "CREATE")
	if err != nil || created == nil {
		t.Fatalf("native insert unavailable: %v", err)
	}
	current, err := store.ForUpdate(ctx, tx, id)
	if err != nil || !reflect.DeepEqual(current, resource) {
		t.Fatalf("stored control changed %+v %v", current, err)
	}
	var by, action string
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT operator_id::text,action,"after" FROM pg_temp.operation_log WHERE entity_id=$1 ORDER BY id DESC LIMIT 1`, id).Scan(&by, &action, &raw); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil || by != "8" || action != "RESOURCE_REVISION" || string(payload["contract"]) != `"supply.resource"` {
		t.Fatalf("wrong audit contract %s %s %s %v", by, action, raw, err)
	}
	if !strings.Contains(string(payload["resource"]), `"revision": "1"`) && !strings.Contains(string(payload["resource"]), `"revision":"1"`) {
		t.Fatal("revision was not a JSON string")
	}
	readerStore, err := NewResourceRepository("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, store.FactsLoader(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	view, err := supply.NewResourceReader(readerStore).Get(ctx, id, "CN")
	if err != nil || view.Revision != 1 || view.Test.Status != "NOT_TESTED" || view.QuoteReadiness.Eligible {
		t.Fatalf("read still needs fake control facts %+v %v", view, err)
	}
	after := *resource
	after.Revision = 2
	after.FormalConfigID = "91"
	after.FormalConfigRevision = 4
	after.UpdatedAt = now.Add(time.Second)
	if _, err := store.Update(ctx, tx, &after, 1, meta, "BIND"); err != nil {
		t.Fatal(err)
	}
	read, err := store.Current(ctx, tx, id)
	if err != nil || read.Revision != 2 || read.FormalConfigID != "91" || read.FormalConfigRevision != 4 || read.OriginRowVersion != 2 || read.TargetVersion != 3 {
		t.Fatalf("lost persisted metadata %+v %v", read, err)
	}
	if got, err := store.Update(ctx, tx, &after, 1, meta, "BIND"); !errors.Is(err, supply.ErrRevisionConflict) || got != nil {
		t.Fatal("stale write accepted", err)
	}
	if got, err := supply.NewResourceReader(readerStore).Get(ctx, id, "CN"); !errors.Is(err, supply.ErrDependencyUnavailable) || got != nil {
		t.Fatal("missing formal config provider invented facts", err)
	}
	// Control records must be selected independently of ordinary later logs.
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,"after") VALUES('resource',$1,'VIEW','{}')`, id); err != nil {
		t.Fatal(err)
	}
	if read, err := store.Current(ctx, tx, id); err != nil || read.Revision != 2 {
		t.Fatal("unrelated log replaced control head", err)
	}
	for _, failure := range []string{"log_sql", "empty_logger", "projection_drift", "corrupt_head", "identity_conflict", "foreign_report", "outer_rollback"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT state_failure"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				failLog = false
				dropLog = false
				if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT state_failure"); err != nil {
					t.Fatal(err)
				}
			}()
			next := after
			next.Revision = 3
			next.Coefficient = "0.6000"
			next.UpdatedAt = after.UpdatedAt.Add(time.Second)
			switch failure {
			case "log_sql":
				failLog = true
				if got, err := store.Update(ctx, tx, &next, 2, meta, "CHANGE_COEFFICIENT"); err == nil || got != nil {
					t.Fatal("log SQL failure accepted")
				}
			case "empty_logger":
				dropLog = true
				if got, err := store.Update(ctx, tx, &next, 2, meta, "CHANGE_COEFFICIENT"); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("empty logger accepted", err)
				}
			case "projection_drift":
				if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.resource SET coefficient=0.5000 WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Current(ctx, tx, id); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("drift accepted", err)
				}
			case "corrupt_head":
				if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,"after") VALUES('resource',$1,'RESOURCE_REVISION','{"contract":"bad"}')`, id); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Current(ctx, tx, id); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("fell back to older snapshot", err)
				}
			case "identity_conflict":
				duplicate := *resource
				duplicate.ID = "9223372036854775806"
				if got, err := store.Insert(ctx, tx, &duplicate, meta, "CREATE"); !errors.Is(err, supply.ErrIdentityConflict) || got != nil {
					t.Fatal("identity uniqueness bypassed", err)
				}
			case "outer_rollback":
				if _, err := store.Update(ctx, tx, &next, 2, meta, "CHANGE_COEFFICIENT"); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Current(ctx, tx, id); err != nil || got.Revision != 3 {
					t.Fatal("write not visible inside outer transaction", err)
				}
			case "foreign_report":
				var job, report string
				if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_job(resource_id,category) VALUES(999,'stress') RETURNING id::text`).Scan(&job); err != nil {
					t.Fatal(err)
				}
				if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_report(resource_id,test_job_id,connection_passed,stress_passed) VALUES(999,$1,true,true) RETURNING id::text`, job).Scan(&report); err != nil {
					t.Fatal(err)
				}
				next.LatestReportID = report
				next.LatestReportSequence = 1
				if got, err := store.Update(ctx, tx, &next, 2, meta, "ATTACH_TEST_EVIDENCE"); !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("another resource report persisted as own evidence", err)
				}
			}
		})
		read, err = store.Current(ctx, tx, id)
		if err != nil || read.Revision != 2 || read.Coefficient != "0.7000" {
			t.Fatalf("failed branch escaped rollback %+v %v", read, err)
		}
	}
	if writes < 3 {
		t.Fatal("writes did not pass real SQL logger")
	}
}
