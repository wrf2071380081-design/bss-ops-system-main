package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelConcurrentWritersPostgres34(t *testing.T) {
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
	db.SetMaxOpenConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	label := fmt.Sprintf("A-concurrent-%d", now.UnixNano())
	logger := func(ctx context.Context, tx *sql.Tx, op supply.ModelOperation) error {
		before, _ := json.Marshal(op.Before)
		after, _ := json.Marshal(op.After)
		_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, string(before), string(after), op.Reason, op.RequestID, op.OccurredAt)
		return err
	}
	service := func(tx *sql.Tx) *supply.ModelService {
		work, err := NewModelUnitOfWork("public", func(context.Context) (*sql.Tx, error) { return tx, nil }, logger)
		if err != nil {
			t.Fatal(err)
		}
		return supply.NewModelService(work, nil, func() time.Time { return now.Add(time.Second) })
	}
	fixture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback()
	var orgID, staffID string
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, orgID, label, fmt.Sprint(now.UnixNano())).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	meta := supply.WriteMeta{ActorID: staffID, Reason: "专用库并发验收", RequestID: label}
	created, err := service(fixture).Create(ctx, meta, supply.ModelData{Company: label, OfficialID: label, Name: "原资料"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.operation_log WHERE entity_type='standard_model' AND entity_id=$1`, created.ID); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.standard_model WHERE id=$1 AND company=$2`, created.ID, label); err != nil {
			t.Error(err)
		}
		var n int
		if err := db.QueryRowContext(cleanup, `SELECT count(*) FROM public.standard_model_version WHERE standard_model_id=$1`, created.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("version fixture remains %d %v", n, err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.internal_staff WHERE id=$1 AND name=$2`, staffID, label); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.org_unit WHERE id=$1 AND name=$2`, orgID, label); err != nil {
			t.Error(err)
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
	data := created.Data
	data.Name = "先提交资料"
	changed, err := service(first).Update(ctx, meta, created.ID, 1, data)
	if err != nil || changed.Revision != 2 {
		t.Fatalf("first writer %+v %v", changed, err)
	}
	var visible string
	if err := db.QueryRowContext(ctx, `SELECT name FROM public.standard_model WHERE id=$1`, created.ID).Scan(&visible); err != nil || visible != "原资料" {
		t.Fatalf("adapter committed early %s %v", visible, err)
	}
	secondService := service(second)
	done := make(chan error, 1)
	go func() {
		copy := created.Data
		copy.Name = "过期请求的资料"
		_, err := secondService.Update(ctx, meta, created.ID, 1, copy)
		done <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
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
			t.Fatalf("second writer bypassed row lock: %v", err)
		case <-deadline.C:
			t.Fatal("did not observe actual row lock contention")
		case <-ticker.C:
		}
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, supply.ErrRevisionConflict) {
			t.Fatalf("waiting writer did not read committed snapshot: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}
	repository, err := NewModelRepository("public", ModelReadTransactions(db))
	if err != nil {
		t.Fatal(err)
	}
	model, err := repository.GetModel(ctx, created.ID)
	if err != nil || model.Revision != 2 || model.Data.Name != "先提交资料" {
		t.Fatalf("lost update %+v %v", model, err)
	}
	var versions, logs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.standard_model_version WHERE standard_model_id=$1`, created.ID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.operation_log WHERE entity_type='standard_model' AND entity_id=$1`, created.ID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if versions != 2 || logs != 2 {
		t.Fatalf("stale writer left partial effects: versions=%d logs=%d", versions, logs)
	}
}
