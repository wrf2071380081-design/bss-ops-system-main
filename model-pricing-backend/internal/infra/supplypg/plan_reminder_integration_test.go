package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlanReminderPostgres36IgnoresRevisedAndCanceledSchedules(t *testing.T) {
	dbName := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if dbName == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(dbName, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != dbName {
		t.Fatal("dedicated cloned v36 database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	var currentDB string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil || currentDB != dbName {
		t.Fatalf("wrong test database %q: %v", currentDB, err)
	}
	var staffExists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.internal_staff WHERE id=3)`).Scan(&staffExists); err != nil || !staffExists {
		t.Fatalf("test initiator staff 3 unavailable: %v", err)
	}

	stamp := time.Now().UTC().Format("20060102150405.000000")
	unique := "qa-plan-reminder-" + strings.ReplaceAll(stamp, ".", "-")
	var modelID string
	supplierIDs := []string{}
	resourceIDs := []string{}
	planIDs := []string{}
	requestPrefix := unique + "-"
	t.Cleanup(func() {
		for _, id := range planIDs {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.notice WHERE target_id IN (SELECT resource_id::text FROM public.plan WHERE id::text=$1)`, id)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.plan WHERE id::text=$1`, id)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.operation_log WHERE entity_type='plan' AND entity_id=$1`, id)
		}
		for _, id := range resourceIDs {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.notice WHERE target_id=$1`, id)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.operation_log WHERE entity_type='resource' AND entity_id=$1`, id)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.resource WHERE id::text=$1`, id)
		}
		_, _ = db.ExecContext(context.Background(), `DELETE FROM public.event_outbox WHERE request_id LIKE $1`, requestPrefix+"%")
		if modelID != "" {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.standard_model WHERE id::text=$1`, modelID)
		}
		for _, id := range supplierIDs {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM public.supplier WHERE id::text=$1`, id)
		}
		_ = db.Close()
	})
	for _, suffix := range []string{"revise", "cancel"} {
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,status) VALUES($1,'CN','ACTIVE') RETURNING id::text`, unique+"-"+suffix).Scan(&id); err != nil {
			t.Fatal(err)
		}
		supplierIDs = append(supplierIDs, id)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO public.standard_model(company,model_type,name,official_id) VALUES('QA ONLY','TEXT',$1,$2) RETURNING id::text`, unique, unique).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	stateStore, err := NewResourceStateStore("public", func(ctx context.Context, tx *sql.Tx, entry ResourceStateLog) error {
		var before any
		if len(entry.Before) > 0 {
			before = string(entry.Before)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, entry.EntityType, entry.EntityID, entry.Action, entry.ActorID, before, string(entry.After), entry.Reason, entry.RequestID, entry.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var activeTx *sql.Tx
	resolve := func(c context.Context) (*sql.Tx, error) {
		var err error
		activeTx, err = db.BeginTx(c, nil)
		return activeTx, err
	}
	read := func(c context.Context, run func(*sql.Tx) error) error {
		tx, err := db.BeginTx(c, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := run(tx); err != nil {
			return err
		}
		return tx.Commit()
	}
	log := func(c context.Context, tx *sql.Tx, entry ResourceStateLog) error {
		var before any
		if len(entry.Before) > 0 {
			before = string(entry.Before)
		}
		_, err := tx.ExecContext(c, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, entry.EntityType, entry.EntityID, entry.Action, entry.ActorID, before, string(entry.After), entry.Reason, entry.RequestID, entry.OccurredAt)
		return err
	}
	hooks := &PlanHooks{
		Resolve:              resolve,
		Log:                  log,
		PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return false, nil },
		Outbox: func(c context.Context, tx *sql.Tx, entry AdmissionOutboxEntry) (string, error) {
			var id string
			err := tx.QueryRowContext(c, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by,created_at,updated_at) VALUES($1,$2::jsonb,$3,$4,$5,$5) RETURNING id::text`, entry.EventType, string(entry.Payload), entry.RequestID, entry.ActorID, entry.OccurredAt).Scan(&id)
			return id, err
		},
	}
	plans, err := NewPlanStore("public", read, stateStore, hooks)
	if err != nil {
		t.Fatal(err)
	}
	maintenance := supply.NewPlanMaintenanceService(plans, func() time.Time { return now })
	addResource := func(resourceSupplierID, label string) string {
		t.Helper()
		resource := &supply.Resource{SupplierID: resourceSupplierID, StandardModelID: modelID, Region: "CN", SupplierModelID: unique + "-" + label, Coefficient: "0.7000", BuyerID: "3", State: "ADMITTED", EverAdmitted: true, Revision: 1, OriginRowID: "91001", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
		if err := db.QueryRowContext(ctx, `INSERT INTO public.resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,created_at,updated_at) VALUES($1,$2,$3,$4,'CN',0.7,'ADMITTED',true,3,$5,$5) RETURNING id::text`, modelID, resourceSupplierID, resource.SupplierModelID, unique, now).Scan(&resource.ID); err != nil {
			t.Fatal(err)
		}
		resourceIDs = append(resourceIDs, resource.ID)
		raw, err := json.Marshal(resourceControlFrom(resource, "QA_FIXTURE"))
		if err != nil {
			t.Fatal(err)
		}
		// Commit the initial native control head so real plan transactions can read it.
		initialTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := log(ctx, initialTx, ResourceStateLog{EntityType: "resource", EntityID: resource.ID, Action: resourceRevisionAction, ActorID: "3", Reason: "QA fixture", RequestID: unique + "-resource-" + label, After: raw, OccurredAt: now}); err != nil {
			_ = initialTx.Rollback()
			t.Fatal(err)
		}
		if err := initialTx.Commit(); err != nil {
			t.Fatal(err)
		}
		return resource.ID
	}
	createPlan := func(resourceID, key string) *supply.ResourcePlan {
		t.Helper()
		activeTx = nil
		plan, err := maintenance.Create(ctx, supply.WriteMeta{ActorID: "3", Reason: "QA ONLY reminder integration", RequestID: requestPrefix + key}, resourceID, 1, "OFFLINE", now.Add(-time.Hour))
		if err != nil {
			if activeTx != nil {
				_ = activeTx.Rollback()
			}
			t.Fatal(err)
		}
		if activeTx == nil {
			t.Fatal("plan maintenance did not open its transaction")
		}
		if err := activeTx.Commit(); err != nil {
			t.Fatal(err)
		}
		planIDs = append(planIDs, plan.ID)
		return plan
	}
	revisedResourceID := addResource(supplierIDs[0], "revised-model")
	revisedPlan := createPlan(revisedResourceID, "create-revised")
	activeTx = nil
	revised, err := maintenance.Revise(ctx, supply.WriteMeta{ActorID: "3", Reason: "QA revise reminder", RequestID: requestPrefix + "revise"}, revisedPlan.ID, 1, now.Add(-30*time.Minute))
	if err != nil || activeTx == nil {
		if activeTx != nil {
			_ = activeTx.Rollback()
		}
		t.Fatalf("revise plan: %+v %v", revised, err)
	}
	if err := activeTx.Commit(); err != nil {
		t.Fatal(err)
	}
	var oldScheduleEvent, currentScheduleEvent string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM public.event_outbox WHERE event_type='PLAN_REMINDER_SCHEDULED' AND payload->>'plan_id'=$1 AND payload->>'revision'='1' AND payload->>'schedule'='true'`, revisedPlan.ID).Scan(&oldScheduleEvent); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM public.event_outbox WHERE event_type='PLAN_REMINDER_SCHEDULED' AND payload->>'plan_id'=$1 AND payload->>'revision'='2' AND payload->>'schedule'='true'`, revisedPlan.ID).Scan(&currentScheduleEvent); err != nil {
		t.Fatal(err)
	}
	canceledResourceID := addResource(supplierIDs[1], "canceled-model")
	canceledPlan := createPlan(canceledResourceID, "create-canceled")
	activeTx = nil
	canceled, err := maintenance.Cancel(ctx, supply.WriteMeta{ActorID: "3", Reason: "QA cancel reminder", RequestID: requestPrefix + "cancel"}, canceledPlan.ID, 1)
	if err != nil || activeTx == nil {
		if activeTx != nil {
			_ = activeTx.Rollback()
		}
		t.Fatalf("cancel plan: %+v %v", canceled, err)
	}
	if err := activeTx.Commit(); err != nil {
		t.Fatal(err)
	}
	if revised == nil || revised.Revision != 2 || canceled == nil || canceled.Revision != 2 || canceled.Status != "CANCELED" {
		t.Fatalf("unexpected plan revisions: revised=%+v canceled=%+v", revised, canceled)
	}

	reminders, err := NewPlanReminderStore(db, plans)
	if err != nil {
		t.Fatal(err)
	}
	created, err := reminders.PublishDuePlanReminders(ctx, now, 10)
	if err != nil || created != 1 {
		t.Fatalf("only the revised active plan should produce a reminder: count=%d err=%v", created, err)
	}
	var revisedNoticeEvent string
	if err := db.QueryRowContext(ctx, `SELECT payload->>'source_event_id' FROM public.notice WHERE type='ONLINE_OFFLINE_PLAN' AND target_id=$1`, revisedResourceID).Scan(&revisedNoticeEvent); err != nil {
		t.Fatal(err)
	}
	if revisedNoticeEvent != currentScheduleEvent || revisedNoticeEvent == oldScheduleEvent {
		t.Fatalf("revised notice used schedule %s; expected current %s (stale was %s)", revisedNoticeEvent, currentScheduleEvent, oldScheduleEvent)
	}
	var canceledNoticeCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.notice WHERE type='ONLINE_OFFLINE_PLAN' AND target_id=$1`, canceledResourceID).Scan(&canceledNoticeCount); err != nil || canceledNoticeCount != 0 {
		t.Fatalf("canceled plan reminders=%d err=%v", canceledNoticeCount, err)
	}
	created, err = reminders.PublishDuePlanReminders(ctx, now, 10)
	if err != nil || created != 0 {
		t.Fatalf("reminder replay count=%d err=%v", created, err)
	}
	for _, id := range []string{revisedResourceID, canceledResourceID} {
		var state string
		if err := db.QueryRowContext(ctx, `SELECT state FROM public.resource WHERE id=$1`, id).Scan(&state); err != nil || state != "ADMITTED" {
			t.Fatalf("reminder changed resource %s state=%s err=%v", id, state, err)
		}
	}
}
