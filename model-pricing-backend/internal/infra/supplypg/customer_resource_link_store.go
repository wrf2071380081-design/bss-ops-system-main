package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"model_bss/internal/domain/customer"
)

// 客户「接入模型」绑定的仓储（迁移 000049）。
//
// 这一层只做三件事：
//  1. 把 SQL 写对（含两个部分唯一索引的触发与识别）；
//  2. 把 PG 错误翻译成领域错误（23505 按**约束名**区分「同资源重复」与「同模型已有生效来源」，
//     否则前端只能看到一句「数据已存在」，无法指导用户该归档哪条）；
//  3. 「重新配置来源」用同一个事务完成「归档旧的 + 插入新的」，
//     否则中途失败会留下「旧已归档、新的没建」的空窗。
//
// 所有查询都带 schema 前缀，与仓库既有 supplypg 写法保持一致。

// CustomerResourceLinkStore 绑定仓储。
type CustomerResourceLinkStore struct {
	db     *sql.DB
	schema string
}

// 编译期断言：仓储必须实现领域端口。
var _ customer.ResourceLinkStore = (*CustomerResourceLinkStore)(nil)

// NewCustomerResourceLinkStore 构造。schema 只允许合法的标识符（与 modelSchemaName 同口径）。
func NewCustomerResourceLinkStore(db *sql.DB, schema string) (*CustomerResourceLinkStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("customer resource link store")
	}
	return &CustomerResourceLinkStore{db: db, schema: schema}, nil
}

func (s *CustomerResourceLinkStore) table(name string) string { return s.schema + "." + name }

// linkColumns 是 RETURNING / SELECT 的统一列顺序，供 scanLink 消费。
const linkColumns = `id, customer_profile_id, standard_model_id, resource_id, quote_id, state,
       COALESCE(state_before_offline, ''), COALESCE(activation_note, ''), activated_at, archived,
       created_by, created_at, updated_at, revision, COALESCE(request_id, '')`

func scanLink(row interface{ Scan(dest ...any) error }) (*customer.ResourceLink, error) {
	var (
		link      customer.ResourceLink
		quoteID   sql.NullInt64
		activated sql.NullTime
	)
	if err := row.Scan(
		&link.ID, &link.CustomerProfileID, &link.StandardModelID, &link.ResourceID, &quoteID, &link.State,
		&link.StateBeforeOffline, &link.ActivationNote, &activated, &link.Archived,
		&link.CreatedBy, &link.CreatedAt, &link.UpdatedAt, &link.Revision, &link.RequestID,
	); err != nil {
		return nil, err
	}
	if quoteID.Valid {
		value := quoteID.Int64
		link.QuoteID = &value
	}
	if activated.Valid {
		value := activated.Time
		link.ActivatedAt = &value
	}
	return &link, nil
}

// mapLinkStorageError 把 PG 错误翻译成领域错误。
func mapLinkStorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return customer.ErrLinkNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			// 两个部分唯一索引的语义完全不同，必须按约束名区分：
			// 同资源重复 → 提示「已绑定此资源」；同模型第二条生效 → 提示「先归档旧绑定」。
			switch pgErr.ConstraintName {
			case "uk_crl_customer_model":
				return customer.ErrLinkModelAlreadyActive
			case "uk_crl_customer_resource":
				return customer.ErrLinkDuplicateResource
			default:
				return customer.ErrLinkDuplicateResource
			}
		case "40001":
			return customer.ErrLinkRevisionConflict
		case "23503":
			// 外键失败：客户/模型/资源不存在。
			return customer.ErrLinkCustomerNotFound
		case "23514":
			// CHECK 约束（状态枚举 / revision / 报价 ID）失败：说明上层绕过状态机直写了非法值。
			return customer.ErrLinkTransitionInvalid
		case "42703", "42P01", "42804", "42846", "42883":
			return customer.ErrLinkStoreUnavailable
		}
	}
	return err
}

