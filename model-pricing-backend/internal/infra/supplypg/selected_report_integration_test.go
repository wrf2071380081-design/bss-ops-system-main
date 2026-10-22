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

func TestSelectedReportPostgres34(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	for _, ddl := range []string{
		`CREATE TEMP TABLE test_report (LIKE public.test_report INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
		`CREATE TEMP TABLE test_job(id bigint PRIMARY KEY,resource_id bigint) ON COMMIT DROP`,
		`CREATE TEMP TABLE resource(id bigint PRIMARY KEY,supplier_id bigint,standard_model_id bigint,region text) ON COMMIT DROP`,
		`INSERT INTO pg_temp.resource VALUES(11,21,31,'CN'),(12,22,32,'OVERSEAS')`,
		`INSERT INTO pg_temp.test_job VALUES(41,11),(42,12)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	var id, other string
	if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_report(test_job_id,resource_id,connection_ms,connection_passed,stress_passed,rps,tps,cache_hit_tokens,cache_input_tokens,scene) VALUES(41,11,0,true,true,NULL,123456789.12,9007199254740993,9007199254740994,'原始场景') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO pg_temp.test_report(test_job_id,resource_id,connection_ms,connection_passed,stress_passed,rps,tps,cache_hit_tokens,cache_input_tokens) VALUES(42,12,99,false,false,99,99,1,1) RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	ref := ReportEvidenceRef{ReportID: id, JobID: "41", ResourceID: "11", SupplierID: "21", StandardModelID: "31", Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, Sequence: 4, Valid: true}
	report, err := ReadSelectedReport(ctx, tx, "pg_temp", ref)
	if err != nil {
		t.Fatal(err)
	}
	if report.ID != id || report.Sequence != 4 || !report.Valid || !report.ConnectivityPassed || !report.PressurePassed || report.Metrics.ConnectionMS == nil || *report.Metrics.ConnectionMS != "0.00" || report.Metrics.RPS != nil || report.Metrics.TPS == nil || *report.Metrics.TPS != "123456789.12" || report.Cache.HitTokens != "9007199254740993" {
		t.Fatalf("selected data lost %+v", report)
	}
	for _, change := range []func(*ReportEvidenceRef){
		func(r *ReportEvidenceRef) { r.ReportID = other }, func(r *ReportEvidenceRef) { r.JobID = "42" }, func(r *ReportEvidenceRef) { r.ResourceID = "12" }, func(r *ReportEvidenceRef) { r.SupplierID = "22" }, func(r *ReportEvidenceRef) { r.StandardModelID = "32" }, func(r *ReportEvidenceRef) { r.Region = "OVERSEAS" },
	} {
		wrong := ref
		change(&wrong)
		if got, err := ReadSelectedReport(ctx, tx, "pg_temp", wrong); !errors.Is(err, supply.ErrReadContract) || got != nil {
			t.Fatalf("unrelated facts accepted %+v %v", got, err)
		}
	}
	missing := ref
	missing.ReportID = "9223372036854775807"
	if _, err := ReadSelectedReport(ctx, tx, "pg_temp", missing); !errors.Is(err, supply.ErrNotFound) {
		t.Fatalf("missing report %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.test_report SET stress_passed=NULL,cache_input_tokens=0 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	report, err = ReadSelectedReport(ctx, tx, "pg_temp", ref)
	if err != nil || report.Valid || report.PressurePassed || report.Cache.Status != "NOT_PROVIDED" {
		t.Fatalf("missing verdict invented %+v %v", report, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.test_report SET stress_passed=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	report, err = ReadSelectedReport(ctx, tx, "pg_temp", ref)
	if err != nil || !report.Valid || report.PressurePassed {
		t.Fatalf("failed and incomplete verdict conflated %+v %v", report, err)
	}
	ref.Valid = false
	report, err = ReadSelectedReport(ctx, tx, "pg_temp", ref)
	if err != nil || report.Valid {
		t.Fatal("B invalidated evidence revived", err)
	}
}
