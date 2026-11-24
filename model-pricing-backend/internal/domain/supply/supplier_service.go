package supply

import (
	"context"
	"math"
	"reflect"
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
	if err := validateWriteMetaStorage(meta); err != nil {
		return err
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
func (s *SupplierService) Create(ctx context.Context, meta WriteMeta, data SupplierData) (*Supplier, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	var result *Supplier
	err := s.work.Within(ctx, func(tx SupplierTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		if err := validateSupplierDirectory(ctx, tx, meta.ActorID, data.SettlementCurrency); err != nil {
			return err
		}
		id, err := tx.NextID(ctx)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		created, err := NewSupplier(id, meta.ActorID, data, now)
		if err != nil {
			return err
		}
		if err := tx.CreateSupplier(ctx, created.Clone()); err != nil {
			return err
		}
		if err := tx.CreateSupplierAssignment(ctx, created.ID, meta.ActorID, meta.ActorID, now); err != nil {
			return err
		}
		if err := appendSupplierOperation(ctx, tx, nil, created, meta, "CREATE", now); err != nil {
			return err
		}
		result = created.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
		// 审计链不可信（Revision==0 或没有归属采购人）的档案：允许持 M3:E 的内部人员
		// 直接维护，并在保存时重建一条基线流水（BASELINE）。这类档案的典型来源是
		// 供应商自助注册——注册那一刻既没有内部员工能当归属人，也还没有任何业务流水。
		// 2026-11-23 业务明确：内部不做这么细的权限区分，缺历史不该把人挡在编辑入口外。
		baseline := current.Revision == 0 || !validID(current.BuyerID)
		if baseline && !meta.CanMaintain {
			return ErrReadContract
		}
		if current.ID != id || current.Revision > math.MaxInt64 || (current.Status != "ACTIVE" && current.Status != "FROZEN") {
			return ErrReadContract
		}
		// 归属采购人本人，或上层已判定其具备 M3:E 维护授权（WriteMeta.CanMaintain）。
		if current.BuyerID != meta.ActorID && !meta.CanMaintain {
			return ErrForbidden
		}
		// 断链档案没有可信修订号，改用「必须显式传 0」表达「我知道这条链是断的」；
		// 完整的档案仍按原口径做乐观锁。
		if baseline {
			if expected != 0 {
				return ErrRevisionConflict
			}
		} else if expected == 0 || expected != current.Revision {
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
		now := s.now().UTC().Truncate(time.Microsecond)
		action := "UPDATE_NOOP"
		if !reflect.DeepEqual(current.Data, canonical) {
			if current.Revision == math.MaxInt64 {
				return ErrRevisionConflict
			}
			next.Data = canonical
			next.Aliases = SupplierAliasesAfterRename(current.Aliases, current.Data.Name, canonical.Name)
			if baseline {
				// 断链档案：用 rev=1 的 BASELINE 重建基线（verify 只接受 BASELINE rev>=1，
				// 而 UPDATE 要求 rev>=2，这里没有可比的前序版本）。
				next.Revision = 1
				action = "BASELINE"
			} else {
				next.Revision++
				action = "UPDATE"
			}
			next.UpdatedAt = now
			if err := tx.UpdateSupplier(ctx, next.Clone(), expected); err != nil {
				return err
			}
		} else if baseline {
			// 断链且内容没变：无事可做。这里不写流水——rev=0 的 NOOP 快照不合法，
			// 写进去只会把档案钉得更死。
			result = current.Clone()
			return nil
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
