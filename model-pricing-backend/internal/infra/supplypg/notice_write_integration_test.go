package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

// Fixture logger only: production must inject the public helper and the request
// transaction. This is deliberately not a second shared log implementation.
func appendNoticeTestLog(ctx context.Context, tx *sql.Tx, op supply.NoticeReadOperation) error {
	before, _ := json.Marshal(op.Before)
	after, _ := json.Marshal(op.After)
	_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, string(before), string(after), op.Reason, op.RequestID, op.OccurredAt)
	return err
}
func TestNoticeWritePostgres34(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("dedicated PostgreSQL write test not requested")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("explicit dedicated test database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong test database")
	}
	fixture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback()
	var org, id string
	staff := make([]string, 2)
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.org_unit(name,path) VALUES ('A公告写入测试','/a-notice-write/') RETURNING id::text`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	for i := range staff {
		if err := fixture.QueryRowContext(ctx, `INSERT INTO public.internal_staff(org_unit_id,name,mobile) VALUES ($1,$2,$3) RETURNING id::text`, org, fmt.Sprintf("写入人员%d", i), fmt.Sprintf("anw-%s-%d", org, i)).Scan(&staff[i]); err != nil {
			t.Fatal(err)
		}
	}
	n := noticeDataFixture()
	raw, err := EncodeNoticePayload(n)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.QueryRowContext(ctx, `INSERT INTO public.notice(type,target_type,target_id,payload) VALUES ($1,'RESOURCE',$2,$3::jsonb) RETURNING id::text`, n.Kind, n.ResourceID, string(raw)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range []string{`DELETE FROM public.operation_log WHERE entity_type='notice' AND entity_id=$1`, `DELETE FROM public.notice WHERE id=$1`} {
			if _, err := db.ExecContext(cleanup, statement, id); err != nil {
				t.Error(err)
			}
		}
		for _, s := range staff {
			if _, err := db.ExecContext(cleanup, `DELETE FROM public.internal_staff WHERE id=$1`, s); err != nil {
				t.Error(err)
			}
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM public.org_unit WHERE id=$1`, org); err != nil {
			t.Error(err)
		}
	}()
	now := time.Date(2026, 10, 20, 3, 0, 0, 123456789, time.UTC)
	storedNow := now.Truncate(time.Microsecond)
	service := func(tx *sql.Tx, logger NoticeLogAppender, at time.Time) *supply.NoticeService {
		return supply.NewNoticeService(NewNoticeUnitOfWork(func(context.Context) (*sql.Tx, error) { return tx, nil }, logger), func() time.Time { return at })
	}
	meta := func(s, trace string) supply.WriteMeta {
		return supply.WriteMeta{ActorID: s, Reason: "阅读公告", RequestID: trace}
	}
	counts := func(s string) (int, int) {
		t.Helper()
		var reads, logs int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.notice_read WHERE notice_id=$1 AND staff_id=$2`, id, s).Scan(&reads); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.operation_log WHERE entity_type='notice' AND entity_id=$1 AND operator_id=$2`, id, s).Scan(&logs); err != nil {
			t.Fatal(err)
		}
		return reads, logs
	}
	t.Run("first receipt stays uncommitted until outer commit", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		logger := func(c context.Context, passed *sql.Tx, op supply.NoticeReadOperation) error {
			if passed != tx {
				t.Fatal("logger got another transaction")
			}
			return appendNoticeTestLog(c, passed, op)
		}
		receipt, err := service(tx, logger, now).MarkRead(ctx, meta(staff[0], "a-notice-read-first"), id)
		if err != nil || receipt == nil || !receipt.Created || !receipt.ReadAt.Equal(storedNow) {
			t.Fatalf("first %+v %v", receipt, err)
		}
		if reads, logs := counts(staff[0]); reads != 0 || logs != 0 {
			t.Fatal("adapter committed outside outer transaction")
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if reads, logs := counts(staff[0]); reads != 1 || logs != 1 {
			t.Fatalf("missing atomic records %d %d", reads, logs)
		}
	})
	t.Run("new repeat intent preserves first time and logs NOOP", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		r, err := service(tx, appendNoticeTestLog, now.Add(time.Hour)).MarkRead(ctx, meta(staff[0], "a-notice-read-again"), id)
		if err != nil || r == nil || r.Created || !r.ReadAt.Equal(storedNow) {
			t.Fatalf("repeat %+v %v", r, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var action string
		if err := db.QueryRowContext(ctx, `SELECT action FROM public.operation_log WHERE entity_type='notice' AND entity_id=$1 AND request_id='a-notice-read-again'`, id).Scan(&action); err != nil || action != "READ_NOOP" {
			t.Fatalf("NOOP %s %v", action, err)
		}
		if reads, logs := counts(staff[0]); reads != 1 || logs != 2 {
			t.Fatalf("repeat records %d %d", reads, logs)
		}
	})
	t.Run("SQL log failure rolls read back with outer transaction", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		brokenLog := func(c context.Context, tx *sql.Tx, op supply.NoticeReadOperation) error {
			op.RequestID = strings.Repeat("x", 65)
			return appendNoticeTestLog(c, tx, op)
		}
		r, err := service(tx, brokenLog, now).MarkRead(ctx, meta(staff[1], "a-notice-read-failed"), id)
		var pgError *pgconn.PgError
		if r != nil || !errors.As(err, &pgError) || pgError.Code != "22001" {
			t.Fatalf("expected actual SQL varchar failure, got receipt=%+v err=%v", r, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if reads, logs := counts(staff[1]); reads != 0 || logs != 0 {
			t.Fatalf("log failure leaked %d %d", reads, logs)
		}
	})
	t.Run("outer rollback discards otherwise valid read and log", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := service(tx, appendNoticeTestLog, now).MarkRead(ctx, meta(staff[1], "a-notice-outer-rollback"), id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if reads, logs := counts(staff[1]); reads != 0 || logs != 0 {
			t.Fatalf("outer rollback leaked %d %d", reads, logs)
		}
	})
	t.Run("unknown staff cannot create read or log", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := service(tx, appendNoticeTestLog, now).MarkRead(ctx, meta("9223372036854775807", "a-notice-unknown-staff"), id); !errors.Is(err, supply.ErrForbidden) {
			t.Fatalf("unknown staff %v", err)
		}
	})
	t.Run("concurrent first reads serialize on notice lock", func(t *testing.T) {
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
		one, err := service(first, appendNoticeTestLog, now).MarkRead(ctx, meta(staff[1], "a-notice-concurrent-first"), id)
		if err != nil || one == nil || !one.Created {
			t.Fatalf("first %+v %v", one, err)
		}
		type outcome struct {
			receipt *supply.NoticeReadReceipt
			err     error
		}
		done := make(chan outcome, 1)
		go func() {
			r, err := service(second, appendNoticeTestLog, now.Add(time.Hour)).MarkRead(ctx, meta(staff[1], "a-notice-concurrent-second"), id)
			done <- outcome{r, err}
		}()
		deadline := time.After(3 * time.Second)
		for {
			var blocked bool
			if err := db.QueryRowContext(ctx, `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, firstPID, secondPID).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			select {
			case result := <-done:
				t.Fatalf("second bypassed lock: %+v %v", result.receipt, result.err)
			case <-deadline:
				t.Fatal("second backend did not wait for first notice lock")
			case <-time.After(10 * time.Millisecond):
			}
		}
		if err := first.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-done:
			if result.err != nil || result.receipt == nil || result.receipt.Created || !result.receipt.ReadAt.Equal(storedNow) {
				t.Fatalf("second %+v %v", result.receipt, result.err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := second.Commit(); err != nil {
			t.Fatal(err)
		}
		if reads, logs := counts(staff[1]); reads != 1 || logs != 2 {
			t.Fatalf("concurrent records %d %d", reads, logs)
		}
	})
}
