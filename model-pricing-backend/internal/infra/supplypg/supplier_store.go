package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"model_bss/internal/domain/supply"
)

type SupplierStore struct {
	db      *sql.DB
	schema  string
	resolve ModelTxResolver
	log     ResourceStateLogAppender
}

var _ supply.SupplierUnitOfWork = (*SupplierStore)(nil)
var _ supply.SupplierReadStore = (*SupplierStore)(nil)

func NewSupplierStore(schema string, db *sql.DB, resolve ModelTxResolver, log ResourceStateLogAppender) (*SupplierStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	if db == nil || resolve == nil || log == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return &SupplierStore{db: db, schema: schema, resolve: resolve, log: log}, nil
}

func supplierTaxRateToDB(ratio string) (string, error) {
	rate, err := decimal.NewFromString(ratio)
	if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(1)) {
		return "", &supply.FieldError{Path: "tax_rate", Reason: "expected ratio between zero and one"}
	}
	percent := rate.Mul(decimal.NewFromInt(100))
	if !percent.Equal(percent.Round(2)) {
		return "", &supply.FieldError{Path: "tax_rate", Reason: "native database supports at most 0.01 percentage-point precision"}
	}
	return percent.StringFixed(2), nil
}

func supplierTaxRateFromDB(percent string) (string, error) {
	rate, err := decimal.NewFromString(percent)
	if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(100)) || !rate.Equal(rate.Round(2)) {
		return "", supply.ErrReadContract
	}
	return rate.Div(decimal.NewFromInt(100)).StringFixed(8), nil
}

func validSupplierAuditTimestamp(action string, createdAt, updatedAt, loggedAt time.Time) bool {
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	updatedAt = updatedAt.UTC().Truncate(time.Microsecond)
	loggedAt = loggedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() || updatedAt.Before(createdAt) || loggedAt.Before(createdAt) {
		return false
	}
	switch action {
	case "CREATE":
		return updatedAt.Equal(createdAt) && loggedAt.Equal(createdAt)
	case "UPDATE":
		return loggedAt.Equal(updatedAt)
	case "UPDATE_NOOP":
		return !loggedAt.Before(updatedAt)
	default:
		return false
	}
}

func (s *SupplierStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func (s *SupplierStore) Within(ctx context.Context, run func(supply.SupplierTransaction) error) error {
	if s == nil || s.resolve == nil || s.log == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	unit := &supplierTransaction{store: s, tx: tx}
	if err := run(unit); err != nil {
		return err
	}
	if !unit.logged {
		return supply.ErrReadContract
	}
	return nil
}

type supplierTransaction struct {
	store     *SupplierStore
	tx        *sql.Tx
	allocated string
	current   *supply.Supplier
	created   *supply.Supplier
	staged    *supply.Supplier
	assigned  bool
	logged    bool
}

func (t *supplierTransaction) NextID(ctx context.Context) (string, error) {
	if t == nil || t.tx == nil || t.allocated != "" || t.current != nil || t.created != nil {
		return "", supply.ErrReadContract
	}
	err := t.tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, t.store.table("supplier")).Scan(&t.allocated)
	if err != nil {
		return "", err
	}
	if !modelIdentity(t.allocated) {
		return "", supply.ErrReadContract
	}
	return t.allocated, nil
}

func nullableSupplierText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func canonicalSupplierStorage(input *supply.Supplier) (*supply.Supplier, string, error) {
	if input == nil || !modelIdentity(input.ID) || !modelIdentity(input.BuyerID) || input.Revision != 1 || input.Established || input.Status != "ACTIVE" || input.CreatedAt.IsZero() || input.UpdatedAt.Before(input.CreatedAt) {
		return nil, "", supply.ErrReadContract
	}
	canonical, err := supply.NewSupplier(input.ID, input.BuyerID, input.Data, input.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	if !reflect.DeepEqual(canonical.Data, input.Data) || !canonical.CreatedAt.Equal(input.CreatedAt) || !canonical.UpdatedAt.Equal(input.UpdatedAt) {
		return nil, "", supply.ErrReadContract
	}
	taxRate, err := supplierTaxRateToDB(canonical.Data.TaxRate)
	if err != nil {
		return nil, "", err
	}
	return canonical, taxRate, nil
}

func (t *supplierTransaction) CreateSupplier(ctx context.Context, input *supply.Supplier) error {
	if t == nil || t.tx == nil || t.created != nil || t.allocated == "" || input == nil || input.ID != t.allocated {
		return supply.ErrReadContract
	}
	profile, taxRate, err := canonicalSupplierStorage(input)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("supplier")+` (id,name,region,registration_no,contact,phone,established,tax_rate,settlement_currency,status,created_at,updated_at,created_by,updated_by) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,false,$7,$8,'ACTIVE',$9,$9,$10,$10)`, profile.ID, profile.Data.Name, profile.Data.Region, nullableSupplierText(profile.Data.RegistrationNo), nullableSupplierText(profile.Data.Contact), nullableSupplierText(profile.Data.Phone), taxRate, profile.Data.SettlementCurrency, profile.CreatedAt, profile.BuyerID)
	if err != nil {
		return supplierUniqueError(err)
	}
	t.created = profile.Clone()
	return nil
}

