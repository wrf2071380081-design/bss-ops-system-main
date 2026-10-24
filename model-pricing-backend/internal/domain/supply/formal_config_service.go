package supply

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type FormalConfigService struct {
	work FormalConfigUnitOfWork
	urls FormalURLPolicy
	now  func() time.Time
}

func NewFormalConfigService(work FormalConfigUnitOfWork, urls FormalURLPolicy, clock func() time.Time) *FormalConfigService {
	if clock == nil {
		clock = time.Now
	}
	return &FormalConfigService{work: work, urls: urls, now: clock}
}
func (s *FormalConfigService) checkWrite(meta WriteMeta, supplierID string) error {
	if s == nil || s.work == nil {
		return ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return invalid("actor_id", "expected authenticated staff bigint string")
	}
	if !validID(supplierID) {
		return invalid("supplier_id", "expected positive bigint string")
	}
	if err := validateWriteMetaStorage(meta); err != nil {
		return err
	}
	return nil
}
func lockFormalSupplier(ctx context.Context, tx FormalConfigTransaction, supplierID, actorID string) (FormalConfigSupplier, error) {
	if tx == nil {
		return FormalConfigSupplier{}, ErrDependencyUnavailable
	}
	supplier, err := tx.SupplierForUpdate(ctx, supplierID)
	if err != nil {
		return supplier, err
	}
	if supplier.ID != supplierID || !validID(supplier.BuyerID) || (supplier.Region != "CN" && supplier.Region != "OVERSEAS") {
		return supplier, ErrReadContract
	}
	if supplier.BuyerID != actorID {
		return supplier, ErrForbidden
	}
	if supplier.Status != "ACTIVE" {
		return supplier, ErrInactive
	}
	return supplier, nil
}
func (s *FormalConfigService) validateInput(ctx context.Context, input FormalConfigInput, create bool) (FormalConfigInput, error) {
	input.Scope = strings.TrimSpace(input.Scope)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"scope", input.Scope, 255}, {"base_url", input.BaseURL, 512}} {
		if !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return input, invalid(field.name, "invalid text or native character limit exceeded")
		}
	}
	if input.Scope == "" {
		return input, invalid("scope", "scope is required")
	}
	if !publicURL(input.BaseURL) {
		return input, invalid("base_url", "expected HTTP(S) URL without embedded credentials")
	}
	if !input.ExternalConfirmed {
		return input, invalid("external_confirmed", "external platform operation must be explicitly confirmed")
	}
	if (create && input.APIKey == nil) || (input.APIKey != nil && strings.TrimSpace(*input.APIKey) == "") {
		return input, invalid("api_key", "a nonempty key is required; omission on update preserves it")
	}
	if input.APIKey != nil && (!utf8.ValidString(*input.APIKey) || strings.ContainsRune(*input.APIKey, 0) || utf8.RuneCountInString(*input.APIKey) > 512) {
		return input, invalid("api_key", "invalid text or native storage character limit exceeded")
	}
	if s.urls == nil {
		return input, ErrDependencyUnavailable
	}
	if err := s.urls.ValidateFormalURL(ctx, input.BaseURL); err != nil {
		return input, invalid("base_url", "URL is not permitted by environment policy")
	}
	return input, nil
}
func appendConfigOperation(ctx context.Context, tx FormalConfigTransaction, before, after *FormalConfig, meta WriteMeta, action string, now time.Time) error {
	return tx.AppendConfigOperation(ctx, FormalConfigOperation{EntityType: "supplier_formal_config", EntityID: after.ID, SupplierID: after.SupplierID,
		Action: action, ActorID: meta.ActorID, Reason: meta.Reason, RequestID: meta.RequestID, Before: before.View(), After: after.View(), OccurredAt: now.UTC()})
}
func (s *FormalConfigService) Create(ctx context.Context, meta WriteMeta, supplierID string, input FormalConfigInput) (*FormalConfigView, error) {
	if err := s.checkWrite(meta, supplierID); err != nil {
		return nil, err
	}
	var result *FormalConfigView
	err := s.work.Within(ctx, func(tx FormalConfigTransaction) error {
		supplier, err := lockFormalSupplier(ctx, tx, supplierID, meta.ActorID)
		if err != nil {
			return err
		}
		input, err = s.validateInput(ctx, input, true)
		if err != nil {
			return err
		}
		id, err := tx.NextID(ctx)
		if err != nil {
			return err
		}
		if !validID(id) {
			return ErrReadContract
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		config := &FormalConfig{ID: id, SupplierID: supplierID, Region: supplier.Region, Scope: input.Scope, BaseURL: input.BaseURL, APIKey: *input.APIKey, Status: "ACTIVE", Revision: 1,
			CreatedBy: meta.ActorID, UpdatedBy: meta.ActorID, CreatedAt: now, UpdatedAt: now, ConfirmedBy: meta.ActorID, ConfirmedAt: now, ConfirmedRevision: 1}
		if err := tx.CreateConfig(ctx, config.Clone()); err != nil {
			return err
		}
		if err := appendConfigOperation(ctx, tx, nil, config, meta, "CREATE", now); err != nil {
			return err
		}
		result = config.View()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func lockFormalConfig(ctx context.Context, tx FormalConfigTransaction, supplier FormalConfigSupplier, id string, expected uint64) (*FormalConfig, error) {
	if !validID(id) {
		return nil, invalid("config_id", "expected positive bigint string")
	}
	config, err := tx.ConfigForUpdate(ctx, supplier.ID, id)
	if err != nil {
		return nil, err
	}
	if config == nil || config.ID != id || config.SupplierID != supplier.ID {
		return nil, ErrNotFound
	}
	if config.Region != supplier.Region || config.Revision == 0 || config.Revision > math.MaxInt64 || (config.Status != "ACTIVE" && config.Status != "INACTIVE") || config.APIKey == "" {
		return nil, ErrReadContract
	}
	if expected == 0 || config.Revision != expected {
		return nil, ErrRevisionConflict
	}
	return config.Clone(), nil
}
func (s *FormalConfigService) Update(ctx context.Context, meta WriteMeta, supplierID, configID string, expected uint64, input FormalConfigInput) (*FormalConfigView, error) {
	if err := s.checkWrite(meta, supplierID); err != nil {
		return nil, err
	}
	var result *FormalConfigView
	err := s.work.Within(ctx, func(tx FormalConfigTransaction) error {
		supplier, err := lockFormalSupplier(ctx, tx, supplierID, meta.ActorID)
		if err != nil {
			return err
		}
		current, err := lockFormalConfig(ctx, tx, supplier, configID, expected)
		if err != nil {
			return err
		}
		if current.Status != "ACTIVE" {
			return ErrConfigInactive
		}
		input, err = s.validateInput(ctx, input, false)
		if err != nil {
			return err
		}
		if current.LegacyEncrypted && input.APIKey == nil {
			return invalid("api_key", "replace the previously encrypted key before saving this configuration")
		}
		next := current.Clone()
		now := s.now().UTC().Truncate(time.Microsecond)
		action := "UPDATE_NOOP"
		if input.Scope != current.Scope || input.BaseURL != current.BaseURL || input.APIKey != nil {
			if current.Revision == math.MaxInt64 {
				return ErrRevisionConflict
			}
			if input.APIKey != nil {
				next.APIKey = *input.APIKey
				next.LegacyEncrypted = false
			}
			next.Scope = input.Scope
			next.BaseURL = input.BaseURL
			next.Revision++
			next.UpdatedAt = now
			next.UpdatedBy = meta.ActorID
			next.ConfirmedRevision = next.Revision
			next.ConfirmedBy = meta.ActorID
			next.ConfirmedAt = now
			action = "UPDATE"
			if err := tx.UpdateConfig(ctx, next.Clone(), expected); err != nil {
				return err
			}
		}
		if err := appendConfigOperation(ctx, tx, current, next, meta, action, now); err != nil {
			return err
		}
		result = next.View()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *FormalConfigService) Disable(ctx context.Context, meta WriteMeta, supplierID, configID string, expected uint64) (*FormalConfigView, error) {
	if err := s.checkWrite(meta, supplierID); err != nil {
		return nil, err
	}
	var result *FormalConfigView
	err := s.work.Within(ctx, func(tx FormalConfigTransaction) error {
		supplier, err := lockFormalSupplier(ctx, tx, supplierID, meta.ActorID)
		if err != nil {
			return err
		}
		current, err := lockFormalConfig(ctx, tx, supplier, configID, expected)
		if err != nil {
			return err
		}
		referenced, err := tx.HasResourceReferences(ctx, configID)
		if err != nil {
			return err
		}
		if referenced {
			return ErrConfigInUse
		}
		next := current.Clone()
		now := s.now().UTC().Truncate(time.Microsecond)
		action := "DISABLE_NOOP"
		if current.Status != "INACTIVE" {
			if current.Revision == math.MaxInt64 {
				return ErrRevisionConflict
			}
			next.Status = "INACTIVE"
			next.Revision++
			next.UpdatedAt = now
			next.UpdatedBy = meta.ActorID
			action = "DISABLE"
			if err := tx.UpdateConfig(ctx, next.Clone(), expected); err != nil {
				return err
			}
		}
		if err := appendConfigOperation(ctx, tx, current, next, meta, action, now); err != nil {
			return err
		}
		result = next.View()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
