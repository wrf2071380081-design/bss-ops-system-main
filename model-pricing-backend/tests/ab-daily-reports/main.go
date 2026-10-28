// Explicit local fixture for A/B report-consumer integration. No Inspector call
// and no approval/handling qualification is fabricated or granted by this tool.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
	tp "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/infra/supplypg"
)

type manifest struct {
	Actor          string   `json:"actor_id"`
	OtherActor     string   `json:"other_actor_id"`
	OtherActorName string   `json:"other_actor_name"`
	Run            string   `json:"run"`
	Model          string   `json:"model_id"`
	Suppliers      []string `json:"supplier_ids"`
	Resources      []string `json:"resource_ids"`
	Submissions    []string `json:"submission_ids"`
	Jobs           []string `json:"job_ids"`
	Reports        []string `json:"report_ids"`
	ReportDir      string   `json:"report_dir"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" || len(os.Args) != 3 {
		panic("explicit model_bss opt-in and setup|cleanup manifest path required")
	}
	cfg, err := config.Load()
	must(err)
	if cfg.Database.DBName != "model_bss" {
		panic("wrong configured database")
	}
	client, err := infradb.New(cfg.Database)
	must(err)
	defer client.Close()
	db, err := client.DB.DB()
	must(err)
	ctx := context.Background()
	var name string
	var v int
	var dirty bool
	must(db.QueryRowContext(ctx, "select current_database(),version,dirty from schema_migrations").Scan(&name, &v, &dirty))
	if name != "model_bss" || v != 42 || dirty {
		panic("expected model_bss 42 clean")
	}
	path := os.Args[2]
	if strings.HasPrefix(os.Args[1], "workbench-") {
		workbench(ctx, db, path, os.Args[1])
		return
	}
	if os.Args[1] == "hide-file" || os.Args[1] == "restore-file" {
		raw, err := os.ReadFile(path)
		must(err)
		var m manifest
		must(json.Unmarshal(raw, &m))
		dir := cfg.TestPlatform.ReportDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join("/go", dir)
		}
		if !strings.HasPrefix(m.Run, "QA-AB-") || len(m.Reports) == 0 || m.ReportDir != dir {
			panic("unexpected report fixture")
		}
		id := m.Reports[len(m.Reports)-1]
		n, err := strconv.ParseInt(id, 10, 64)
		must(err)
		if n <= 0 {
			panic("invalid report")
		}
		original := filepath.Join(dir, id+".json")
		hidden := original + ".qa-missing"
		if os.Args[1] == "hide-file" {
			must(os.Rename(original, hidden))
		} else {
			must(os.Rename(hidden, original))
		}
		fmt.Println("fixture report file toggled")
		return
	}
	if os.Args[1] == "cleanup" {
		cleanup(ctx, db, path)
		return
	}
	if os.Args[1] != "setup" && os.Args[1] != "setup-workbench" {
		panic("unknown action")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		panic("manifest exists: cleanup prior run first")
	}
	run := "QA-AB-" + time.Now().UTC().Format("20060102-150405")
	dir := cfg.TestPlatform.ReportDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join("/go", dir)
	}
	m := manifest{Run: run, ReportDir: dir}
	tx, err := db.BeginTx(ctx, nil)
	must(err)
	defer tx.Rollback()
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	var actor string
	must(tx.QueryRowContext(ctx, "select owner_id::text from account where login_id='smoke_admin' and portal_type='INTERNAL'").Scan(&actor))
	m.Actor = actor
	must(tx.QueryRowContext(ctx, "select owner_id::text from account where login_id='buyer_a' and portal_type='INTERNAL'").Scan(&m.OtherActor))
	must(tx.QueryRowContext(ctx, "select name from internal_staff where id=$1", m.OtherActor).Scan(&m.OtherActorName))
	meta := supply.WriteMeta{ActorID: actor, RequestID: run, Reason: "A/B报告读取联调夹具，不代表真实审批或Inspector测试"}
	work, err := supplypg.NewModelUnitOfWork("public", resolve, supplypg.AppendModelOperationLog)
	must(err)
	model, err := supply.NewModelService(work, nil, time.Now).Create(ctx, meta, supply.ModelData{Name: run, Company: "QA-AB", OfficialID: strings.ToLower(run), ModelType: "TEXT"})
	must(err)
	m.Model = model.ID
	logfn := func(c context.Context, t *sql.Tx, e supplypg.ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := t.ExecContext(c, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	suppliers, err := supplypg.NewSupplierStore("public", db, resolve, logfn)
	must(err)
	state, err := supplypg.NewResourceStateStore("public", logfn)
	must(err)
	for i := 0; i < 2; i++ {
		supplier, err := supply.NewSupplierService(suppliers, time.Now).Create(ctx, meta, supply.SupplierData{Name: fmt.Sprintf("%s-%d", run, i), Region: "CN", SettlementCurrency: "CNY"})
		must(err)
		m.Suppliers = append(m.Suppliers, supplier.ID)
		var submission, row string
		must(tx.QueryRowContext(ctx, `INSERT INTO submission(supplier_id,kind,region,status,reason,submitted_by,submitted_at) VALUES($1,'MODEL_NEW','CN','DRAFT',$2,$3,NULL) RETURNING id::text`, supplier.ID, run, actor).Scan(&submission))
		must(tx.QueryRowContext(ctx, `INSERT INTO submission_row(submission_id,standard_model_id,supplier_model_name,coefficient,test_fingerprint) VALUES($1,$2,$3,0.5,$4) RETURNING id::text`, submission, model.ID, run, run+"-target").Scan(&row))
		m.Submissions = append(m.Submissions, submission)
		id, err := state.NextID(ctx, tx)
		must(err)
		now := time.Now().UTC()
		_, err = state.Insert(ctx, tx, &supply.Resource{ID: id, SupplierID: supplier.ID, StandardModelID: model.ID, Region: "CN", SupplierModelID: run, BuyerID: actor, Coefficient: "0.5", State: "PENDING_TEST", Revision: 1, OriginRowID: row, OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}, meta, "CREATE")
		must(err)
		_, err = tx.ExecContext(ctx, "update submission_row set resource_id=$1 where id=$2", id, row)
		must(err)
		m.Resources = append(m.Resources, id)
		if os.Args[1] == "setup-workbench" {
			r, err := state.Current(ctx, tx, id)
			must(err)
			r.Revision++
			r.EverAdmitted = true
			r.State = "OFFLINE"
			r.UpdatedAt = time.Now().UTC()
			_, err = state.Update(ctx, tx, r, 1, meta, "QA_FIXTURE")
			must(err)
		}
	}
	must(tx.Commit())
	save(path, m) // Preserve exact base IDs even if report construction later fails.
	if os.Args[1] == "setup-workbench" {
		fmt.Println("workbench fixture ready: two OFFLINE QA resources, no test evidence or quote eligibility")
		return
	}
	store, err := supplypg.NewTestPlatformStore(db, "public", dir)
	must(err)
	for i := 0; i < 12; i++ {
		status := "QUEUED"
		if i == 1 {
			status = "RUNNING"
		}
		// 每个任务给不同的目标指纹：库里现在有「同一资源+类别+受测目标只能有一个活跃任务」
		// 的唯一索引（000047），共用指纹会让第 2 条起被判定为重复而拿不到新任务。
		job, created, err := store.CreateJob(ctx, tp.NewJobRecord{ResourceID: m.Resources[0], Category: tp.CategoryStress, ActorID: actor, Status: status, IdempotencyKey: fmt.Sprintf("%s-job-%d", run, i), TargetFingerprint: fmt.Sprintf("%s-target-%d", run, i)})
		must(err)
		if !created {
			panic(fmt.Sprintf("fixture job %d unexpectedly deduplicated", i))
		}
		m.Jobs = append(m.Jobs, job)
		save(path, m)
		if i < 2 {
			continue
		}
		final := "SUCCEEDED"
		if i == 2 {
			final = "FAILED"
		}
		zero, rps, tps := "0.00", "0.00", "64.00"
		outcome := tp.Outcome{PressurePassed: true, RPS: &rps, TPS: &tps, Scene: run}
		if i == 4 {
			outcome.CacheRatioProvided = true
			outcome.CacheRatioPercent = &zero
		}
		if i == 5 {
			ratio := "37.25"
			outcome.CacheRatioProvided = true
			outcome.CacheRatioPercent = &ratio
		}
		if i == 7 {
			var hit int64
			input := int64(100)
			outcome.CacheHitTokens = &hit
			outcome.CacheInputTokens = &input
		}
		body, _ := json.Marshal(map[string]any{"fixture": run, "item": i, "purpose": "A/B report consumer integration, no Inspector invocation"})
		report, err := store.CompleteJob(ctx, tp.Completion{JobID: job, ResourceID: m.Resources[0], Category: tp.CategoryStress, FinalStatus: final, Verdict: "pass", Outcome: outcome, TestedBy: actor, ReportJSON: body, FinishedAt: time.Now().UTC()})
		must(err)
		if report != "" {
			m.Reports = append(m.Reports, report)
		}
		save(path, m)
	}
	fmt.Printf("fixture ready: resources=%d jobs=%d reports=%d (local B Store, no Inspector)\n", len(m.Resources), len(m.Jobs), len(m.Reports))
}
func save(path string, m manifest) {
	must(os.MkdirAll(filepath.Dir(path), 0700))
	raw, err := json.MarshalIndent(m, "", "  ")
	must(err)
	must(os.WriteFile(path, raw, 0600))
}
func cleanup(ctx context.Context, db *sql.DB, path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var m manifest
	must(json.Unmarshal(raw, &m))
	if !strings.HasPrefix(m.Run, "QA-AB-") || m.Model == "" || len(m.Resources) != 2 || len(m.Suppliers) != 2 {
		panic("invalid exact-ID fixture manifest")
	}
	tx, err := db.BeginTx(ctx, nil)
	must(err)
	defer tx.Rollback()
	var modelName string
	must(tx.QueryRowContext(ctx, "select name from standard_model where id=$1", m.Model).Scan(&modelName))
	if modelName != m.Run {
		panic("fixture identity changed")
	}
	for _, id := range m.Resources {
		_, err = tx.ExecContext(ctx, "delete from notice_read where notice_id in(select id from notice where target_type='RESOURCE' and target_id=$1)", id)
		must(err)
		_, err = tx.ExecContext(ctx, "delete from notice where target_type='RESOURCE' and target_id=$1", id)
		must(err)
		_, err = tx.ExecContext(ctx, "delete from operation_log where entity_type='plan' and entity_id in(select id from plan where resource_id=$1)", id)
		must(err)
		_, err = tx.ExecContext(ctx, "delete from plan where resource_id=$1", id)
		must(err)
	}
	_, err = tx.ExecContext(ctx, "delete from event_outbox where request_id like $1", m.Run+"%")
	must(err)
	for _, id := range m.Submissions {
		_, err = tx.ExecContext(ctx, "delete from submission where id=$1 and reason=$2", id, m.Run)
		must(err)
	}
	for _, id := range m.Resources {
		_, err = tx.ExecContext(ctx, "delete from resource where id=$1 and standard_model_id=$2", id, m.Model)
		must(err)
	}
	_, err = tx.ExecContext(ctx, "delete from operation_log where request_id=$1", m.Run)
	must(err)
	for _, id := range m.Suppliers {
		_, err = tx.ExecContext(ctx, "delete from assignment_log where entity_type='supplier' and entity_id=$1", id)
		must(err)
		_, err = tx.ExecContext(ctx, "delete from supplier where id=$1 and name like $2", id, m.Run+"-%")
		must(err)
	}
	_, err = tx.ExecContext(ctx, "delete from standard_model where id=$1 and name=$2", m.Model, m.Run)
	must(err)
	must(tx.Commit())
	for _, id := range m.Reports {
		n, err := strconv.ParseInt(id, 10, 64)
		must(err)
		if n <= 0 {
			panic("invalid report ID")
		}
		for _, ext := range []string{".json", ".md"} {
			err = os.Remove(filepath.Join(m.ReportDir, id+ext))
			if err != nil && !os.IsNotExist(err) {
				must(err)
			}
		}
	}
	must(os.Rename(path, path+".cleaned"))
	fmt.Println("exact fixture business rows and report files removed")
}

func workbench(ctx context.Context, db *sql.DB, path, action string) {
	raw, err := os.ReadFile(path)
	must(err)
	var m manifest
	must(json.Unmarshal(raw, &m))
	if !strings.HasPrefix(m.Run, "QA-AB-") || len(m.Resources) != 2 {
		panic("unexpected workbench fixture")
	}
	var name string
	must(db.QueryRowContext(ctx, "select name from standard_model where id=$1", m.Model).Scan(&name))
	if name != m.Run {
		panic("fixture identity changed")
	}
	state, err := supplypg.NewResourceStateStore("public", nil)
	must(err)
	switch action {
	case "workbench-snapshot":
		var resources, plans string
		var logs, notices, outbox int
		must(db.QueryRowContext(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.id)::text,'')) FROM resource r WHERE id::text=ANY($1::text[])`, m.Resources).Scan(&resources))
		must(db.QueryRowContext(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(p) ORDER BY p.id)::text,'')) FROM plan p WHERE resource_id::text=ANY($1::text[])`, m.Resources).Scan(&plans))
		must(db.QueryRowContext(ctx, `SELECT count(*) FROM operation_log WHERE (entity_type='resource' AND entity_id::text=ANY($1::text[])) OR (entity_type='plan' AND entity_id IN(SELECT id FROM plan WHERE resource_id::text=ANY($1::text[])))`, m.Resources).Scan(&logs))
		must(db.QueryRowContext(ctx, `SELECT count(*) FROM notice WHERE target_type='RESOURCE' AND target_id::text=ANY($1::text[])`, m.Resources).Scan(&notices))
		must(db.QueryRowContext(ctx, `SELECT count(*) FROM event_outbox WHERE request_id LIKE $1`, m.Run+"%").Scan(&outbox))
		b, _ := json.Marshal(map[string]any{"resources": resources, "plans": plans, "logs": logs, "notices": notices, "outbox": outbox})
		fmt.Println(string(b))
	case "workbench-remind":
		var others int
		at := time.Now().UTC().Add(2 * time.Hour)
		must(db.QueryRowContext(ctx, `SELECT count(*) FROM plan WHERE status='PENDING' AND due_at<=$1 AND NOT(resource_id::text=ANY($2::text[]))`, at, m.Resources).Scan(&others))
		if others != 0 {
			panic("other due plans exist: refusing global worker probe")
		}
		plans, err := supplypg.NewPlanStore("public", supplypg.ModelReadTransactions(db), state, nil)
		must(err)
		worker, err := supplypg.NewPlanReminderStore(db, plans)
		must(err)
		n, err := worker.PublishDuePlanReminders(ctx, at, 50)
		must(err)
		fmt.Printf("{\"published\":%d}\n", n)
	case "workbench-transfer", "workbench-transfer-second":
		i := 0
		if action == "workbench-transfer-second" {
			i = 1
		}
		tx, err := db.BeginTx(ctx, nil)
		must(err)
		defer tx.Rollback()
		logger := func(c context.Context, t *sql.Tx, e supplypg.ResourceStateLog) error {
			_, err := t.ExecContext(c, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, string(e.Before), string(e.After), e.Reason, e.RequestID, e.OccurredAt)
			return err
		}
		writer, err := supplypg.NewResourceStateStore("public", logger)
		must(err)
		r, err := writer.ForUpdate(ctx, tx, m.Resources[i])
		must(err)
		old := r.Revision
		r.Revision++
		r.BuyerID = m.OtherActor
		r.UpdatedAt = time.Now().UTC()
		_, err = writer.Update(ctx, tx, r, old, supply.WriteMeta{ActorID: m.Actor, RequestID: m.Run, Reason: "QA ownership transition for old-plan authorization"}, "QA_TRANSFER")
		must(err)
		must(tx.Commit())
		fmt.Println("{\"transferred\":true}")
	case "workbench-fail-on", "workbench-fail-off":
		id, err := strconv.ParseInt(m.Resources[1], 10, 64)
		must(err)
		model, err := strconv.ParseInt(m.Model, 10, 64)
		must(err)
		fn := fmt.Sprintf("qa_a_notice_fail_m%d", model)
		if action == "workbench-fail-on" {
			_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION public.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.target_type='RESOURCE' AND NEW.target_id=%d THEN RAISE EXCEPTION 'QA_WORKBENCH_NOTICE_FAILURE'; END IF; RETURN NEW; END; $$; CREATE TRIGGER %s BEFORE INSERT ON public.notice FOR EACH ROW EXECUTE FUNCTION public.%s()`, fn, id, fn, fn))
			must(err)
		} else {
			_, err = db.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON public.notice; DROP FUNCTION IF EXISTS public.%s()`, fn, fn))
			must(err)
		}
		fmt.Println("{\"fault_changed\":true}")
	default:
		panic("unknown workbench action")
	}
}
