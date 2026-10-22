package supply

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type PlanExecutionEffect struct {
	BeforePlan, AfterPlan                                                                *StatePlan
	BeforeResource, AfterResource                                                        *Resource
	ActorID, Reason, RequestID, CompletionNote, FailureReason, EventType, AffectedStatus string
	OccurredAt                                                                           time.Time
}
type PlanExecutionTransaction interface {
	// Resolve the plan's resource ID, lock resource first then plan, and recheck
	// the relationship. All plan creation/edit/cancel paths use the same order.
	LockPlanExecution(context.Context, string) (*StatePlan, *ResourceRecord, error)
	SaveExecutedPlan(context.Context, *StatePlan, uint64) error
	SavePlanResource(context.Context, *Resource, uint64) error
	// Always log the attempt and retain completion evidence. On success also
	// append auto-notice/outbox with one unique source_event_id. BLOCKED has no
	// resource event/notice; keep failure reason as part of the plan history.
	AppendPlanExecutionEffect(context.Context, PlanExecutionEffect) error
}
type PlanExecutionUnitOfWork interface {
	// Join the caller transaction; no inner independent commits. A nil return
	// means staged successfully when an outer transaction is still in progress.
	WithinPlanExecution(context.Context, func(PlanExecutionTransaction) error) error
}
type PlanExecutionResult struct {
	Plan     *ResourcePlan
	Conflict bool
}
type PlanExecutionService struct {
	work PlanExecutionUnitOfWork
	now  func() time.Time
}

func NewPlanExecutionService(work PlanExecutionUnitOfWork, clock func() time.Time) *PlanExecutionService {
	if clock == nil {
		clock = time.Now
	}
	return &PlanExecutionService{work: work, now: clock}
}
func (s *PlanExecutionService) Execute(ctx context.Context, meta WriteMeta, id string, expected uint64, confirmed bool, note string) (*PlanExecutionResult, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return nil, err
	}
	if !validID(id) || !validID(meta.ActorID) || expected == 0 || expected > math.MaxInt64 {
		return nil, invalid("command", "identity, revision, reason and trace required")
	}
	if !confirmed || strings.TrimSpace(note) == "" || !utf8.ValidString(note) || strings.ContainsRune(note, 0) {
		return nil, invalid("completion", "actual operation confirmation and evidence required")
	}
	var result *PlanExecutionResult
	err := s.work.WithinPlanExecution(ctx, func(tx PlanExecutionTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		plan, record, err := tx.LockPlanExecution(ctx, id)
		if err != nil {
			return err
		}
		if plan == nil {
			return ErrNotFound
		}
		if plan.ID != id {
			return ErrReadContract
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		change, err := plan.ExecuteStatePlan(meta.ActorID, expected, record, confirmed, note, now)
		conflict := errors.Is(err, ErrRevisionConflict) && change != nil && change.Plan != nil && change.Plan.Status == "BLOCKED"
		if err != nil && !conflict {
			return err
		}
		if change == nil || change.Plan == nil {
			return ErrReadContract
		}
		beforeResource := *record.Resource
		if change.Resource != nil {
			if err := tx.SavePlanResource(ctx, change.Resource, beforeResource.Revision); err != nil {
				return err
			}
		}
		if err := tx.SaveExecutedPlan(ctx, change.Plan, expected); err != nil {
			return err
		}
		effect := PlanExecutionEffect{BeforePlan: plan.copy(), AfterPlan: change.Plan.copy(), BeforeResource: &beforeResource, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, CompletionNote: change.CompletionNote, FailureReason: change.FailureReason, OccurredAt: now}
		if change.Resource != nil {
			copy := *change.Resource
			effect.AfterResource = &copy
			effect.AffectedStatus = "PENDING"
			effect.EventType = "RESOURCE_OFFLINED"
			if plan.Kind == "ONLINE" {
				effect.EventType = "RESOURCE_ONLINED"
			}
		}
		if err := tx.AppendPlanExecutionEffect(ctx, effect); err != nil {
			return err
		}
		result = &PlanExecutionResult{Plan: change.Plan.ResourcePlan.Clone(), Conflict: conflict}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Conflict is an outcome, not an error: returning a business error to an
	// outer UOW would erase BLOCKED. The HTTP/idem assembler must commit the outer
	// transaction before mapping this outcome to409, and persist that response.
	return result, nil
}
