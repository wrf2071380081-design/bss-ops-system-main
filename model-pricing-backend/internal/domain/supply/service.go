package supply

import (
	"context"
	"slices"
	"time"
)

type ModelService struct {
	work    ModelUnitOfWork
	sources OfficialSourcePolicy
	now     func() time.Time
}

func NewModelService(work ModelUnitOfWork, sources OfficialSourcePolicy, clock func() time.Time) *ModelService {
	if clock == nil {
		clock = time.Now
	}
	return &ModelService{work: work, sources: sources, now: clock}
}

func (s *ModelService) checkWrite(meta WriteMeta, id string) error {
	if s == nil || s.work == nil {
		return ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return invalid("actor_id", "expected an authenticated staff bigint string")
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return err
	}
	if id != "" && !validID(id) {
		return invalid("id", "expected a positive bigint string")
	}
	return nil
}

func appendModelChange(ctx context.Context, tx ModelTransaction, before, after *StandardModel, meta WriteMeta, action string) error {
	if err := tx.AppendVersion(ctx, after.Clone()); err != nil {
		return err
	}
	return appendModelOperation(ctx, tx, before, after, meta, action, after.UpdatedAt)
}

func appendModelOperation(ctx context.Context, tx ModelTransaction, before, after *StandardModel, meta WriteMeta, action string, occurredAt time.Time) error {
	return tx.AppendOperation(ctx, ModelOperation{EntityType: "standard_model", EntityID: after.ID, Action: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: before.Clone(), After: after.Clone(), OccurredAt: occurredAt.UTC()})
}

func (s *ModelService) validateOfficialSources(ctx context.Context, model *StandardModel) error {
	if s.sources == nil {
		return ErrDependencyUnavailable
	}
	policySources := slices.Clone(model.Data.Sources)
	seenURLs := map[string]bool{}
	for _, source := range policySources {
		seenURLs[source.URL] = true
	}
	for _, price := range model.Data.PriceItems {
		if price.SourceURL != "" && !seenURLs[price.SourceURL] {
			policySources = append(policySources, Source{Label: price.Label, URL: price.SourceURL, Kind: "OFFICIAL_PRICING"})
			seenURLs[price.SourceURL] = true
		}
	}
	return s.sources.Validate(ctx, model.Data.Company, policySources)
}

