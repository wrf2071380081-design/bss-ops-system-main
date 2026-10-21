package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func TestNoticeWriteOperationPreservesFirstReadAndSchemaLimits(t *testing.T) {
	now := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	op := supply.NoticeReadOperation{EntityType: "notice", EntityID: "51", Action: "READ", ActorID: "7", Reason: "阅读公告", RequestID: strings.Repeat("a", 64), OccurredAt: now, After: &supply.NoticeRead{NoticeID: "51", StaffID: "7", ReadAt: now}}
	if err := validateReadOperation(op); err != nil {
		t.Fatal(err)
	}
	noop := op
	noop.Action = "READ_NOOP"
	noop.Before = op.After.Clone()
	noop.After = op.After.Clone()
	noop.OccurredAt = now.Add(time.Hour)
	if err := validateReadOperation(noop); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*supply.NoticeReadOperation){func(o *supply.NoticeReadOperation) { o.RequestID = strings.Repeat("a", 65) }, func(o *supply.NoticeReadOperation) { o.Reason = strings.Repeat("界", 501) }, func(o *supply.NoticeReadOperation) { o.ActorID = "8" }, func(o *supply.NoticeReadOperation) { o.Action = "UPDATE" }, func(o *supply.NoticeReadOperation) { o.After = nil }, func(o *supply.NoticeReadOperation) { o.EntityType = "resource" }} {
		bad := op
		bad.After = op.After.Clone()
		change(&bad)
		if err := validateReadOperation(bad); err == nil {
			t.Fatal("invalid read operation accepted")
		}
	}
	bad := noop
	bad.After = noop.After.Clone()
	bad.After.ReadAt = now.Add(time.Minute)
	if err := validateReadOperation(bad); err == nil {
		t.Fatal("NOOP changed first read time")
	}
}
func TestNoticeWriteCannotRunWithoutRequestTransactionAndLogger(t *testing.T) {
	called := false
	run := func(supply.NoticeTransaction) error { called = true; return nil }
	resolve := func(context.Context) (*sql.Tx, error) { return nil, nil }
	logger := func(context.Context, *sql.Tx, supply.NoticeReadOperation) error { return nil }
	for _, work := range []*NoticeUnitOfWork{nil, NewNoticeUnitOfWork(nil, logger), NewNoticeUnitOfWork(resolve, nil), NewNoticeUnitOfWork(resolve, logger)} {
		if err := work.Within(context.Background(), run); !errors.Is(err, supply.ErrDependencyUnavailable) || called {
			t.Fatalf("missing dependency called write: %v", err)
		}
	}
}

func TestNoticeWriteResolvesTransactionForEveryRequestAndPropagatesErrors(t *testing.T) {
	resolveErr := errors.New("request transaction unavailable")
	calls := 0
	work := NewNoticeUnitOfWork(func(context.Context) (*sql.Tx, error) { calls++; return nil, resolveErr }, func(context.Context, *sql.Tx, supply.NoticeReadOperation) error { return nil })
	for i := 0; i < 2; i++ {
		if err := work.Within(context.Background(), func(supply.NoticeTransaction) error { t.Fatal("callback ran after resolver error"); return nil }); !errors.Is(err, resolveErr) {
			t.Fatalf("resolver error: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("request transaction was cached: calls=%d", calls)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := work.Within(canceled, func(supply.NoticeTransaction) error { return nil }); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("canceled request touched resolver: %v calls=%d", err, calls)
	}
}
