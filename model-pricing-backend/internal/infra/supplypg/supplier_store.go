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
	"strconv"
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
var _ supply.SupplierQualificationUploadAuthorizer = (*SupplierStore)(nil)
var _ supply.SupplierAliasCapabilityStore = (*SupplierStore)(nil)

func NewSupplierStore(schema string, db *sql.DB, resolve ModelTxResolver, log ResourceStateLogAppender) (*SupplierStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	if db == nil || resolve == nil || log == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return &SupplierStore{db: db, schema: schema, resolve: resolve, log: log}, nil
}

func supplierTaxRateToDB(ratio *string) (any, error) {
	if ratio == nil {
		return nil, nil
	}
	rate, err := decimal.NewFromString(*ratio)
	if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(1)) {
		return "", &supply.FieldError{Path: "tax_rate", Reason: "expected ratio between zero and one"}
	}
	percent := rate.Mul(decimal.NewFromInt(100))
	if !percent.Equal(percent.Round(2)) {
		return "", &supply.FieldError{Path: "tax_rate", Reason: "native database supports at most 0.01 percentage-point precision"}
	}
	return percent.StringFixed(2), nil
}

func supplierTaxRateFromDB(percent string) (*string, error) {
	rate, err := decimal.NewFromString(percent)
	if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(100)) || !rate.Equal(rate.Round(2)) {
		return nil, supply.ErrReadContract
	}
	canonical := rate.Div(decimal.NewFromInt(100)).StringFixed(8)
	return &canonical, nil
}

func supplierDataEqual(left, right supply.SupplierData) bool { return reflect.DeepEqual(left, right) }

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

func (s *SupplierStore) supplierAliasStructureAvailable(ctx context.Context, tx *sql.Tx) (bool, error) {
	if s == nil || tx == nil {
		return false, supply.ErrDependencyUnavailable
	}
	var available bool
	if err := tx.QueryRowContext(ctx, `SELECT count(*) = 5
FROM information_schema.columns
WHERE table_schema=$1 AND table_name='supplier_alias'
  AND (
    (column_name='id' AND data_type='bigint' AND is_nullable='NO' AND (is_identity='YES' OR column_default IS NOT NULL)) OR
    (column_name='supplier_id' AND data_type='bigint' AND is_nullable='NO') OR
    (column_name='alias_name' AND data_type IN ('character varying','text') AND is_nullable='NO' AND COALESCE(character_maximum_length,128)>=128) OR
    (column_name='normalized_name' AND data_type IN ('character varying','text') AND is_nullable='NO' AND COALESCE(character_maximum_length,128)>=128) OR
    (column_name='created_at' AND data_type='timestamp with time zone' AND is_nullable='NO' AND column_default IS NOT NULL)
  )`, s.schema).Scan(&available); err != nil {
		return false, err
	}
	return available, nil
}

func (s *SupplierStore) SupplierAliasesSupported(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, supply.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	available, err := s.supplierAliasStructureAvailable(ctx, tx)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return available, nil
}

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