func (s *ModelService) Create(ctx context.Context, meta WriteMeta, data ModelData) (*StandardModel, error) {
	if err := s.checkWrite(meta, ""); err != nil {
		return nil, err
	}
	now := s.now()
	if err := validateModelData(data, now); err != nil {
		return nil, err
	}
	var result *StandardModel
	err := s.work.Within(ctx, func(tx ModelTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		id, err := tx.NextID(ctx)
		if err != nil {
			return err
		}
		model, err := NewStandardModel(id, meta.ActorID, data, now)
		if err != nil {
			return err
		}
		if err = tx.Create(ctx, model.Clone()); err != nil {
			return err
		}
		if err = appendModelChange(ctx, tx, nil, model, meta, "CREATE"); err != nil {
			return err
		}
		result = model.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SaveManual creates or updates a model from the prototype's single "save and record"
// action. Incomplete data remains unverified; complete data is source-checked and
// attested by the maintainer. The model, immutable version and operation record share
// one transaction.
//
// 2026-11-24：按产品口径**取消并发/版本与来源白名单拦截**——当前阶段只保留
// 「身份唯一（厂商 + 官方 Model ID）」这一条硬约束。具体：
//   - 不再比较客户端传来的 expected_revision，一律以库里当前版本为基准自增
//     （同一模型多人/多标签页先后保存时后者覆盖前者，不再回 409「记录状态已变化」）；
//   - 不再校验官方资料 URL 是否落在厂商白名单里（资料完整性由 missing_fields 提示，
//     判断权交回人工）；
//   - 不再因为「资料完整但模型非 ACTIVE」拒绝保存。
//
// 身份重复仍由两处守：本流程入口的预检（api.identityTaken，给人话文案）+ 数据库
// 唯一索引 uk_standard_model_company_official（并发兜底）。
func (s *ModelService) SaveManual(ctx context.Context, meta WriteMeta, id string, expectedRevision uint64, data ModelData) (*StandardModel, error) {
	if err := s.checkWrite(meta, id); err != nil {
		return nil, err
	}
	// expectedRevision 自 2026-11-24 起不再参与判断（保留入参兼容调用方）。
	_ = expectedRevision
	creating := id == ""
	now := s.now()
	if err := validateModelData(data, now); err != nil {
		return nil, err
	}
	canonical := canonicalData(data)
	completeProbe := &StandardModel{Data: canonical}
	complete := len(completeProbe.MissingFields()) == 0
	var result *StandardModel
	err := s.work.Within(ctx, func(tx ModelTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		if creating {
			newID, err := tx.NextID(ctx)
			if err != nil {
				return err
			}
			model, err := NewStandardModel(newID, meta.ActorID, canonical, now)
			if err != nil {
				return err
			}
			if complete {
				if err = model.attestInitialVersion(meta.ActorID); err != nil {
					return err
				}
			}
			if err = tx.Create(ctx, model.Clone()); err != nil {
				return err
			}
			// 新建与补全是两件事：操作记录里要能分辨「新增标准模型」和「手动补全」。
			if err = appendModelChange(ctx, tx, nil, model, meta, "MANUAL_CREATE"); err != nil {
				return err
			}
			result = model.Clone()
			return nil
		}

		current, err := tx.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNotFound
		}
		// 以库里当前版本为基准：客户端 revision 过期也照常保存（后者覆盖前者）。
		baseRevision := current.Revision
		next, changed, _, err := current.ManuallySaved(meta, baseRevision, canonical, now)
		if err != nil {
			return err
		}
		if changed {
			if err = tx.Update(ctx, next.Clone(), baseRevision); err != nil {
				return err
			}
			if err = appendModelChange(ctx, tx, current, next, meta, "MANUAL_SAVE"); err != nil {
				return err
			}
		} else if err = appendModelOperation(ctx, tx, current, next, meta, "MANUAL_SAVE_NOOP", now); err != nil {
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

func (s *ModelService) Update(ctx context.Context, meta WriteMeta, id string, expectedRevision uint64, data ModelData) (*StandardModel, error) {
	if err := s.checkWrite(meta, id); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	now := s.now()
	var result *StandardModel
	err := s.work.Within(ctx, func(tx ModelTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		current, err := tx.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNotFound
		}
		next, changed, err := current.Updated(meta, expectedRevision, data, now)
		if err != nil {
			return err
		}
		if changed {
			if err = tx.Update(ctx, next.Clone(), expectedRevision); err != nil {
				return err
			}
			if err = appendModelChange(ctx, tx, current, next, meta, "UPDATE"); err != nil {
				return err
			}
		} else if err = appendModelOperation(ctx, tx, current, next, meta, "UPDATE_NOOP", now); err != nil {
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

func (s *ModelService) Verify(ctx context.Context, meta WriteMeta, id string, expectedRevision uint64, date string) (*StandardModel, error) {
	if err := s.checkWrite(meta, id); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	now := s.now()
	var result *StandardModel
	err := s.work.Within(ctx, func(tx ModelTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		current, err := tx.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrNotFound
		}
		next, changed, err := current.verified(meta, expectedRevision, date, now)
		if err != nil {
			return err
		}
		if err = s.validateOfficialSources(ctx, next); err != nil {
			return err
		}
		if changed {
			if err = tx.Update(ctx, next.Clone(), expectedRevision); err != nil {
				return err
			}
			if err = appendModelChange(ctx, tx, current, next, meta, "VERIFY"); err != nil {
				return err
			}
		} else if err = appendModelOperation(ctx, tx, current, next, meta, "VERIFY_NOOP", now); err != nil {
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
