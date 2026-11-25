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
	raw, err := scanRawModel(w.tx.QueryRowContext(ctx, `SELECT `+modelProjection(w.table("standard_model_version"))+` FROM `+w.table("standard_model")+` m WHERE m.id=$1`, id))
	if err != nil {
		return nil, err
	}
	// 写前读取必须用宽松版：内容不合规的老数据也要能取出 revision，否则维护人
	// 永远改不动它。写回仍然走 validateStoredModel（严格），不合规的新数据进不了库。
	model, _, err := raw.modelLenient()
	return model, err
}

func modelVersionValue(table, expression string) string {
	return `(SELECT ` + expression + ` FROM ` + table + ` v WHERE v.standard_model_id=m.id ORDER BY v.version_no DESC,v.id DESC LIMIT 1)`
}
func modelProjection(versions string) string {
	return `m.id::text,m.status,m.created_at,m.updated_at,
 m.company,m.name,m.official_id,m.model_type,to_json(m.input_modalities),to_json(m.output_modalities),m.capacity,m.abilities,m.price_items,m.api_access,m.lifecycle,m.sources,m.checked_at,` + modelVersionValue(versions, "v.version_no") + `,` + modelVersionValue(versions, "v.snapshot")
}

// rawModelRow 是列表投影未经解析的原始值。Scan 只做类型映射、不做契约校验，
// 所以单行数据不合契约时，仍能拿到可展示的平面列（名称/公司/类型/状态）用于降级展示。
type rawModelRow struct {
	id, status           string
	createdAt, updatedAt time.Time
	row                  ModelDataRow
	inputs, outputs      []byte
	version              sql.NullInt64
	snapshot             []byte
}

func scanRawModel(source interface{ Scan(...any) error }) (rawModelRow, error) {
	var raw rawModelRow
	err := source.Scan(&raw.id, &raw.status, &raw.createdAt, &raw.updatedAt,
		&raw.row.Company, &raw.row.Name, &raw.row.OfficialID, &raw.row.ModelType, &raw.inputs, &raw.outputs, &raw.row.Capacity, &raw.row.Abilities, &raw.row.PriceItems, &raw.row.APIAccess, &raw.row.Lifecycle, &raw.row.Sources, &raw.row.CheckedAt, &raw.version, &raw.snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return raw, supply.ErrNotFound
	}
	return raw, err
}