func supplierUniqueError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return supply.ErrIdentityConflict
	}
	return err
}

func (t *supplierTransaction) CreateSupplierAssignment(ctx context.Context, supplierID, buyerID, changedBy string, at time.Time) error {
	if t == nil || t.tx == nil || t.created == nil || t.assigned || supplierID != t.created.ID || buyerID != t.created.BuyerID || changedBy != t.created.BuyerID || !at.Equal(t.created.CreatedAt) {
		return supply.ErrReadContract
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO `+t.store.table("assignment_log")+` (entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by,changed_at) VALUES('supplier',$1,'buyer_id',NULL,$2,$2,$3)`, supplierID, buyerID, at)
	if err != nil {
		return err
	}
	t.assigned = true
	return nil
}

func (t *supplierTransaction) SupplierForUpdate(ctx context.Context, id string) (*supply.Supplier, error) {
	if t == nil || t.tx == nil || t.current != nil || t.created != nil || !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	profile, err := t.store.loadSupplier(ctx, t.tx, id, true)
	if err != nil {
		return nil, err
	}
	t.current = profile.Clone()
	return profile, nil
}

func (t *supplierTransaction) UpdateSupplier(ctx context.Context, input *supply.Supplier, expected uint64) error {
	if t == nil || t.tx == nil || t.current == nil || t.staged != nil || input == nil || input.ID != t.current.ID || expected != t.current.Revision || input.Revision != expected+1 || input.BuyerID != t.current.BuyerID || input.Established != t.current.Established || input.Status != t.current.Status || input.CreatedAt != t.current.CreatedAt || input.Revision > math.MaxInt64 {
		return supply.ErrRevisionConflict
	}
	canonical, err := supply.NewSupplier(input.ID, input.BuyerID, input.Data, input.CreatedAt)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical.Data, input.Data) || input.UpdatedAt.Before(t.current.UpdatedAt) || input.Data == t.current.Data {
		return supply.ErrReadContract
	}
	taxRate, err := supplierTaxRateToDB(input.Data.TaxRate)
	if err != nil {
		return err
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE `+t.store.table("supplier")+` SET name=$2,region=$3,registration_no=$4,contact=$5,phone=$6,tax_rate=$7,settlement_currency=$8,updated_at=$9,updated_by=$10 WHERE id=$1 AND updated_at=$11`, input.ID, input.Data.Name, input.Data.Region, nullableSupplierText(input.Data.RegistrationNo), nullableSupplierText(input.Data.Contact), nullableSupplierText(input.Data.Phone), taxRate, input.Data.SettlementCurrency, input.UpdatedAt, input.BuyerID, t.current.UpdatedAt)
	if err != nil {
		return supplierUniqueError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return supply.ErrRevisionConflict
	}
	t.staged = input.Clone()
	return nil
}

func (t *supplierTransaction) SupplierUsage(ctx context.Context, id string) (supply.SupplierUsage, error) {
	if t == nil || t.tx == nil || t.current == nil || id != t.current.ID {
		return supply.SupplierUsage{}, supply.ErrReadContract
	}
	var usage supply.SupplierUsage
	if err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.store.table("submission")+` WHERE supplier_id=$1), EXISTS(SELECT 1 FROM `+t.store.table("resource")+` WHERE supplier_id=$1), EXISTS(SELECT 1 FROM `+t.store.table("contract")+` WHERE subject_type='SUPPLIER' AND subject_id=$1)`, id).Scan(&usage.HasSubmissions, &usage.HasResources, &usage.HasContracts); err != nil {
		return supply.SupplierUsage{}, err
	}
	return usage, nil
}

