package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceCreationServicePostgres34(t *testing.T) {
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
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "resource", "operation_log", "submission_row", "test_job", "test_report"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	// Restore the native mutual FK relationship: resource first, then reports.
	for _, ddl := range []string{
		`ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id),ADD UNIQUE(supplier_id,standard_model_id,region)`,
		`ALTER TABLE pg_temp.test_job ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id)`,
		`ALTER TABLE pg_temp.test_report ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id),ADD FOREIGN KEY(test_job_id) REFERENCES pg_temp.test_job(id)`,
		`ALTER TABLE pg_temp.resource ADD FOREIGN KEY(latest_test_report_id) REFERENCES pg_temp.test_report(id)`,
		`INSERT INTO pg_temp.supplier(id,name,region) OVERRIDING SYSTEM VALUE VALUES(21,'办理供应商','CN')`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 21, 5, 0, 0, 0, time.UTC)
	mw, err := NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	model, err := supply.NewModelService(mw, nil, func() time.Time { return now }).Create(ctx, supply.WriteMeta{ActorID: "7", Reason: "创建夹具", RequestID: "create-fixture"}, supply.ModelData{Name: "办理模型", Company: "Example", OfficialID: "creation", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.submission_row(id,submission_id,standard_model_id,supplier_model_name,coefficient) OVERRIDING SYSTEM VALUE VALUES(71,61,$1,'vendor-created',0.68)`, model.ID); err != nil {
		t.Fatal(err)
	}
	state, err := NewResourceStateStore("pg_temp", func(c context.Context, current *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	proof := &supply.SubmissionResourceProof{RowID: "71", RowVersion: 2, TargetVersion: 3, SupplierID: "21", StandardModelID: model.ID, Region: "CN", SupplierModelID: "vendor-created", Coefficient: "0.6800", BuyerID: "7", FlowID: "81", FlowRevision: 4, AuthorizedActorID: "8", ApprovedAndAssigned: true, EnteredHandling: true}
	// Draft report is a trusted B fact until B materializes it for this resource.
	proof.Report = &supply.ResourceReportSummary{ID: "51", Sequence: 1, SupplierID: "21", StandardModelID: model.ID, Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, ConnectivityPassed: true, PressurePassed: true, Valid: true}
	failRow, badReport := false, false
	hooks := ResourceCreationHooks{
		LockSource: func(c context.Context, current *sql.Tx, row string, version uint64) (*supply.ResourceCreationContext, error) {
			if current != tx || row != "71" || version != 2 {
				t.Fatal("source snapshot/reference changed")
			}
			var locked string
			if err := current.QueryRowContext(c, `SELECT id::text FROM pg_temp.submission_row WHERE id=$1 FOR UPDATE`, row).Scan(&locked); err != nil {
				return nil, err
			}
			copy := *proof
			return &supply.ResourceCreationContext{Proof: &copy, Supplier: supply.ResourceSupplier{ID: "21", Region: "CN", Status: "ACTIVE"}, ModelID: model.ID}, nil
		},
		AttachRow: func(c context.Context, current *sql.Tx, e supply.ResourceCreationEffect) error {
			if failRow {
				return errors.New("B row CAS failed")
			}
			result, err := current.ExecContext(c, `UPDATE pg_temp.submission_row SET resource_id=$1 WHERE id=$2 AND (resource_id IS NULL OR resource_id=$1)`, e.Resource.ID, e.RowID)
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
		PrepareReport: func(c context.Context, current *sql.Tx, r *supply.Resource, report *supply.ResourceReportSummary) error {
			if report.ID != "51" || r.LatestReportID != "51" {
				t.Fatal("draft evidence reference replaced")
			}
			var job string
			if err := current.QueryRowContext(c, `INSERT INTO pg_temp.test_job(resource_id,category) VALUES($1,'stress') RETURNING id::text`, r.ID).Scan(&job); err != nil {
				return err
			}
			_, err := current.ExecContext(c, `INSERT INTO pg_temp.test_report(id,test_job_id,resource_id,connection_passed,stress_passed) OVERRIDING SYSTEM VALUE VALUES(51,$1,$2,true,$3)`, job, r.ID, !badReport)
			return err
		},
	}
	work := NewResourceCreationWork(state, func(context.Context) (*sql.Tx, error) { return tx, nil }, hooks)
	service := supply.NewResourceCreationService(work, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "8", Reason: "报价已审批分派进入办理", RequestID: "pg-create-resource"}
	cmd := supply.ResourceCreationCommand{RowID: "71", RowVersion: 2}
	for _, failure := range []string{"not_assigned", "wrong_actor", "row_failure", "bad_report", "missing_report_adapter", "outer_rollback"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT creation_failure"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				failRow = false
				badReport = false
				work.hooks.PrepareReport = hooks.PrepareReport
				proof.ApprovedAndAssigned = true
				if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT creation_failure"); err != nil {
					t.Fatal(err)
				}
			}()
			actor := meta
			switch failure {
			case "not_assigned":
				proof.ApprovedAndAssigned = false
			case "wrong_actor":
				actor.ActorID = "7"
			case "row_failure":
				failRow = true
			case "bad_report":
				badReport = true
			case "missing_report_adapter":
				work.hooks.PrepareReport = nil
			}
			got, err := service.CreateFromSubmission(ctx, actor, cmd)
			if failure == "outer_rollback" {
				if err != nil || got == nil {
					t.Fatalf("real service unavailable: %v", err)
				}
			} else if err == nil || got != nil {
				t.Fatal("failed creation returned success")
			}
			if failure == "not_assigned" && !errors.Is(err, supply.ErrRevisionConflict) {
				t.Fatal("wrong unassigned error", err)
			}
			if failure == "wrong_actor" && !errors.Is(err, supply.ErrForbidden) {
				t.Fatal("buyer substituted for current assignee", err)
			}
			if failure == "bad_report" && !errors.Is(err, supply.ErrReadContract) {
				t.Fatal("materialized report changed approval facts", err)
			}
			if failure == "missing_report_adapter" && !errors.Is(err, supply.ErrDependencyUnavailable) {
				t.Fatal("missing conversion invented report", err)
			}
		})
		var resources, logs, reports, attached int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pg_temp.resource),(SELECT count(*) FROM pg_temp.operation_log),(SELECT count(*) FROM pg_temp.test_report),(SELECT count(*) FROM pg_temp.submission_row WHERE resource_id IS NOT NULL)`).Scan(&resources, &logs, &reports, &attached); err != nil {
			t.Fatal(err)
		}
		if resources+logs+reports+attached != 0 {
			t.Fatalf("creation escaped outer rollback: %d %d %d %d", resources, logs, reports, attached)
		}
	}
	created, err := service.CreateFromSubmission(ctx, meta, cmd)
	if err != nil || created == nil {
		t.Fatalf("native service still unavailable: %v", err)
	}
	if !created.Created || created.Resource.State != "PENDING_ACCESS" || created.Resource.EverAdmitted || created.Resource.LatestReportID != "51" || created.Resource.BuyerID != "7" {
		t.Fatalf("wrong admission/ownership %+v", created)
	}
	second, err := service.CreateFromSubmission(ctx, meta, cmd)
	if err != nil || second.Created || second.Resource.ID != created.Resource.ID || second.Resource.Revision != 1 {
		t.Fatalf("creation replay reset/duplicated resource %+v %v", second, err)
	}
	if current, err := state.Current(ctx, tx, created.Resource.ID); err != nil || current.Revision != 1 || current.LatestReportSequence != 1 {
		t.Fatal("control head lost", err)
	}
	repository, err := NewResourceRepository("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, state.FactsLoader(func(c context.Context, current *sql.Tx, r *supply.Resource) (*ReportEvidenceRef, error) {
		var job string
		if err := current.QueryRowContext(c, `SELECT test_job_id::text FROM pg_temp.test_report WHERE id=$1`, r.LatestReportID).Scan(&job); err != nil {
			return nil, err
		}
		return &ReportEvidenceRef{ReportID: r.LatestReportID, JobID: job, ResourceID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, OriginRowID: proof.RowID, OriginRowVersion: proof.RowVersion, TargetVersion: proof.TargetVersion, Sequence: proof.Report.Sequence, Valid: proof.Report.Valid}, nil
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := supply.NewResourceReader(repository).Get(ctx, created.Resource.ID, "CN")
	if err != nil || snapshot.State != "PENDING_ACCESS" || snapshot.QuoteReadiness.Eligible || !snapshot.Test.ConnectivityPassed || !snapshot.Test.PressurePassed || snapshot.Test.ReportID != "51" {
		t.Fatalf("creation/read evidence chain %+v %v", snapshot, err)
	}
	var count int
	var linked string
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pg_temp.operation_log),resource_id::text FROM pg_temp.submission_row WHERE id=71`).Scan(&count, &linked); err != nil || count != 2 || linked != created.Resource.ID {
		t.Fatalf("row/log not atomic %d %s %v", count, linked, err)
	}
}
