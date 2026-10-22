package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"model_bss/internal/domain/supply"
)

type F12ReadStore struct {
	schema string
	read   ModelReadRunner
}

var _ supply.F12ReadStore = (*F12ReadStore)(nil)

func NewF12ReadStore(schema string, read ModelReadRunner) (*F12ReadStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &F12ReadStore{schema: schema, read: read}, nil
}

func (s *F12ReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func f12Offset(page, size int) (int, error) {
	if page < 1 || size < 1 || size > 100 || page > int(^uint(0)>>1)/size {
		return 0, supply.ErrReadContract
	}
	return (page - 1) * size, nil
}

func (s *F12ReadStore) withSupplier(ctx context.Context, tx *sql.Tx, subjectID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM `+s.table("supplier")+` WHERE id=$1`, subjectID).Scan(&id)
	if err == sql.ErrNoRows {
		return supply.ErrNotFound
	}
	if err != nil {
		return err
	}
	if id != subjectID {
		return supply.ErrReadContract
	}
	return nil
}

func (s *F12ReadStore) ListSupplierContracts(ctx context.Context, q supply.F12ContractQuery) ([]*supply.F12ContractRecord, int64, error) {
	if s == nil || s.read == nil {
		return nil, 0, supply.ErrDependencyUnavailable
	}
	offset, err := f12Offset(q.Page, q.Size)
	if err != nil || q.SubjectType != "SUPPLIER" || !modelIdentity(q.SubjectID) || (q.Status != "" && q.Status != "PROCESSING" && q.Status != "ARCHIVED") {
		return nil, 0, supply.ErrReadContract
	}
	var records []*supply.F12ContractRecord
	var total int64
	err = (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		if err := s.withSupplier(ctx, tx, q.SubjectID); err != nil {
			return err
		}
		where, args := f12ContractWhere(q)
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("contract")+` c WHERE `+where, args...).Scan(&total); err != nil {
			return err
		}
		queryArgs := append(append([]any{}, args...), q.Size, offset)
		rows, err := tx.QueryContext(ctx, `SELECT c.id::text,c.subject_type,c.subject_id::text,c.status,c.version,
(c.file_ref IS NOT NULL AND btrim(c.file_ref) <> ''),c.signed_at,c.created_at,c.updated_at
FROM `+s.table("contract")+` c WHERE `+where+fmt.Sprintf(` ORDER BY c.created_at DESC,c.id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		records = make([]*supply.F12ContractRecord, 0, q.Size)
		for rows.Next() {
			var row supply.F12ContractRecord
			var version int64
			var signed sql.NullTime
			if err := rows.Scan(&row.ID, &row.SubjectType, &row.SubjectID, &row.Status, &version, &row.HasAttachment, &signed, &row.CreatedAt, &row.UpdatedAt); err != nil {
				return err
			}
			if version < 1 {
				return supply.ErrReadContract
			}
			row.Version = uint64(version)
			if signed.Valid {
				t := signed.Time.UTC()
				row.SignedAt = &t
			}
			records = append(records, &row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func f12ContractWhere(q supply.F12ContractQuery) (string, []any) {
	where := `c.subject_type='SUPPLIER' AND c.subject_id=$1`
	args := []any{q.SubjectID}
	if q.Status != "" {
		args = append(args, q.Status)
		where += fmt.Sprintf(" AND c.status=$%d", len(args))
	}
	return where, args
}

func f12CreditWhere(q supply.F12CreditQuery) (string, []any) {
	where := `c.subject_type='SUPPLIER' AND c.subject_id=$1`
	args := []any{q.SubjectID}
	if q.Direction != "" {
		args = append(args, q.Direction)
		where += fmt.Sprintf(" AND c.direction=$%d", len(args))
	}
	if q.Currency != "" {
		args = append(args, q.Currency)
		where += fmt.Sprintf(" AND c.currency=$%d", len(args))
	}
	if q.AsOf != nil {
		args = append(args, q.AsOf.UTC())
		where += fmt.Sprintf(" AND c.created_at <= $%d", len(args))
	}
	return where, args
}

func scanF12Credit(scanner interface{ Scan(...any) error }) (*supply.F12CreditRecord, error) {
	var row supply.F12CreditRecord
	var version int64
	var effective sql.NullTime
	if err := scanner.Scan(&row.ID, &row.SubjectType, &row.SubjectID, &row.Direction, &row.Amount, &row.Currency, &version, &effective, &row.CreatedAt); err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, supply.ErrReadContract
	}
	row.Version = uint64(version)
	if effective.Valid {
		t := effective.Time.UTC()
		row.EffectiveAt = &t
	}
	return &row, nil
}

func (s *F12ReadStore) ListSupplierCredits(ctx context.Context, q supply.F12CreditQuery) ([]*supply.F12CreditRecord, int64, []*supply.F12CreditRecord, error) {
	if s == nil || s.read == nil {
		return nil, 0, nil, supply.ErrDependencyUnavailable
	}
	offset, err := f12Offset(q.Page, q.Size)
	if err != nil || q.SubjectType != "SUPPLIER" || !modelIdentity(q.SubjectID) || (q.Direction != "" && q.Direction != "SUPPLIER_TO_US") || (q.Currency != "" && len(q.Currency) != 3) {
		return nil, 0, nil, supply.ErrReadContract
	}
	var records, latest []*supply.F12CreditRecord
	var total int64
	err = (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, func(tx *sql.Tx) error {
		if err := s.withSupplier(ctx, tx, q.SubjectID); err != nil {
			return err
		}
		where, args := f12CreditWhere(q)
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("credit_line")+` c WHERE `+where, args...).Scan(&total); err != nil {
			return err
		}
		queryArgs := append(append([]any{}, args...), q.Size, offset)
		query := `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at
FROM ` + s.table("credit_line") + ` c WHERE ` + where + fmt.Sprintf(` ORDER BY c.created_at DESC,c.id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
		rows, err := tx.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return err
		}
		records = make([]*supply.F12CreditRecord, 0, q.Size)
		for rows.Next() {
			row, scanErr := scanF12Credit(rows)
			if scanErr != nil {
				rows.Close()
				return scanErr
			}
			records = append(records, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		latestQuery := `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at
FROM ` + s.table("credit_line") + ` c JOIN (
 SELECT direction,currency,MAX(version) AS version FROM ` + s.table("credit_line") + ` c WHERE ` + where + ` GROUP BY direction,currency
) current ON current.direction=c.direction AND current.currency=c.currency AND current.version=c.version
WHERE ` + where + ` ORDER BY c.direction,c.currency,c.id`
		latestArgs := append(append([]any{}, args...), args...)
		latestRows, err := tx.QueryContext(ctx, latestQuery, latestArgs...)
		if err != nil {
			return err
		}
		defer latestRows.Close()
		latest = make([]*supply.F12CreditRecord, 0)
		for latestRows.Next() {
			row, scanErr := scanF12Credit(latestRows)
			if scanErr != nil {
				return scanErr
			}
			latest = append(latest, row)
		}
		return latestRows.Err()
	})
	if err != nil {
		return nil, 0, nil, err
	}
	return records, total, latest, nil
}
