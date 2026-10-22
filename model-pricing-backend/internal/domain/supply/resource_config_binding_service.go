package supply

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type ResourceConfigBindingContext struct {
	Resource *Resource
	Config   *ResourceFormalConfig
	Proof    *AdmissionProof
}
type ResourceConfigBindingEffect struct {
	Before, After                      Resource
	Proof                              AdmissionProof
	ActorID, Reason, RequestID, Action string
	OccurredAt                         time.Time
}
type ResourceConfigBindingTransaction interface {
	// Lock supplier/resource/config and B facts in the same order used by
	// formal-config update/disable and admission. Missing dependencies fail.
	LockResourceConfigBinding(context.Context, string, string) (*ResourceConfigBindingContext, error)
	SaveResourceConfigBinding(context.Context, *Resource, uint64) error
	AppendResourceConfigBindingEffect(context.Context, ResourceConfigBindingEffect) error
}
type ResourceConfigBindingUnitOfWork interface {
	WithinResourceConfigBinding(context.Context, func(ResourceConfigBindingTransaction) error) error
}
type ResourceConfigBindingService struct {
	work ResourceConfigBindingUnitOfWork
	now  func() time.Time
}

func NewResourceConfigBindingService(work ResourceConfigBindingUnitOfWork, clock func() time.Time) *ResourceConfigBindingService {
	if clock == nil {
		clock = time.Now
	}
	return &ResourceConfigBindingService{work: work, now: clock}
}
func (s *ResourceConfigBindingService) BindFormalConfig(ctx context.Context, meta WriteMeta, cmd ResourceConfigBindingCommand) (*ResourceConfigBindingChange, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(meta.ActorID) || !validID(cmd.ResourceID) || !validID(cmd.ConfigID) {
		return nil, invalid("binding", "valid authenticated actor and references required")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"request_id", meta.RequestID, 64}, {"reason", meta.Reason, 500}} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return nil, invalid(field.name, "required value exceeds storage contract or contains invalid text")
		}
	}
	var result *ResourceConfigBindingChange
	err := s.work.WithinResourceConfigBinding(ctx, func(tx ResourceConfigBindingTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		source, err := tx.LockResourceConfigBinding(ctx, cmd.ResourceID, cmd.ConfigID)
		if err != nil {
			return err
		}
		if source == nil {
			return ErrDependencyUnavailable
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		change, err := ProposeResourceConfigBinding(source.Resource, source.Config, source.Proof, meta.ActorID, cmd, now)
		if err != nil {
			return err
		}
		before := *source.Resource
		proof := *source.Proof
		action := "BIND_NOOP"
		if change.Changed {
			action = "BIND"
			copy := *change.Resource
			if err := tx.SaveResourceConfigBinding(ctx, &copy, cmd.ExpectedRevision); err != nil {
				return err
			}
		}
		if err := tx.AppendResourceConfigBindingEffect(ctx, ResourceConfigBindingEffect{Before: before, After: *change.Resource, Proof: proof, ActorID: meta.ActorID, Reason: strings.TrimSpace(meta.Reason), RequestID: meta.RequestID, Action: action, OccurredAt: now}); err != nil {
			return err
		}
		result = change
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
