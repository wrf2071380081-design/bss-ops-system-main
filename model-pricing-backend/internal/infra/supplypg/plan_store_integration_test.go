package supplypg

import (
	"bytes"
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

func TestPlanMaintenancePostgres34Native(t *testing.T) {
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
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"plan", "resource", "operation_log", "event_outbox"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{`SET LOCAL TIME ZONE 'Asia/Shanghai'`, `ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id)`, `ALTER TABLE pg_temp.plan ADD PRIMARY KEY(id), ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id)`} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC)
	resource := &supply.Resource{ID: "41", SupplierID: "21", StandardModelID: "31", Region: "CN", SupplierModelID: "vendor-model", Coefficient: "0.7000", BuyerID: "7", State: "ADMITTED", EverAdmitted: true, Revision: 4, OriginRowID: "71", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.resource(id,standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,created_at,updated_at) OVERRIDING SYSTEM VALUE VALUES(41,31,21,'vendor-model','fixture','CN',0.7,'ADMITTED',true,7,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	logMode, outMode := "", ""
	logger := func(c context.Context, current *sql.Tx, e ResourceStateLog) error {
		if current != tx {
			t.Fatal("escaped tx")
		}
		if logMode == "drop" && e.EntityType == "plan" {
			return nil
		}
		if logMode == "fail" && e.EntityType == "plan" {
			return errors.New("audit unavailable")
		}
		if logMode == "tamper" && e.EntityType == "plan" {
			needle := []byte(`"revision": "2"`)
			at := bytes.Index(e.Before, needle)
			if at < 0 {
				t.Fatal("missing prior revision in native jsonb")
			}
			e.Before[at+len(needle)-2] = '1'
		}
		if logMode == "sql" && e.EntityType == "plan" {
			e.RequestID = strings.Repeat("x", 65)
		}
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	raw, _ := json.Marshal(resourceControlFrom(resource, "FIXTURE"))
	if err := logger(ctx, tx, ResourceStateLog{EntityType: "resource", EntityID: "41", Action: resourceRevisionAction, ActorID: "7", Reason: "reviewed fixture", RequestID: "plan-fixture", After: raw, OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	state, _ := NewResourceStateStore("pg_temp", logger)
	pending := false
	hooks := &PlanHooks{Resolve: func(context.Context) (*sql.Tx, error) { return tx, nil }, Log: logger, PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return pending, nil }, Outbox: func(c context.Context, current *sql.Tx, e AdmissionOutboxEntry) (string, error) {
		if outMode == "fail" {
			return "", errors.New("outbox unavailable")
		}
		if outMode == "drop" {
			return "9999", nil
		}
		if outMode == "sql" {
			e.EventType = strings.Repeat("x", 49)
		}
		var id string
		err := current.QueryRowContext(c, `INSERT INTO pg_temp.event_outbox(event_type,payload,created_at,updated_at,request_id,created_by) VALUES($1,$2::jsonb,$3,$3,$4,$5) RETURNING id::text`, e.EventType, string(e.Payload), e.OccurredAt, e.RequestID, e.ActorID).Scan(&id)
		return id, err
	}}
	store, err := NewPlanStore("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, state, hooks)
	if err != nil {
		t.Fatal(err)
	}
	svc := supply.NewPlanMaintenanceService(store, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", Reason: "计划下架", RequestID: "plan-create"}
	// Due time is deliberately in the past; reads and reminders must not execute.
	p, err := svc.Create(ctx, meta, "41", 4, "OFFLINE", now.Add(-time.Hour))
	if err != nil || p == nil {
		t.Fatalf("plan create unavailable: %+v %v", p, err)
	}
	reader := supply.NewPlanReader(store)
	view, err := reader.Get(ctx, "8", p.ID)
	if err != nil || view.IsInitiator || view.Permissions.CanCancel || view.Permissions.CanExecute || view.Status != "PENDING" {
		t.Fatalf("other staff summary %+v %v", view, err)
	}
	view, err = reader.Get(ctx, "7", p.ID)
	if err != nil || !view.Permissions.CanRevise || !view.PreconditionMatches {
		t.Fatalf("initiator summary %+v %v", view, err)
	}
	if got, err := state.Current(ctx, tx, "41"); err != nil || !reflect.DeepEqual(got, resource) {
		t.Fatalf("plan changed resource %+v %v", got, err)
	}
	var original []byte
	if err := tx.QueryRowContext(ctx, `SELECT "before" FROM pg_temp.plan WHERE id=$1`, p.ID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, meta, "41", 4, "OFFLINE", now); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("duplicate pending plan", err)
	}
	other := meta
	other.ActorID = "8"
	if _, err := svc.Revise(ctx, other, p.ID, 1, now); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("other actor revised", err)
	}
	if _, err := svc.Revise(ctx, meta, p.ID, 9, now); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("stale revision accepted", err)
	}
	meta.RequestID = "plan-revise"
	meta.Reason = "调整预计时间"
	p, err = svc.Revise(ctx, meta, p.ID, 1, now.Add(time.Hour))
	if err != nil || p.Revision != 2 || p.ExpectedResourceRevision != 4 {
		t.Fatalf("revise %+v %v", p, err)
	}
	// Callback failures must be rolled back by the outer public transaction.
	for _, mode := range []string{"log-fail", "log-drop", "log-tamper", "log-sql", "out-fail", "out-drop", "out-sql"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, `SAVEPOINT failed_plan`); err != nil {
				t.Fatal(err)
			}
			logMode, outMode = "", ""
			if strings.HasPrefix(mode, "log-") {
				logMode = strings.TrimPrefix(mode, "log-")
			} else {
				outMode = strings.TrimPrefix(mode, "out-")
			}
			if _, err := svc.Cancel(ctx, meta, p.ID, 2); err == nil {
				t.Fatal("missing effect accepted")
			}
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT failed_plan`); err != nil {
				t.Fatal(err)
			}
			logMode, outMode = "", ""
			got, err := reader.Get(ctx, "7", p.ID)
			if err != nil || got.Revision != 2 || got.Status != "PENDING" {
				t.Fatalf("partial write survived %+v %v", got, err)
			}
		})
	}
	meta.RequestID = "plan-cancel"
	p, err = svc.Cancel(ctx, meta, p.ID, 2)
	if err != nil || p.Revision != 3 || p.Status != "CANCELED" {
		t.Fatalf("cancel %+v %v", p, err)
	}
	var retained []byte
	if err := tx.QueryRowContext(ctx, `SELECT "before" FROM pg_temp.plan WHERE id=$1`, p.ID).Scan(&retained); err != nil || string(retained) != string(original) {
		t.Fatal("original basis rewritten", err)
	}
	pending = true
	if _, err := svc.Create(ctx, meta, "41", 4, "OFFLINE", now); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("pending approved config ignored", err)
	}
	pending = false
	second, err := svc.Create(ctx, meta, "41", 4, "OFFLINE", now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.List(ctx, "8", supply.PlanListQuery{ResourceID: "41", Region: "CN", Page: 1, Size: 1})
	if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != second.ID {
		t.Fatalf("stable numeric tie/paging %+v %v", page, err)
	}
	page, err = reader.List(ctx, "8", supply.PlanListQuery{Status: "CANCELED"})
	if err != nil || page.Total != 1 || page.List[0].ID != p.ID {
		t.Fatalf("filtered total %+v %v", page, err)
	}
	var logs, events int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.operation_log WHERE entity_type='plan'`).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.event_outbox`).Scan(&events); err != nil || logs != 4 || events != 4 {
		t.Fatalf("effects count log=%d events=%d %v", logs, events, err)
	}
	// Resource transfer does not transfer the old plan or silently rebase it.
	transferred := *resource
	transferred.BuyerID = "8"
	transferred.Revision = 5
	transferred.UpdatedAt = now.Add(time.Second)
	if _, err := state.Update(ctx, tx, &transferred, 4, supply.WriteMeta{ActorID: "7", Reason: "公共转交夹具", RequestID: "plan-transfer"}, "TRANSFER"); err != nil {
		t.Fatal(err)
	}
	view, err = reader.Get(ctx, "8", second.ID)
	if err != nil || view.IsInitiator || view.PreconditionMatches || view.Permissions.CanRevise {
		t.Fatalf("transferred buyer claimed plan %+v %v", view, err)
	}
	if _, err := svc.Revise(ctx, other, second.ID, 1, now); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("new buyer revised old plan", err)
	}
	revised, err := svc.Revise(ctx, meta, second.ID, 1, now.Add(2*time.Hour))
	if err != nil || revised.CreatedBy != "7" || revised.ExpectedResourceRevision != 4 {
		t.Fatalf("original basis changed %+v %v", revised, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.plan SET "after"='{}' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Get(ctx, "7", p.ID); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("legacy/corrupt snapshot guessed", err)
	}
}
