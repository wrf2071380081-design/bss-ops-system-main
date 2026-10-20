package supply

import (
	"context"
	"math"
	"strings"
	"time"
)

type PlanMaintenanceEffect struct {
	Action, ActorID, Reason, RequestID string
	Before, After                      *StatePlan
	ScheduleReminder                   bool
	OccurredAt                         time.Time
}
type PlanMaintenanceTransaction interface {
	// All operations lock resource first, then plan. Plan creation and execution
	// share this order and the same resource lock for the state-change dimension.
	ResourceForPlanCreation(context.Context, string) (*Resource, error)
	HasPendingStatePlan(context.Context, string) (bool, error)
	NextPlanID(context.Context) (string, error)
	// Enforce pending ONLINE/OFFLINE dimension uniqueness at the DB boundary.
	CreateStatePlan(context.Context, *StatePlan) error
	LockPlanMaintenance(context.Context, string) (*StatePlan, error)
	UpdateStatePlan(context.Context, *StatePlan, uint64) error
	// Append plan revision + operation log + reminder outbox in the same tx.
	// Invalidate older reminders on every revision. Schedule only if requested;
	// reminder workers verify plan ID/revision/status and never change resources.
	AppendPlanMaintenanceEffect(context.Context, PlanMaintenanceEffect) error
}
type PlanMaintenanceUnitOfWork interface {
	WithinPlanMaintenance(context.Context, func(PlanMaintenanceTransaction) error) error
}
type PlanMaintenanceService struct {
	work PlanMaintenanceUnitOfWork
	now  func() time.Time
}

func NewPlanMaintenanceService(work PlanMaintenanceUnitOfWork, clock func() time.Time) *PlanMaintenanceService {
	if clock == nil {
		clock = time.Now
	}
	return &PlanMaintenanceService{work: work, now: clock}
}
func (s *PlanMaintenanceService) validate(meta WriteMeta, id string, revision uint64) error {
	if s == nil || s.work == nil {
		return ErrDependencyUnavailable
	}
	if !validID(id) || !validID(meta.ActorID) || revision == 0 || revision > math.MaxInt64 || strings.TrimSpace(meta.Reason) == "" || strings.TrimSpace(meta.RequestID) == "" || len(meta.RequestID) > 128 {
		return invalid("command", "identity, revision, reason and trace required")
	}
	return nil
}
func (s *PlanMaintenanceService) Create(ctx context.Context, meta WriteMeta, resourceID string, expected uint64, kind string, due time.Time) (*ResourcePlan, error) {
	if err := s.validate(meta, resourceID, expected); err != nil {
		return nil, err
	}
	if due.IsZero() || (kind != "ONLINE" && kind != "OFFLINE") {
		return nil, invalid("plan", "state kind and due date required")
	}
	var result *ResourcePlan
	err := s.work.WithinPlanMaintenance(ctx, func(tx PlanMaintenanceTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		resource, err := tx.ResourceForPlanCreation(ctx, resourceID)
		if err != nil {
			return err
		}
		if resource == nil {
			return ErrNotFound
		}
		if resource.ID != resourceID {
			return ErrReadContract
		}
		if resource.BuyerID != meta.ActorID {
			return ErrForbidden
		}
		if resource.Revision != expected {
			return ErrRevisionConflict
		}
		pending, err := tx.HasPendingStatePlan(ctx, resourceID)
		if err != nil {
			return err
		}
		if pending {
			return ErrRevisionConflict
		}
		id, err := tx.NextPlanID(ctx)
		if err != nil {
			return err
		}
		if !validID(id) {
			return ErrReadContract
		}
		now := s.now().UTC()
		plan, err := NewStatePlan(id, meta.ActorID, resource, kind, due, meta.Reason, now)
		if err != nil {
			return err
		}
		if err := tx.CreateStatePlan(ctx, plan.copy()); err != nil {
			return err
		}
		if err := tx.AppendPlanMaintenanceEffect(ctx, PlanMaintenanceEffect{Action: "CREATE", ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, After: plan.copy(), ScheduleReminder: true, OccurredAt: now}); err != nil {
			return err
		}
		result = plan.ResourcePlan.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *PlanMaintenanceService) Revise(ctx context.Context, meta WriteMeta, id string, expected uint64, due time.Time) (*ResourcePlan, error) {
	if due.IsZero() {
		return nil, invalid("due_at", "due date required")
	}
	return s.change(ctx, meta, id, expected, "REVISE", due)
}
func (s *PlanMaintenanceService) Cancel(ctx context.Context, meta WriteMeta, id string, expected uint64) (*ResourcePlan, error) {
	return s.change(ctx, meta, id, expected, "CANCEL", time.Time{})
}
func (s *PlanMaintenanceService) change(ctx context.Context, meta WriteMeta, id string, expected uint64, action string, due time.Time) (*ResourcePlan, error) {
	if err := s.validate(meta, id, expected); err != nil {
		return nil, err
	}
	var result *ResourcePlan
	err := s.work.WithinPlanMaintenance(ctx, func(tx PlanMaintenanceTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		before, err := tx.LockPlanMaintenance(ctx, id)
		if err != nil {
			return err
		}
		if before == nil {
			return ErrNotFound
		}
		if before.ID != id {
			return ErrReadContract
		}
		var after *StatePlan
		if action == "CANCEL" {
			after, err = before.CanceledStatePlan(meta.ActorID, expected)
		} else {
			after, err = before.RevisedStatePlan(meta.ActorID, expected, due, meta.Reason)
		}
		if err != nil {
			return err
		}
		if err := tx.UpdateStatePlan(ctx, after.copy(), expected); err != nil {
			return err
		}
		if err := tx.AppendPlanMaintenanceEffect(ctx, PlanMaintenanceEffect{Action: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: before.copy(), After: after.copy(), ScheduleReminder: action == "REVISE", OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
		result = after.ResourcePlan.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
