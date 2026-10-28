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

func modelWriteCompleteData() supply.ModelData {
	input := supply.PriceItem{ItemKey: "text_input_standard", Label: "输入", TaskType: "TEXT_INPUT", Amount: "2.00000000", Currency: "USD", UnitCode: "TOKEN", UnitQuantity: "1000000", Condition: "标准计费", SourceURL: "https://vendor.example/pricing"}
	output := input
	output.ItemKey = "text_output_standard"
	output.Label = "输出"
	output.TaskType = "TEXT_OUTPUT"
	output.Amount = "8.00000000"
	return supply.ModelData{Company: "Example", Name: "完整资料", OfficialID: "example-v1", ModelType: "TEXT", InputModalities: []string{"TEXT"}, OutputModalities: []string{"TEXT"}, Capacity: []supply.OfficialFact{{Key: "context", Disclosure: "DISCLOSED", Value: "128000"}}, Abilities: []supply.Ability{{Key: "tool_call", State: "UNDISCLOSED"}}, APIAccess: []supply.OfficialFact{{Key: "protocol", Disclosure: "DISCLOSED", Value: "HTTPS"}}, Lifecycle: []supply.OfficialFact{{Key: "release", Disclosure: "UNDISCLOSED"}}, Sources: []supply.Source{{Label: "官方", URL: input.SourceURL, Kind: "OFFICIAL_PRICING"}}, PriceItems: []supply.PriceItem{input, output}}
}

