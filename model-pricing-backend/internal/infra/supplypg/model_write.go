package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"math"
	"model_bss/internal/domain/supply"
	appdb "model_bss/internal/infra/db"
	"reflect"
	"regexp"
	"strconv"
	"time"
)

type ModelTxResolver func(context.Context) (*sql.Tx, error)
type ModelLogAppender func(context.Context, *sql.Tx, supply.ModelOperation) error

// GORMModelTxFromContext adapts the public idempotency middleware's GORM
// transaction to the native SQL transaction used by supply repositories.
// Missing or non-SQL transaction contexts fail closed so model writes cannot
// escape the request's shared commit/rollback boundary.
func GORMModelTxFromContext(ctx context.Context) (*sql.Tx, error) {
	if ctx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	gormTx := appdb.FromContext(ctx)
	if gormTx == nil || gormTx.Statement == nil || gormTx.Statement.ConnPool == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	tx, ok := gormTx.Statement.ConnPool.(*sql.Tx)
	if !ok || tx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return tx, nil
}

type ModelUnitOfWork struct {
	schema  string
	resolve ModelTxResolver
	log     ModelLogAppender
}

var modelSchemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var _ supply.ModelUnitOfWork = (*ModelUnitOfWork)(nil)

func NewModelUnitOfWork(schema string, resolve ModelTxResolver, log ModelLogAppender) (*ModelUnitOfWork, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &ModelUnitOfWork{schema: schema, resolve: resolve, log: log}, nil
}

// Within only joins the caller transaction. The caller owns rollback on any
// error and must commit before publishing a model or idempotent response.
func (w *ModelUnitOfWork) Within(ctx context.Context, run func(supply.ModelTransaction) error) error {
	if w == nil || w.resolve == nil || w.log == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	return run(&modelWriteTransaction{tx: tx, schema: w.schema, log: w.log})
}

type modelWriteTransaction struct {
	tx     *sql.Tx
	schema string
	log    ModelLogAppender
}

var _ supply.ModelTransaction = (*modelWriteTransaction)(nil)

func (w *modelWriteTransaction) table(name string) string { return `"` + w.schema + `"."` + name + `"` }
func modelIdentity(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}
func (w *modelWriteTransaction) NextID(ctx context.Context) (string, error) {
	var id string
	err := w.tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence($1,'id'))::text`, w.table("standard_model")).Scan(&id)
	if err != nil {
		return "", err
	}
	if !modelIdentity(id) {
		return "", supply.ErrReadContract
	}
	return id, nil
}
func (w *modelWriteTransaction) GetForUpdate(ctx context.Context, id string) (*supply.StandardModel, error) {
	if !modelIdentity(id) {
		return nil, mappingError("id")
	}
	// Acquire the current row first. A fresh statement then sees the version
	// committed by a previous lock holder under READ COMMITTED.
	var locked string
	if err := w.tx.QueryRowContext(ctx, `SELECT id::text FROM `+w.table("standard_model")+` WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	return scanStandardModel(w.tx.QueryRowContext(ctx, `SELECT `+modelProjection(w.table("standard_model_version"))+` FROM `+w.table("standard_model")+` m WHERE m.id=$1`, id))
}

func modelVersionValue(table, expression string) string {
	return `(SELECT ` + expression + ` FROM ` + table + ` v WHERE v.standard_model_id=m.id ORDER BY v.version_no DESC,v.id DESC LIMIT 1)`
}
func modelProjection(versions string) string {
	return `m.id::text,m.status,m.created_at,m.updated_at,
 m.company,m.name,m.official_id,m.model_type,to_json(m.input_modalities),to_json(m.output_modalities),m.capacity,m.abilities,m.price_items,m.api_access,m.lifecycle,m.sources,m.checked_at,` + modelVersionValue(versions, "v.version_no") + `,` + modelVersionValue(versions, "v.snapshot")
}