// CreateLink 插入一条绑定。
func (s *CustomerResourceLinkStore) CreateLink(ctx context.Context, in customer.CreateLinkRecord) (*customer.ResourceLink, error) {
	query := fmt.Sprintf(`
INSERT INTO %s (customer_profile_id, standard_model_id, resource_id, quote_id, state,
                activation_note, activated_at, created_by, request_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, NULLIF($9, ''))
RETURNING %s`, s.table("customer_resource_link"), linkColumns)
	row := s.db.QueryRowContext(ctx, query,
		in.CustomerProfileID, in.StandardModelID, in.ResourceID, nullableLinkInt64(in.QuoteID),
		in.State, in.ActivationNote, nullableLinkTime(in.ActivatedAt), in.CreatedBy, in.RequestID,
	)
	link, err := scanLink(row)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// GetLink 按主键取一条（含已归档行）。
func (s *CustomerResourceLinkStore) GetLink(ctx context.Context, id int64) (*customer.ResourceLink, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE id = $1`, linkColumns, s.table("customer_resource_link"))
	link, err := scanLink(s.db.QueryRowContext(ctx, query, id))
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// ApplyTransition 带 revision 乐观锁的状态迁移。
//
// $3 在两个位置被使用（赋值 + CASE 判断），必须显式定型：
// 否则 PostgreSQL 会报 42P08「inconsistent types deduced for parameter $3」
// （一处按 state 列推成 varchar，一处按 CASE 里的字面量推成 text）。
//
// WHERE archived = FALSE 让「已归档的绑定不能再被系统事件改动」由 SQL 兜住；
// 更新 0 行时再查一次以便区分「不存在 / 已归档 / 版本冲突」。
func (s *CustomerResourceLinkStore) ApplyTransition(ctx context.Context, in customer.TransitionRecord) (*customer.ResourceLink, error) {
	query := fmt.Sprintf(`
UPDATE %s
   SET state = $3::varchar(24),
       state_before_offline = NULLIF($4::varchar(24), ''),
       archived = $5,
       activation_note = COALESCE(NULLIF($6, ''), activation_note),
       activated_at = CASE WHEN $3::varchar(24) = 'ACTIVE' THEN now() ELSE activated_at END,
       revision = revision + 1,
       updated_at = now(),
       request_id = COALESCE(NULLIF($7, ''), request_id)
 WHERE id = $1 AND revision = $2 AND archived = FALSE
RETURNING %s`, s.table("customer_resource_link"), linkColumns)
	link, err := scanLink(s.db.QueryRowContext(ctx, query,
		in.ID, in.ExpectedRevision, in.To, in.StateBeforeOffline, in.Archived, in.Note, in.RequestID,
	))
	if err != nil {
		if errors.Is(mapLinkStorageError(err), customer.ErrLinkNotFound) {
			return nil, s.diagnoseTransition(ctx, in.ID, in.ExpectedRevision)
		}
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// diagnoseTransition 在 UPDATE 影响 0 行后判断真实原因。
func (s *CustomerResourceLinkStore) diagnoseTransition(ctx context.Context, id int64, expectedRevision int) error {
	var (
		archived bool
		revision int
	)
	query := fmt.Sprintf(`SELECT archived, revision FROM %s WHERE id = $1`, s.table("customer_resource_link"))
	if err := s.db.QueryRowContext(ctx, query, id).Scan(&archived, &revision); err != nil {
		return mapLinkStorageError(err)
	}
	if archived {
		return customer.ErrLinkArchived
	}
	if revision != expectedRevision {
		return customer.ErrLinkRevisionConflict
	}
	return customer.ErrLinkRevisionConflict
}

// RebindLink 同一事务内「归档旧绑定 + 插入新绑定」。
func (s *CustomerResourceLinkStore) RebindLink(ctx context.Context, in customer.RebindRecord) (*customer.ResourceLink, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	defer func() { _ = tx.Rollback() }()

	archiveQuery := fmt.Sprintf(`
UPDATE %s
   SET archived = TRUE,
       activation_note = COALESCE(NULLIF($3, ''), activation_note),
       revision = revision + 1,
       updated_at = now(),
       request_id = COALESCE(NULLIF($4, ''), request_id)
 WHERE id = $1 AND revision = $2 AND archived = FALSE
RETURNING id`, s.table("customer_resource_link"))
	var archivedID int64
	if err := tx.QueryRowContext(ctx, archiveQuery, in.ArchiveID, in.ExpectedRevision, in.Note, in.RequestID).Scan(&archivedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, s.diagnoseTransition(ctx, in.ArchiveID, in.ExpectedRevision)
		}
		return nil, mapLinkStorageError(err)
	}

	insertQuery := fmt.Sprintf(`
INSERT INTO %s (customer_profile_id, standard_model_id, resource_id, quote_id, state,
                activation_note, activated_at, created_by, request_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, NULLIF($9, ''))
RETURNING %s`, s.table("customer_resource_link"), linkColumns)
	link, err := scanLink(tx.QueryRowContext(ctx, insertQuery,
		in.Next.CustomerProfileID, in.Next.StandardModelID, in.Next.ResourceID, nullableLinkInt64(in.Next.QuoteID),
		in.Next.State, in.Next.ActivationNote, nullableLinkTime(in.Next.ActivatedAt), in.Next.CreatedBy, in.Next.RequestID,
	))
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// LoadResourceFacts 取资源事实（模型、部署节点、准入状态）。
func (s *CustomerResourceLinkStore) LoadResourceFacts(ctx context.Context, resourceID int64) (*customer.ResourceFacts, error) {
	query := fmt.Sprintf(`SELECT id, standard_model_id, region, state FROM %s WHERE id = $1`, s.table("resource"))
	var facts customer.ResourceFacts
	if err := s.db.QueryRowContext(ctx, query, resourceID).Scan(&facts.ID, &facts.StandardModelID, &facts.Region, &facts.State); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, customer.ErrLinkResourceNotFound
		}
		return nil, mapLinkStorageError(err)
	}
	return &facts, nil
}

// ListCustomerModelServices 按客户聚合「接入模型」投影。
//
// region 的取法：(array_agg(... ORDER BY (state='ACTIVE') DESC, id))[1]
// —— ACTIVE 的绑定优先，没有 ACTIVE 时取最早那条绑定。
// 因为 uk_crl_customer_model 保证同一客户同一模型最多一条 ACTIVE，
// 所以有 ACTIVE 时 region 是唯一的、不会含糊。
func (s *CustomerResourceLinkStore) ListCustomerModelServices(ctx context.Context, customerProfileID int64) ([]customer.CustomerModelServiceRow, error) {
	query := fmt.Sprintf(`
WITH bound AS (
    SELECT l.standard_model_id,
           count(*)                                                     AS bound_count,
           count(*) FILTER (WHERE l.state = 'ACTIVE')                    AS active_count,
           count(*) FILTER (WHERE l.state = 'DISABLED')                  AS disabled_count,
           count(*) FILTER (WHERE l.state = 'SUPPLY_OFFLINE_PENDING')    AS offline_pending_count,
           (array_agg(l.state  ORDER BY (l.state = 'ACTIVE') DESC, l.id ASC))[1] AS primary_state,
           (array_agg(r.region ORDER BY (l.state = 'ACTIVE') DESC, l.id ASC))[1] AS region
      FROM %s l
      JOIN %s r ON r.id = l.resource_id
     WHERE l.customer_profile_id = $1 AND l.archived = FALSE
     GROUP BY l.standard_model_id
)
SELECT b.standard_model_id,
       COALESCE(sm.name, '')  AS model_name,
       COALESCE(b.region, '') AS region,
       b.primary_state,
       b.bound_count, b.active_count, b.disabled_count, b.offline_pending_count,
       (SELECT count(*)
          FROM %s r2
         WHERE r2.standard_model_id = b.standard_model_id
           AND r2.region = b.region
           AND r2.state = 'ADMITTED') AS available_count
  FROM bound b
  LEFT JOIN %s sm ON sm.id = b.standard_model_id
 ORDER BY COALESCE(sm.name, '') ASC, b.standard_model_id ASC`,
		s.table("customer_resource_link"), s.table("resource"), s.table("resource"), s.table("standard_model"))

	rows, err := s.db.QueryContext(ctx, query, customerProfileID)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]customer.CustomerModelServiceRow, 0, 8)
	for rows.Next() {
		var (
			row          customer.CustomerModelServiceRow
			primaryState sql.NullString
		)
		if err := rows.Scan(
			&row.StandardModelID, &row.ModelName, &row.Region, &primaryState,
			&row.BoundCount, &row.ActiveCount, &row.DisabledCount, &row.OfflinePendingCount,
			&row.AvailableCount,
		); err != nil {
			return nil, mapLinkStorageError(err)
		}
		row.PrimaryState = primaryState.String
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapLinkStorageError(err)
	}
	return out, nil
}

// nullableLinkInt64 / nullableLinkTime 把 Go 指针转成 SQL NULL 友好的参数。
// （database/sql 不接受 *int64 直接作为参数，必须解引用或给 nil。）
func nullableLinkInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableLinkTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
