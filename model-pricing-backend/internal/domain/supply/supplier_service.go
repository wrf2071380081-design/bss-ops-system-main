package supply

import (
	"context"
	"math"
	"strings"
	"time"
)

type SupplierService struct {
	work SupplierUnitOfWork
	now  func() time.Time
}

func NewSupplierService(work SupplierUnitOfWork, clock func() time.Time) *SupplierService {
	if clock == nil {
		clock = time.Now
	}
	return &SupplierService{work: work, now: clock}
}
func (s *SupplierService) checkWrite(meta WriteMeta) error {
	if s == nil || s.work == nil {
		return ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return invalid("actor_id", "expected authenticated staff bigint string")
	}
	if strings.TrimSpace(meta.Reason) == "" {
		return invalid("reason", "an operation reason is required")
	}
	if strings.TrimSpace(meta.RequestID) == "" || len(meta.RequestID) > 128 {
		return invalid("request_id", "a valid trace identifier is required")
	}
	return nil
}
func validateSupplierDirectory(ctx context.Context, tx SupplierTransaction, actorID, currency string) error {
	active, err := tx.StaffActive(ctx, actorID)
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	enabled, err := tx.CurrencyEnabled(ctx, currency)
	if err != nil {
		return err
	}
	if !enabled {
		return invalid("settlement_currency", "currency is not enabled in the directory")
	}
	return nil
}
func appendSupplierOperation(ctx context.Context, tx SupplierTransaction, before, after *Supplier, meta WriteMeta, action string, now time.Time) error {
	return tx.AppendSupplierOperation(ctx, SupplierOperation{EntityType: "supplier", EntityID: after.ID, Action: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: before.Clone(), After: after.Clone(), OccurredAt: now.UTC()})
}
func (s *SupplierService) Update(ctx context.Context, meta WriteMeta, id string, expected uint64, data SupplierData) (*Supplier, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, invalid("id", "expected positive bigint string")
	}
	var result *Supplier
	err := s.work.Within(ctx, func(tx SupplierTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		current, err := tx.SupplierForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNotFound
		}
		if current.ID != id || !validID(current.BuyerID) || current.Revision == 0 || current.Revision > math.MaxInt64 || (current.Status != "ACTIVE" && current.Status != "INACTIVE") {
			return ErrReadContract
		}
		if current.BuyerID != meta.ActorID {
			return ErrForbidden
		}
		if expected == 0 || expected != current.Revision {
			return ErrRevisionConflict
		}
		canonical, err := canonicalSupplierData(data)
		if err != nil {
			return err
		}
		if err := validateSupplierDirectory(ctx, tx, meta.ActorID, canonical.SettlementCurrency); err != nil {
			return err
		}
		if canonical.Region != current.Data.Region {
			usage, err := tx.SupplierUsage(ctx, id)
			if err != nil {
				return err
			}
			if usage.LocksRegion() {
				return ErrSupplierRegionLocked
			}
		}
		next := current.Clone()
		now := s.now().UTC()
		action := "UPDATE_NOOP"
		if current.Data != canonical {
			if current.Revision == math.MaxInt64 {
				return ErrRevisionConflict
			}
			next.Data = canonical
			next.Revision++
			next.UpdatedAt = now
			action = "UPDATE"
			if err := tx.UpdateSupplier(ctx, next.Clone(), expected); err != nil {
				return err
			}
		}
		if err := appendSupplierOperation(ctx, tx, current, next, meta, action, now); err != nil {
			return err
		}
		result = next.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
