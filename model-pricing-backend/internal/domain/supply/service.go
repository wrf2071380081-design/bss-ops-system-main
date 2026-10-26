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
func (s *ModelService) SaveManual(ctx context.Context, meta WriteMeta, id string, expectedRevision uint64, data ModelData) (*StandardModel, error) {
	if err := s.checkWrite(meta, id); err != nil {
		return nil, err
	}
	creating := id == ""
	if creating && expectedRevision != 0 {
		return nil, ErrRevisionConflict
	}
	if !creating && expectedRevision == 0 {
		return nil, ErrRevisionConflict
	}
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
				if err = s.validateOfficialSources(ctx, model); err != nil {
					return err
				}
				if err = model.attestInitialVersion(meta.ActorID); err != nil {
					return err
				}
			}
			if err = tx.Create(ctx, model.Clone()); err != nil {
				return err
			}
			if err = appendModelChange(ctx, tx, nil, model, meta, "MANUAL_SAVE"); err != nil {
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
		next, changed, complete, err := current.ManuallySaved(meta.ActorID, expectedRevision, canonical, now)
		if err != nil {
			return err
		}
		if complete {
			if err = s.validateOfficialSources(ctx, next); err != nil {
				return err
			}
		}
		if changed {
			if err = tx.Update(ctx, next.Clone(), expectedRevision); err != nil {
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
		next, changed, err := current.Updated(meta.ActorID, expectedRevision, data, now)
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
		next, changed, err := current.verified(meta.ActorID, expectedRevision, date, now)
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
