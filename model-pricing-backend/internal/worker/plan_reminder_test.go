package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"model_bss/internal/domain/supplier"
	"model_bss/internal/infra/config"
)

type reminderPublisherFake struct {
	calls int
	limit int
	err   error
}

func (f *reminderPublisherFake) PublishDuePlanReminders(_ context.Context, _ time.Time, limit int) (int, error) {
	f.calls++
	f.limit = limit
	return 2, f.err
}

func TestRegisterPlanReminderJobRunsPublisherWithBatchLimit(t *testing.T) {
	store := &reminderPublisherFake{}
	sched := NewScheduler(nil, zap.NewNop(), time.UTC)
	cfg := config.Worker{Enable: true, Jobs: config.WorkerJobs{PlanReminder: config.JobConfig{Enable: true}}}
	if registered := RegisterPlanReminderJob(sched, cfg, store, zap.NewNop()); registered != 1 || len(sched.jobs) != 1 {
		t.Fatalf("registered=%d jobs=%d", registered, len(sched.jobs))
	}
	if err := sched.jobs[0].fn(context.Background(), supplier.SystemJob("test-reminder")); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.limit != 100 {
		t.Fatalf("publisher calls=%d limit=%d", store.calls, store.limit)
	}
}

func TestRegisterPlanReminderJobHonorsFeatureSwitchAndReportsFailure(t *testing.T) {
	store := &reminderPublisherFake{err: errors.New("database unavailable")}
	sched := NewScheduler(nil, zap.NewNop(), time.UTC)
	cfg := config.Worker{Enable: true, Jobs: config.WorkerJobs{PlanReminder: config.JobConfig{Enable: false}}}
	if got := RegisterPlanReminderJob(sched, cfg, store, zap.NewNop()); got != 0 || len(sched.jobs) != 0 {
		t.Fatalf("disabled reminder registered: %d", got)
	}
	cfg.Jobs.PlanReminder.Enable = true
	if got := RegisterPlanReminderJob(sched, cfg, store, zap.NewNop()); got != 1 {
		t.Fatalf("enabled reminder registration=%d", got)
	}
	if err := sched.jobs[0].fn(context.Background(), supplier.SystemJob("test-reminder-error")); err == nil || store.calls != 1 {
		t.Fatalf("worker error=%v calls=%d", err, store.calls)
	}
}
