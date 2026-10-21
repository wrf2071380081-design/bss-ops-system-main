package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type planMaintenanceFixture struct {
	resource                        Resource
	plan                            *StatePlan
	effects                         []PlanMaintenanceEffect
	pending, failEffect, failCommit bool
	pendingConfiguration            bool
	configurationError              error
	configurationResourceID         string
}

func (f *planMaintenanceFixture) WithinPlanMaintenance(ctx context.Context, fn func(PlanMaintenanceTransaction) error) error {
	copy := *f
	copy.plan = f.plan.copy()
	copy.effects = append([]PlanMaintenanceEffect{}, f.effects...)
	if err := fn(&copy); err != nil {
		return err
	}
	if f.failCommit {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *planMaintenanceFixture) ResourceForPlanCreation(context.Context, string) (*Resource, error) {
	r := f.resource
	return &r, nil
}
func (f *planMaintenanceFixture) HasPendingStatePlan(context.Context, string) (bool, error) {
	return f.pending, nil
}
func (f *planMaintenanceFixture) HasPendingConfigurationChange(_ context.Context, id string) (bool, error) {
	f.configurationResourceID = id
	return f.pendingConfiguration, f.configurationError
}

func TestStatePlanCreationBlocksUnappliedConfigurationChange(t *testing.T) {
	for _, kind := range []string{"pending", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			f := maintenanceStore()
			f.pendingConfiguration = kind == "pending"
			if kind == "unavailable" {
				f.configurationError = ErrDependencyUnavailable
			}
			before := f.resource
			result, err := NewPlanMaintenanceService(f, nil).Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow)
			if err == nil || result != nil || f.plan != nil || f.resource != before || len(f.effects) != 0 {
				t.Fatalf("bypassed pending config: %+v %v", f, err)
			}
			if kind == "pending" && !errors.Is(err, ErrRevisionConflict) {
				t.Fatalf("expected conflict %v", err)
			}
			if kind == "unavailable" && !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatalf("expected dependency error %v", err)
			}
		})
	}
}
func (f *planMaintenanceFixture) NextPlanID(context.Context) (string, error) { return "81", nil }
func (f *planMaintenanceFixture) CreateStatePlan(_ context.Context, p *StatePlan) error {
	if f.pending {
		return ErrRevisionConflict
	}
	f.plan = p.copy()
	f.pending = true
	return nil
}
func (f *planMaintenanceFixture) LockPlanMaintenance(context.Context, string) (*StatePlan, error) {
	return f.plan.copy(), nil
}
func (f *planMaintenanceFixture) UpdateStatePlan(_ context.Context, p *StatePlan, expected uint64) error {
	if f.plan.Revision != expected {
		return ErrRevisionConflict
	}
	f.plan = p.copy()
	f.pending = p.Status == "PENDING"
	return nil
}
func (f *planMaintenanceFixture) AppendPlanMaintenanceEffect(_ context.Context, e PlanMaintenanceEffect) error {
	if f.failEffect {
		return errors.New("effect failed")
	}
	f.effects = append(f.effects, e)
	return nil
}
func maintenanceStore() *planMaintenanceFixture {
	return &planMaintenanceFixture{resource: *lifecycleResource()}
}
func maintenanceMeta(actor string) WriteMeta {
	return WriteMeta{ActorID: actor, Reason: "维护安排", RequestID: "trace"}
}
func TestPlanMaintenanceCreatesWithoutResourceChangeAndSchedulesReminder(t *testing.T) {
	f := maintenanceStore()
	before := f.resource
	s := NewPlanMaintenanceService(f, func() time.Time { return modelNow })
	p, err := s.Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow.Add(time.Hour))
	if err != nil || p == nil || f.resource != before || !f.pending || len(f.effects) != 1 || f.effects[0].Action != "CREATE" || !f.effects[0].ScheduleReminder {
		t.Fatalf("bad create %+v %v", f, err)
	}
	p.Reason = "changed"
	if f.plan.Reason != "维护安排" {
		t.Fatal("result aliases storage")
	}
	if _, err := s.Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("duplicate state dimension %v", err)
	}
}
func TestPlanMaintenanceOriginalInitiatorRevisesAndCancels(t *testing.T) {
	f := maintenanceStore()
	s := NewPlanMaintenanceService(f, nil)
	_, _ = s.Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow)
	f.resource.BuyerID = "8"
	if _, err := s.Revise(context.Background(), maintenanceMeta("8"), "81", 1, modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("new buyer impersonation %v", err)
	}
	p, err := s.Revise(context.Background(), maintenanceMeta("7"), "81", 1, modelNow.Add(time.Hour))
	if err != nil || p.Revision != 2 || f.effects[1].Before.Revision != 1 || !f.effects[1].ScheduleReminder {
		t.Fatalf("bad revise %+v %v", f, err)
	}
	if _, err := s.Cancel(context.Background(), maintenanceMeta("7"), "81", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale cancel %v", err)
	}
	p, err = s.Cancel(context.Background(), maintenanceMeta("7"), "81", 2)
	if err != nil || p.Status != "CANCELED" || f.pending || f.effects[2].ScheduleReminder || f.effects[2].Action != "CANCEL" {
		t.Fatalf("bad cancel %+v %v", f, err)
	}
}
func TestPlanMaintenanceFailuresRollbackPlanHistoryAndReminders(t *testing.T) {
	for _, operation := range []string{"create", "revise", "cancel"} {
		for _, failure := range []string{"effect", "commit"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				f := maintenanceStore()
				s := NewPlanMaintenanceService(f, nil)
				if operation != "create" {
					_, _ = s.Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow)
				}
				count := len(f.effects)
				f.failEffect = failure == "effect"
				f.failCommit = failure == "commit"
				var p *ResourcePlan
				var err error
				switch operation {
				case "create":
					p, err = s.Create(context.Background(), maintenanceMeta("7"), "11", 2, "OFFLINE", modelNow)
				case "revise":
					p, err = s.Revise(context.Background(), maintenanceMeta("7"), "81", 1, modelNow)
				case "cancel":
					p, err = s.Cancel(context.Background(), maintenanceMeta("7"), "81", 1)
				}
				if err == nil || p != nil || len(f.effects) != count || (operation == "create" && f.plan != nil) || (operation != "create" && (f.plan.Revision != 1 || f.plan.Status != "PENDING")) {
					t.Fatalf("partial write %+v %v", f, err)
				}
			})
		}
	}
}
func TestPlanMaintenanceRejectsStaleResourceOrWrongCreator(t *testing.T) {
	f := maintenanceStore()
	s := NewPlanMaintenanceService(f, nil)
	for _, actor := range []string{"7", "8"} {
		_, err := s.Create(context.Background(), maintenanceMeta(actor), "11", 1, "OFFLINE", modelNow)
		if err == nil || f.plan != nil || len(f.effects) != 0 {
			t.Fatalf("invalid creation %v", err)
		}
	}
}
