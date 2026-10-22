package supply

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type TestEvidenceCommand struct{ ResourceID, ReportID string }
type TestEvidenceContext struct {
	Resource *Resource
	Report   *ResourceReportSummary
	TrustedB bool
}
type TestEvidenceEffect struct {
	Before, After                                 Resource
	ReportID, ActorID, Reason, RequestID, Outcome string
	Sequence, OriginRowVersion, TargetVersion     uint64
	OccurredAt                                    time.Time
}
type TestEvidenceTransaction interface {
	// Authenticate the trusted B call from context, lock the resource and read
	// the immutable requested report/task order inside this shared transaction.
	LockTestEvidence(context.Context, string, string) (*TestEvidenceContext, error)
	SaveTestEvidence(context.Context, *Resource, uint64) error
	AppendTestEvidenceEffect(context.Context, TestEvidenceEffect) error
}
type TestEvidenceUnitOfWork interface {
	WithinTestEvidence(context.Context, func(TestEvidenceTransaction) error) error
}
type TestEvidenceService struct {
	work TestEvidenceUnitOfWork
	now  func() time.Time
}

func NewTestEvidenceService(work TestEvidenceUnitOfWork, clock func() time.Time) *TestEvidenceService {
	if clock == nil {
		clock = time.Now
	}
	return &TestEvidenceService{work: work, now: clock}
}
func (s *TestEvidenceService) AttachTestEvidence(ctx context.Context, meta WriteMeta, cmd TestEvidenceCommand) (*ResourceEvidenceChange, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(meta.ActorID) || !validID(cmd.ResourceID) || !validID(cmd.ReportID) {
		return nil, invalid("evidence", "valid actor and resource/report references required")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"request_id", meta.RequestID, 64}, {"reason", meta.Reason, 500}} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return nil, invalid(field.name, "required value exceeds storage contract or contains invalid text")
		}
	}
	var result *ResourceEvidenceChange
	err := s.work.WithinTestEvidence(ctx, func(tx TestEvidenceTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		source, err := tx.LockTestEvidence(ctx, cmd.ResourceID, cmd.ReportID)
		if err != nil {
			return err
		}
		if source == nil {
			return ErrDependencyUnavailable
		}
		if !source.TrustedB {
			return ErrForbidden
		}
		if source.Resource == nil || source.Report == nil {
			return ErrDependencyUnavailable
		}
		if source.Resource.ID != cmd.ResourceID || source.Report.ID != cmd.ReportID {
			return ErrReadContract
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		change, err := ProposeTestEvidence(source.Resource, source.Report, now)
		if err != nil {
			return err
		}
		before := *source.Resource
		report := source.Report
		if change.Changed {
			copy := *change.Resource
			if err := tx.SaveTestEvidence(ctx, &copy, before.Revision); err != nil {
				return err
			}
		}
		effect := TestEvidenceEffect{Before: before, After: *change.Resource, ReportID: report.ID, Sequence: report.Sequence, OriginRowVersion: report.OriginRowVersion, TargetVersion: report.TargetVersion, ActorID: meta.ActorID, Reason: strings.TrimSpace(meta.Reason), RequestID: meta.RequestID, Outcome: change.Outcome, OccurredAt: now}
		if err := tx.AppendTestEvidenceEffect(ctx, effect); err != nil {
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