func scanStandardModel(source interface{ Scan(...any) error }) (*supply.StandardModel, error) {
	var id, status string
	var createdAt, updatedAt time.Time
	var row ModelDataRow
	var inputs, outputs []byte
	var version sql.NullInt64
	var snapshot []byte
	err := source.Scan(&id, &status, &createdAt, &updatedAt,
		&row.Company, &row.Name, &row.OfficialID, &row.ModelType, &inputs, &outputs, &row.Capacity, &row.Abilities, &row.PriceItems, &row.APIAccess, &row.Lifecycle, &row.Sources, &row.CheckedAt, &version, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !version.Valid || version.Int64 <= 0 {
		return nil, supply.ErrReadContract
	}
	m, err := decodeModelSnapshot(snapshot, id, uint64(version.Int64))
	if err != nil {
		return nil, err
	}
	if row.InputModalities, err = modelModalities(inputs, "input_modalities"); err != nil {
		return nil, err
	}
	if row.OutputModalities, err = modelModalities(outputs, "output_modalities"); err != nil {
		return nil, err
	}
	data, err := row.DomainData()
	if err != nil {
		return nil, err
	}
	// JSONB arrays decode empty lists as non-nil slices. Restore the domain's
	// canonical form so re-saving unchanged data remains an actual NOOP.
	canonical, err := supply.NewStandardModel(m.ID, m.MaintainerID, data, m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(m.Data, canonical.Data) || m.Status != status || !m.CreatedAt.Truncate(time.Microsecond).Equal(createdAt.Truncate(time.Microsecond)) || !m.UpdatedAt.Truncate(time.Microsecond).Equal(updatedAt.Truncate(time.Microsecond)) {
		return nil, supply.ErrReadContract
	}
	return m, nil
}
func validateStoredModel(m *supply.StandardModel) error {
	if m == nil || !modelIdentity(m.ID) || !modelIdentity(m.MaintainerID) || m.Revision == 0 || m.Revision > math.MaxInt64 || m.VersionNo == 0 || m.VersionNo > m.Revision || m.CreatedAt.IsZero() || m.UpdatedAt.Before(m.CreatedAt) {
		return supply.ErrReadContract
	}
	if m.Status != "ACTIVE" && m.Status != "INACTIVE" {
		return supply.ErrReadContract
	}
	// Domain validation permits incomplete drafts but rejects illegal supplied
	// values. Never reconstruct ownership or verification from missing columns.
	if _, err := supply.NewStandardModel(m.ID, m.MaintainerID, m.Data, m.UpdatedAt); err != nil {
		return err
	}
	switch m.VerifyStatus {
	case "VERIFIED":
		if !modelIdentity(m.CheckedBy) || m.Data.CheckedAt == "" || len(m.MissingFields()) != 0 {
			return supply.ErrReadContract
		}
	case "INCOMPLETE":
		if m.CheckedBy != "" || len(m.MissingFields()) == 0 {
			return supply.ErrReadContract
		}
	case "UNVERIFIED":
		if m.CheckedBy != "" || len(m.MissingFields()) != 0 {
			return supply.ErrReadContract
		}
	default:
		return supply.ErrReadContract
	}
	return nil
}
func modelWriteValues(m *supply.StandardModel) ([]any, error) {
	if err := validateStoredModel(m); err != nil {
		return nil, err
	}
	row, err := EncodeModelData(m.Data)
	if err != nil {
		return nil, err
	}
	return []any{m.ID, m.MaintainerID, m.Status, m.CreatedAt, m.UpdatedAt,
		row.Company, row.Name, row.OfficialID, row.ModelType, row.InputModalities, row.OutputModalities, string(row.Capacity), string(row.Abilities), string(row.PriceItems), string(row.APIAccess), string(row.Lifecycle), string(row.Sources), row.CheckedAt}, nil
}
func modelUniqueError(err error, conflict error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return conflict
	}
	return err
}
func (w *modelWriteTransaction) Create(ctx context.Context, m *supply.StandardModel) error {
	values, err := modelWriteValues(m)
	if err != nil {
		return err
	}
	if m.Revision != 1 || m.VersionNo != 1 {
		return supply.ErrReadContract
	}
	_, err = w.tx.ExecContext(ctx, `INSERT INTO `+w.table("standard_model")+` (id,created_by,updated_by,status,created_at,updated_at,
 company,name,official_id,model_type,input_modalities,output_modalities,capacity,abilities,price_items,api_access,lifecycle,sources,checked_at)
 OVERRIDING SYSTEM VALUE VALUES($1,$2,$2,$3,$4,$5,$6,$7,$8,$9,$10::text[],$11::text[],$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16::jsonb,$17::jsonb,$18)`, values...)
	return modelUniqueError(err, supply.ErrIdentityConflict)
}
func (w *modelWriteTransaction) Update(ctx context.Context, m *supply.StandardModel, expected uint64) error {
	values, err := modelWriteValues(m)
	if err != nil {
		return err
	}
	if expected == 0 || expected >= math.MaxInt64 || m.Revision != expected+1 {
		return supply.ErrRevisionConflict
	}
	current, err := w.GetForUpdate(ctx, m.ID)
	if err != nil {
		return err
	}
	if current.Revision != expected || current.MaintainerID != m.MaintainerID || m.VersionNo != current.VersionNo+1 {
		return supply.ErrRevisionConflict
	}
	values = append(values, current.UpdatedAt)
	result, err := w.tx.ExecContext(ctx, `UPDATE `+w.table("standard_model")+` SET status=$3,updated_at=$5,updated_by=$2,
 company=$6,name=$7,official_id=$8,model_type=$9,input_modalities=$10::text[],output_modalities=$11::text[],capacity=$12::jsonb,abilities=$13::jsonb,price_items=$14::jsonb,api_access=$15::jsonb,lifecycle=$16::jsonb,sources=$17::jsonb,checked_at=$18
 WHERE id=$1 AND created_at=$4 AND updated_at=$19`, values...)
	if err != nil {
		return modelUniqueError(err, supply.ErrIdentityConflict)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return supply.ErrRevisionConflict
	}
	return nil
}
func (w *modelWriteTransaction) AppendVersion(ctx context.Context, m *supply.StandardModel) error {
	if err := validateStoredModel(m); err != nil {
		return err
	}
	if m.VersionNo > math.MaxInt32 {
		return &supply.FieldError{Path: "version_no", Reason: "exceeds schema34 integer range"}
	}
	raw, err := json.Marshal(struct {
		Contract      string                `json:"contract"`
		SchemaVersion int                   `json:"schema_version"`
		Model         *supply.StandardModel `json:"model"`
	}{"supply.standard_model", 1, m})
	if err != nil {
		return err
	}
	_, err = w.tx.ExecContext(ctx, `INSERT INTO `+w.table("standard_model_version")+` (standard_model_id,version_no,snapshot,checked_at,created_at) VALUES($1,$2,$3::jsonb,$4,$5)`, m.ID, m.VersionNo, string(raw), nullableModelDate(m.Data.CheckedAt), m.UpdatedAt)
	return modelUniqueError(err, supply.ErrRevisionConflict)
}
func nullableModelDate(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func (w *modelWriteTransaction) AppendOperation(ctx context.Context, op supply.ModelOperation) error {
	if op.EntityType != "standard_model" || !modelIdentity(op.EntityID) || !modelIdentity(op.ActorID) || op.After == nil || op.After.ID != op.EntityID {
		return supply.ErrReadContract
	}
	return w.log(ctx, w.tx, op)
}

// AppendModelOperationLog maps the typed F1 operation to the shared public
// operation_log table using the caller's transaction. It does not create a
// second audit store or commit independently.
func AppendModelOperationLog(ctx context.Context, tx *sql.Tx, op supply.ModelOperation) error {
	if ctx == nil {
		return supply.ErrReadContract
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || op.EntityType != "standard_model" || !modelIdentity(op.EntityID) || !modelIdentity(op.ActorID) || op.Action == "" || op.After == nil || op.After.ID != op.EntityID || op.OccurredAt.IsZero() {
		return supply.ErrReadContract
	}
	if err := validateStoredModel(op.After); err != nil {
		return err
	}
	var before any
	if op.Before != nil {
		if op.Before.ID != op.EntityID {
			return supply.ErrReadContract
		}
		if err := validateStoredModel(op.Before); err != nil {
			return err
		}
		encoded, err := json.Marshal(op.Before)
		if err != nil {
			return err
		}
		before = string(encoded)
	}
	after, err := json.Marshal(op.After)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
 VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, before, string(after), op.Reason, op.RequestID, op.OccurredAt.UTC())
	return err
}
