package supply

import (
	"context"
	"math"
	"time"
)

// Source is a reference, never an authorization flag supplied by an HTTP caller.
type CoefficientSource struct {
	Kind, ID string
	Revision uint64
}
type CoefficientSourceProof struct {
	Authorized               bool
	ResourceID, Coefficient  string
	ExpectedResourceRevision uint64
}
type ResourceCoefficientOperation struct {
	ResourceID, ActorID, Reason, RequestID, Action string
	Source                                         CoefficientSource
	Before, After                                  Resource
	OccurredAt                                     time.Time
}

// The adapter allocates an event_id and persists the event in the shared outbox.
// C consumes the facts; this command does not rewrite existing customer prices.
type ResourceCoefficientEvent struct {
	ResourceID, Region, ActorID, Reason, RequestID, BeforeCoefficient, AfterCoefficient string
	Revision                                                                            uint64
	Source                                                                              CoefficientSource
	OccurredAt                                                                          time.Time
}
type ResourceCoefficientTransaction interface {
	ResourceForUpdate(context.Context, string) (*Resource, error)
	// Resolve the immutable submission row or pending plan revision on the server,
	// verify actor and exact resource/revision/amount in this same transaction.
	// PLAN checks the original initiator, not the resource's current buyer.
	// Lock order must be coordinated with B/F13. No network calls under these locks.
	VerifyCoefficientSource(context.Context, CoefficientSource, string, *Resource) (CoefficientSourceProof, error)
	UpdateCoefficient(context.Context, *Resource, uint64) error
	AppendCoefficientOperation(context.Context, ResourceCoefficientOperation) error
	AppendCoefficientEvent(context.Context, ResourceCoefficientEvent) error
}
type ResourceCoefficientUnitOfWork interface {
	// Join the caller's transaction; resource, log, outbox and source completion
	// must commit together. Never independently commit inside this adapter.
	WithinCoefficient(context.Context, func(ResourceCoefficientTransaction) error) error
}
type ResourceCoefficientService struct {
	work ResourceCoefficientUnitOfWork
	now  func() time.Time
}

func NewResourceCoefficientService(work ResourceCoefficientUnitOfWork, clock func() time.Time) *ResourceCoefficientService {
	if clock == nil {
		clock = time.Now
	}
	return &ResourceCoefficientService{work: work, now: clock}
}
func (s *ResourceCoefficientService) Change(ctx context.Context, meta WriteMeta, id string, expected uint64, amount string, source CoefficientSource) (*Resource, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return nil, err
	}
	if !validID(id) || !validID(meta.ActorID) || expected == 0 || expected > math.MaxInt64 {
		return nil, invalid("command", "identity, revision, reason and request trace required")
	}
	value, err := resourceCoefficient(amount)
	if err != nil {
		return nil, invalid("coefficient", "expected decimal (0,1] with at most four places")
	}
	if (source.Kind != "SUBMISSION" && source.Kind != "PLAN") || !validID(source.ID) || source.Revision == 0 || source.Revision > math.MaxInt64 {
		return nil, invalid("source", "expected versioned submission or plan reference")
	}
	var result *Resource
	err = s.work.WithinCoefficient(ctx, func(tx ResourceCoefficientTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		current, err := tx.ResourceForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNotFound
		}
		if current.ID != id || !validID(current.BuyerID) || current.Revision == 0 || current.Revision > math.MaxInt64 || !resourceStateKnown(current.State) || (current.Region != "CN" && current.Region != "OVERSEAS") {
			return ErrReadContract
		}
		if current.Revision != expected {
			return ErrRevisionConflict
		}
		before := *current
		old, err := resourceCoefficient(before.Coefficient)
		if err != nil {
			return ErrReadContract
		}
		// Give the adapter a copy so it cannot mutate the loaded resource as a side effect.
		proofResource := before
		proof, err := tx.VerifyCoefficientSource(ctx, source, meta.ActorID, &proofResource)
		if err != nil {
			return err
		}
		if !proof.Authorized {
			return ErrForbidden
		}
		approved, err := resourceCoefficient(proof.Coefficient)
		if err != nil || proof.ResourceID != id || proof.ExpectedResourceRevision != expected || !approved.Equal(value) {
			return ErrRevisionConflict
		}
		after := before
		action := "CHANGE_COEFFICIENT_NOOP"
		now := s.now().UTC()
		if !old.Equal(value) {
			if before.Revision == math.MaxInt64 {
				return ErrRevisionConflict
			}
			after.Coefficient = value.StringFixed(4)
			after.Revision++
			after.UpdatedAt = now
			action = "CHANGE_COEFFICIENT"
			if err := tx.UpdateCoefficient(ctx, &after, expected); err != nil {
				return err
			}
		}
		if err := tx.AppendCoefficientOperation(ctx, ResourceCoefficientOperation{ResourceID: id, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Action: action, Source: source, Before: before, After: after, OccurredAt: now}); err != nil {
			return err
		}
		if action == "CHANGE_COEFFICIENT" {
			if err := tx.AppendCoefficientEvent(ctx, ResourceCoefficientEvent{ResourceID: id, Region: after.Region, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, BeforeCoefficient: old.StringFixed(4), AfterCoefficient: after.Coefficient, Revision: after.Revision, Source: source, OccurredAt: now}); err != nil {
				return err
			}
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
