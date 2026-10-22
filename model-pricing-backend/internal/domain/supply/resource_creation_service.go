package supply

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type ResourceCreationContext struct {
	Proof    *SubmissionResourceProof
	Supplier ResourceSupplier
	ModelID  string
	Existing *Resource
}
type ResourceCreationEffect struct {
	Resource                                          Resource
	RowID, FlowID, ActorID, Action, Reason, RequestID string
	RowVersion, FlowRevision                          uint64
	OccurredAt                                        time.Time
}
type ResourceCreationTransaction interface {
	// Lock the B source and the identity key consistently, including absence;
	// enforce unique source and supplier/model/region at the database boundary.
	LockSubmissionCreation(context.Context, string, uint64) (*ResourceCreationContext, error)
	NextResourceID(context.Context) (string, error)
	InsertSubmissionResource(context.Context, *Resource) error
	// Attach B row.resource_id with CAS (never replace a different reference)
	// and append CREATE/CREATE_NOOP log in the same transaction.
	AppendResourceCreationEffect(context.Context, ResourceCreationEffect) error
}
type ResourceCreationUnitOfWork interface {
	WithinResourceCreation(context.Context, func(ResourceCreationTransaction) error) error
}
type ResourceCreationResult struct {
	Resource *Resource
	Created  bool
}
type ResourceCreationService struct {
	work ResourceCreationUnitOfWork
	now  func() time.Time
}

func NewResourceCreationService(work ResourceCreationUnitOfWork, clock func() time.Time) *ResourceCreationService {
	if clock == nil {
		clock = time.Now
	}
	return &ResourceCreationService{work: work, now: clock}
}
func (s *ResourceCreationService) CreateFromSubmission(ctx context.Context, meta WriteMeta, cmd ResourceCreationCommand) (*ResourceCreationResult, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(meta.ActorID) || !validID(cmd.RowID) || cmd.RowVersion == 0 || cmd.RowVersion > math.MaxInt64 {
		return nil, invalid("source", "valid authenticated actor and immutable source reference required")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"request_id", meta.RequestID, 64}, {"reason", meta.Reason, 500}} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return nil, invalid(field.name, "required value exceeds storage contract or contains invalid text")
		}
	}
	var result *ResourceCreationResult
	err := s.work.WithinResourceCreation(ctx, func(tx ResourceCreationTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		source, err := tx.LockSubmissionCreation(ctx, cmd.RowID, cmd.RowVersion)
		if err != nil {
			return err
		}
		if source == nil || source.Proof == nil {
			return ErrDependencyUnavailable
		}
		p := source.Proof
		if p.RowID != cmd.RowID || p.RowVersion != cmd.RowVersion {
			return ErrRevisionConflict
		}
		var id string
		if source.Existing != nil {
			id = source.Existing.ID
		} else {
			id, err = tx.NextResourceID(ctx)
			if err != nil {
				return err
			}
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		candidate, err := NewResourceFromSubmission(id, meta.ActorID, p, source.Supplier, source.ModelID, now)
		if err != nil {
			return err
		}
		action := "CREATE"
		created := true
		if existing := source.Existing; existing != nil {
			if existing.SupplierID != candidate.SupplierID || existing.StandardModelID != candidate.StandardModelID || existing.Region != candidate.Region || existing.OriginRowID != candidate.OriginRowID || existing.OriginRowVersion != candidate.OriginRowVersion {
				return ErrIdentityConflict
			}
			if existing.Revision == 0 || existing.Revision > math.MaxInt64 || !resourceStateKnown(existing.State) {
				return ErrReadContract
			}
			copy := *existing
			candidate = &copy
			created = false
			action = "CREATE_NOOP"
		} else {
			copy := *candidate
			if err := tx.InsertSubmissionResource(ctx, &copy); err != nil {
				return err
			}
		}
		if err := tx.AppendResourceCreationEffect(ctx, ResourceCreationEffect{Resource: *candidate, RowID: p.RowID, RowVersion: p.RowVersion, FlowID: p.FlowID, FlowRevision: p.FlowRevision, ActorID: meta.ActorID, Action: action, Reason: strings.TrimSpace(meta.Reason), RequestID: meta.RequestID, OccurredAt: now}); err != nil {
			return err
		}
		copy := *candidate
		result = &ResourceCreationResult{Resource: &copy, Created: created}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrReadContract
	}
	return result, nil
}
