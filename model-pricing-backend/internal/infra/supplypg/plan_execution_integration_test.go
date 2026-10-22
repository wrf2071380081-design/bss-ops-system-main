package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"slices"
	"testing"
	"time"
)

// Reuses the real model/report/config/admission chain, with changes isolated by
// an outer savepoint. All providers still operate on this exact SQL transaction.
func testPlanExecutionWithNativeChain(t *testing.T, ctx context.Context, tx *sql.Tx, state *ResourceStateStore, configs *FormalConfigStore, report ResourceReportRefLoader, log ResourceStateLogAppender, resourceID string, now time.Time) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT plan_chain`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT plan_chain`); err != nil {
			t.Error(err)
		}
	}()
	failMode := ""
	logger := func(c context.Context, current *sql.Tx, e ResourceStateLog) error {
		if failMode == "log" && e.EntityType == "plan" {
			return nil
		}
		return log(c, current, e)
	}
	outbox := func(c context.Context, current *sql.Tx, e AdmissionOutboxEntry) (string, error) {
		if failMode == "event" && e.EventType == "RESOURCE_OFFLINED" {
			return "9999", nil
		}
		var id string
		err := current.QueryRowContext(c, `INSERT INTO pg_temp.event_outbox(event_type,payload,request_id,created_by,created_at) VALUES($1,$2::jsonb,$3,$4,$5) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID, e.OccurredAt).Scan(&id)
		return id, err
	}
	plans, err := NewPlanStore("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, state, &PlanHooks{Resolve: func(context.Context) (*sql.Tx, error) { return tx, nil }, Log: logger, Outbox: outbox, PendingConfiguration: func(context.Context, *sql.Tx, string) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	maintenance := supply.NewPlanMaintenanceService(plans, func() time.Time { return now })
	execute := supply.NewPlanExecutionService(NewPlanExecutionWork(plans, configs, report), func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", Reason: "实际上下架", RequestID: "plan-execute-chain"}
	p, err := maintenance.Create(ctx, meta, resourceID, 5, "OFFLINE", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"other", "stale", "log", "event", "notice"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, `SAVEPOINT execution_failure`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				failMode = ""
				if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT execution_failure`); err != nil {
					t.Error(err)
				}
			}()
			failMode = failure
			m := meta
			revision := uint64(1)
			if failure == "other" {
				m.ActorID = "8"
			}
			if failure == "stale" {
				revision = 2
			}
			if failure == "notice" {
				if _, err := tx.ExecContext(ctx, `ALTER TABLE pg_temp.notice ADD CONSTRAINT reject_plan_notice CHECK(false)`); err != nil {
					t.Fatal(err)
				}
			}
			got, err := execute.Execute(ctx, m, p.ID, revision, true, "外部已执行")
			if err == nil || got != nil {
				t.Fatal("invalid execution succeeded")
			}
			if failure == "other" && !errors.Is(err, supply.ErrForbidden) {
				t.Fatal(err)
			}
		})
		current, err := state.Current(ctx, tx, resourceID)
		if err != nil || current.State != "ADMITTED" || current.Revision != 5 {
			t.Fatal("partial execution persisted", err)
		}
	}
	result, err := execute.Execute(ctx, meta, p.ID, 1, true, "外部已下架，操作单OFF-001")
	if err != nil || result == nil || result.Conflict || result.Plan.Status != "EXECUTED" {
		t.Fatalf("offline execution unavailable %+v %v", result, err)
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM pg_temp.plan WHERE id=$1`, p.ID).Scan(&status); err != nil || status != "DONE" {
		t.Fatal("public work item status mismatch", err)
	}
	current, err := state.Current(ctx, tx, resourceID)
	if err != nil || current.State != "OFFLINE" || current.Revision != 6 {
		t.Fatal("offline resource not saved", err)
	}
	if _, err := execute.Execute(ctx, meta, p.ID, 1, true, "重复执行"); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("duplicate completion", err)
	}
	var notices, events int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pg_temp.notice),(SELECT count(*) FROM pg_temp.event_outbox WHERE event_type IN ('RESOURCE_ONLINED','RESOURCE_OFFLINED'))`).Scan(&notices, &events); err != nil || notices != 1 || events != 1 {
		t.Fatal("missing notice/event", err)
	}
	var noticeID, eventID string
	var payload []byte
	var published time.Time
	if err := tx.QueryRowContext(ctx, `SELECT id::text,payload,created_at FROM pg_temp.notice`).Scan(&noticeID, &payload, &published); err != nil {
		t.Fatal(err)
	}
	n, err := (NoticeRow{ID: noticeID, Kind: "RESOURCE_CHANGE", TargetType: "RESOURCE", ResourceID: resourceID, Payload: payload, PublishedAt: published}).DomainNotice()
	if err != nil || n.AffectedStatus != "PENDING" || n.AffectedCustomerCount != nil {
		t.Fatal("invalid safe notice", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM pg_temp.event_outbox WHERE event_type='RESOURCE_OFFLINED'`).Scan(&eventID); err != nil || eventID != n.SourceEventID {
		t.Fatal("notice source event mismatch", err)
	}
	up, err := maintenance.Create(ctx, meta, resourceID, 6, "ONLINE", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT blocked_online`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.test_report SET stress_passed=false WHERE id=$1`, current.LatestReportID); err != nil {
		t.Fatal(err)
	}
	blocked, err := execute.Execute(ctx, meta, up.ID, 1, true, "尝试恢复上架")
	if err != nil || blocked == nil || !blocked.Conflict || blocked.Plan.Status != "BLOCKED" {
		t.Fatalf("blocked outcome was not retained %+v %v", blocked, err)
	}
	view, err := supply.NewPlanReader(plans).Get(ctx, "7", up.ID)
	if err != nil || view.Status != "BLOCKED" || !view.Permissions.CanCancel || !slices.Contains(view.BlockedReasons, "RESOURCE_NOT_READY") {
		t.Fatal("blocked read missing", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.notice`).Scan(&notices); err != nil || notices != 1 {
		t.Fatal("blocked emitted notice", err)
	}
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT blocked_online`); err != nil {
		t.Fatal(err)
	}
	result, err = execute.Execute(ctx, meta, up.ID, 1, true, "外部已上架，操作单ON-001")
	if err != nil || result == nil || result.Conflict {
		t.Fatalf("online readiness unavailable %+v %v", result, err)
	}
	current, err = state.Current(ctx, tx, resourceID)
	if err != nil || current.State != "ADMITTED" || current.Revision != 7 {
		t.Fatal("online state not saved", err)
	}
}