// model 把原始行解析成领域模型。任一处不合契约都返回错误——调用方决定是
// 「整页失败」（单条读取/写前加锁）还是「单行降级」（列表）。
func (raw rawModelRow) model() (*supply.StandardModel, error) {
	row := raw.row
	if !raw.version.Valid || raw.version.Int64 <= 0 {
		return nil, supply.ErrReadContract
	}
	m, err := decodeModelSnapshot(raw.snapshot, raw.id, uint64(raw.version.Int64))
	if err != nil {
		return nil, err
	}
	if row.InputModalities, err = modelModalities(raw.inputs, "input_modalities"); err != nil {
		return nil, err
	}
	if row.OutputModalities, err = modelModalities(raw.outputs, "output_modalities"); err != nil {
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
	if !reflect.DeepEqual(m.Data, canonical.Data) || m.Status != raw.status || !m.CreatedAt.Truncate(time.Microsecond).Equal(raw.createdAt.Truncate(time.Microsecond)) || !m.UpdatedAt.Truncate(time.Microsecond).Equal(raw.updatedAt.Truncate(time.Microsecond)) {
		return nil, supply.ErrReadContract
	}
	return m, nil
}

// modelLenient 只保证「结构能读」：内容不合规不阻断，降级成 warning 交给上层展示。
// 用途只有一个——让维护人把一条读得出来但不合规的模型打开、改好、再存回去。
func (raw rawModelRow) modelLenient() (*supply.StandardModel, []string, error) {
	row := raw.row
	if !raw.version.Valid || raw.version.Int64 <= 0 {
		return nil, nil, supply.ErrReadContract
	}
	m, snapshotWarning, err := decodeModelSnapshotLenient(raw.snapshot, raw.id, uint64(raw.version.Int64))
	if err != nil {
		return nil, nil, err
	}
	warnings := []string{}
	if snapshotWarning != "" {
		warnings = append(warnings, snapshotWarning)
	}
	if row.InputModalities, err = modelModalities(raw.inputs, "input_modalities"); err != nil {
		return nil, nil, err
	}
	if row.OutputModalities, err = modelModalities(raw.outputs, "output_modalities"); err != nil {
		return nil, nil, err
	}
	data, err := row.DomainData()
	if err != nil {
		return nil, nil, err
	}
	canonical, rowErr := supply.NewStandardModel(m.ID, m.MaintainerID, data, m.UpdatedAt)
	if rowErr != nil {
		// 表记录也不合规：详情以快照内容为主，但要同时告诉用户两边都有问题。
		warnings = append(warnings, rowErr.Error())
	} else if len(warnings) == 0 {
		// 只有两边都能规范化时，漂移比较才有意义（否则比的是两份都不合规的数据）。
		if !reflect.DeepEqual(m.Data, canonical.Data) || m.Status != raw.status ||
			!m.CreatedAt.Truncate(time.Microsecond).Equal(raw.createdAt.Truncate(time.Microsecond)) ||
			!m.UpdatedAt.Truncate(time.Microsecond).Equal(raw.updatedAt.Truncate(time.Microsecond)) {
			warnings = append(warnings, "snapshot: 快照与表记录不一致，以快照为准，保存一次即可对齐")
		}
	}
	return m, warnings, nil
}

// entry 把原始行转成列表中的一行：解析成功就是完整模型，失败就降级成占位行，
// 让这一行显示「资料异常」，而不是把整页打掉（2026-11-25）。
func (raw rawModelRow) entry() supply.ModelEntry {
	m, err := raw.model()
	if err == nil {
		return supply.ModelEntry{Model: m}
	}
	return supply.ModelEntry{Failure: &supply.ModelFailure{
		ID: raw.id, Name: raw.row.Name, Company: raw.row.Company, ModelType: raw.row.ModelType,
		Status: raw.status, UpdatedAt: raw.updatedAt, Reason: err.Error(),
	}}
}

// scanStandardModel 是严格版本：单条读取与写前加锁用它，一行不合格就是错误。
func scanStandardModel(source interface{ Scan(...any) error }) (*supply.StandardModel, error) {
	raw, err := scanRawModel(source)
	if err != nil {
		return nil, err
	}
	return raw.model()
}
// checkStoredIdentity 只判「这条记录还能不能被安全地定位」：身份、版本号、时间线、状态枚举。
func checkStoredIdentity(m *supply.StandardModel) error {
	if m == nil || !modelIdentity(m.ID) || !modelIdentity(m.MaintainerID) || m.Revision == 0 || m.Revision > math.MaxInt64 || m.VersionNo == 0 || m.VersionNo > m.Revision || m.CreatedAt.IsZero() || m.UpdatedAt.Before(m.CreatedAt) {
		return supply.ErrReadContract
	}
	if m.Status != "ACTIVE" && m.Status != "INACTIVE" {
		return supply.ErrReadContract
	}
	return nil
}

// checkStoredVerification 判「核验状态与缺失项是否自洽」。它只看字段有没有值，不看值合不合规，
// 所以内容不合规的记录也能通过这一关。
func checkStoredVerification(m *supply.StandardModel) error {
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

// validateStoredContent 判「资料本身是否符合当前规则」（价格、维度、日期、枚举）。
// 这一层代表"今天的规范"，历史数据可能不合规——读路径不该用它判生死。
func validateStoredContent(m *supply.StandardModel) error {
	// Domain validation permits incomplete drafts but rejects illegal supplied
	// values. Never reconstruct ownership or verification from missing columns.
	_, err := supply.NewStandardModel(m.ID, m.MaintainerID, m.Data, m.UpdatedAt)
	return err
}

// checkStoredShape 是读路径要的那一层：结构 + 自洽，不含业务内容校验。
func checkStoredShape(m *supply.StandardModel) error {
	if err := checkStoredIdentity(m); err != nil {
		return err
	}
	return checkStoredVerification(m)
}

func validateStoredModel(m *supply.StandardModel) error {
	if err := checkStoredIdentity(m); err != nil {
		return err
	}
	// 顺序不能换：写入时先报「哪个字段不合法」（FieldError → 400 带字段路径），
	// 再报「核验状态不自洽」，否则用户拿不到可执行的提示。
	if err := validateStoredContent(m); err != nil {
		return err
	}
	return checkStoredVerification(m)
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
	// The row lock plus the current snapshot revision above is the CAS boundary.
	// Snapshot timestamps retain Go nanoseconds, while PostgreSQL timestamptz
	// stores microseconds; comparing the snapshot timestamp to the row would
	// reject valid writes after a create on most clocks.
	updateValues := make([]any, 0, len(values)-1)
	updateValues = append(updateValues, values[:3]...)
	updateValues = append(updateValues, values[4:]...)
	result, err := w.tx.ExecContext(ctx, `UPDATE `+w.table("standard_model")+` SET status=$3,updated_at=$4,updated_by=$2,
 company=$5,name=$6,official_id=$7,model_type=$8,input_modalities=$9::text[],output_modalities=$10::text[],capacity=$11::jsonb,abilities=$12::jsonb,price_items=$13::jsonb,api_access=$14::jsonb,lifecycle=$15::jsonb,sources=$16::jsonb,checked_at=$17
 WHERE id=$1`, updateValues...)
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
		// before 是"变更前的历史快照"，只做结构校验：内容不合规（正是这次要修的那种）
		// 不能挡住修复本身——否则维护人改好也存不进去（2026-11-25 实锤）。
		if err := checkStoredShape(op.Before); err != nil {
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
