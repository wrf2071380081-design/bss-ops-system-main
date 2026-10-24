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

func TestResourceBindingDisableContentionPostgres34(t *testing.T) {
	for _, bindFirst := range []bool{true, false} {
		label := "binding_wins"
		if !bindFirst {
			label = "disable_wins"
		}
		t.Run(label, func(t *testing.T) { testResourceBindingDisableContention(t, bindFirst) })
	}
}
func testResourceBindingDisableContention(t *testing.T, bindFirst bool) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	label := fmt.Sprintf("A-bind-disable-%d", now.UnixNano())
	fixture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback()
	var org, supplier string
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&org); err != nil {
		t.Fatal(err)
	}
	staff := []string{}
	for i := 0; i < 2; i++ {
		var id string
		if err := fixture.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, org, label, fmt.Sprint(now.UnixNano()+int64(i))).Scan(&id); err != nil {
			t.Fatal(err)
		}
		staff = append(staff, id)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.supplier(name,region,created_by) VALUES($1,'CN',$2) RETURNING id::text`, label, staff[0]).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.ExecContext(ctx, `INSERT INTO public.assignment_log(entity_type,entity_id,field,new_staff_id,changed_by) VALUES('supplier',$1,'buyer_id',$2,$2)`, supplier, staff[0]); err != nil {
		t.Fatal(err)
	}
	logger := func(c context.Context, tx *sql.Tx, e ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(c, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	state, err := NewResourceStateStore("public", logger)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := NewFormalConfigStore("public", func(c context.Context, tx *sql.Tx, e FormalConfigLog) error {
		return logger(c, tx, ResourceStateLog(e))
	})
	if err != nil {
		t.Fatal(err)
	}
	fixtureResolve := func(context.Context) (*sql.Tx, error) { return fixture, nil }
	meta := supply.WriteMeta{ActorID: staff[0], Reason: "绑定停用并发验收", RequestID: label}
	mw, err := NewModelUnitOfWork("public", fixtureResolve, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	model, err := supply.NewModelService(mw, nil, func() time.Time { return now }).Create(ctx, meta, supply.ModelData{Name: label, Company: label, OfficialID: label})
	if err != nil {
		t.Fatal(err)
	}
	configService := func(tx *sql.Tx) *supply.FormalConfigService {
		return supply.NewFormalConfigService(NewFormalConfigWork(configs, func(context.Context) (*sql.Tx, error) { return tx, nil }), configPGURL{}, func() time.Time { return now.Add(time.Second) })
	}
	secret := "concurrency-test-only-provider-key"
	config, err := configService(fixture).Create(ctx, meta, supplier, supply.FormalConfigInput{Scope: "测试范围", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	resourceID, err := state.NextID(ctx, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Insert(ctx, fixture, &supply.Resource{ID: resourceID, SupplierID: supplier, StandardModelID: model.ID, BuyerID: staff[0], Region: "CN", SupplierModelID: label, Coefficient: "0.7000", State: "PENDING_TEST", Revision: 1, OriginRowID: "71", OriginRowVersion: 1, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}, meta, "CREATE"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM public.operation_log WHERE (entity_type='resource' AND entity_id=$1) OR (entity_type='supplier_formal_config' AND entity_id=$2)`, []any{resourceID, config.ID}},
			{`DELETE FROM public.resource WHERE id=$1 AND supplier_id=$2`, []any{resourceID, supplier}},
			{`DELETE FROM public.supplier_formal_config WHERE id=$1 AND supplier_id=$2`, []any{config.ID, supplier}},
			{`DELETE FROM public.standard_model WHERE id=$1 AND company=$2`, []any{model.ID, label}},
			{`DELETE FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1`, []any{supplier}},
			{`DELETE FROM public.supplier WHERE id=$1 AND name=$2`, []any{supplier, label}},
			{`DELETE FROM public.internal_staff WHERE org_unit_id=$1 AND name=$2`, []any{org, label}},
			{`DELETE FROM public.org_unit WHERE id=$1 AND name=$2`, []any{org, label}},
		} {
			if _, err := db.ExecContext(cleanup, q.sql, q.args...); err != nil {
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
	bind := func(tx *sql.Tx) error {
		work := NewResourceBindingWork(state, configs, func(context.Context) (*sql.Tx, error) { return tx, nil }, func(context.Context, *sql.Tx, *supply.Resource) (*supply.AdmissionProof, error) {
			return &supply.AdmissionProof{ResourceID: resourceID, SupplierID: supplier, Region: "CN", OriginRowID: "71", OriginRowVersion: 1, TargetVersion: 1, FlowID: "81", NodeID: "82", FlowRevision: 1, AssigneeID: staff[1], FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}, nil
		})
		actor := meta
		actor.ActorID = staff[1]
		_, err := supply.NewResourceConfigBindingService(work, func() time.Time { return now.Add(time.Second) }).BindFormalConfig(ctx, actor, supply.ResourceConfigBindingCommand{ResourceID: resourceID, ConfigID: config.ID, ExpectedRevision: 1, ExpectedConfigRevision: 1})
		return err
	}
	disable := func(tx *sql.Tx) error {
		_, err := configService(tx).Disable(ctx, meta, supplier, config.ID, 1)
		return err
	}
	firstOp, secondOp := bind, disable
	if !bindFirst {
		firstOp, secondOp = disable, bind
	}
	if err := firstOp(first); err != nil {
		t.Fatal("first operation failed", err)
	}
	var firstPID, secondPID int
	if err := first.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- secondOp(second) }()
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
			t.Fatalf("second operation bypassed common supplier lock: %v", err)
		case <-timer.C:
			t.Fatal("did not observe actual lock wait")
		case <-ticker.C:
		}
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	want := supply.ErrConfigInUse
	if !bindFirst {
		want = supply.ErrRevisionConflict
	}
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("second result: %v, want %v", err, want)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := ModelReadTransactions(db)(ctx, func(tx *sql.Tx) error {
		r, err := state.Current(ctx, tx, resourceID)
		if err != nil {
			return err
		}
		c, err := configs.Current(ctx, tx, supplier, config.ID)
		if err != nil {
			return err
		}
		if bindFirst {
			if r.FormalConfigID != config.ID || r.Revision != 2 || c.Status != "ACTIVE" || c.Revision != 1 {
				t.Fatal("disable bypassed completed binding")
			}
		} else {
			if r.FormalConfigID != "" || r.Revision != 1 || c.Status != "INACTIVE" || c.Revision != 2 {
				t.Fatal("binding bypassed completed disable")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
