package supplypg

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdmissionFailedEvidenceContentionPostgres34(t *testing.T) {
	for _, admitFirst := range []bool{false, true} {
		name := "failed_report_first"
		if admitFirst {
			name = "admission_first"
		}
		t.Run(name, func(t *testing.T) { testAdmissionFailedEvidenceContention(t, admitFirst) })
	}
}
func testAdmissionFailedEvidenceContention(t *testing.T, admitFirst bool) {
	database := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if database == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(database, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != database {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != database {
		t.Fatal("wrong database")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	label := fmt.Sprintf("A-admit-evidence-%d", now.UnixNano())
	fixture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback()
	var org, supplier, submission, row, flow, node string
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&org); err != nil {
		t.Fatal(err)
	}
	staff := []string{}
	for i := 0; i < 2; i++ {
		var id string
		if err := fixture.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, org, label, fmt.Sprint(now.UnixNano()+int64(i))).Scan(&id); err != nil {
			t.Fatal(err)
		}
		staff = append(staff, id)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,created_by) VALUES($1,'CN',$2) RETURNING id::text`, label, staff[0]).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.ExecContext(ctx, `INSERT INTO public.assignment_log(entity_type,entity_id,field,new_staff_id,changed_by) VALUES('supplier',$1,'buyer_id',$2,$2)`, supplier, staff[0]); err != nil {
		t.Fatal(err)
	}
	logger := func(c context.Context, tx *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(c, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	state, err := NewResourceStateStore("public", logger)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := NewFormalConfigStore("public", func(c context.Context, tx *sql.Tx, e FormalConfigLog) error {
		return logger(c, tx, ResourceStateLog(e))
	})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(context.Context) (*sql.Tx, error) { return fixture, nil }
	buyer := supply.WriteMeta{ActorID: staff[0], Reason: "接入与失败报告并发验收", RequestID: label}
	actor := buyer
	actor.ActorID = staff[1]
	mw, err := NewModelUnitOfWork("public", resolve, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewOfficialSourcePolicy(map[string][]string{"Example": {"https://vendor.example"}})
	if err != nil {
		t.Fatal(err)
	}
	models := supply.NewModelService(mw, policy, func() time.Time { return now })
	data := modelWriteCompleteData()
	data.Name = label
	data.OfficialID = label
	model, err := models.Create(ctx, buyer, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := models.Verify(ctx, buyer, model.ID, 1, "2026-10-21"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.submission(supplier_id,kind,region,status) VALUES($1,'MODEL_NEW','CN','PROCESSING') RETURNING id::text`, supplier).Scan(&submission); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.submission_row(submission_id,standard_model_id,supplier_model_name,coefficient) VALUES($1,$2,$3,0.7) RETURNING id::text`, submission, model.ID, label).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.onboarding_flow(kind,owner_id,status) VALUES('SUPPLIER',$1,'PROCESSING') RETURNING id::text`, supplier).Scan(&flow); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.onboarding_node(flow_id,node_key,status,assignee_id) VALUES($1,'CONFIG','IN_PROGRESS',$2) RETURNING id::text`, flow, staff[1]).Scan(&node); err != nil {
		t.Fatal(err)
	}
	resourceID, err := state.NextID(ctx, fixture)
	if err != nil {
		t.Fatal(err)
	}
	initial := &supply.Resource{ID: resourceID, SupplierID: supplier, StandardModelID: model.ID, Region: "CN", SupplierModelID: label, Coefficient: "0.7000", BuyerID: staff[0], State: "PENDING_TEST", Revision: 1, OriginRowID: row, OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := state.Insert(ctx, fixture, initial, actor, "CREATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.ExecContext(ctx, `UPDATE public.submission_row SET resource_id=$1 WHERE id=$2`, resourceID, row); err != nil {
		t.Fatal(err)
	}
	refs := map[string]*ReportEvidenceRef{}
	var passed, failed string
	for i, success := range []bool{true, false} {
		var job, report string
		if err := fixture.QueryRowContext(ctx, `INSERT INTO public.test_job(resource_id,category,status) VALUES($1,'stress','SUCCEEDED') RETURNING id::text`, resourceID).Scan(&job); err != nil {
			t.Fatal(err)
		}
		if err := fixture.QueryRowContext(ctx, `INSERT INTO public.test_report(test_job_id,resource_id,connection_passed,stress_passed) VALUES($1,$2,true,$3) RETURNING id::text`, job, resourceID, success).Scan(&report); err != nil {
			t.Fatal(err)
		}
		refs[report] = &ReportEvidenceRef{ReportID: report, JobID: job, ResourceID: resourceID, SupplierID: supplier, StandardModelID: model.ID, Region: "CN", OriginRowID: row, OriginRowVersion: 1, TargetVersion: 1, Sequence: uint64(i + 1), Valid: true}
		if success {
			passed = report
		} else {
			failed = report
		}
	}
	authority := func(context.Context, *sql.Tx, *supply.Resource, string) (*ReportEvidenceRef, bool, error) {
		return nil, false, supply.ErrDependencyUnavailable
	}
	authority = func(_ context.Context, _ *sql.Tx, _ *supply.Resource, id string) (*ReportEvidenceRef, bool, error) {
		r := refs[id]
		if r == nil {
			return nil, true, supply.ErrNotFound
		}
		copy := *r
		return &copy, true, nil
	}
	writeTime := now.Add(time.Second)
	evidence := func(tx *sql.Tx) *supply.TestEvidenceService {
		return supply.NewTestEvidenceService(NewTestEvidenceWork(state, func(context.Context) (*sql.Tx, error) { return tx, nil }, authority), func() time.Time { return writeTime })
	}
	if _, err := evidence(fixture).AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: resourceID, ReportID: passed}); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("public-race-test-only-key"))
	block, _ := aes.NewCipher(key[:])
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		t.Fatal(err)
	}
	secret := "public-race-test-only-credential"
	config, err := supply.NewFormalConfigService(NewFormalConfigWork(configs, resolve), configPGSealer{aead}, configPGURL{}, func() time.Time { return now }).Create(ctx, buyer, supplier, supply.FormalConfigInput{Scope: "并发测试范围", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	proof := func(c context.Context, tx *sql.Tx, r *supply.Resource) (*supply.AdmissionProof, error) {
		var assigned, status string
		if err := tx.QueryRowContext(c, `SELECT assignee_id::text,status FROM public.onboarding_node WHERE id=$1 AND flow_id=$2 FOR UPDATE`, node, flow).Scan(&assigned, &status); err != nil {
			return nil, err
		}
		return &supply.AdmissionProof{FlowID: flow, NodeID: node, FlowRevision: 1, SupplierID: supplier, ResourceID: r.ID, Region: "CN", OriginRowID: row, OriginRowVersion: 1, TargetVersion: 1, AssigneeID: assigned, FlowInProgress: true, NodeInProgress: status == "IN_PROGRESS", PrerequisitesReady: true}, nil
	}
	if _, err := supply.NewResourceConfigBindingService(NewResourceBindingWork(state, configs, resolve, proof), func() time.Time { return now.Add(2 * time.Second) }).BindFormalConfig(ctx, actor, supply.ResourceConfigBindingCommand{ResourceID: resourceID, ConfigID: config.ID, ExpectedRevision: 2, ExpectedConfigRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(); err != nil {
		t.Fatal(err)
	}
	writeTime = now.Add(3 * time.Second)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, q := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM public.event_outbox WHERE request_id=$1`, []any{label}},
			{`DELETE FROM public.operation_log WHERE (entity_type='resource' AND entity_id=$1) OR (entity_type='supplier_formal_config' AND entity_id=$2)`, []any{resourceID, config.ID}},
			{`DELETE FROM public.submission WHERE id=$1 AND supplier_id=$2`, []any{submission, supplier}},
			{`DELETE FROM public.resource WHERE id=$1 AND supplier_id=$2`, []any{resourceID, supplier}},
			{`DELETE FROM public.onboarding_flow WHERE id=$1 AND owner_id=$2`, []any{flow, supplier}},
			{`DELETE FROM public.supplier_formal_config WHERE id=$1 AND supplier_id=$2`, []any{config.ID, supplier}},
			{`DELETE FROM public.standard_model WHERE id=$1 AND official_id=$2`, []any{model.ID, label}},
			{`DELETE FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1`, []any{supplier}},
			{`DELETE FROM public.supplier WHERE id=$1 AND name=$2`, []any{supplier, label}},
			{`DELETE FROM public.internal_staff WHERE org_unit_id=$1 AND name=$2`, []any{org, label}},
			{`DELETE FROM public.org_unit WHERE id=$1 AND name=$2`, []any{org, label}},
		} {
			if _, err := db.ExecContext(cleanup, q.query, q.args...); err != nil {
				t.Error(err)
			}
		}
	}()
	admit := func(tx *sql.Tx) error {
		work := NewResourceAdmissionWork(state, configs, func(context.Context) (*sql.Tx, error) { return tx, nil }, ResourceAdmissionHooks{Proof: proof,
			Report: func(c context.Context, current *sql.Tx, r *supply.Resource) (*ReportEvidenceRef, error) {
				ref, _, err := authority(c, current, r, r.LatestReportID)
				return ref, err
			},
			CompleteRow: func(c context.Context, current *sql.Tx, e supply.ResourceAdmissionEffect) error {
				result, err := current.ExecContext(c, `UPDATE public.submission_row SET row_status='ADMITTED' WHERE id=$1 AND resource_id=$2 AND row_status='PENDING'`, row, resourceID)
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					return supply.ErrRevisionConflict
				}
				return nil
			},
			Outbox: func(c context.Context, current *sql.Tx, e AdmissionOutboxEntry) (string, error) {
				var id string
				err := current.QueryRowContext(c, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by) VALUES($1,$2::jsonb,$3,$4) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID).Scan(&id)
				return id, err
			},
		})
		_, err := supply.NewResourceAdmissionService(work, func() time.Time { return writeTime }).Admit(ctx, actor, supply.ResourceAdmissionCommand{ResourceID: resourceID, ExpectedRevision: 3, ExternalConfirmed: true, CompletionNote: "公共表并发验收已实际完成"})
		return err
	}
	attachFailure := func(tx *sql.Tx) error {
		_, err := evidence(tx).AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: resourceID, ReportID: failed})
		return err
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
	firstOp, secondOp := attachFailure, admit
	if admitFirst {
		firstOp, secondOp = admit, attachFailure
	}
	if err := firstOp(first); err != nil {
		t.Fatal("first operation", err)
	}
	var firstPID, secondPID int
	if err := first.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- secondOp(second) }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	blocked := false
	for !blocked {
		if err := db.QueryRowContext(ctx, `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, firstPID, secondPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("second operation bypassed shared lock: %v", err)
		case <-timer.C:
			t.Fatal("no actual lock wait observed")
		case <-ticker.C:
		}
	}
	var visibleEvents int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.event_outbox WHERE request_id=$1`, label).Scan(&visibleEvents); err != nil || visibleEvents != 0 {
		t.Fatal("outbox visible before outer commit", err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if admitFirst {
			if err != nil {
				t.Fatal("trusted failed retest rejected", err)
			}
		} else if !errors.Is(err, supply.ErrRevisionConflict) {
			t.Fatal("admission used superseded report", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if admitFirst {
		if err := second.Commit(); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := second.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	if err := ModelReadTransactions(db)(ctx, func(tx *sql.Tx) error {
		r, err := state.Current(ctx, tx, resourceID)
		if err != nil {
			return err
		}
		var rowStatus string
		var events int
		if err := tx.QueryRowContext(ctx, `SELECT row_status,(SELECT count(*) FROM public.event_outbox WHERE request_id=$1) FROM public.submission_row WHERE id=$2`, label, row).Scan(&rowStatus, &events); err != nil {
			return err
		}
		if r.LatestReportID != failed {
			t.Fatal("failed report was lost")
		}
		if admitFirst {
			if r.State != "ADMITTED" || !r.EverAdmitted || r.Revision != 5 || rowStatus != "ADMITTED" || events != 1 {
				t.Fatal("failed retest changed actual state or duplicated admission")
			}
		} else {
			if r.State != "PENDING_TEST" || r.EverAdmitted || r.Revision != 4 || rowStatus != "PENDING" || events != 0 {
				t.Fatal("admission bypassed committed failed report")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