func canonicalSupplierStorage(input *supply.Supplier) (*supply.Supplier, any, error) {
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
	if !reflect.DeepEqual(canonical.Data, input.Data) || input.UpdatedAt.Before(t.current.UpdatedAt) || reflect.DeepEqual(input.Data, t.current.Data) {
		return supply.ErrReadContract
	}
	wantAliases := supply.SupplierAliasesAfterRename(t.current.Aliases, t.current.Data.Name, input.Data.Name)
	if !reflect.DeepEqual(input.Aliases, wantAliases) {
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
	if err := t.store.replaceSupplierAliases(ctx, t.tx, input.ID, input.Data.Name, input.Aliases); err != nil {
		return err
	}
	t.staged = input.Clone()
	return nil
}

func normalizeSupplierAlias(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 128 {
		return "", false
	}
	return strings.ToLower(value), true
}

func (s *SupplierStore) replaceSupplierAliases(ctx context.Context, tx *sql.Tx, supplierID, currentName string, aliases []string) error {
	currentKey, ok := normalizeSupplierAlias(currentName)
	if !ok {
		return supply.ErrReadContract
	}
	wanted := make(map[string]string, len(aliases))
	for _, alias := range aliases {
		key, valid := normalizeSupplierAlias(alias)
		if !valid || key == currentKey {
			return supply.ErrReadContract
		}
		if _, exists := wanted[key]; exists {
			return supply.ErrReadContract
		}
		wanted[key] = strings.TrimSpace(alias)
	}
	aliasTableAvailable, err := s.supplierAliasStructureAvailable(ctx, tx)
	if err != nil {
		return err
	}
	if !aliasTableAvailable {
		if len(aliases) == 0 {
			return nil
		}
		return supply.ErrDependencyUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT alias_name,normalized_name FROM `+s.table("supplier_alias")+` WHERE supplier_id=$1 ORDER BY id`, supplierID)
	if err != nil {
		return err
	}
	existing := make(map[string]string, len(wanted))
	for rows.Next() {
		var alias, normalized string
		if err := rows.Scan(&alias, &normalized); err != nil {
			rows.Close()
			return err
		}
		key, valid := normalizeSupplierAlias(alias)
		if !valid || key != normalized {
			rows.Close()
			return supply.ErrReadContract
		}
		existing[normalized] = alias
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for normalized := range existing {
		if _, keep := wanted[normalized]; !keep {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+s.table("supplier_alias")+` WHERE supplier_id=$1 AND normalized_name=$2`, supplierID, normalized); err != nil {
				return err
			}
		}
	}
	for _, alias := range aliases {
		normalized, _ := normalizeSupplierAlias(alias)
		if _, found := existing[normalized]; found {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("supplier_alias")+` (supplier_id,alias_name,normalized_name) VALUES($1,$2,$3)`, supplierID, alias, normalized); err != nil {
			return supplierUniqueError(err)
		}
	}
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
	aliasTableAvailable, err := s.supplierAliasStructureAvailable(ctx, tx)
	if err != nil {
		return nil, err
	}
	nameMatch := `name ILIKE '%'||$2||'%'`
	if aliasTableAvailable {
		nameMatch += ` OR EXISTS (SELECT 1 FROM ` + s.table("supplier_alias") + ` sa WHERE sa.supplier_id=` + s.table("supplier") + `.id AND sa.alias_name ILIKE '%'||$2||'%')`
	}
	where := ` WHERE ($1='' OR region=$1) AND ($2='' OR (` + nameMatch + `))`
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
	if !currency.Valid {
		return nil, supply.ErrReadContract
	}
	profile.Data.RegistrationNo, profile.Data.Contact, profile.Data.Phone = registration.String, contact.String, phone.String
	if taxRate.Valid {
		profile.Data.TaxRate, err = supplierTaxRateFromDB(taxRate.String)
		if err != nil {
			return nil, err
		}
	}
	profile.Data.SettlementCurrency = currency.String
	profile.Aliases, err = s.supplierAliases(ctx, tx, id, profile.Data.Name)
	if err != nil {
		return nil, err
	}
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
	if snapshot.ID != id || snapshot.Revision == 0 || snapshot.Revision > math.MaxInt64 || snapshot.Established != profile.Established || snapshot.Status != profile.Status || !supplierDataEqual(snapshot.Data, profile.Data) || !reflect.DeepEqual(snapshot.Aliases, profile.Aliases) || !snapshot.CreatedAt.UTC().Truncate(time.Microsecond).Equal(profile.CreatedAt) || !snapshot.UpdatedAt.UTC().Truncate(time.Microsecond).Equal(profile.UpdatedAt) || !actor.Valid || !modelIdentity(actor.String) || !reason.Valid || strings.TrimSpace(reason.String) == "" || !requestID.Valid || !modelIdentity(profile.BuyerID) || !validSupplierAuditTimestamp(action.String, profile.CreatedAt, profile.UpdatedAt, loggedAt) {
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

func (s *SupplierStore) supplierAliases(ctx context.Context, tx *sql.Tx, supplierID, currentName string) ([]string, error) {
	aliasTableAvailable, err := s.supplierAliasStructureAvailable(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !aliasTableAvailable {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT alias_name,normalized_name FROM `+s.table("supplier_alias")+` WHERE supplier_id=$1 ORDER BY id`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	currentKey, ok := normalizeSupplierAlias(currentName)
	if !ok {
		return nil, supply.ErrReadContract
	}
	aliases := make([]string, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var alias, normalized string
		if err := rows.Scan(&alias, &normalized); err != nil {
			return nil, err
		}
		key, valid := normalizeSupplierAlias(alias)
		if !valid || key != normalized || key == currentKey {
			return nil, supply.ErrReadContract
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, supply.ErrReadContract
		}
		seen[key] = struct{}{}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, nil
	}
	return aliases, nil
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

// ListQualifications returns only display metadata and the presence of an
// attachment reference. The legacy file_ref may contain a storage path and is
// never returned by this method; downloads require a separate file provider.
func (s *SupplierStore) ListQualifications(ctx context.Context, supplierID string, query supply.SupplierQualificationQuery) (*supply.SupplierQualificationPage, error) {
	if s == nil || s.db == nil || !modelIdentity(supplierID) || query.Page < 1 || query.Size < 1 || query.Size > 100 || query.Page > math.MaxInt/query.Size {
		return nil, supply.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	metadataSupported, err := qualificationMetadataColumns(ctx, tx, s.schema)
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("supplier_qualification")+` WHERE supplier_id=$1`, supplierID).Scan(&total); err != nil {
		return nil, err
	}
	typeExpr, fromExpr, toExpr, reviewExpr, revisionExpr, createdByExpr := "NULL::text", "NULL::text", "NULL::text", "NULL::text", "NULL::bigint", "NULL::text"
	if metadataSupported {
		typeExpr, fromExpr, toExpr, reviewExpr, revisionExpr, createdByExpr = "qualification_type::text", "valid_from::text", "valid_to::text", "review_status::text", "revision::bigint", "created_by::text"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,supplier_id::text,name,(file_ref IS NOT NULL AND btrim(file_ref)<>'') AS has_attachment,file_ref,uploaded_at,`+typeExpr+`,`+fromExpr+`,`+toExpr+`,`+reviewExpr+`,`+revisionExpr+`,`+createdByExpr+` FROM `+s.table("supplier_qualification")+` WHERE supplier_id=$1 ORDER BY uploaded_at DESC,id DESC LIMIT $2 OFFSET $3`, supplierID, query.Size, (query.Page-1)*query.Size)
	if err != nil {
		return nil, err
	}
	result := &supply.SupplierQualificationPage{List: make([]supply.SupplierQualification, 0), Total: total, Page: query.Page, Size: query.Size, MetadataSupported: metadataSupported}
	for rows.Next() {
		var item supply.SupplierQualification
		var fileRef, qualificationType, validFrom, validTo, reviewStatus, createdBy sql.NullString
		var revision sql.NullInt64
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.Name, &item.HasAttachment, &fileRef, &item.UploadedAt, &qualificationType, &validFrom, &validTo, &reviewStatus, &revision, &createdBy); err != nil {
			rows.Close()
			return nil, err
		}
		item.FileReference = fileRef.String
		item.QualificationType = qualificationType.String
		item.ValidFrom = qualificationDatePointer(validFrom)
		item.ValidTo = qualificationDatePointer(validTo)
		item.ReviewStatus = qualificationStringPointer(reviewStatus)
		item.CreatedBy = qualificationStringPointer(createdBy)
		if revision.Valid {
			if revision.Int64 < 1 || revision.Int64 > math.MaxInt32 {
				rows.Close()
				return nil, supply.ErrReadContract
			}
			value := uint64(revision.Int64)
			item.Revision = &value
		}
		result.List = append(result.List, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SupplierStore) CreateQualification(ctx context.Context, meta supply.WriteMeta, supplierID, name, fileRef string, at time.Time) (*supply.SupplierQualification, error) {
	return s.createQualification(ctx, meta, supplierID, name, fileRef, nil, at)
}

func (s *SupplierStore) CreateQualificationWithMetadata(ctx context.Context, meta supply.WriteMeta, supplierID, name, fileRef string, details supply.SupplierQualificationMetadata, at time.Time) (*supply.SupplierQualification, error) {
	return s.createQualification(ctx, meta, supplierID, name, fileRef, &details, at)
}

// AuthorizeSupplierQualificationUpload performs a preflight ownership check
// before the domain service writes file bytes. The final write repeats this
// check under the supplier row lock in createQualification.
func (s *SupplierStore) AuthorizeSupplierQualificationUpload(ctx context.Context, meta supply.WriteMeta, supplierID string) error {
	if s == nil || s.resolve == nil || s.log == nil || !modelIdentity(meta.ActorID) || !modelIdentity(supplierID) {
		return supply.ErrDependencyUnavailable
	}
	tx, err := s.resolve(ctx)
	if err != nil {
		return mapSupplierStorageError(err)
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	return s.authorizeQualificationWrite(ctx, tx, meta, supplierID, false)
}

func (s *SupplierStore) authorizeQualificationOwner(ctx context.Context, tx *sql.Tx, actorID, supplierID string, lock bool) (*supply.Supplier, error) {
	profile, err := s.loadSupplier(ctx, tx, supplierID, lock)
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if profile.BuyerID != actorID {
		return nil, supply.ErrForbidden
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("internal_staff")+` WHERE id=$1 AND status='ACTIVE')`, actorID).Scan(&active); err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if !active {
		return nil, supply.ErrForbidden
	}
	return profile, nil
}

func (s *SupplierStore) authorizeQualificationWrite(ctx context.Context, tx *sql.Tx, meta supply.WriteMeta, supplierID string, lock bool) error {
	profile, err := s.authorizeQualificationOwner(ctx, tx, meta.ActorID, supplierID, lock)
	if err != nil {
		return err
	}
	if profile.Status != "ACTIVE" {
		return supply.ErrForbidden
	}
	return nil
}

func (s *SupplierStore) createQualification(ctx context.Context, meta supply.WriteMeta, supplierID, name, fileRef string, details *supply.SupplierQualificationMetadata, at time.Time) (*supply.SupplierQualification, error) {
	if s == nil || s.resolve == nil || s.log == nil || !modelIdentity(meta.ActorID) || !modelIdentity(supplierID) || strings.TrimSpace(name) == "" || !validSupplierQualificationFileRef(fileRef) || at.IsZero() {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := s.authorizeQualificationWrite(ctx, tx, meta, supplierID, true); err != nil {
		return nil, err
	}
	if details != nil {
		ready, err := qualificationMetadataColumns(ctx, tx, s.schema)
		if err != nil {
			return nil, mapSupplierStorageError(err)
		}
		if !ready {
			return nil, supply.ErrDependencyUnavailable
		}
	} else {
		// Keep the legacy upload contract only while the target schema lacks the
		// complete metadata set. Once present, omitted fields must not create a
		// qualification that bypasses the required type/revision metadata.
		ready, err := qualificationMetadataColumns(ctx, tx, s.schema)
		if err != nil {
			return nil, mapSupplierStorageError(err)
		}
		if ready {
			return nil, supply.ErrQualificationMetadataRequired
		}
	}
	row := &supply.SupplierQualification{SupplierID: supplierID, Name: strings.TrimSpace(name), HasAttachment: true, CanDownload: true, FileReference: fileRef, UploadedAt: at.UTC().Truncate(time.Microsecond)}
	var after any = map[string]any{"supplier_id": supplierID, "name": row.Name, "has_attachment": true, "uploaded_at": row.UploadedAt}
	if details == nil {
		if err := tx.QueryRowContext(ctx, `INSERT INTO `+s.table("supplier_qualification")+` (supplier_id,name,file_ref,uploaded_at) VALUES($1,$2,$3,$4) RETURNING id::text`, supplierID, row.Name, fileRef, row.UploadedAt).Scan(&row.ID); err != nil {
			return nil, mapSupplierStorageError(err)
		}
	} else {
		if err := tx.QueryRowContext(ctx, `INSERT INTO `+s.table("supplier_qualification")+` (supplier_id,name,file_ref,uploaded_at,qualification_type,valid_from,valid_to,revision,created_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::date,NULLIF($7,'')::date,1,$8) RETURNING id::text`, supplierID, row.Name, fileRef, row.UploadedAt, details.Type, details.ValidFrom, details.ValidTo, meta.ActorID).Scan(&row.ID); err != nil {
			return nil, mapSupplierStorageError(err)
		}
		revision := uint64(1)
		row.QualificationType, row.ValidFrom, row.ValidTo, row.Revision = details.Type, nullableQualificationDate(details.ValidFrom), nullableQualificationDate(details.ValidTo), &revision
		createdBy := meta.ActorID
		row.CreatedBy = &createdBy
		after = map[string]any{"supplier_id": supplierID, "name": row.Name, "qualification_type": details.Type, "valid_from": details.ValidFrom, "valid_to": details.ValidTo, "revision": "1", "created_by": meta.ActorID, "has_attachment": true, "uploaded_at": row.UploadedAt}
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil, err
	}
	if err := s.log(ctx, tx, ResourceStateLog{EntityType: "supplier_qualification", EntityID: row.ID, Action: "UPLOAD", ActorID: meta.ActorID, After: afterJSON, Reason: meta.Reason, RequestID: meta.RequestID, OccurredAt: row.UploadedAt}); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *SupplierStore) UpdateQualificationMetadata(ctx context.Context, meta supply.WriteMeta, supplierID, qualificationID string, expectedRevision uint64, name string, details supply.SupplierQualificationMetadata, at time.Time) (*supply.SupplierQualification, error) {
	if s == nil || s.resolve == nil || s.log == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	for _, field := range []struct{ name, value string }{
		{"actor_id", meta.ActorID}, {"supplier_id", supplierID}, {"qualification_id", qualificationID},
	} {
		if !modelIdentity(field.value) {
			return nil, &supply.FieldError{Path: field.name, Reason: "expected a positive bigint string"}
		}
	}
	if expectedRevision == 0 || expectedRevision >= math.MaxInt32 {
		return nil, &supply.FieldError{Path: "expected_revision", Reason: "expected a positive revision below the storage limit"}
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 || at.IsZero() {
		return nil, supply.ErrReadContract
	}
	tx, err := s.resolve(ctx)
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if tx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := s.authorizeQualificationWrite(ctx, tx, meta, supplierID, true); err != nil {
		return nil, err
	}
	ready, err := qualificationMetadataColumns(ctx, tx, s.schema)
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if !ready {
		return nil, supply.ErrDependencyUnavailable
	}
	var before supply.SupplierQualification
	var fileRef, qualificationType, validFrom, validTo, reviewStatus, createdBy sql.NullString
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT id::text,supplier_id::text,name,file_ref,uploaded_at,qualification_type::text,valid_from::text,valid_to::text,review_status::text,revision::bigint,created_by::text FROM `+s.table("supplier_qualification")+` WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, qualificationID, supplierID).Scan(&before.ID, &before.SupplierID, &before.Name, &fileRef, &before.UploadedAt, &qualificationType, &validFrom, &validTo, &reviewStatus, &revision, &createdBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if revision < 1 || revision > math.MaxInt32 {
		return nil, supply.ErrReadContract
	}
	if uint64(revision) != expectedRevision {
		return nil, supply.ErrRevisionConflict
	}
	updated, err := tx.ExecContext(ctx, `UPDATE `+s.table("supplier_qualification")+` SET name=$1,qualification_type=$2,valid_from=NULLIF($3,'')::date,valid_to=NULLIF($4,'')::date,revision=revision+1 WHERE id=$5 AND supplier_id=$6 AND revision=$7`, name, details.Type, details.ValidFrom, details.ValidTo, qualificationID, supplierID, int64(expectedRevision))
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return nil, mapSupplierStorageError(err)
	}
	if affected != 1 {
		return nil, supply.ErrRevisionConflict
	}
	row := &supply.SupplierQualification{ID: before.ID, SupplierID: before.SupplierID, Name: name, QualificationType: details.Type,
		ValidFrom: nullableQualificationDate(details.ValidFrom), ValidTo: nullableQualificationDate(details.ValidTo), HasAttachment: fileRef.Valid && strings.TrimSpace(fileRef.String) != "", FileReference: fileRef.String, UploadedAt: before.UploadedAt}
	newRevision := uint64(revision + 1)
	row.Revision = &newRevision
	if reviewStatus.Valid {
		row.ReviewStatus = &reviewStatus.String
	}
	if createdBy.Valid {
		row.CreatedBy = &createdBy.String
	}
	beforeJSON, err := json.Marshal(map[string]any{"supplier_id": supplierID, "name": before.Name, "qualification_type": qualificationType.String, "valid_from": validFrom.String, "valid_to": validTo.String, "revision": strconv.FormatInt(revision, 10), "created_by": createdBy.String, "has_attachment": row.HasAttachment})
	if err != nil {
		return nil, err
	}
	afterJSON, err := json.Marshal(map[string]any{"supplier_id": supplierID, "name": name, "qualification_type": details.Type, "valid_from": details.ValidFrom, "valid_to": details.ValidTo, "revision": strconv.FormatUint(newRevision, 10), "created_by": createdBy.String, "has_attachment": row.HasAttachment})
	if err != nil {
		return nil, err
	}
	if err := s.log(ctx, tx, ResourceStateLog{EntityType: "supplier_qualification", EntityID: row.ID, Action: "UPDATE", ActorID: meta.ActorID, Before: beforeJSON, After: afterJSON, Reason: meta.Reason, RequestID: meta.RequestID, OccurredAt: at.UTC().Truncate(time.Microsecond)}); err != nil {
		return nil, err
	}
	return row, nil
}

func qualificationMetadataColumns(ctx context.Context, query interface{ QueryRowContext(context.Context, string, ...any) *sql.Row }, schema string) (bool, error) {
	var supported bool
	err := query.QueryRowContext(ctx, `SELECT count(*) = 6
FROM information_schema.columns
WHERE table_schema=$1 AND table_name='supplier_qualification'
  AND (
    (column_name='qualification_type' AND data_type IN ('character varying','text') AND is_nullable='YES') OR
    (column_name='valid_from' AND data_type='date' AND is_nullable='YES') OR
    (column_name='valid_to' AND data_type='date' AND is_nullable='YES') OR
    (column_name='review_status' AND data_type IN ('character varying','text') AND is_nullable='YES') OR
    (column_name='revision' AND data_type IN ('integer','bigint') AND is_nullable='NO') OR
    (column_name='created_by' AND data_type='bigint' AND is_nullable='YES')
  )`, schema).Scan(&supported)
	return supported, err
}

func qualificationStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func qualificationDatePointer(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	result := value.String
	return &result
}

func nullableQualificationDate(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	result := strings.TrimSpace(value)
	return &result
}

func (s *SupplierStore) QualificationFileReference(ctx context.Context, actorID, supplierID, qualificationID string) (string, string, error) {
	if s == nil || s.db == nil || !modelIdentity(actorID) || !modelIdentity(supplierID) || !modelIdentity(qualificationID) {
		return "", "", supply.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err := s.authorizeQualificationOwner(ctx, tx, actorID, supplierID, false); err != nil {
		return "", "", err
	}
	var fileRef sql.NullString
	var name string
	err = tx.QueryRowContext(ctx, `SELECT file_ref,name FROM `+s.table("supplier_qualification")+` WHERE supplier_id=$1 AND id=$2`, supplierID, qualificationID).Scan(&fileRef, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", supply.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return fileRef.String, name, nil
}

func validSupplierQualificationFileRef(value string) bool {
	if len(value) != len("squal:v1:")+32 || !strings.HasPrefix(value, "squal:v1:") {
		return false
	}
	for _, char := range value[len("squal:v1:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