func (t *supplierTransaction) CurrencyEnabled(ctx context.Context, code string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// The v36 schema has no currency directory. The previous native contract
	// exposes only CNY/USD; reject any other code until a shared directory exists.
	return code == "CNY" || code == "USD", nil
}

func (t *supplierTransaction) StaffActive(ctx context.Context, id string) (bool, error) {
	if t == nil || t.tx == nil || !modelIdentity(id) {
		return false, supply.ErrReadContract
	}
	var active bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.store.table("internal_staff")+` WHERE id=$1 AND status='ACTIVE')`, id).Scan(&active)
	return active, err
}

func (t *supplierTransaction) AppendSupplierOperation(ctx context.Context, op supply.SupplierOperation) error {
	if t == nil || t.tx == nil || t.logged || op.EntityType != "supplier" || !modelIdentity(op.EntityID) || !modelIdentity(op.ActorID) || op.Reason == "" || op.RequestID == "" || op.OccurredAt.IsZero() || op.After == nil || op.After.ID != op.EntityID {
		return supply.ErrReadContract
	}
	switch op.Action {
	case "CREATE":
		if t.created == nil || !t.assigned || op.Before != nil || !reflect.DeepEqual(op.After, t.created) || op.ActorID != t.created.BuyerID {
			return supply.ErrReadContract
		}
	case "UPDATE":
		if t.current == nil || t.staged == nil || op.Before == nil || !reflect.DeepEqual(op.Before, t.current) || !reflect.DeepEqual(op.After, t.staged) || op.After.Revision != op.Before.Revision+1 {
			return supply.ErrReadContract
		}
	case "UPDATE_NOOP":
		if t.current == nil || t.staged != nil || op.Before == nil || !reflect.DeepEqual(op.Before, t.current) || !reflect.DeepEqual(op.After, t.current) {
			return supply.ErrReadContract
		}
	default:
		return supply.ErrReadContract
	}
	after, err := json.Marshal(op.After)
	if err != nil {
		return err
	}
	var before json.RawMessage
	if op.Before != nil {
		before, err = json.Marshal(op.Before)
		if err != nil {
			return err
		}
	}
	if err := t.store.log(ctx, t.tx, ResourceStateLog{EntityType: op.EntityType, EntityID: op.EntityID, Action: op.Action, ActorID: op.ActorID, Before: before, After: after, Reason: op.Reason, RequestID: op.RequestID, OccurredAt: op.OccurredAt.UTC().Truncate(time.Microsecond)}); err != nil {
		return err
	}
	t.logged = true
	return nil
}

