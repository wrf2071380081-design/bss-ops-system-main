package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
)

type FormalConfigReadStore struct {
	schema string
	read   ModelReadRunner
	config *FormalConfigStore
}

var _ supply.FormalConfigReadStore = (*FormalConfigReadStore)(nil)

func NewFormalConfigReadStore(schema string, read ModelReadRunner, config *FormalConfigStore) (*FormalConfigReadStore, error) {
	if !modelSchemaName.MatchString(schema) || (config != nil && config.schema != schema) {
		return nil, mappingError("schema")
	}
	return &FormalConfigReadStore{schema: schema, read: read, config: config}, nil
}

func (s *FormalConfigReadStore) ListFormalConfigs(ctx context.Context, supplierID string) ([]*supply.FormalConfigView, error) {
	if !modelIdentity(supplierID) {
		return nil, supply.ErrReadContract
	}
	if s == nil || s.config == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	var views []*supply.FormalConfigView
	err := (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id::text FROM `+s.table("supplier")+` WHERE id=$1`, supplierID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return supply.ErrNotFound
		}
		if err != nil {
			return err
		}
		if existing != supplierID {
			return supply.ErrReadContract
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text FROM `+s.table("supplier_formal_config")+` WHERE supplier_id=$1 ORDER BY id ASC`, supplierID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !modelIdentity(id) {
				rows.Close()
				return supply.ErrReadContract
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		views = make([]*supply.FormalConfigView, 0, len(ids))
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			config, err := s.config.Current(ctx, tx, supplierID, id)
			if err != nil {
				return err
			}
			if config == nil || config.ID != id || config.SupplierID != supplierID {
				return supply.ErrReadContract
			}
			views = append(views, config.View())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return views, nil
}

func (s *FormalConfigReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }
