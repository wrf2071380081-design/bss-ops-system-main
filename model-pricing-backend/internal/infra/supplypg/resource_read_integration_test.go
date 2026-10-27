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

func TestResourceReadPostgres34Native(t *testing.T) {
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
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "resource", "test_job", "test_report", "operation_log", "submission_row"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE internal_staff(id bigint PRIMARY KEY,name varchar(64) NOT NULL) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.internal_staff(id,name) VALUES(7,'测评人员甲')`); err != nil {
		t.Fatal(err)
	}
	work, err := NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	policy, err := NewOfficialSourcePolicy(map[string][]string{"Example": {"https://vendor.example"}})
	if err != nil {
		t.Fatal(err)
	}
	service := supply.NewModelService(work, policy, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", Reason: "资源读取夹具", RequestID: "resource-read-test"}
	modelIDs := []string{}
	for i, label := range []string{"Alpha", "Zulu"} {
		data := modelWriteCompleteData()
		data.Name = label
		data.OfficialID = strings.ToLower(label)
		m, err := service.Create(ctx, meta, data)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := service.Verify(ctx, meta, m.ID, 1, "2026-10-21"); err != nil {
				t.Fatal(err)
			}
		}
		modelIDs = append(modelIDs, m.ID)
	}
	for _, query := range []string{
		`INSERT INTO pg_temp.supplier(id,name,region,status,password_enc) OVERRIDING SYSTEM VALUE VALUES(21,'Supplier %_','CN','ACTIVE','supplier-secret'),(22,'Other','CN','ACTIVE','other-secret'),(23,'Abroad','OVERSEAS','ACTIVE','foreign-secret')`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	facts := map[string]*ResourceReadFacts{}
	var wantAdmission time.Time
	reportIDs := map[string]string{}
	for i, item := range []struct {
		id, supplier, model, region, coefficient, state string
		admitted, report                                bool
		ms, rps                                         string
	}{
		{"2", "21", modelIDs[0], "CN", "0.8000", "ADMITTED", true, true, "10", "100"},
		{"10", "22", modelIDs[0], "CN", "0.6000", "ADMITTED", true, true, "0", "200"},
		{"11", "23", modelIDs[0], "OVERSEAS", "0.2000", "OFFLINE", true, false, "", ""},
		{"12", "21", modelIDs[1], "CN", "0.3000", "PENDING_ADMIT", false, false, "", ""},
	} {
		var created, updated time.Time
		err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.resource(id,standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,test_base_url,test_api_key_enc) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,'vendor %_','old display',$4,$5,$6,$7,7,'https://secret.invalid','test-secret') RETURNING created_at,updated_at`, item.id, item.model, item.supplier, item.region, item.coefficient, item.state, item.admitted).Scan(&created, &updated)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			wantAdmission = updated.UTC()
			if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,"after",created_at) VALUES('resource',$1,'RESOURCE_REVISION','{"operation":"ADMIT"}'::jsonb,$2)`, item.id, wantAdmission); err != nil {
				t.Fatal(err)
			}
		}
		f := &ResourceReadFacts{ResourceID: item.id, ProjectionUpdatedAt: updated, Revision: 3, OriginRowID: item.id + "00", OriginRowVersion: 1, TargetVersion: 2, FormalConfigID: "91", FormalConfigRevision: 1,
			Config: &supply.FormalConfigView{ID: "91", SupplierID: item.supplier, Region: item.region, Status: "ACTIVE", HasAPIKey: true, Revision: 1, ConfirmedRevision: 1}}
		f.Projection = ResourceProjection{ID: item.id, SupplierID: item.supplier, StandardModelID: item.model, Region: item.region, SupplierModelID: "vendor %_", Coefficient: item.coefficient, State: item.state, EverAdmitted: item.admitted, BuyerID: "7", CreatedAt: created, UpdatedAt: updated}
		if item.report {
			var job, report string
			if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_job(resource_id,category) VALUES($1,'stress') RETURNING id::text`, item.id).Scan(&job); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_report(test_job_id,resource_id,connection_ms,rps,connection_passed,stress_passed,cache_hit_tokens,cache_input_tokens,scene,tested_by,tested_at) VALUES($1,$2,$3,$4,true,$5,$6,9007199254740994,'压力组合场景',7,$7) RETURNING id::text`, job, item.id, item.ms, item.rps, i == 0, []string{"9007199254740993", "0"}[i], now).Scan(&report); err != nil {
				t.Fatal(err)
			}
			reportIDs[item.id] = report
			if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.resource SET latest_test_report_id=$1 WHERE id=$2`, report, item.id); err != nil {
				t.Fatal(err)
			}
			f.LatestReportSequence = 5
			f.Projection.LatestReportID = report
			f.Report = &ReportEvidenceRef{ReportID: report, JobID: job, ResourceID: item.id, SupplierID: item.supplier, StandardModelID: item.model, Region: item.region, OriginRowID: f.OriginRowID, OriginRowVersion: 1, TargetVersion: 2, Sequence: 5, Valid: true}
		}
		facts[item.id] = f
	}
	calls := 0
	runner := func(c context.Context, run func(*sql.Tx) error) error { calls++; return run(tx) }
	loader := func(c context.Context, current *sql.Tx, p ResourceProjection) (*ResourceReadFacts, error) {
		if current != tx {
			t.Fatal("facts escaped read snapshot")
		}
		return facts[p.ID], nil
	}
	store, err := NewResourceRepository("pg_temp", runner, loader)
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewResourceReader(store)
	detail, err := reader.Get(ctx, "2", "CN")
	if err != nil || detail == nil {
		t.Fatalf("native resource unavailable: %v", err)
	}
	if !detail.QuoteReadiness.Eligible || detail.ModelName != "Alpha" || detail.Test.Metrics.ConnectionMS == nil || *detail.Test.Metrics.ConnectionMS != "10.00" || detail.Test.CacheStats.HitTokens != "9007199254740993" {
		t.Fatalf("bad native projection %+v", detail)
	}
	if detail.AdmittedAt == nil || !detail.AdmittedAt.Equal(wantAdmission) {
		t.Fatalf("admission timestamp must come from the admission operation record: %+v", detail.AdmittedAt)
	}
	reportReader, err := NewResourceReportReader("pg_temp", runner)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reportReader.List(ctx, "7", "2", 1, 10)
	if err != nil || history.Total != 1 || len(history.List) != 1 {
		t.Fatalf("report history unavailable: %+v %v", history, err)
	}
	gotReport := history.List[0]
	if !gotReport.TestedAt.Equal(now) || gotReport.TestItem != "stress" || gotReport.OperatorName != "测评人员甲" || gotReport.Source != "" || gotReport.Scene != "压力组合场景" {
		t.Fatalf("report history omitted trustworthy facts or invented a source: %+v", gotReport)
	}
	reportDetail, err := reportReader.Get(ctx, "7", "2", reportIDs["2"])
	if err != nil || reportDetail.TestItem != "stress" || reportDetail.OperatorName != "测评人员甲" || reportDetail.Scene != "压力组合场景" {
		t.Fatalf("report detail lost source facts: %+v %v", reportDetail, err)
	}
	legacy, err := reader.Get(ctx, "10", "CN")
	if err != nil || legacy.AdmittedAt != nil {
		t.Fatalf("missing admission history must remain unrecorded: %+v %v", legacy, err)
	}
	encoded, _ := json.Marshal(detail)
	for _, secret := range []string{"test-secret", "supplier-secret", "secret.invalid", "test_api_key_enc", "password_enc"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("secret leaked")
		}
	}
	group, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped", Page: 1, Size: 1, SortBy: "resource_count", Order: "desc"})
	if err != nil || group.Total != 2 || len(group.Groups) != 1 {
		t.Fatalf("groups %+v %v", group, err)
	}
	g := group.Groups[0]
	if g.ID != modelIDs[0] || g.ResourceCount != 3 || g.SupplierCount != 3 || g.AvailableCount != 1 || g.CoefficientMin != "0.2000" || g.CoefficientMax != "0.8000" {
		t.Fatalf("group paginated before aggregate %+v", g)
	}
	multi, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped", SupplierIDs: []string{"21", "22"}, Regions: []string{"CN"}, CoefficientMax: "0.7"})
	if err != nil || multi.Total != 2 || multi.Groups[0].ResourceCount != 1 {
		t.Fatalf("filtered aggregates wrong: %+v %v", multi, err)
	}
	exact, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", ConnectionMax: "0", RPSMin: "200"})
	if err != nil || exact.Total != 1 || exact.Resources[0].ID != "10" {
		t.Fatalf("zero/missing range wrong: %+v %v", exact, err)
	}
	for _, order := range []string{"asc", "desc"} {
		admitted, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", SortBy: "admitted_at", Order: order})
		if err != nil || admitted.Resources[0].ID != "2" {
			t.Fatalf("admission null-last failed: %+v %v", admitted, err)
		}
	}
	for _, tc := range []struct{ field, ids string }{
		{"id", "2,10,11,12"}, {"supplier_name", "11,10,2,12"}, {"region", "2,10,12,11"}, {"coefficient", "11,12,10,2"},
		{"state", "2,10,11,12"}, {"updated_at", "2,10,11,12"}, {"connection_ms", "10,2,11,12"}, {"rps", "2,10,11,12"}, {"tps", "2,10,11,12"}, {"cache_hit_rate", "10,2,11,12"},
	} {
		result, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", SortBy: tc.field, Order: "asc"})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, r := range result.Resources {
			ids = append(ids, r.ID)
		}
		if strings.Join(ids, ",") != tc.ids {
			t.Fatalf("flat %s order: %v", tc.field, ids)
		}
	}
	for _, tc := range []struct {
		field string
		first int
	}{
		{"id", 0}, {"name", 0}, {"company", 0}, {"model_type", 0}, {"supplier_count", 1}, {"resource_count", 1}, {"available_count", 1}, {"coefficient_min", 0}, {"coefficient_max", 1},
	} {
		result, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped", SortBy: tc.field, Order: "asc", Page: 1, Size: 1})
		if err != nil || result.Total != 2 || len(result.Groups) != 1 || result.Groups[0].ID != modelIDs[tc.first] {
			t.Fatalf("group %s order: %+v %v", tc.field, result, err)
		}
	}
	cn, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped", Region: "CN", StandardModelID: modelIDs[0]})
	if err != nil || cn.Total != 1 || cn.Groups[0].ResourceCount != 2 || cn.Groups[0].CoefficientMin != "0.6000" {
		t.Fatalf("cross-region aggregation %+v %v", cn, err)
	}
	literal, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", Keyword: "%_", SupplierID: "21"})
	if err != nil || literal.Total != 2 {
		t.Fatalf("literal search %+v %v", literal, err)
	}
	page, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", Page: 2, Size: 1, SortBy: "coefficient", Order: "asc"})
	if err != nil || page.Total != 4 || len(page.Resources) != 1 || page.Resources[0].ID != "12" || page.Resources[0].State != "PENDING_ACCESS" {
		t.Fatalf("flat sorted pagination %+v %v", page, err)
	}
	for _, order := range []string{"asc", "desc"} {
		metrics, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", SortBy: "connection_ms", Order: order})
		if err != nil || len(metrics.Resources) != 4 {
			t.Fatal(err)
		}
		want := "10"
		if order == "desc" {
			want = "2"
		}
		if metrics.Resources[0].ID != want || metrics.Resources[2].ID != "11" || metrics.Resources[3].ID != "12" {
			t.Fatalf("NULL/zero/id ordering %+v", metrics.Resources)
		}
	}
	empty, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped", Page: 9, Size: 1})
	if err != nil || empty.Total != 2 || len(empty.Groups) != 0 {
		t.Fatalf("empty page loses total %+v %v", empty, err)
	}
	if _, err := reader.Get(ctx, "2", "OVERSEAS"); !errors.Is(err, supply.ErrNotFound) {
		t.Fatal("wrong region", err)
	}
	if _, err := reader.Get(ctx, "999", "CN"); !errors.Is(err, supply.ErrNotFound) {
		t.Fatal("missing resource", err)
	}
	original := *facts["2"]
	for _, change := range []func(*ResourceReadFacts){
		func(f *ResourceReadFacts) { f.ResourceID = "10" }, func(f *ResourceReadFacts) { f.Revision = 0 },
		func(f *ResourceReadFacts) { f.ProjectionUpdatedAt = f.ProjectionUpdatedAt.Add(time.Second) },
		func(f *ResourceReadFacts) { f.Report = nil }, func(f *ResourceReadFacts) { f.LatestReportSequence = 6 },
		func(f *ResourceReadFacts) { f.FormalConfigID = "" },
	} {
		copy := original
		change(&copy)
		facts["2"] = &copy
		if got, err := reader.List(ctx, supply.ResourceListQuery{View: "grouped"}); !errors.Is(err, supply.ErrReadContract) || got != nil {
			t.Fatalf("corrupt facts accepted %+v %v", got, err)
		}
	}
	facts["2"] = &original
	originalRef := original.Report
	stale := *originalRef
	stale.TargetVersion = 1
	facts["2"].Report = &stale
	detail, err = reader.Get(ctx, "2", "CN")
	if err != nil || detail.Test.Status != "STALE" || detail.QuoteReadiness.Eligible || detail.Test.Metrics.ConnectionMS != nil {
		t.Fatalf("stale report reused %+v %v", detail, err)
	}
	facts["2"].Report = originalRef
	facts["2"] = nil
	if got, err := reader.Get(ctx, "2", "CN"); !errors.Is(err, supply.ErrReadContract) || got != nil {
		t.Fatal("missing metadata invented", err)
	}
	if calls == 0 {
		t.Fatal("SQL runner not used")
	}
	options, err := reader.Options(ctx, "OVERSEAS", "")
	if err != nil || len(options.Companies) != 1 || len(options.Suppliers) != 1 || options.Suppliers[0].ID != "23" {
		t.Fatalf("repository options %+v %v", options, err)
	}
	// Both accepted spellings must behave identically under a state filter.
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.resource SET state='PENDING_ACCESS' WHERE id=12`); err != nil {
		t.Fatal(err)
	}
	access, err := reader.List(ctx, supply.ResourceListQuery{View: "flat", State: "PENDING_ACCESS"})
	if err != nil || access.Total != 1 || access.Resources[0].ID != "12" {
		t.Fatalf("state alias lost under filter %+v %v", access, err)
	}
	facts["2"] = &original
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.resource SET coefficient=0.7100 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if got, err := reader.Get(ctx, "2", "CN"); !errors.Is(err, supply.ErrReadContract) || got != nil {
		t.Fatal("native projection changed without matching revision snapshot", err)
	}
}
