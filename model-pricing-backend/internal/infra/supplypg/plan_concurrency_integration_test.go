package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlanConcurrentCreationPostgres34(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database", err)
	}
	schema := fmt.Sprintf("a_plan_race_%d", time.Now().UnixNano())
	if !modelSchemaName.MatchString(schema) {
		t.Fatal("unsafe test schema")
	}
	q := func(table string) string { return `"` + schema + `"."` + table + `"` }
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, err := db.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Error("fixture cleanup", err)
		}
	}()
	for _, table := range []string{"plan", "resource", "operation_log", "event_outbox", "supplier"} {
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+q(table)+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS)`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+q("resource")+` ADD PRIMARY KEY(id)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+q("plan")+` ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES `+q("resource")+`(id)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 7, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO `+q("supplier")+` (id,name,region) OVERRIDING SYSTEM VALUE VALUES(21,'计划并发供应商','CN')`); err != nil {
		t.Fatal(err)
	}
	r := &supply.Resource{ID: "41", SupplierID: "21", StandardModelID: "31", Region: "CN", SupplierModelID: "race", Coefficient: "0.7000", BuyerID: "7", State: "ADMITTED", EverAdmitted: true, Revision: 4, OriginRowID: "71", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := db.ExecContext(ctx, `INSERT INTO `+q("resource")+` (id,standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,created_at,updated_at) OVERRIDING SYSTEM VALUE VALUES(41,31,21,'race','fixture','CN',0.7,'ADMITTED',true,7,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resourceControlFrom(r, "FIXTURE"))
	if _, err := db.ExecContext(ctx, `INSERT INTO `+q("operation_log")+` (entity_type,entity_id,action,operator_id,"after",reason,request_id,created_at) VALUES('resource',41,$1,7,$2::jsonb,'fixture','race-fixture',$3)`, resourceRevisionAction, string(raw), now); err != nil {
		t.Fatal(err)
	}
	logger := func(c context.Context, tx *sql.Tx, e ResourceStateLog) error {
		_, err := tx.ExecContext(c, `INSERT INTO `+q("operation_log")+` (entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, nullablePlanJSON(e.Before), string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	outbox := func(c context.Context, tx *sql.Tx, e AdmissionOutboxEntry) (string, error) {
		var id string
		err := tx.QueryRowContext(c, `INSERT INTO `+q("event_outbox")+` (event_type,payload,request_id,created_by,created_at) VALUES($1,$2::jsonb,$3,$4,$5) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID, e.OccurredAt).Scan(&id)
		return id, err
	}
	makeService := func(tx *sql.Tx) *supply.PlanMaintenanceService {
		state, _ := NewResourceStateStore(schema, logger)
		store, err := NewPlanStore(schema, nil, state, &PlanHooks{Resolve: func(context.Context) (*sql.Tx, error) { return tx, nil }, Log: logger, Outbox: outbox, PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return false, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return supply.NewPlanMaintenanceService(store, func() time.Time { return now })
	}
	first, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	second, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback()
	meta := supply.WriteMeta{ActorID: "7", Reason: "竞争创建", RequestID: "race-first"}
	if _, err := makeService(first).Create(ctx, meta, "41", 4, "OFFLINE", now); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	other := makeService(second)
	go func() {
		m := meta
		m.RequestID = "race-second"
		_, err := other.Create(ctx, m, "41", 4, "OFFLINE", now)
		finished <- err
	}()
	blocked := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if err := db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("competing writer failed before lock %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !blocked {
		t.Fatal("second creator did not wait on the shared resource lock")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+q("plan")).Scan(&count); err != nil || count != 0 {
		t.Fatal("uncommitted plan visible", err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, supply.ErrRevisionConflict) {
			t.Fatal("duplicate creator not rejected", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT count(*) FROM ` + q("plan"), `SELECT count(*) FROM ` + q("operation_log") + ` WHERE entity_type='plan'`, `SELECT count(*) FROM ` + q("event_outbox")} {
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 1 {
			t.Fatal("duplicate or missing effect", count, err)
		}
	}
	t.Run("blocked_commits_without_resource_event", func(t *testing.T) {
		blockTx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer blockTx.Rollback()
		state, _ := NewResourceStateStore(schema, logger)
		changed := *r
		changed.BuyerID = "8"
		changed.Revision = 5
		changed.UpdatedAt = now.Add(time.Second)
		if _, err := state.Update(ctx, blockTx, &changed, 4, supply.WriteMeta{ActorID: "7", Reason: "公共转交事实", RequestID: "before-blocked"}, "TRANSFER"); err != nil {
			t.Fatal(err)
		}
		plans, err := NewPlanStore(schema, ModelReadTransactions(db), state, &PlanHooks{Resolve: func(context.Context) (*sql.Tx, error) { return blockTx, nil }, Log: logger, Outbox: outbox, PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return false, nil }})
		if err != nil {
			t.Fatal(err)
		}
		var id string
		if err := blockTx.QueryRowContext(ctx, `SELECT id::text FROM `+q("plan")).Scan(&id); err != nil {
			t.Fatal(err)
		}
		result, err := supply.NewPlanExecutionService(NewPlanExecutionWork(plans, nil, nil), func() time.Time { return now.Add(2 * time.Second) }).Execute(ctx, meta, id, 1, true, "外部操作前发现资源已转交")
		if err != nil || result == nil || !result.Conflict || result.Plan.Status != "BLOCKED" {
			t.Fatalf("business conflict returned rollback error %+v %v", result, err)
		}
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM `+q("plan")+` WHERE id=$1`, id).Scan(&status); err != nil || status != "PENDING" {
			t.Fatal("uncommitted failure visible", err)
		}
		if err := blockTx.Commit(); err != nil {
			t.Fatal(err)
		}
		view, err := supply.NewPlanReader(plans).Get(ctx, "7", id)
		if err != nil || view.Status != "BLOCKED" || view.Revision != 2 || view.PreconditionMatches {
			t.Fatalf("committed blocked outcome lost %+v %v", view, err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+q("event_outbox")+` WHERE event_type LIKE 'RESOURCE_%'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("blocked published resource event", err)
		}
		var failure, evidence string
		if err := db.QueryRowContext(ctx, `SELECT "after"->'execution'->>'failure_reason',"after"->'execution'->>'completion_note' FROM `+q("plan")+` WHERE id=$1`, id).Scan(&failure, &evidence); err != nil || failure != "RESOURCE_CHANGED" || evidence == "" {
			t.Fatal("blocked evidence missing", err)
		}
	})
}
