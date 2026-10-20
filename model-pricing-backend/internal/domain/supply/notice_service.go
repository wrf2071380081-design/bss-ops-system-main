package supply

import (
	"context"
	"strings"
	"time"
)

type NoticeReadOperation struct {
	EntityType, EntityID, Action, ActorID, Reason, RequestID string
	Before, After                                            *NoticeRead
	OccurredAt                                               time.Time
}
type NoticeTransaction interface {
	NoticeForUpdate(context.Context, string) (*Notice, error)
	InternalStaffExists(context.Context, string) (bool, error)
	GetReadForUpdate(context.Context, string, string) (*NoticeRead, error)
	CreateRead(context.Context, *NoticeRead) error
	AppendReadOperation(context.Context, NoticeReadOperation) error
}
type NoticeUnitOfWork interface {
	// Join an outer idempotency transaction; notice/read/log must commit together.
	Within(context.Context, func(NoticeTransaction) error) error
}
type NoticeReadReceipt struct {
	NoticeID string    `json:"notice_id"`
	StaffID  string    `json:"staff_id"`
	ReadAt   time.Time `json:"read_at"`
	Created  bool      `json:"created"`
}
type NoticeService struct {
	work NoticeUnitOfWork
	now  func() time.Time
}

func NewNoticeService(work NoticeUnitOfWork, clock func() time.Time) *NoticeService {
	if clock == nil {
		clock = time.Now
	}
	return &NoticeService{work: work, now: clock}
}
func (s *NoticeService) MarkRead(ctx context.Context, meta WriteMeta, id string) (*NoticeReadReceipt, error) {
	if s == nil || s.work == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(id) || !validID(meta.ActorID) {
		return nil, invalid("id", "expected notice and authenticated staff bigint strings")
	}
	if strings.TrimSpace(meta.Reason) == "" || strings.TrimSpace(meta.RequestID) == "" || len(meta.RequestID) > 128 {
		return nil, invalid("request_id", "trace and operation reason are required")
	}
	var result *NoticeReadReceipt
	err := s.work.Within(ctx, func(tx NoticeTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		notice, err := tx.NoticeForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !validNotice(notice) || notice.ID != id {
			return ErrReadContract
		}
		known, err := tx.InternalStaffExists(ctx, meta.ActorID)
		if err != nil {
			return err
		}
		if !known {
			return ErrForbidden
		}
		before, err := tx.GetReadForUpdate(ctx, id, meta.ActorID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		after := before.Clone()
		action := "READ_NOOP"
		created := false
		if before == nil {
			after = &NoticeRead{NoticeID: id, StaffID: meta.ActorID, ReadAt: now}
			if err := tx.CreateRead(ctx, after.Clone()); err != nil {
				return err
			}
			action = "READ"
			created = true
		} else if before.NoticeID != id || before.StaffID != meta.ActorID || before.ReadAt.IsZero() {
			return ErrReadContract
		}
		if err := tx.AppendReadOperation(ctx, NoticeReadOperation{EntityType: "notice", EntityID: id, Action: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: before.Clone(), After: after.Clone(), OccurredAt: now}); err != nil {
			return err
		}
		result = &NoticeReadReceipt{NoticeID: id, StaffID: meta.ActorID, ReadAt: after.ReadAt.UTC(), Created: created}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
