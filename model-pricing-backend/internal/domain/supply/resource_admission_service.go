package supply

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type ResourceAdmissionEffect struct {
	Before, After                                         Resource
	Proof                                                 AdmissionProof
	ActorID, Reason, RequestID, CompletionNote, EventType string
	OccurredAt                                            time.Time
}
type ResourceAdmissionTransaction interface {
	// Lock resource, then linked B row/flow/node in the common lock order;
	// return consistent, sanitized facts from the same transaction.
	LockAdmission(context.Context, string) (*ResourceRecord, *AdmissionProof, error)
	SaveAdmittedResource(context.Context, *Resource, uint64) error
	// Persist B per-row completion, original proof/revisions, operation log,
	// completion evidence and resource outbox atomically. A single admitted row
	// never blindly completes its batch/node or marks supplier established.
	AppendAdmissionEffect(context.Context, ResourceAdmissionEffect) error
}
type ResourceAdmissionUnitOfWork interface {
	// Join the outer B/idempotency transaction. Commit before publishing receipt.
	WithinAdmission(context.Context, func(ResourceAdmissionTransaction) error) error
}
type ResourceAdmissionService struct {
	work ResourceAdmissionUnitOfWork
	now  func() time.Time
}

func NewResourceAdmissionService(work ResourceAdmissionUnitOfWork, clock func() time.Time) *ResourceAdmissionService {
	if clock == nil {
		clock = time.Now
	}
	return &ResourceAdmissionService{work: work, now: clock}
}
func (s *ResourceAdmissionService) Admit(ctx context.Context, meta WriteMeta, cmd ResourceAdmissionCommand) (*Resource, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(meta.ActorID) || !validID(cmd.ResourceID) || cmd.ExpectedRevision == 0 || cmd.ExpectedRevision >= math.MaxInt64 {
		return nil, invalid("admission", "valid actor, resource and incrementable revision required")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"request_id", meta.RequestID, 64}, {"reason", meta.Reason, 500}} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return nil, invalid(field.name, "required value exceeds storage contract or contains invalid text")
		}
	}
	if !cmd.ExternalConfirmed || strings.TrimSpace(cmd.CompletionNote) == "" {
		return nil, invalid("completion", "actual configuration confirmation and evidence required")
	}
	if !utf8.ValidString(cmd.CompletionNote) || strings.ContainsRune(cmd.CompletionNote, 0) {
		return nil, invalid("completion_note", "valid UTF-8 evidence without NUL required")
	}
	var result *Resource
	err := s.work.WithinAdmission(ctx, func(tx ResourceAdmissionTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		record, proof, err := tx.LockAdmission(ctx, cmd.ResourceID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		after, err := ProposeResourceAdmission(record, proof, meta.ActorID, cmd, now)
		if err != nil {
			return err
		}
		before := *record.Resource
		toSave := *after
		if err := tx.SaveAdmittedResource(ctx, &toSave, cmd.ExpectedRevision); err != nil {
			return err
		}
		effect := ResourceAdmissionEffect{Before: before, After: *after, Proof: *proof, ActorID: meta.ActorID, Reason: strings.TrimSpace(meta.Reason), RequestID: meta.RequestID, CompletionNote: strings.TrimSpace(cmd.CompletionNote), EventType: "RESOURCE_ADMITTED", OccurredAt: now}
		if err := tx.AppendAdmissionEffect(ctx, effect); err != nil {
			return err
		}
		copy := *after
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
