package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"model_bss/internal/domain/supply"
	"strings"
	"unicode/utf8"
)

type ModelReadRunner func(context.Context, func(*sql.Tx) error) error
type ModelReadStore struct {
	schema string
	read   ModelReadRunner
}

var _ supply.ModelReadStore = (*ModelReadStore)(nil)

func NewModelReadStore(schema string, read ModelReadRunner) (*ModelReadStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &ModelReadStore{schema, read}, nil
}

// ModelReadTransactions provides one read-only repeatable snapshot per store
// method. A caller borrowing its own transaction must provide the same guarantee.
func ModelReadTransactions(db *sql.DB) ModelReadRunner {
	return func(ctx context.Context, run func(*sql.Tx) error) error {
		if db == nil || run == nil {
			return supply.ErrDependencyUnavailable
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = run(tx); err != nil {
			return err
		}
		return tx.Commit()
	}
}
func (s *ModelReadStore) within(ctx context.Context, run func(*sql.Tx) error) error {
	if s == nil || s.read == nil {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.read(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		return run(tx)
	})
}
func (s *ModelReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }
func (s *ModelReadStore) current(ctx context.Context, tx *sql.Tx, id string) (*supply.StandardModel, error) {
	return scanStandardModel(tx.QueryRowContext(ctx, `SELECT `+modelProjection(s.table("standard_model_version"))+` FROM `+s.table("standard_model")+` m WHERE m.id=$1`, id))
}
func (s *ModelReadStore) GetModel(ctx context.Context, id string) (*supply.StandardModel, error) {
	if !modelIdentity(id) {
		return nil, mappingError("id")
	}
	var model *supply.StandardModel
	err := s.within(ctx, func(tx *sql.Tx) error { var err error; model, err = s.current(ctx, tx, id); return err })
	if err != nil {
		return nil, err
	}
	return model, nil
}
func modelPageValid(page, size int) bool {
	return page >= 1 && size >= 1 && size <= 100 && page <= math.MaxInt/size
}
func (s *ModelReadStore) listQuery(q supply.ModelListQuery) (string, string, []any, error) {
	if !modelPageValid(q.Page, q.Size) || !q.NullsLast || q.TieBreakBy != "id" || !utf8.ValidString(q.Keyword) || strings.ContainsRune(q.Keyword, 0) {
		return "", "", nil, supply.ErrReadContract
	}
	verify := modelVersionValue(s.table("standard_model_version"), `v.snapshot->'model'->>'verify_status'`)
	columns := map[string]string{"id": "m.id", "name": "m.name", "company": "m.company", "model_type": "m.model_type", "verify_status": verify, "checked_at": "m.checked_at", "updated_at": "m.updated_at"}
	col, ok := columns[q.SortBy]
	if !ok || (q.Order != "asc" && q.Order != "desc") || q.Company != "" || q.ModelType != "" {
		return "", "", nil, supply.ErrReadContract
	}
	args := []any{}
	where := []string{"TRUE"}
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	in := func(column string, values []string, valid func(string) bool) error {
		if len(values) > 50 {
			return supply.ErrReadContract
		}
		if len(values) == 0 {
			return nil
		}
		seen := map[string]bool{}
		params := []string{}
		for _, v := range values {
			if !valid(v) || seen[v] {
				return supply.ErrReadContract
			}
			seen[v] = true
			params = append(params, add(v))
		}
		where = append(where, column+" IN ("+strings.Join(params, ",")+")")
		return nil
	}
	if q.Keyword != "" {
		p := add(q.Keyword)
		where = append(where, `(strpos(lower(m.name),lower(`+p+`::text))>0 OR strpos(lower(m.company),lower(`+p+`::text))>0 OR strpos(lower(m.official_id),lower(`+p+`::text))>0)`)
	}
	text := func(v string) bool { return v != "" && utf8.ValidString(v) && !strings.ContainsRune(v, 0) }
	if err := in("m.company", q.Companies, text); err != nil {
		return "", "", nil, err
	}
	if err := in("m.model_type", q.ModelTypes, func(v string) bool { return v == "TEXT" || v == "IMAGE" || v == "AUDIO" || v == "VIDEO" }); err != nil {
		return "", "", nil, err
	}
	if q.Status != "" {
		if q.Status != "ACTIVE" && q.Status != "INACTIVE" {
			return "", "", nil, supply.ErrReadContract
		}
		where = append(where, "m.status="+add(q.Status))
	}
	if q.VerifyStatus != "" {
		if q.VerifyStatus != "VERIFIED" && q.VerifyStatus != "UNVERIFIED" && q.VerifyStatus != "INCOMPLETE" {
			return "", "", nil, supply.ErrReadContract
		}
		where = append(where, verify+"="+add(q.VerifyStatus))
	}
	if len(q.SupplierIDs) > 0 || len(q.Regions) > 0 {
		outer := where
		where = []string{"r.standard_model_id=m.id", "s.id=r.supplier_id", "s.region=r.region"}
		if err := in("r.supplier_id", q.SupplierIDs, modelIdentity); err != nil {
			return "", "", nil, err
		}
		if err := in("r.region", q.Regions, func(v string) bool { return v == "CN" || v == "OVERSEAS" }); err != nil {
			return "", "", nil, err
		}
		relation := "EXISTS (SELECT 1 FROM " + s.table("resource") + " r JOIN " + s.table("supplier") + " s ON s.id=r.supplier_id WHERE " + strings.Join(where, " AND ") + ")"
		where = append(outer, relation)
	}
	from := " FROM " + s.table("standard_model") + " m WHERE " + strings.Join(where, " AND ")
	order := " ORDER BY " + col + " " + strings.ToUpper(q.Order) + " NULLS LAST,m.id ASC"
	return from, order, args, nil
}
func (s *ModelReadStore) ListModels(ctx context.Context, q supply.ModelListQuery) ([]*supply.StandardModel, int64, error) {
	if s == nil {
		return nil, 0, supply.ErrDependencyUnavailable
	}
	from, order, args, err := s.listQuery(q)
	if err != nil {
		return nil, 0, err
	}
	result := []*supply.StandardModel{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&total); err != nil {
			return err
		}
		values := append(append([]any{}, args...), q.Size, (q.Page-1)*q.Size)
		rows, err := tx.QueryContext(ctx, "SELECT "+modelProjection(s.table("standard_model_version"))+from+order+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), values...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanStandardModel(rows)
			if err != nil {
				return err
			}
			result = append(result, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
func decodeModelSnapshot(raw []byte, id string, version uint64) (*supply.StandardModel, error) {
	var envelope struct {
		Contract      string                `json:"contract"`
		SchemaVersion int                   `json:"schema_version"`
		Model         *supply.StandardModel `json:"model"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return nil, supply.ErrReadContract
	}
	if _, err := d.Token(); err != io.EOF || envelope.Contract != "supply.standard_model" || envelope.SchemaVersion != 1 || envelope.Model == nil || envelope.Model.ID != id || envelope.Model.VersionNo != version {
		return nil, supply.ErrReadContract
	}
	if err := validateStoredModel(envelope.Model); err != nil {
		return nil, supply.ErrReadContract
	}
	return envelope.Model, nil
}
func (s *ModelReadStore) ListModelVersions(ctx context.Context, id string, page, size int) ([]*supply.StandardModel, int64, error) {
	if !modelIdentity(id) || !modelPageValid(page, size) {
		return nil, 0, supply.ErrReadContract
	}
	result := []*supply.StandardModel{}
	var total int64
	err := s.within(ctx, func(tx *sql.Tx) error {
		current, err := s.current(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+s.table("standard_model_version")+" WHERE standard_model_id=$1", id).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT version_no,snapshot FROM "+s.table("standard_model_version")+" WHERE standard_model_id=$1 ORDER BY version_no DESC,id ASC LIMIT $2 OFFSET $3", id, size, (page-1)*size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var version int64
			var raw []byte
			if err := rows.Scan(&version, &raw); err != nil {
				return err
			}
			if version <= 0 || uint64(version) > current.VersionNo {
				return supply.ErrReadContract
			}
			m, err := decodeModelSnapshot(raw, id, uint64(version))
			if err != nil {
				return err
			}
			result = append(result, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
func (s *ModelReadStore) GetModelVersion(ctx context.Context, id string, version uint64) (*supply.StandardModel, error) {
	if !modelIdentity(id) || version == 0 || version > math.MaxInt64 {
		return nil, supply.ErrReadContract
	}
	var model *supply.StandardModel
	err := s.within(ctx, func(tx *sql.Tx) error {
		current, err := s.current(ctx, tx, id)
		if err != nil {
			return err
		}
		if version > current.VersionNo {
			return supply.ErrNotFound
		}
		var raw []byte
		err = tx.QueryRowContext(ctx, "SELECT snapshot FROM "+s.table("standard_model_version")+" WHERE standard_model_id=$1 AND version_no=$2", id, version).Scan(&raw)
		if err == sql.ErrNoRows {
			return supply.ErrNotFound
		}
		if err != nil {
			return err
		}
		model, err = decodeModelSnapshot(raw, id, version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return model, nil
}
