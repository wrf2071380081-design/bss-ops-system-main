package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
)

func lockResourceUnderSupplier(ctx context.Context, tx *sql.Tx, state *ResourceStateStore, id string) (*supply.Resource, supply.ResourceSupplier, error) {
	var supplier supply.ResourceSupplier
	if !modelIdentity(id) {
		return nil, supplier, supply.ErrReadContract
	}
	var supplierID string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id::text FROM `+state.table("resource")+` WHERE id=$1`, id).Scan(&supplierID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supplier, supply.ErrNotFound
		}
		return nil, supplier, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text,name,region,status FROM `+state.table("supplier")+` WHERE id=$1 FOR UPDATE`, supplierID).Scan(&supplier.ID, &supplier.Name, &supplier.Region, &supplier.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supplier, supply.ErrReadContract
		}
		return nil, supplier, err
	}
	r, err := state.ForUpdate(ctx, tx, id)
	if err != nil {
		return nil, supplier, err
	}
	if r.SupplierID != supplierID {
		return nil, supplier, supply.ErrReadContract
	}
	return r, supplier, nil
}
