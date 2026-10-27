package supplypg

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"model_bss/internal/domain/supply"
)

// mapSupplierStorageError translates database conditions into the supplier
// domain errors exposed by the API. Unknown errors are preserved for callers
// that need to handle transient storage failures.
func mapSupplierStorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return supply.ErrIdentityConflict
		case "40001":
			return supply.ErrRevisionConflict
		case "23503":
			return supply.ErrForbidden
		case "42703", "42P01", "42804", "42846", "42883":
			return supply.ErrDependencyUnavailable
		}
	}
	return err
}

func (s *SupplierStore) supplierAliasTableAvailable(ctx context.Context, tx *sql.Tx) (bool, error) {
	available, err := s.supplierAliasStructureAvailable(ctx, tx)
	if err != nil {
		return false, mapSupplierStorageError(err)
	}
	return available, nil
}