func TestModelWritePostgres34Native(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "select current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong test database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Copy native 33/34 columns, integer version and identity uniqueness only.
	// No control columns are added; public schema and migration tracking stay unchanged.
	for _, ddl := range []string{
		`CREATE TEMP TABLE standard_model (LIKE public.standard_model INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
		`ALTER TABLE pg_temp.standard_model ADD PRIMARY KEY(id)`,
		`CREATE UNIQUE INDEX f1_test_identity ON pg_temp.standard_model(company,official_id)`,
		`CREATE TEMP TABLE standard_model_version (LIKE public.standard_model_version INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
		`ALTER TABLE pg_temp.standard_model_version ADD UNIQUE(standard_model_id,version_no)`,
		`CREATE TEMP TABLE operation_log (LIKE public.operation_log INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	failLog := false
	work, err := NewModelUnitOfWork("pg_temp", func(context.Context) (*sql.Tx, error) { return tx, nil }, func(ctx context.Context, tx *sql.Tx, op supply.ModelOperation) error {
		if failLog {
			// Deliberately violate the real varchar constraint in the injected
			// logger, after projection/version writes have been staged.
			op.RequestID = strings.Repeat("x", 65)
		}
		before, _ := json.Marshal(op.Before)
		after, _ := json.Marshal(op.After)
		_, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, string(before), string(after), op.Reason, op.RequestID, op.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Keep sub-microsecond precision to verify CAS does not compare a JSON
	// snapshot timestamp directly with PostgreSQL's truncated timestamptz.
	now := time.Date(2026, 10, 21, 1, 0, 0, 200235044, time.UTC)
	svc := supply.NewModelService(work, nil, func() time.Time { return now })
	meta := supply.WriteMeta{ActorID: "7", Reason: "真实SQL测试", RequestID: "f1-pg-write"}
	first, err := svc.Create(ctx, meta, supply.ModelData{Name: "草稿一"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, meta, supply.ModelData{Name: "草稿二", OfficialID: "second-model"})
	if err != nil || first.ID == second.ID {
		t.Fatalf("multiple incomplete drafts: %v", err)
	}
	var read *supply.StandardModel
	if err := work.Within(ctx, func(repo supply.ModelTransaction) error {
		var e error
		read, e = repo.GetForUpdate(ctx, first.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if read.MaintainerID != "7" || read.Revision != 1 || read.VerifyStatus != "INCOMPLETE" || read.Data.Name != "草稿一" {
		t.Fatalf("invented or lost facts %+v", read)
	}
	data := read.Data
	data.Name = "修改资料"
	now = now.Add(time.Second)
	updated, err := svc.Update(ctx, meta, first.ID, 1, data)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("update %+v %v", updated, err)
	}
	var snapshots, logs int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.standard_model_version WHERE standard_model_id=$1`, first.ID).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.operation_log WHERE entity_id=$1`, first.ID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if snapshots != 2 || logs != 2 {
		t.Fatalf("effects missing %d %d", snapshots, logs)
	}
	var oldName, version string
	if err := tx.QueryRowContext(ctx, `SELECT snapshot->'model'->'data'->>'name',snapshot->'model'->>'version_no' FROM pg_temp.standard_model_version WHERE standard_model_id=$1 AND version_no=1`, first.ID).Scan(&oldName, &version); err != nil || oldName != "草稿一" || version != "1" {
		t.Fatalf("immutable snapshot %s %s %v", oldName, version, err)
	}
	if _, err := svc.Update(ctx, meta, first.ID, 1, data); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("stale CAS %v", err)
	}
	other := meta
	other.ActorID = "8"
	if _, err := svc.Update(ctx, other, first.ID, 2, data); !errors.Is(err, supply.ErrForbidden) {
		t.Fatalf("maintainer bypass %v", err)
	}
	if _, err := svc.Update(ctx, meta, first.ID, 2, data); err != nil {
		t.Fatalf("NOOP %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.standard_model_version WHERE standard_model_id=$1`, first.ID).Scan(&snapshots); err != nil || snapshots != 2 {
		t.Fatal("NOOP added version", err)
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT log_failure"); err != nil {
		t.Fatal(err)
	}
	failLog = true
	data.Name = "不应保留"
	if m, err := svc.Update(ctx, meta, first.ID, 2, data); err == nil || m != nil {
		t.Fatal("log failure returned success")
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT log_failure"); err != nil {
		t.Fatal(err)
	}
	failLog = false
	if err := work.Within(ctx, func(repo supply.ModelTransaction) error {
		m, e := repo.GetForUpdate(ctx, first.ID)
		if e == nil && (m.Data.Name != "修改资料" || m.Revision != 2) {
			t.Fatal("partial write after rollback")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.standard_model_version WHERE standard_model_id=$1`, first.ID).Scan(&snapshots); err != nil || snapshots != 2 {
		t.Fatal("failed log left version", err)
	}
	// Continue the real domain workflow over the proposed SQL projection.
	complete := modelWriteCompleteData()
	complete.CheckedAt = "2026-10-21"
	now = now.Add(time.Second)
	prepared, err := svc.Update(ctx, meta, first.ID, 2, complete)
	if err != nil || prepared.VerifyStatus != "UNVERIFIED" {
		t.Fatalf("complete data %+v %v", prepared, err)
	}
	rejectedPolicy, err := NewOfficialSourcePolicy(map[string][]string{"Example": {"https://other.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := supply.NewModelService(work, rejectedPolicy, func() time.Time { return now }).Verify(ctx, meta, first.ID, 3, "2026-10-21"); !errors.Is(err, supply.ErrSourceRejected) || result != nil {
		t.Fatalf("unapproved source was verified %+v %v", result, err)
	}
	policy, err := NewOfficialSourcePolicy(map[string][]string{"Example": {"https://vendor.example"}})
	if err != nil {
		t.Fatal(err)
	}
	verifiedSvc := supply.NewModelService(work, policy, func() time.Time { return now })
	verified, err := verifiedSvc.Verify(ctx, meta, first.ID, 3, "2026-10-21")
	if err != nil || !verified.Readiness().Ready || verified.CheckedBy != "7" {
		t.Fatalf("verification %+v %v", verified, err)
	}
	complete = verified.Data
	complete.PriceItems[0].Amount = "3.00000000"
	now = now.Add(time.Second)
	changed, err := verifiedSvc.Update(ctx, meta, first.ID, 4, complete)
	if err != nil || changed.VerifyStatus != "UNVERIFIED" || changed.CheckedBy != "" || changed.Readiness().Ready {
		t.Fatalf("price update retained verification %+v %v", changed, err)
	}
	var oldPrice, oldVerifier string
	if err := tx.QueryRowContext(ctx, `SELECT snapshot->'model'->'data'->'price_items'->0->>'amount',snapshot->'model'->>'checked_by' FROM pg_temp.standard_model_version WHERE standard_model_id=$1 AND version_no=4`, first.ID).Scan(&oldPrice, &oldVerifier); err != nil || oldPrice != "2.00000000" || oldVerifier != "7" {
		t.Fatalf("verified history changed %s %s %v", oldPrice, oldVerifier, err)
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT duplicate_identity"); err != nil {
		t.Fatal(err)
	}
	if result, err := svc.Create(ctx, meta, complete); !errors.Is(err, supply.ErrIdentityConflict) || result != nil {
		t.Fatalf("identity uniqueness %v", err)
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT duplicate_identity"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT duplicate_version"); err != nil {
		t.Fatal(err)
	}
	if err := work.Within(ctx, func(repo supply.ModelTransaction) error { return repo.AppendVersion(ctx, changed) }); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("version rewrite allowed %v", err)
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT duplicate_version"); err != nil {
		t.Fatal(err)
	}
	if err := work.Within(ctx, func(repo supply.ModelTransaction) error { return repo.Update(ctx, changed, 4) }); !errors.Is(err, supply.ErrRevisionConflict) {
		t.Fatalf("SQL CAS accepted stale revision %v", err)
	}
	t.Run("read repository", func(t *testing.T) {
		store, err := NewModelReadStore("pg_temp", func(ctx context.Context, run func(*sql.Tx) error) error { return run(tx) })
		if err != nil {
			t.Fatal(err)
		}
		reader := supply.NewModelReader(store)
		got, err := reader.Get(ctx, first.ID)
		if err != nil || got.Revision != 5 || got.Data.PriceItems[0].Amount != "3.00000000" {
			t.Fatalf("current %+v %v", got, err)
		}
		history, err := reader.Versions(ctx, first.ID, 2, 2)
		if err != nil || history.Total != 5 || len(history.List) != 2 || history.List[0].VersionNo != 3 {
			t.Fatalf("history page %+v %v", history, err)
		}
		snapshot, err := reader.Snapshot(ctx, first.ID, 4)
		if err != nil || snapshot.IsCurrent || snapshot.QuoteReadiness.Ready || snapshot.Model.Data.PriceItems[0].Amount != "2.00000000" {
			t.Fatalf("historical snapshot %+v %v", snapshot, err)
		}
		empty, err := reader.Versions(ctx, first.ID, 20, 2)
		if err != nil || empty.Total != 5 || len(empty.List) != 0 {
			t.Fatalf("empty page %+v %v", empty, err)
		}
		page, err := reader.List(ctx, supply.ModelListQuery{Size: 1, SortBy: "checked_at", Order: "desc"})
		if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != first.ID {
			t.Fatalf("NULLS LAST %+v %v", page, err)
		}
		// Minimal relationship fixtures exercise the EXISTS semantics only.
		for _, sql := range []string{`CREATE TEMP TABLE supplier(id bigint PRIMARY KEY,region text,name text) ON COMMIT DROP`, `CREATE TEMP TABLE resource(id bigint PRIMARY KEY,standard_model_id bigint,supplier_id bigint,region text) ON COMMIT DROP`, `INSERT INTO pg_temp.supplier VALUES(21,'CN','国内来源'),(22,'OVERSEAS','海外来源')`} {
			if _, err := tx.ExecContext(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pg_temp.resource VALUES(101,$1,21,'CN'),(102,$1,22,'OVERSEAS'),(103,$1,21,'CN')`, first.ID); err != nil {
			t.Fatal(err)
		}
		combined, err := NewModelRepository("pg_temp", func(ctx context.Context, run func(*sql.Tx) error) error { return run(tx) })
		if err != nil {
			t.Fatal(err)
		}
		combinedReader := supply.NewModelReader(combined)
		options, err := combinedReader.Options(ctx, []string{"CN"}, "")
		if err != nil || len(options.Companies) != 1 || options.Companies[0] != "Example" || len(options.Suppliers) != 1 || options.Suppliers[0].ID != "21" {
			t.Fatalf("combined options %+v %v", options, err)
		}
		if _, err := combinedReader.Get(ctx, first.ID); err != nil {
			t.Fatalf("combined current %v", err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{SupplierIDs: []string{"21"}, Regions: []string{"OVERSEAS"}})
		if err != nil || page.Total != 0 {
			t.Fatalf("cross-resource false match %+v %v", page, err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{SupplierIDs: []string{"21"}, Regions: []string{"CN"}, Companies: []string{"Example"}})
		if err != nil || page.Total != 1 || len(page.List) != 1 {
			t.Fatalf("resource duplicates %+v %v", page, err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{Keyword: "%"})
		if err != nil || page.Total != 0 {
			t.Fatalf("keyword wildcard %+v %v", page, err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{Keyword: "EXAMPLE-V1"})
		if err != nil || page.Total != 1 {
			t.Fatalf("official ID search %+v %v", page, err)
		}
		if _, err := store.GetModelVersion(ctx, first.ID, 99); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("future version %v", err)
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT malformed_snapshot"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.standard_model_version SET snapshot='{}' WHERE standard_model_id=$1 AND version_no=4`, first.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetModelVersion(ctx, first.ID, 4); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("legacy snapshot accepted %v", err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT malformed_snapshot"); err != nil {
			t.Fatal(err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{VerifyStatus: "UNVERIFIED", SortBy: "verify_status"})
		if err != nil || page.Total != 1 || page.List[0].ID != first.ID {
			t.Fatalf("snapshot metadata filter %+v %v", page, err)
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT drifted_projection"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pg_temp.standard_model SET name='绕过业务的修改' WHERE id=$1`, first.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.Get(ctx, first.ID); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("projection/snapshot mismatch ignored %v", err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT drifted_projection"); err != nil {
			t.Fatal(err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{SortBy: "checked_at", Order: "asc", Size: 1, Page: 2})
		if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != second.ID {
			t.Fatalf("null last pagination %+v %v", page, err)
		}
		if _, err := reader.Get(ctx, "9223372036854775807"); !errors.Is(err, supply.ErrNotFound) {
			t.Fatalf("missing current %v", err)
		}
		var beforeLogs, afterLogs int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.operation_log`).Scan(&beforeLogs); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.List(ctx, supply.ModelListQuery{}); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.operation_log`).Scan(&afterLogs); err != nil || beforeLogs != afterLogs {
			t.Fatal("GET added operation", err)
		}
		tied, err := svc.Create(ctx, meta, supply.ModelData{Name: "草稿二"})
		if err != nil {
			t.Fatal(err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{Keyword: "草稿二", SortBy: "name", Order: "desc"})
		if err != nil || page.Total != 2 || page.List[0].ID != second.ID || page.List[1].ID != tied.ID {
			t.Fatalf("stable ID tie break %+v %v", page, err)
		}
		if _, err := svc.Create(ctx, meta, supply.ModelData{Name: "literal%_", OfficialID: "literal-model"}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT duplicate_blank_identity"); err != nil {
			t.Fatal(err)
		}
		if result, err := svc.Create(ctx, meta, supply.ModelData{Name: "另一个空身份"}); !errors.Is(err, supply.ErrIdentityConflict) || result != nil {
			t.Fatalf("schema34 uniqueness bypassed %v", err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT duplicate_blank_identity"); err != nil {
			t.Fatal(err)
		}
		page, err = reader.List(ctx, supply.ModelListQuery{Keyword: "%_"})
		if err != nil || page.Total != 1 || page.List[0].Data.Name != "literal%_" {
			t.Fatalf("literal wildcard characters %+v %v", page, err)
		}
	})
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var tempExists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('pg_temp.standard_model') IS NOT NULL`).Scan(&tempExists); err != nil || tempExists {
		t.Fatal("caller rollback did not remove candidate data", err)
	}
	t.Run("production read transaction runner", func(t *testing.T) {
		run := ModelReadTransactions(db)
		var captured *sql.Tx
		if err := run(ctx, func(read *sql.Tx) error {
			captured = read
			var isolation, readOnly string
			if err := read.QueryRowContext(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
				return err
			}
			if err := read.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&readOnly); err != nil {
				return err
			}
			if isolation != "repeatable read" || readOnly != "on" {
				t.Fatalf("wrong read transaction %s %s", isolation, readOnly)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := captured.QueryRowContext(ctx, "SELECT 1").Scan(&n); !errors.Is(err, sql.ErrTxDone) {
			t.Fatal("read transaction not closed", err)
		}
		expected := errors.New("read callback failed")
		if err := run(ctx, func(read *sql.Tx) error { captured = read; return expected }); !errors.Is(err, expected) {
			t.Fatal("read error hidden", err)
		}
		if err := captured.QueryRowContext(ctx, "SELECT 1").Scan(&n); !errors.Is(err, sql.ErrTxDone) {
			t.Fatal("failed read transaction not rolled back", err)
		}
	})
}
