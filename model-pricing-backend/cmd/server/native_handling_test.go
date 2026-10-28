package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"model_bss/internal/api"
	"model_bss/internal/domain/supply"
	tp "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"model_bss/internal/infra/supplyfiles"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/repo"
	"os"
	"strconv"
	"testing"
	"time"
)

type nativeFixture struct {
	SupplierLogin, SupplierPhone                             string
	Run, Model, Supplier, Resource, Leader, Config, PriceRow string
	LeaderLogin, SourceRow                                   string
	Submissions                                              []string
	ExtraModels                                              []string
}

func cleanupNativeFixture(ctx context.Context, db *sql.DB, f nativeFixture) error {
	if len(f.Run) < 10 || f.Run[:10] != "QA-NATIVE-" || f.Model == "" || f.Supplier == "" {
		return fmt.Errorf("invalid native fixture")
	}
	if f.Resource == "" {
		_ = db.QueryRowContext(ctx, `SELECT id::text FROM resource WHERE supplier_id=$1 ORDER BY id LIMIT 1`, f.Supplier).Scan(&f.Resource)
		if f.Resource == "" {
			f.Resource = "0"
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM notice_read WHERE notice_id IN(SELECT id FROM notice WHERE target_type='RESOURCE' AND target_id=$1)`, []any{f.Resource}},
		{`DELETE FROM notice WHERE target_type='RESOURCE' AND target_id=$1`, []any{f.Resource}},
		{`DELETE FROM event_outbox WHERE request_id LIKE $1 OR payload->>'resource_id'=$2`, []any{f.Run + "%", f.Resource}},
		{`DELETE FROM operation_log WHERE entity_type='plan' AND entity_id IN(SELECT id FROM plan WHERE resource_id=$1)`, []any{f.Resource}},
		{`DELETE FROM plan WHERE resource_id=$1`, []any{f.Resource}},
		{`DELETE FROM operation_log WHERE (entity_type='contract' AND entity_id IN(SELECT id FROM contract WHERE subject_type='SUPPLIER' AND subject_id=$1)) OR (entity_type='credit_line' AND entity_id IN(SELECT id FROM credit_line WHERE subject_type='SUPPLIER' AND subject_id=$1)) OR (entity_type='supplier_qualification' AND entity_id IN(SELECT id FROM supplier_qualification WHERE supplier_id=$1)) OR (entity_type='onboarding_node' AND entity_id IN(SELECT n.id FROM onboarding_node n JOIN onboarding_flow f ON f.id=n.flow_id WHERE f.kind='SUPPLIER' AND f.owner_id=$1))`, []any{f.Supplier}},
		{`DELETE FROM contract WHERE subject_type='SUPPLIER' AND subject_id=$1`, []any{f.Supplier}},
		{`DELETE FROM credit_line WHERE subject_type='SUPPLIER' AND subject_id=$1`, []any{f.Supplier}},
		{`DELETE FROM operation_log WHERE (entity_type='submission' AND entity_id IN(SELECT id FROM submission WHERE supplier_id=$1)) OR (entity_type='resource' AND entity_id IN(SELECT id FROM resource WHERE supplier_id=$1)) OR (entity_type='supplier_formal_config' AND entity_id IN(SELECT id FROM supplier_formal_config WHERE supplier_id=$1)) OR (entity_type='supplier' AND entity_id=$1) OR (entity_type='standard_model' AND entity_id=$2)`, []any{f.Supplier, f.Model}},
		{`DELETE FROM submission WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM onboarding_node WHERE flow_id IN(SELECT id FROM onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1)`, []any{f.Supplier}},
		{`DELETE FROM onboarding_flow WHERE kind='SUPPLIER' AND owner_id=$1`, []any{f.Supplier}},
		{`UPDATE resource SET latest_test_report_id=NULL WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM test_report WHERE resource_id IN(SELECT id FROM resource WHERE supplier_id=$1)`, []any{f.Supplier}},
		{`DELETE FROM test_job WHERE resource_id IN(SELECT id FROM resource WHERE supplier_id=$1)`, []any{f.Supplier}},
		{`DELETE FROM resource WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM supplier_formal_config WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM login_session WHERE account_id IN(SELECT id FROM account WHERE owner_type='SUPPLIER' AND owner_id=$1 AND login_id=$2)`, []any{f.Supplier, f.SupplierLogin}},
		{`DELETE FROM account WHERE owner_type='SUPPLIER' AND owner_id=$1 AND login_id=$2`, []any{f.Supplier, f.SupplierLogin}},
		{`DELETE FROM assignment_log WHERE entity_type='supplier' AND entity_id=$1`, []any{f.Supplier}},
		{`DELETE FROM supplier_alias WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM operation_log WHERE entity_type='supplier_qualification' AND entity_id IN(SELECT id FROM supplier_qualification WHERE supplier_id=$1)`, []any{f.Supplier}},
		{`DELETE FROM supplier_qualification WHERE supplier_id=$1`, []any{f.Supplier}},
		{`DELETE FROM supplier WHERE id=$1 AND name=$2`, []any{f.Supplier, f.Run}},
		{`DELETE FROM standard_model WHERE id=$1 AND name=$2`, []any{f.Model, f.Run}},
		{`DELETE FROM login_session WHERE account_id IN(SELECT id FROM account WHERE owner_type='STAFF' AND owner_id=$1 AND login_id=$2)`, []any{f.Leader, f.LeaderLogin}},
		{`DELETE FROM account WHERE owner_type='STAFF' AND owner_id=$1 AND login_id=$2`, []any{f.Leader, f.LeaderLogin}},
		{`DELETE FROM internal_staff WHERE id=$1 AND name=$2`, []any{f.Leader, f.Run}},
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return err
		}
	}
	for _, id := range f.ExtraModels {
		if _, err := tx.ExecContext(ctx, `DELETE FROM operation_log WHERE entity_type='standard_model' AND entity_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM standard_model WHERE id=$1 AND name LIKE $2`, id, f.Run+" extra%"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Actual B Approve + B report persistence + native providers + A repositories.
// The only substituted boundary is external Inspector output: no network call.
func TestNativeHandlingDaily42Success(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("daily config required")
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	db, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("BSS_NATIVE_MODE") == "evidence" || os.Getenv("BSS_NATIVE_MODE") == "supplier-evidence" {
		path := os.Getenv("BSS_NATIVE_MANIFEST")
		raw, err := os.ReadFile(path)
		must(err)
		var f nativeFixture
		must(json.Unmarshal(raw, &f))
		if len(f.Run) < 10 || f.Run[:10] != "QA-NATIVE-" || f.SourceRow == "" {
			t.Fatal("explicit native approval fixture required")
		}
		if os.Getenv("BSS_NATIVE_MODE") == "supplier-evidence" {
			submissions, err := supplypg.NewSubmissionStore(db, "public")
			must(err)
			row, err := submissions.TestRow(ctx, mustInt(f.Supplier), mustInt(f.Submissions[0]), mustInt(f.SourceRow))
			must(err)
			id, err := submissions.EnsureTestResource(ctx, row)
			must(err)
			f.Resource = fmt.Sprint(id)
		} else {
			must(db.QueryRowContext(ctx, `SELECT resource_id::text FROM submission_row WHERE id=$1`, f.SourceRow).Scan(&f.Resource))
		}
		store, err := supplypg.NewTestPlatformStore(db, "public", t.TempDir())
		must(err)
		var jobs []string
		for _, category := range []tp.Category{tp.CategoryBaseline, tp.CategoryStress} {
			var fp string
			must(db.QueryRowContext(ctx, `SELECT test_fingerprint FROM submission_row WHERE id=$1`, f.SourceRow).Scan(&fp))
			if fp == "" {
				t.Fatal("actual supplier target fingerprint required")
			}
			job, err := store.CreateJob(ctx, tp.NewJobRecord{ResourceID: f.Resource, Category: category, ActorID: "1", TargetFingerprint: fp, IdempotencyKey: f.Run + string(category)})
			must(err)
			jobs = append(jobs, job)
			zero, rps, tps := "0.00", "12.00", "34.00"
			_, err = store.CompleteJob(ctx, tp.Completion{JobID: job, ResourceID: f.Resource, Category: category, FinalStatus: "SUCCEEDED", Verdict: "pass", Outcome: tp.Outcome{ConnectivityPassed: category == tp.CategoryBaseline, PressurePassed: category == tp.CategoryStress, ConnectionMS: &zero, RPS: &rps, TPS: &tps}, TestedBy: "1", ReportJSON: json.RawMessage(`{"purpose":"QA report output; no Inspector call"}`), FinishedAt: time.Now().UTC()})
			must(err)
		}
		_, err = db.ExecContext(ctx, `UPDATE submission_row SET test_job_id=$2,stress_job_id=$3 WHERE id=$1`, f.SourceRow, jobs[0], jobs[1])
		must(err)
		if os.Getenv("BSS_NATIVE_MODE") == "supplier-evidence" {
			raw, _ = json.Marshal(f)
			must(os.WriteFile(path, raw, 0600))
			return
		}
		deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
		must(err)
		key := "QA formal key"
		must(client.DB.Transaction(func(tx *gorm.DB) error {
			v, err := deps.FormalConfigs.Create(infradb.WithDB(ctx, tx), supply.WriteMeta{ActorID: "3", Reason: f.Run, RequestID: f.Run + "-config"}, f.Supplier, supply.FormalConfigInput{Scope: f.Run, BaseURL: "https://api.openai.com/v1", APIKey: &key, ExternalConfirmed: true})
			if err == nil {
				f.Config = v.ID
			}
			return err
		}))
		raw, _ = json.Marshal(f)
		must(os.WriteFile(path, raw, 0600))
		return
	}
	if mode := os.Getenv("BSS_NATIVE_MODE"); mode == "cleanup" || mode == "prepare-price" {
		raw, err := os.ReadFile(os.Getenv("BSS_NATIVE_MANIFEST"))
		must(err)
		var f nativeFixture
		must(json.Unmarshal(raw, &f))
		if mode == "cleanup" {
			refs := []string{}
			rows, err := db.QueryContext(ctx, `SELECT file_ref FROM supplier_qualification WHERE supplier_id=$1 AND file_ref IS NOT NULL`, f.Supplier)
			must(err)
			for rows.Next() {
				var ref string
				must(rows.Scan(&ref))
				refs = append(refs, ref)
			}
			must(rows.Err())
			rows.Close()
			must(cleanupNativeFixture(ctx, db, f))
			if root := os.Getenv("MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR"); root != "" {
				files, err := supplyfiles.NewQualificationFileStore(root)
				must(err)
				for _, ref := range refs {
					must(files.Delete(ctx, ref))
				}
			}
			if root := os.Getenv("MODEL_BSS_F12_ATTACHMENT_STORE_DIR"); root != "" {
				files, err := supplyfiles.NewF12AttachmentFileStore(root)
				must(err)
				items, err := files.ListBefore(ctx, time.Now().Add(time.Minute))
				must(err)
				for _, item := range items {
					// ListBefore intentionally returns opaque IDs only, including
					// crash orphans. Open the manifest before filtering our fixture.
					file, err := files.Open(ctx, item.ID)
					if err != nil {
						continue
					}
					if file.SubjectType == "SUPPLIER" && file.SubjectID == f.Supplier {
						must(files.Delete(ctx, item.ID))
					}
				}
			}
			return
		}
		var id, row string
		must(db.QueryRowContext(ctx, `INSERT INTO submission(supplier_id,kind,region,status,reason) VALUES($1,'PRICE_CHANGE','CN','PENDING_APPROVAL',$2) RETURNING id::text`, f.Supplier, f.Run).Scan(&id))
		f.Submissions = append(f.Submissions, id)
		fp := tp.TargetFingerprint(mustInt(f.Model), "qa-native-model", "https://supplier.example/api", "QA fixture")
		must(db.QueryRowContext(ctx, `INSERT INTO submission_row(submission_id,standard_model_id,supplier_model_name,coefficient,test_fingerprint) VALUES($1,$2,'qa-native-model',0.33,$3) RETURNING id::text`, id, f.Model, fp).Scan(&row))
		must(api.NewSupplyChainRuntime(db, nil).Approve(ctx, mustInt(id), mustInt(f.Leader), 1, 1, f.Run))
		f.PriceRow = row
		raw, _ = json.Marshal(f)
		must(os.WriteFile(os.Getenv("BSS_NATIVE_MANIFEST"), raw, 0600))
		return
	}
	deps, err := newSupplyRuntimeDeps(client.DB, repo.NewIdempotencyRepo(client.DB))
	must(err)
	f := nativeFixture{Run: "QA-NATIVE-" + time.Now().Format("20060102-150405.000000")}
	keep := false
	defer func() {
		if !keep && f.Supplier != "" {
			if err := cleanupNativeFixture(ctx, db, f); err != nil {
				t.Errorf("fixture cleanup: %v", err)
			}
		}
	}()
	var serial int
	write := func(actor string, fn func(context.Context, supply.WriteMeta) error) error {
		serial++
		meta := supply.WriteMeta{ActorID: actor, Reason: f.Run, RequestID: fmt.Sprintf("%s-%d", f.Run, serial)}
		return client.DB.Transaction(func(tx *gorm.DB) error { return fn(infradb.WithDB(ctx, tx), meta) })
	}
	must(write("3", func(c context.Context, m supply.WriteMeta) error {
		model, err := deps.Writer.Create(c, m, supply.ModelData{Company: "OpenAI", Name: f.Run, OfficialID: f.Run, ModelType: "TEXT"})
		if err != nil {
			return err
		}
		f.Model = model.ID
		supplier, err := deps.SupplierWriter.Create(c, m, supply.SupplierData{Name: f.Run, Region: "CN", SettlementCurrency: "CNY"})
		if err != nil {
			return err
		}
		f.Supplier = supplier.ID
		return nil
	}))
	must(db.QueryRowContext(ctx, `INSERT INTO internal_staff(org_unit_id,name,mobile,can_approve_supplier) SELECT min(id),$1,$2,true FROM org_unit RETURNING id::text`, f.Run, "QA"+time.Now().Format("150405000000")).Scan(&f.Leader))
	if os.Getenv("BSS_NATIVE_MODE") == "leadership" {
		password := os.Getenv("BSS_TEST_PASSWORD")
		if password == "" {
			t.Fatal("test password required")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		must(err)
		f.LeaderLogin = f.Run
		_, err = db.ExecContext(ctx, `INSERT INTO account(portal_type,owner_type,owner_id,login_id,password_hash) VALUES('INTERNAL','STAFF',$1,$2,$3)`, f.Leader, f.LeaderLogin, string(hash))
		must(err)
		f.SupplierLogin = f.Run + "-supplier"
		f.SupplierPhone = fmt.Sprintf("139%08d", time.Now().UnixNano()%100000000)
		must(write("3", func(c context.Context, m supply.WriteMeta) error {
			p, err := deps.SupplierReader.Get(c, f.Supplier)
			if err != nil {
				return err
			}
			data := p.Data
			data.Phone = f.SupplierPhone
			_, err = deps.SupplierWriter.Update(c, m, p.ID, p.Revision, data)
			return err
		}))
		_, err = db.ExecContext(ctx, `INSERT INTO account(portal_type,owner_type,owner_id,login_id,password_hash) VALUES('SUPPLIER','SUPPLIER',$1,$2,$3)`, f.Supplier, f.SupplierLogin, string(hash))
		must(err)
	}
	approve := api.NewSupplyChainRuntime(db, nil)
	submission := func(kind, amount string) string {
		var id, row string
		must(db.QueryRowContext(ctx, `INSERT INTO submission(supplier_id,kind,region,status,reason) VALUES($1,$2,'CN','PENDING_APPROVAL',$3) RETURNING id::text`, f.Supplier, kind, f.Run).Scan(&id))
		f.Submissions = append(f.Submissions, id)
		fingerprint := tp.TargetFingerprint(mustInt(f.Model), "qa-native-model", "https://supplier.example/api", "QA fixture")
		must(db.QueryRowContext(ctx, `INSERT INTO submission_row(submission_id,standard_model_id,supplier_model_name,coefficient,test_fingerprint) VALUES($1,$2,'qa-native-model',$3,$4) RETURNING id::text`, id, f.Model, amount, fingerprint).Scan(&row))
		if os.Getenv("BSS_NATIVE_MODE") == "leadership" {
			return row
		}
		must(approve.Approve(ctx, mustInt(id), mustInt(f.Leader), 1, 1, f.Run))
		must(db.QueryRowContext(ctx, `SELECT resource_id::text FROM submission_row WHERE id=$1`, row).Scan(&f.Resource))
		return row
	}
	sourceRow := submission("MODEL_NEW", "0.5000")
	f.SourceRow = sourceRow
	if os.Getenv("BSS_NATIVE_MODE") == "leadership" {
		if os.Getenv("BSS_NATIVE_EXTRA_ROWS") == "2" {
			for i := 1; i <= 2; i++ {
				var modelID string
				must(write("3", func(c context.Context, m supply.WriteMeta) error {
					model, err := deps.Writer.Create(c, m, supply.ModelData{Company: "OpenAI", Name: fmt.Sprintf("%s extra%d", f.Run, i), OfficialID: fmt.Sprintf("%s-extra%d", f.Run, i), ModelType: "TEXT"})
					if err == nil {
						modelID = model.ID
						f.ExtraModels = append(f.ExtraModels, modelID)
					}
					return err
				}))
				fp := tp.TargetFingerprint(mustInt(modelID), fmt.Sprintf("qa-extra-%d", i), "https://supplier.example/api", "QA fixture")
				_, err := db.ExecContext(ctx, `INSERT INTO submission_row(submission_id,standard_model_id,supplier_model_name,coefficient,test_fingerprint) VALUES($1,$2,$3,0.5,$4)`, f.Submissions[0], modelID, fmt.Sprintf("qa-extra-%d", i), fp)
				must(err)
			}
		}
		raw, _ := json.Marshal(f)
		must(os.WriteFile(os.Getenv("BSS_NATIVE_MANIFEST"), raw, 0600))
		keep = true
		return
	}
	state, err := supplypg.NewResourceStateStore("public", appendSupplyResourceLog)
	must(err)
	err = write("1", func(c context.Context, m supply.WriteMeta) error {
		tx, err := supplypg.GORMModelTxFromContext(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(c, `UPDATE submission SET revision=revision+1 WHERE id=$1`, f.Submissions[0]); err != nil {
			return err
		}
		resource, err := state.Current(c, tx, f.Resource)
		if err != nil {
			return err
		}
		_, err = (&supplypg.NativeHandling{DB: db, State: state}).Proof(c, tx, resource)
		return err
	})
	if !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("approval source revision drift must be rejected, got %v", err)
	}
	actions, err := deps.ResourceWorkflow.GetResourceWorkflow(ctx, "1", f.Resource)
	must(err)
	if !actions.CanBind || !actions.CanRefreshTests {
		t.Fatal("assigned person cannot operate")
	}
	actions, err = deps.ResourceWorkflow.GetResourceWorkflow(ctx, "3", f.Resource)
	must(err)
	if actions.CanBind || actions.CanAdmit {
		t.Fatal("buyer/admin gained handling authority")
	}
	// Current designated baseline + stress slots are persisted through real B Store.
	store, err := supplypg.NewTestPlatformStore(db, "public", t.TempDir())
	must(err)
	var jobs []string
	for _, category := range []tp.Category{tp.CategoryBaseline, tp.CategoryStress} {
		fp := tp.TargetFingerprint(mustInt(f.Model), "qa-native-model", "https://supplier.example/api", "QA fixture")
		job, err := store.CreateJob(ctx, tp.NewJobRecord{ResourceID: f.Resource, Category: category, ActorID: "1", TargetFingerprint: fp, IdempotencyKey: f.Run + string(category)})
		must(err)
		jobs = append(jobs, job)
		zero, rps, tps := "0.00", "12.00", "34.00"
		outcome := tp.Outcome{ConnectivityPassed: category == tp.CategoryBaseline, PressurePassed: category == tp.CategoryStress, ConnectionMS: &zero, RPS: &rps, TPS: &tps}
		_, err = store.CompleteJob(ctx, tp.Completion{JobID: job, ResourceID: f.Resource, Category: category, FinalStatus: "SUCCEEDED", Verdict: "pass", Outcome: outcome, TestedBy: "1", ReportJSON: json.RawMessage(`{"purpose":"native-provider integration; no Inspector call"}`), FinishedAt: time.Now().UTC()})
		must(err)
	}
	_, err = db.ExecContext(ctx, `UPDATE submission_row SET test_job_id=$2,stress_job_id=$3 WHERE id=$1`, sourceRow, jobs[0], jobs[1])
	must(err)
	snap, err := deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	must(write("1", func(c context.Context, m supply.WriteMeta) error {
		r, err := deps.ResourceEvidence.RefreshEvidence(c, m, f.Resource, snap.Revision)
		if err == nil && r.State != "PENDING_ACCESS" {
			return fmt.Errorf("both reports did not enable access")
		}
		return err
	}))
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	var formal *supply.FormalConfigView
	key := "QA formal key"
	must(write("3", func(c context.Context, m supply.WriteMeta) error {
		var err error
		formal, err = deps.FormalConfigs.Create(c, m, f.Supplier, supply.FormalConfigInput{Scope: f.Run, BaseURL: "https://api.openai.com/v1", APIKey: &key, ExternalConfirmed: true})
		return err
	}))
	f.Config = formal.ID
	if os.Getenv("BSS_NATIVE_MODE") == "browser" {
		raw, _ := json.Marshal(f)
		must(os.WriteFile(os.Getenv("BSS_NATIVE_MANIFEST"), raw, 0600))
		keep = true
		return
	}
	err = write("3", func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.ResourceConfigBinder.BindFormalConfig(c, m, supply.ResourceConfigBindingCommand{ResourceID: f.Resource, ExpectedRevision: snap.Revision, ConfigID: formal.ID, ExpectedConfigRevision: formal.Revision})
		return err
	})
	if !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("buyer binding expected403, got %v", err)
	}
	must(write("1", func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.ResourceConfigBinder.BindFormalConfig(c, m, supply.ResourceConfigBindingCommand{ResourceID: f.Resource, ExpectedRevision: snap.Revision, ConfigID: formal.ID, ExpectedConfigRevision: formal.Revision})
		return err
	}))
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	must(write("1", func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.ResourceAdmitter.Admit(c, m, supply.ResourceAdmissionCommand{ResourceID: f.Resource, ExpectedRevision: snap.Revision, ExternalConfirmed: true, CompletionNote: "QA platform configuration ticket"})
		return err
	}))
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if snap.QuoteReadiness.Eligible {
		t.Fatal("incomplete model became quotable")
	}
	input := supply.PriceItem{ItemKey: "text_input_standard", Label: "输入", TaskType: "TEXT_INPUT", Amount: "2.00000000", Currency: "USD", UnitCode: "TOKEN", UnitQuantity: "1000000", Condition: "标准计费", SourceURL: "https://openai.com/pricing"}
	output := input
	output.ItemKey = "text_output_standard"
	output.TaskType = "TEXT_OUTPUT"
	output.Label = "输出"
	must(write("3", func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.Writer.(api.SupplyManualModelWriter).SaveManual(c, m, f.Model, 1, supply.ModelData{Company: "OpenAI", Name: f.Run, OfficialID: f.Run, ModelType: "TEXT", InputModalities: []string{"TEXT"}, OutputModalities: []string{"TEXT"}, Capacity: []supply.OfficialFact{{Key: "context", Disclosure: "DISCLOSED", Value: "128000"}}, Abilities: []supply.Ability{{Key: "tool_call", State: "UNDISCLOSED"}}, APIAccess: []supply.OfficialFact{{Key: "protocol", Disclosure: "DISCLOSED", Value: "HTTPS"}}, Lifecycle: []supply.OfficialFact{{Key: "release", Disclosure: "UNDISCLOSED"}}, Sources: []supply.Source{{Label: "official", URL: input.SourceURL, Kind: "OFFICIAL_PRICING"}}, PriceItems: []supply.PriceItem{input, output}, CheckedAt: time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")})
		return err
	}))
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if !snap.QuoteReadiness.Eligible {
		t.Fatalf("expected quotable after verification: %+v", snap.QuoteReadiness)
	}
	row := submission("PRICE_CHANGE", "0.4000")
	options, err := deps.ResourceWorkflow.GetResourceWorkflow(ctx, "1", f.Resource)
	must(err)
	if len(options.ApprovedPrices) != 1 {
		t.Fatal("approved price source missing")
	}
	p := options.ApprovedPrices[0]
	revision, _ := strconv.ParseUint(p.SourceRevision, 10, 64)
	must(write("1", func(c context.Context, m supply.WriteMeta) error {
		_, err := deps.ApprovedPriceWriter.Change(supplypg.WithApprovedPriceCompletion(c, "QA price completion"), m, f.Resource, snap.Revision, p.Coefficient, supply.CoefficientSource{Kind: "SUBMISSION", ID: row, Revision: revision})
		return err
	}))
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if snap.Coefficient != "0.4000" || !snap.QuoteReadiness.Eligible {
		t.Fatal("price change lost eligibility or failed")
	}
	// A later report for a non-designated job must not replace the selected pair.
	late, err := store.CreateJob(ctx, tp.NewJobRecord{ResourceID: f.Resource, Category: tp.CategoryStress, ActorID: "1", TargetFingerprint: "obsolete-target", IdempotencyKey: f.Run + "-late"})
	must(err)
	_, err = store.CompleteJob(ctx, tp.Completion{JobID: late, ResourceID: f.Resource, Category: tp.CategoryStress, FinalStatus: "FAILED", Verdict: "fail", Outcome: tp.Outcome{}, TestedBy: "1", FinishedAt: time.Now().UTC()})
	must(err)
	pinned := snap.QuoteReadiness.TestEvidenceRef
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if !snap.QuoteReadiness.Eligible || snap.QuoteReadiness.TestEvidenceRef != pinned {
		t.Fatal("late unrelated report replaced selected evidence")
	}
	_, err = db.ExecContext(ctx, `UPDATE submission_row SET stress_job_id=$2 WHERE id=$1`, sourceRow, late)
	must(err)
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if snap.QuoteReadiness.Eligible {
		t.Fatal("replaced target remained quotable")
	}
	_, err = db.ExecContext(ctx, `UPDATE submission_row SET stress_job_id=$2 WHERE id=$1`, sourceRow, jobs[1])
	must(err)
	snap, err = deps.Resources.Get(ctx, f.Resource, "CN")
	must(err)
	if !snap.QuoteReadiness.Eligible {
		t.Fatal("matching pinned pair not restored")
	}
	var status string
	must(db.QueryRowContext(ctx, `SELECT row_status FROM submission_row WHERE id=$1`, row).Scan(&status))
	if status != "COMPLETED" {
		t.Fatal("B row not completed")
	}
	if path := os.Getenv("BSS_NATIVE_MANIFEST"); path != "" {
		f.PriceRow = submission("PRICE_CHANGE", "0.3300")
		raw, _ := json.Marshal(f)
		must(os.WriteFile(path, raw, 0600))
		keep = true
	}
}
func mustInt(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return v
}
