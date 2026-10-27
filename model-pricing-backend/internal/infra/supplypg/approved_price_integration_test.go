package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"testing"
	"time"
)

// A consumer transaction test on the daily v42 database. The B ports are
// controlled test implementations; this does NOT prove B approval/assignment.
// Every fixture and write is rolled back; no database/container is created.
func TestApprovedPriceDaily42Transaction(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatal("wrong daily config")
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
	ctx := WithApprovedPriceCompletion(context.Background(), "QA approved change external receipt")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var version int
	var dirty bool
	must(tx.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
	if version != 42 || dirty {
		t.Fatal("requires 42 clean")
	}
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	logfn := func(ctx context.Context, tx *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	store, err := NewSupplierStore("public", db, resolve, logfn)
	must(err)
	meta := supply.WriteMeta{ActorID: "3", Reason: "QA approved price transaction fixture", RequestID: "QA-approved-price"}
	supplier, err := supply.NewSupplierService(store, time.Now).Create(ctx, meta, supply.SupplierData{Name: "QA-approved-price-" + time.Now().Format("150405.000000"), Region: "CN", SettlementCurrency: "CNY"})
	must(err)
	var modelID, submission, row string
	must(tx.QueryRowContext(ctx, "SELECT id::text FROM public.standard_model ORDER BY id LIMIT 1").Scan(&modelID))
	must(tx.QueryRowContext(ctx, `INSERT INTO public.submission(supplier_id,kind,region,status,reason,submitted_by) VALUES($1,'PRICE_CHANGE','CN','PROCESSING',$2,3) RETURNING id::text`, supplier.ID, meta.Reason).Scan(&submission))
	must(tx.QueryRowContext(ctx, `INSERT INTO public.submission_row(submission_id,standard_model_id,supplier_model_name,coefficient) VALUES($1,$2,'QA approved model',0.4) RETURNING id::text`, submission, modelID).Scan(&row))
	state, err := NewResourceStateStore("public", logfn)
	must(err)
	id, err := state.NextID(ctx, tx)
	must(err)
	now := time.Now().UTC()
	resource, err := state.Insert(ctx, tx, &supply.Resource{ID: id, SupplierID: supplier.ID, StandardModelID: modelID, Region: "CN", SupplierModelID: "QA approved model", Coefficient: "0.5000", BuyerID: "3", State: "PENDING_TEST", Revision: 1, OriginRowID: row, OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}, meta, "CREATE")
	must(err)
	resource.State = "OFFLINE"
	resource.EverAdmitted = true
	resource.Revision = 2
	resource.UpdatedAt = time.Now().UTC()
	resource, err = state.Update(ctx, tx, resource, 1, meta, "QA_FIXTURE")
	must(err)
	_, err = tx.ExecContext(ctx, `UPDATE public.submission_row SET resource_id=$1 WHERE id=$2`, id, row)
	must(err)
	source := supply.CoefficientSource{Kind: "SUBMISSION", ID: row, Revision: 1}
	for _, tc := range []struct {
		name, actor, amount     string
		revision                uint64
		failEvent, failComplete bool
		want                    error
	}{{"changed", "3", "0.4", 2, false, false, nil}, {"noop", "3", "0.5", 2, false, false, nil}, {"wrong actor", "1", "0.4", 2, false, false, supply.ErrForbidden}, {"stale resource", "3", "0.4", 1, false, false, supply.ErrRevisionConflict}, {"completion failure", "3", "0.4", 2, false, true, supply.ErrDependencyUnavailable}, {"outbox failure", "3", "0.4", 2, true, false, supply.ErrDependencyUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tx.ExecContext(ctx, "SAVEPOINT qa_price_case")
			must(err)
			// Baseline is queried before each request, and compared after rollback.
			count := func(table, where string) int {
				var n int
				must(tx.QueryRowContext(ctx, "SELECT count(*) FROM public."+table+" WHERE "+where, id).Scan(&n))
				return n
			}
			logs, notices := count("operation_log", "entity_type='resource' AND entity_id=$1"), count("notice", "target_type='RESOURCE' AND target_id=$1")
			hooks := ApprovedPriceHooks{
				Verify: func(c context.Context, current *sql.Tx, s supply.CoefficientSource, actor string, r *supply.Resource) (supply.CoefficientSourceProof, error) {
					if current != tx || s != source || r.ID != id {
						return supply.CoefficientSourceProof{}, supply.ErrRevisionConflict
					}
					return supply.CoefficientSourceProof{Authorized: actor == "3", ResourceID: id, Coefficient: tc.amount, ExpectedResourceRevision: 2}, nil
				},
				Complete: func(c context.Context, current *sql.Tx, e supply.ResourceCoefficientOperation) error {
					if current != tx || e.Source != source {
						return supply.ErrReadContract
					}
					_, err := current.ExecContext(c, "UPDATE public.submission_row SET row_status='COMPLETED' WHERE id=$1", row)
					if err != nil {
						return err
					}
					if tc.failComplete {
						return supply.ErrDependencyUnavailable
					}
					return nil
				},
				Outbox: func(c context.Context, current *sql.Tx, e AdmissionOutboxEntry) (string, error) {
					if tc.failEvent {
						return "", supply.ErrDependencyUnavailable
					}
					var eventID string
					err := current.QueryRowContext(c, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by,updated_by,created_at) VALUES($1,$2::jsonb,$3,$4,$4,$5) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID, e.OccurredAt).Scan(&eventID)
					return eventID, err
				},
			}
			input := meta
			input.ActorID = tc.actor
			result, err := supply.NewResourceCoefficientService(NewApprovedPriceWork(state, resolve, hooks), time.Now).Change(ctx, input, id, tc.revision, tc.amount, source)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v want %v", err, tc.want)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				changed := tc.amount != "0.5"
				expected := uint64(2)
				if changed {
					expected++
				}
				if result.Revision != expected || result.TargetVersion != resource.TargetVersion || result.State != resource.State {
					t.Fatal("unrelated state mutated")
				}
				if got := count("notice", "target_type='RESOURCE' AND target_id=$1"); got != notices+map[bool]int{true: 1, false: 0}[changed] {
					t.Fatal("wrong notice count")
				}
				var status string
				must(tx.QueryRowContext(ctx, "SELECT row_status FROM public.submission_row WHERE id=$1", row).Scan(&status))
				if status != "COMPLETED" {
					t.Fatal("source not completed")
				}
				var raw json.RawMessage
				must(tx.QueryRowContext(ctx, `SELECT "after" FROM public.operation_log WHERE entity_type='resource' AND entity_id=$1 AND action IN ('CHANGE_COEFFICIENT','CHANGE_COEFFICIENT_NOOP') ORDER BY id DESC LIMIT 1`, id).Scan(&raw))
				var receipt map[string]string
				must(json.Unmarshal(raw, &receipt))
				if receipt["source_row_id"] != row || receipt["completion_note"] == "" {
					t.Fatal("missing safe source receipt")
				}
			}
			_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT qa_price_case")
			must(err)
			stored, err := state.Current(ctx, tx, id)
			must(err)
			if *stored != *resource || count("operation_log", "entity_type='resource' AND entity_id=$1") != logs || count("notice", "target_type='RESOURCE' AND target_id=$1") != notices {
				t.Fatal("caller rollback did not restore resource/log/notice")
			}
		})
	}
}
