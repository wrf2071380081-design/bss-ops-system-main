package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceEvidenceAdmissionChainPostgres34(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "supplier_formal_config", "assignment_log", "operation_log", "resource", "submission_row", "test_job", "test_report", "onboarding_flow", "onboarding_node", "event_outbox", "plan", "notice"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`SET LOCAL TIME ZONE 'Asia/Shanghai'`,
		`ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id),ADD UNIQUE(supplier_id,standard_model_id,region)`,
		`ALTER TABLE pg_temp.test_job ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id)`,
		`ALTER TABLE pg_temp.test_report ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id),ADD FOREIGN KEY(test_job_id) REFERENCES pg_temp.test_job(id)`,
		`ALTER TABLE pg_temp.resource ADD FOREIGN KEY(latest_test_report_id) REFERENCES pg_temp.test_report(id)`,
		`ALTER TABLE pg_temp.submission_row ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id)`,
		`ALTER TABLE pg_temp.plan ADD PRIMARY KEY(id),ADD FOREIGN KEY(resource_id) REFERENCES pg_temp.resource(id)`,
		`INSERT INTO pg_temp.supplier(id,name,region,created_by) OVERRIDING SYSTEM VALUE VALUES(21,'接入供应商','CN',7)`,
		`INSERT INTO pg_temp.assignment_log(entity_type,entity_id,field,new_staff_id,changed_by) VALUES('supplier',21,'buyer_id',7,7)`,
		`INSERT INTO pg_temp.onboarding_flow(id,kind,owner_id,status) OVERRIDING SYSTEM VALUE VALUES(81,'SUPPLIER',21,'PROCESSING')`,
		`INSERT INTO pg_temp.onboarding_node(id,flow_id,node_key,status,assignee_id) OVERRIDING SYSTEM VALUE VALUES(82,81,'CONFIG','IN_PROGRESS',8)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	buyer := supply.WriteMeta{ActorID: "7", Reason: "接入完整链夹具", RequestID: "admission-chain"}
	actor := supply.WriteMeta{ActorID: "8", Reason: "当前节点登记接入", RequestID: "admit-node-assignee"}
	dropLog, failRow, failOutbox, emptyOutbox, wrongOutbox, stripCompletion := false, false, false, false, false, false
	log := func(c context.Context, current *sql.Tx, e ResourceStateLog) error {
		if current != tx {
			t.Fatal("log escaped shared transaction")
		}
		if dropLog {
			return nil
		}
		if stripCompletion {
			var body map[string]json.RawMessage
			if err := json.Unmarshal(e.After, &body); err != nil {
				return err
			}
			delete(body, "completion_note")
			e.After, _ = json.Marshal(body)
		}
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	state, err := NewResourceStateStore("pg_temp", log)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := NewFormalConfigStore("pg_temp", func(c context.Context, current *sql.Tx, e FormalConfigLog) error {
		return log(c, current, ResourceStateLog(e))
	})
	if err != nil {
		t.Fatal(err)
	}
	modelWork, err := NewModelUnitOfWork("pg_temp", resolve, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	official, err := NewOfficialSourcePolicy(map[string][]string{"Example": {"https://vendor.example"}})
	if err != nil {
		t.Fatal(err)
	}
	models := supply.NewModelService(modelWork, official, func() time.Time { return now })
	complete := modelWriteCompleteData()
	complete.Name = "链路完整但待核验模型"
	complete.OfficialID = "admission-complete"
	model, err := models.Create(ctx, buyer, complete)
	if err != nil {
		t.Fatal(err)
	}
	other, err := models.Create(ctx, buyer, supply.ModelData{Name: "同批另一模型", Company: "Example", OfficialID: "admission-other", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.submission_row(id,submission_id,standard_model_id,supplier_model_name,coefficient) OVERRIDING SYSTEM VALUE VALUES(71,61,$1,'vendor-first',0.68),(72,61,$2,'vendor-second',0.7)`, model.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	createWork := NewResourceCreationWork(state, resolve, ResourceCreationHooks{
		LockSource: func(c context.Context, current *sql.Tx, row string, version uint64) (*supply.ResourceCreationContext, error) {
			var mid, vendor, coefficient string
			if err := current.QueryRowContext(c, `SELECT standard_model_id::text,supplier_model_name,coefficient::text FROM pg_temp.submission_row WHERE id=$1 FOR UPDATE`, row).Scan(&mid, &vendor, &coefficient); err != nil {
				return nil, err
			}
			return &supply.ResourceCreationContext{Proof: &supply.SubmissionResourceProof{RowID: row, RowVersion: 2, TargetVersion: 3, SupplierID: "21", StandardModelID: mid, Region: "CN", SupplierModelID: vendor, Coefficient: coefficient, BuyerID: "7", FlowID: "81", FlowRevision: 4, AuthorizedActorID: "8", ApprovedAndAssigned: true, EnteredHandling: true}}, nil
		},
		AttachRow: func(c context.Context, current *sql.Tx, e supply.ResourceCreationEffect) error {
			_, err := current.ExecContext(c, `UPDATE pg_temp.submission_row SET resource_id=$1 WHERE id=$2`, e.Resource.ID, e.RowID)
			return err
		},
	})
	create := supply.NewResourceCreationService(createWork, func() time.Time { return now })
	first, err := create.CreateFromSubmission(ctx, actor, supply.ResourceCreationCommand{RowID: "71", RowVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := create.CreateFromSubmission(ctx, actor, supply.ResourceCreationCommand{RowID: "72", RowVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]*ReportEvidenceRef{}
	addReport := func(r *supply.Resource, sequence, target uint64, connection, pressure bool) string {
		t.Helper()
		var job, id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_job(resource_id,category,status) VALUES($1,'stress','SUCCEEDED') RETURNING id::text`, r.ID).Scan(&job); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_report(test_job_id,resource_id,connection_passed,stress_passed,connection_ms,rps,tps,cache_hit_tokens,cache_input_tokens) VALUES($1,$2,$3,$4,0,20,100,0,20) RETURNING id::text`, job, r.ID, connection, pressure).Scan(&id); err != nil {
			t.Fatal(err)
		}
		refs[id] = &ReportEvidenceRef{ReportID: id, JobID: job, ResourceID: r.ID, SupplierID: r.SupplierID, StandardModelID: r.StandardModelID, Region: r.Region, OriginRowID: r.OriginRowID, OriginRowVersion: r.OriginRowVersion, TargetVersion: target, Sequence: sequence, Valid: true}
		return id
	}
	trusted := true
	authority := func(c context.Context, current *sql.Tx, r *supply.Resource, report string) (*ReportEvidenceRef, bool, error) {
		if current != tx {
			t.Fatal("evidence authority escaped transaction")
		}
		ref := refs[report]
		if ref == nil {
			return nil, trusted, supply.ErrNotFound
		}
		copy := *ref
		return &copy, trusted, nil
	}
	evidence := supply.NewTestEvidenceService(NewTestEvidenceWork(state, resolve, authority), func() time.Time { return now })
	failed := addReport(first.Resource, 1, 3, true, false)
	if got, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: failed}); err != nil || got == nil || got.Resource.State != "PENDING_TEST" || got.Resource.Revision != 2 {
		t.Fatalf("actual evidence adapter unavailable: %+v %v", got, err)
	}
	peerFailed := addReport(second.Resource, 1, 3, true, false)
	if _, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: second.Resource.ID, ReportID: peerFailed}); err != nil {
		t.Fatal(err)
	}
	passed := addReport(first.Resource, 3, 3, true, true)
	selected, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: passed})
	if err != nil || selected.Resource.State != "PENDING_ACCESS" || selected.Resource.Revision != 3 {
		t.Fatal("own successful tests did not prepare resource", err)
	}
	older := addReport(first.Resource, 2, 3, true, true)
	stale := addReport(first.Resource, 100, 2, true, true)
	for _, tc := range []struct{ id, outcome string }{{passed, "NOOP"}, {older, "IGNORED_OLDER"}, {stale, "IGNORED_STALE_TARGET"}} {
		got, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: tc.id})
		if err != nil || got.Changed || got.Outcome != tc.outcome || got.Resource.Revision != 3 || got.Resource.LatestReportID != passed {
			t.Fatalf("late report overwrote selected evidence %+v %v", got, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT evidence_failure"); err != nil {
		t.Fatal(err)
	}
	failedWrite := addReport(first.Resource, 4, 3, true, true)
	dropLog = true
	if got, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: failedWrite}); !errors.Is(err, supply.ErrReadContract) || got != nil {
		t.Fatal("evidence without required log accepted", err)
	}
	dropLog = false
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT evidence_failure"); err != nil {
		t.Fatal(err)
	}
	var reportRemains int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.test_report WHERE id=$1`, failedWrite).Scan(&reportRemains); err != nil || reportRemains != 0 {
		t.Fatal("B report insert escaped outer evidence rollback", err)
	}
	if r, err := state.Current(ctx, tx, first.Resource.ID); err != nil || r.LatestReportID != passed || r.Revision != 3 {
		t.Fatal("evidence state escaped outer rollback", err)
	}
	trusted = false
	if _, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: passed}); !errors.Is(err, supply.ErrForbidden) {
		t.Fatal("untrusted B caller accepted", err)
	}
	trusted = true
	future := addReport(first.Resource, 4, 4, true, true)
	if _, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: first.Resource.ID, ReportID: future}); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatal("future target accepted", err)
	}
	configService := supply.NewFormalConfigService(NewFormalConfigWork(configs, resolve), configPGURL{}, func() time.Time { return now })
	secret := "formal-chain-test-secret"
	input := supply.FormalConfigInput{Scope: "全部模型", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true}
	config, err := configService.Create(ctx, buyer, "21", input)
	if err != nil {
		t.Fatal(err)
	}
	prerequisites := true
	proof := func(c context.Context, current *sql.Tx, r *supply.Resource) (*supply.AdmissionProof, error) {
		if current != tx {
			t.Fatal("B proof escaped transaction")
		}
		var assignee, nodeStatus, flowStatus string
		if err := current.QueryRowContext(c, `SELECT n.assignee_id::text,n.status,f.status FROM pg_temp.onboarding_node n JOIN pg_temp.onboarding_flow f ON f.id=n.flow_id WHERE n.id=82 AND f.id=81 AND f.kind='SUPPLIER' AND f.owner_id=$1 FOR UPDATE OF n,f`, r.SupplierID).Scan(&assignee, &nodeStatus, &flowStatus); err != nil {
			return nil, err
		}
		return &supply.AdmissionProof{FlowID: "81", NodeID: "82", FlowRevision: 4, ResourceID: r.ID, SupplierID: "21", Region: "CN", OriginRowID: r.OriginRowID, OriginRowVersion: 2, TargetVersion: 3, AssigneeID: assignee, FlowInProgress: flowStatus == "PROCESSING", NodeInProgress: nodeStatus == "IN_PROGRESS", PrerequisitesReady: prerequisites}, nil
	}
	binding := supply.NewResourceConfigBindingService(NewResourceBindingWork(state, configs, resolve, proof), func() time.Time { return now })
	bound, err := binding.BindFormalConfig(ctx, actor, supply.ResourceConfigBindingCommand{ResourceID: first.Resource.ID, ConfigID: config.ID, ExpectedRevision: 3, ExpectedConfigRevision: 1})
	if err != nil || bound.Resource.Revision != 4 {
		t.Fatal("binding failed", err)
	}
	readRef := func(c context.Context, current *sql.Tx, r *supply.Resource) (*ReportEvidenceRef, error) {
		ref, _, err := authority(c, current, r, r.LatestReportID)
		return ref, err
	}
	repo, err := NewResourceRepository("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, state.FactsLoader(readRef, configs.ResourceView))
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewResourceReader(repo)
	admission := supply.NewResourceAdmissionService(NewResourceAdmissionWork(state, configs, resolve, ResourceAdmissionHooks{
		Proof: proof, Report: readRef,
		CompleteRow: func(c context.Context, current *sql.Tx, e supply.ResourceAdmissionEffect) error {
			if failRow {
				return errors.New("B row completion failed")
			}
			result, err := current.ExecContext(c, `UPDATE pg_temp.submission_row SET row_status='ADMITTED' WHERE id=$1 AND resource_id=$2 AND row_status='PENDING'`, e.After.OriginRowID, e.After.ID)
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
			if current != tx {
				t.Fatal("outbox escaped transaction")
			}
			if emptyOutbox {
				return "9223372036854775807", nil
			}
			if failOutbox {
				e.EventType = strings.Repeat("x", 49)
			}
			if wrongOutbox {
				at := strings.Index(string(e.Payload), `"contract":"`)
				if at < 0 {
					t.Fatal("missing event contract")
				}
				e.Payload[at+len(`"contract":"`)] = 'x'
			}
			var id string
			err := current.QueryRowContext(c, `INSERT INTO pg_temp.event_outbox(event_type,payload,request_id,created_by) VALUES($1,$2::jsonb,$3,$4) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID).Scan(&id)
			return id, err
		},
	}), func() time.Time { return now })
	command := supply.ResourceAdmissionCommand{ResourceID: first.Resource.ID, ExpectedRevision: 4, ExternalConfirmed: true, CompletionNote: "本模型已在外部平台实际配置，逐条登记完成"}
	for _, failure := range []string{"wrong_actor", "stale_revision", "not_ready", "own_report_failed", "stale_config", "log_failure", "redacted_completion", "B_row_failure", "outbox_sql", "empty_outbox", "wrong_outbox", "outer_rollback"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT admit_failure"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				dropLog = false
				failRow = false
				failOutbox = false
				emptyOutbox = false
				wrongOutbox = false
				stripCompletion = false
				prerequisites = true
				if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT admit_failure"); err != nil {
					t.Fatal(err)
				}
			}()
			meta := actor
			cmd := command
			switch failure {
			case "wrong_actor":
				meta.ActorID = "7"
			case "stale_revision":
				cmd.ExpectedRevision = 3
			case "not_ready":
				prerequisites = false
			case "own_report_failed":
				if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.test_report SET stress_passed=false WHERE id=$1`, passed); err != nil {
					t.Fatal(err)
				}
			case "stale_config":
				update := input
				update.APIKey = nil
				update.Scope = "新确认范围"
				if _, err := configService.Update(ctx, buyer, "21", config.ID, 1, update); err != nil {
					t.Fatal(err)
				}
			case "log_failure":
				dropLog = true
			case "B_row_failure":
				failRow = true
			case "outbox_sql":
				failOutbox = true
			case "empty_outbox":
				emptyOutbox = true
			case "wrong_outbox":
				wrongOutbox = true
			case "redacted_completion":
				stripCompletion = true
			}
			got, err := admission.Admit(ctx, meta, cmd)
			if failure == "outer_rollback" {
				if err != nil || got == nil || got.State != "ADMITTED" {
					t.Fatalf("actual admission adapter unavailable: %+v %v", got, err)
				}
			} else if err == nil || got != nil {
				t.Fatal("failed admission returned success")
			}
			if failure == "wrong_actor" && !errors.Is(err, supply.ErrForbidden) {
				t.Fatal("buyer substituted for node assignee", err)
			}
			if (failure == "stale_revision" || failure == "not_ready" || failure == "own_report_failed" || failure == "stale_config") && !errors.Is(err, supply.ErrRevisionConflict) {
				t.Fatal("bad readiness accepted", err)
			}
			if (failure == "log_failure" || failure == "empty_outbox" || failure == "wrong_outbox" || failure == "redacted_completion") && !errors.Is(err, supply.ErrReadContract) {
				t.Fatal("empty required effect accepted", err)
			}
		})
		current, err := state.Current(ctx, tx, first.Resource.ID)
		if err != nil || current.State != "PENDING_ACCESS" || current.Revision != 4 {
			t.Fatal("resource escaped admission rollback", err)
		}
		var rows, events int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pg_temp.submission_row WHERE row_status<>'PENDING'),(SELECT count(*) FROM pg_temp.event_outbox)`).Scan(&rows, &events); err != nil || rows+events != 0 {
			t.Fatal("B row/outbox escaped rollback", err)
		}
	}
	// Another model's successful report cannot admit the untested/failed peer.
	if got, err := admission.Admit(ctx, actor, supply.ResourceAdmissionCommand{ResourceID: second.Resource.ID, ExpectedRevision: 2, ExternalConfirmed: true, CompletionNote: command.CompletionNote}); !errors.Is(err, supply.ErrRevisionConflict) || got != nil {
		t.Fatal("batch result admitted failed peer", err)
	}
	admitted, err := admission.Admit(ctx, actor, command)
	if err != nil || admitted.State != "ADMITTED" || !admitted.EverAdmitted || admitted.Revision != 5 {
		t.Fatal("valid own-resource admission failed", err)
	}
	if got, err := admission.Admit(ctx, actor, command); !errors.Is(err, supply.ErrRevisionConflict) || got != nil {
		t.Fatal("duplicate admission succeeded", err)
	}
	view, err := reader.Get(ctx, first.Resource.ID, "CN")
	if err != nil || view.QuoteReadiness.Eligible || !strings.Contains(strings.Join(view.QuoteReadiness.BlockedReasons, ","), "MODEL_NOT_READY") {
		t.Fatal("admission implied model verification", err)
	}
	now = now.Add(time.Second)
	if _, err := models.Verify(ctx, buyer, model.ID, 1, "2026-10-21"); err != nil {
		t.Fatal(err)
	}
	view, err = reader.Get(ctx, first.Resource.ID, "CN")
	if err != nil || !view.QuoteReadiness.Eligible || view.Test.CacheStats.HitTokens != "0" || view.Test.CacheStats.HitPercent != "0.00" {
		t.Fatalf("complete chain not quote eligible: %+v %v", view, err)
	}
	var peer, node, flow string
	var established bool
	var eventCount int
	var eventRaw, logRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT row_status FROM pg_temp.submission_row WHERE id=72),(SELECT status FROM pg_temp.onboarding_node WHERE id=82),(SELECT status FROM pg_temp.onboarding_flow WHERE id=81),(SELECT established FROM pg_temp.supplier WHERE id=21),(SELECT count(*) FROM pg_temp.event_outbox)`).Scan(&peer, &node, &flow, &established, &eventCount); err != nil || peer != "PENDING" || node != "IN_PROGRESS" || flow != "PROCESSING" || established || eventCount != 1 {
		t.Fatal("single row completed batch or duplicated outbox", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM pg_temp.event_outbox`).Scan(&eventRaw); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT "after" FROM pg_temp.operation_log WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION' ORDER BY id DESC LIMIT 1`, first.Resource.ID).Scan(&logRaw); err != nil {
		t.Fatal(err)
	}
	var event map[string]json.RawMessage
	if err := json.Unmarshal(eventRaw, &event); err != nil || string(event["resource_revision"]) != `"5"` || !strings.Contains(string(logRaw), command.CompletionNote) {
		t.Fatal("event/receipt omitted completion or string revision", err)
	}
	for _, bad := range []string{secret, "formal.example", "api_key_enc"} {
		if strings.Contains(string(logRaw), bad) || strings.Contains(string(eventRaw), bad) {
			t.Fatal("credential entered admission log/outbox")
		}
	}
	t.Run("plan_execution", func(t *testing.T) {
		testPlanExecutionWithNativeChain(t, ctx, tx, state, configs, readRef, log, admitted.ID, now.Add(time.Second))
	})
	// Failed retest changes evidence/readiness, never the actual admitted state.
	retest := addReport(admitted, 6, 3, true, false)
	now = now.Add(time.Second)
	changed, err := evidence.AttachTestEvidence(ctx, actor, supply.TestEvidenceCommand{ResourceID: admitted.ID, ReportID: retest})
	if err != nil || changed.Resource.State != "ADMITTED" || !changed.Resource.EverAdmitted {
		t.Fatal("failed retest auto changed actual state", err)
	}
	view, err = reader.Get(ctx, admitted.ID, "CN")
	if err != nil || view.QuoteReadiness.Eligible || !strings.Contains(strings.Join(view.QuoteReadiness.BlockedReasons, ","), "TESTS_NOT_PASSED") {
		t.Fatal("failed retest still allowed quote", err)
	}
}
