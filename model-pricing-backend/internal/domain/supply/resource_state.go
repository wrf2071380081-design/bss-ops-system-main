package supply

import (
	"context"
	"math"
	"strings"
	"time"
)

// This is a direct buyer command. Plan execution requires separate server-side
// initiator authorization; it cannot bypass this API with a client plan_id.
type ResourceStateCommand struct {
	ResourceID, Action, CompletionNote string
	ExpectedRevision                   uint64
	ExternalConfirmed                  bool
}
type ResourceStateEffect struct {
	EventType, ActorID, Reason, RequestID, CompletionNote, AffectedStatus string
	Before, After                                                         Resource
	OccurredAt                                                            time.Time
}
type ResourceStateTransaction interface {
	// Lock the resource and read current model, supplier, config and selected B
	// evidence consistently. Return sanitized facts, never API credentials.
	ResourceStateForUpdate(context.Context, string) (*ResourceRecord, error)
	// Check PENDING/EXECUTING plans for the shared ONLINE/OFFLINE dimension.
	// Plan creation/cancellation/execution must acquire the same resource lock
	// before changing that dimension, preventing a check-then-create race.
	HasPendingStatePlan(context.Context, string) (bool, error)
	UpdateResourceState(context.Context, *Resource, uint64) error
	// Append operation log, auto-notice and outbox as one atomic effect. Allocate
	// event_id once, use it as notice.source_event_id, and deduplicate retries.
	// PENDING impact means unknown, not zero; C computes impact asynchronously.
	AppendResourceStateEffect(context.Context, ResourceStateEffect) error
}
type ResourceStateUnitOfWork interface {
	WithinResourceState(context.Context, func(ResourceStateTransaction) error) error
}
type ResourceStateService struct {
	work ResourceStateUnitOfWork
	now  func() time.Time
}

func NewResourceStateService(work ResourceStateUnitOfWork, clock func() time.Time) *ResourceStateService {
	if clock == nil {
		clock = time.Now
	}
	return &ResourceStateService{work: work, now: clock}
}
func (s *ResourceStateService) Change(ctx context.Context, meta WriteMeta, cmd ResourceStateCommand) (*Resource, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return nil, err
	}
	if !validID(cmd.ResourceID) || !validID(meta.ActorID) || cmd.ExpectedRevision == 0 || cmd.ExpectedRevision > math.MaxInt64 {
		return nil, invalid("command", "identity, revision, reason and trace required")
	}
	if cmd.Action != "OFFLINE" && cmd.Action != "ONLINE" {
		return nil, invalid("action", "expected OFFLINE or ONLINE")
	}
	if !cmd.ExternalConfirmed || strings.TrimSpace(cmd.CompletionNote) == "" {
		return nil, invalid("external_confirmed", "actual platform operation and completion evidence required")
	}
	var result *Resource
	err := s.work.WithinResourceState(ctx, func(tx ResourceStateTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		record, err := tx.ResourceStateForUpdate(ctx, cmd.ResourceID)
		if err != nil {
			return err
		}
		if record == nil || record.Resource == nil {
			return ErrNotFound
		}
		r := record.Resource
		if r.ID != cmd.ResourceID || !validID(r.BuyerID) || r.Revision == 0 || r.Revision > math.MaxInt64 || !resourceStateKnown(r.State) || (r.Region != "CN" && r.Region != "OVERSEAS") {
			return ErrReadContract
		}
		if r.BuyerID != meta.ActorID {
			return ErrForbidden
		}
		if r.Revision != cmd.ExpectedRevision || r.Revision == math.MaxInt64 {
			return ErrRevisionConflict
		}
		if !r.EverAdmitted || (cmd.Action == "OFFLINE" && r.State != "ADMITTED") || (cmd.Action == "ONLINE" && r.State != "OFFLINE") {
			return ErrRevisionConflict
		}
		pending, err := tx.HasPendingStatePlan(ctx, r.ID)
		if err != nil {
			return err
		}
		if pending {
			return ErrRevisionConflict
		}
		before := *r
		after := before
		event := "RESOURCE_OFFLINED"
		after.State = "OFFLINE"
		if cmd.Action == "ONLINE" {
			after.State = "ADMITTED"
			event = "RESOURCE_ONLINED"
			// Re-evaluate readiness on a copy in the proposed state. Do not discard
			// model/config/test blockers or rewrite the immutable selected report.
			proposed := *record
			candidate := after
			proposed.Resource = &candidate
			snapshot, err := BuildResourceSnapshot(&proposed)
			if err != nil {
				return err
			}
			if !snapshot.QuoteReadiness.Eligible {
				return ErrRevisionConflict
			}
		}
		now := s.now().UTC()
		after.Revision++
		after.UpdatedAt = now
		if err := tx.UpdateResourceState(ctx, &after, cmd.ExpectedRevision); err != nil {
			return err
		}
		if err := tx.AppendResourceStateEffect(ctx, ResourceStateEffect{EventType: event, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, CompletionNote: strings.TrimSpace(cmd.CompletionNote), AffectedStatus: "PENDING", Before: before, After: after, OccurredAt: now}); err != nil {
			return err
		}
		copy := after
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