func (s *SupplierStore) Get(ctx context.Context, id string) (*supply.Supplier, error) {
	if s == nil || s.db == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if !modelIdentity(id) {
		return nil, &supply.FieldError{Path: "id", Reason: "expected positive bigint string"}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	profile, err := s.loadSupplier(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *SupplierStore) List(ctx context.Context, query supply.SupplierListQuery) (*supply.SupplierPage, error) {
	if s == nil || s.db == nil || query.Page < 1 || query.Size < 1 || query.Size > 100 {
		return nil, supply.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := ` WHERE ($1='' OR region=$1) AND ($2='' OR name ILIKE '%'||$2||'%')`
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("supplier")+where, query.Region, query.Keyword).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM `+s.table("supplier")+where+` ORDER BY updated_at DESC,id DESC LIMIT $3 OFFSET $4`, query.Region, query.Keyword, query.Size, (query.Page-1)*query.Size)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, query.Size)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	page := &supply.SupplierPage{List: make([]supply.Supplier, 0, len(ids)), Total: total, Page: query.Page, Size: query.Size}
	for _, id := range ids {
		profile, err := s.loadSupplier(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		page.List = append(page.List, *profile)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *SupplierStore) loadSupplier(ctx context.Context, tx *sql.Tx, id string, lock bool) (*supply.Supplier, error) {
	if s == nil || tx == nil || !modelIdentity(id) {
		return nil, supply.ErrDependencyUnavailable
	}
	query := `SELECT id::text,name,region,registration_no,contact,phone,established,tax_rate::text,settlement_currency,status,created_at,updated_at FROM ` + s.table("supplier") + ` WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	var profile supply.Supplier
	var registration, contact, phone, taxRate, currency sql.NullString
	var createdAt, updatedAt time.Time
	err := tx.QueryRowContext(ctx, query, id).Scan(&profile.ID, &profile.Data.Name, &profile.Data.Region, &registration, &contact, &phone, &profile.Established, &taxRate, &currency, &profile.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !taxRate.Valid || !currency.Valid {
		return nil, supply.ErrReadContract
	}
	profile.Data.RegistrationNo, profile.Data.Contact, profile.Data.Phone = registration.String, contact.String, phone.String
	profile.Data.TaxRate, err = supplierTaxRateFromDB(taxRate.String)
	if err != nil {
		return nil, err
	}
	profile.Data.SettlementCurrency = currency.String
	profile.CreatedAt, profile.UpdatedAt = createdAt.UTC().Truncate(time.Microsecond), updatedAt.UTC().Truncate(time.Microsecond)
	if (profile.Status != "ACTIVE" && profile.Status != "FROZEN") || (profile.Data.Region != "CN" && profile.Data.Region != "OVERSEAS") {
		return nil, supply.ErrReadContract
	}
	canonical, err := supply.NewSupplier(id, "1", profile.Data, profile.CreatedAt)
	if err != nil || !reflect.DeepEqual(canonical.Data, profile.Data) {
		return nil, supply.ErrReadContract
	}
	profile.BuyerID, err = s.currentSupplierBuyer(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var action, actor, reason, requestID sql.NullString
	var loggedAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT "after",action,operator_id::text,reason,request_id,created_at FROM `+s.table("operation_log")+` WHERE entity_type='supplier' AND entity_id=$1 AND action IN ('CREATE','UPDATE','UPDATE_NOOP') ORDER BY id DESC LIMIT 1`, id).Scan(&raw, &action, &actor, &reason, &requestID, &loggedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrReadContract
		}
		return nil, err
	}
	var snapshot supply.Supplier
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, supply.ErrReadContract
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, supply.ErrReadContract
	}
	if snapshot.ID != id || snapshot.Revision == 0 || snapshot.Revision > math.MaxInt64 || snapshot.Established != profile.Established || snapshot.Status != profile.Status || snapshot.Data != profile.Data || !snapshot.CreatedAt.UTC().Truncate(time.Microsecond).Equal(profile.CreatedAt) || !snapshot.UpdatedAt.UTC().Truncate(time.Microsecond).Equal(profile.UpdatedAt) || !actor.Valid || !modelIdentity(actor.String) || !reason.Valid || strings.TrimSpace(reason.String) == "" || !requestID.Valid || !modelIdentity(profile.BuyerID) || !validSupplierAuditTimestamp(action.String, profile.CreatedAt, profile.UpdatedAt, loggedAt) {
		return nil, supply.ErrReadContract
	}
	switch action.String {
	case "CREATE":
		if snapshot.Revision != 1 || actor.String != profile.BuyerID {
			return nil, supply.ErrReadContract
		}
	case "UPDATE":
		if snapshot.Revision < 2 {
			return nil, supply.ErrReadContract
		}
	case "UPDATE_NOOP":
		if snapshot.Revision < 1 {
			return nil, supply.ErrReadContract
		}
	default:
		return nil, supply.ErrReadContract
	}
	profile.Revision = snapshot.Revision
	return profile.Clone(), nil
}

func (s *SupplierStore) currentSupplierBuyer(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT old_staff_id::text,new_staff_id::text,changed_by::text FROM `+s.table("assignment_log")+` WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id' ORDER BY id`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var current string
	for rows.Next() {
		var old, next, changedBy sql.NullString
		if err := rows.Scan(&old, &next, &changedBy); err != nil {
			return "", err
		}
		if (current == "" && old.Valid) || (current != "" && (!old.Valid || old.String != current)) || !next.Valid || !changedBy.Valid || !modelIdentity(next.String) || !modelIdentity(changedBy.String) {
			return "", supply.ErrReadContract
		}
		current = next.String
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if current == "" {
		return "", supply.ErrReadContract
	}
	return current, nil
}
