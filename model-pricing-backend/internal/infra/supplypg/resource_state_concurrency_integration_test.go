package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceConcurrentWritersPostgres34(t *testing.T) {
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
	db.SetMaxOpenConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	label := fmt.Sprintf("A-resource-%d", now.UnixNano())
	fixture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback()
	var org, staff, supplier string
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, org, label, fmt.Sprint(now.UnixNano())).Scan(&staff); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region) VALUES($1,'CN') RETURNING id::text`, label).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	modelWork, err := NewModelUnitOfWork("public", func(context.Context) (*sql.Tx, error) { return fixture, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	meta := supply.WriteMeta{ActorID: staff, Reason: "资源控制快照真实并发验收", RequestID: label}
	model, err := supply.NewModelService(modelWork, nil, func() time.Time { return now }).Create(ctx, meta, supply.ModelData{Name: label, Company: label, OfficialID: label})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewResourceStateStore("public", func(c context.Context, tx *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(c, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.NextID(ctx, fixture)
	if err != nil {
		t.Fatal(err)
	}
	initial := &supply.Resource{ID: id, SupplierID: supplier, StandardModelID: model.ID, Region: "CN", SupplierModelID: label, Coefficient: "0.7000", BuyerID: staff, State: "PENDING_TEST", Revision: 1, OriginRowID: "71", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := store.Insert(ctx, fixture, initial, meta, "CREATE"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, item := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM public.operation_log WHERE entity_type='resource' AND entity_id=$1`, []any{id}},
			{`DELETE FROM public.resource WHERE id=$1 AND supplier_id=$2`, []any{id, supplier}},
			{`DELETE FROM public.standard_model WHERE id=$1 AND company=$2`, []any{model.ID, label}},
			{`DELETE FROM public.supplier WHERE id=$1 AND name=$2`, []any{supplier, label}},
			{`DELETE FROM public.internal_staff WHERE id=$1 AND name=$2`, []any{staff, label}},
			{`DELETE FROM public.org_unit WHERE id=$1 AND name=$2`, []any{org, label}},
		} {
			if _, err := db.ExecContext(cleanup, item.query, item.args...); err != nil {
				t.Error(err)
			}
		}
	}()
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
	var firstPID, secondPID int
	if err := first.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	winner := *initial
	winner.Coefficient = "0.6000"
	winner.Revision = 2
	winner.UpdatedAt = now.Add(time.Second)
	if _, err := store.Update(ctx, first, &winner, 1, meta, "CHANGE_COEFFICIENT"); err != nil {
		t.Fatal(err)
	}
	readCurrent := func() (*supply.Resource, error) {
		var r *supply.Resource
		err := ModelReadTransactions(db)(ctx, func(tx *sql.Tx) error { var err error; r, err = store.Current(ctx, tx, id); return err })
		return r, err
	}
	if visible, err := readCurrent(); err != nil || visible.Revision != 1 || visible.Coefficient != "0.7000" {
		t.Fatalf("store committed early %+v %v", visible, err)
	}
	done := make(chan error, 1)
	go func() {
		stale := *initial
		stale.Coefficient = "0.5000"
		stale.Revision = 2
		stale.UpdatedAt = now.Add(2 * time.Second)
		_, err := store.Update(ctx, second, &stale, 1, meta, "CHANGE_COEFFICIENT")
		done <- err
	}()
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
			t.Fatalf("second writer bypassed lock: %v", err)
		case <-timer.C:
			t.Fatal("did not observe real row lock contention")
		case <-ticker.C:
		}
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, supply.ErrRevisionConflict) {
			t.Fatalf("stale writer: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}
	final, err := readCurrent()
	if err != nil || final.Revision != 2 || final.Coefficient != "0.6000" {
		t.Fatalf("lost concurrent winner %+v %v", final, err)
	}
	var logs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.operation_log WHERE entity_type='resource' AND entity_id=$1`, id).Scan(&logs); err != nil || logs != 2 {
		t.Fatalf("stale writer logged a change %d %v", logs, err)
	}
}
