package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
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
			// 外键失败：按**约束名**区分到底是谁不存在。
			// 统一报「客户不存在」会把人往错的方向带（资源或模型被并发删掉时尤其误导）。
			switch pgErr.ConstraintName {
			case "customer_resource_link_customer_profile_id_fkey":
				return customer.ErrLinkCustomerNotFound
			case "customer_resource_link_standard_model_id_fkey":
				return customer.ErrLinkStandardModelNotFound
			case "customer_resource_link_resource_id_fkey":
				return customer.ErrLinkResourceNotFound
			default:
				// 约束名识别不出来（比如以后加了新外键）：回落到通用文案，不再猜具体对象。
				return customer.ErrLinkReferenceMissing
			}
		case "23514":
			// CHECK 约束（状态枚举 / revision / 报价 ID）失败：说明上层绕过状态机直写了非法值。
			return customer.ErrLinkTransitionInvalid
		case "42703", "42P01", "42804", "42846", "42883":
			return customer.ErrLinkStoreUnavailable
		}
	}
	return err
}

// linkQueryer 让插入语句既能在 *sql.DB 上跑，也能在 *sql.Tx 上跑。
// 客户开通流程（内部 infra/supplypg/customer_onboarding_store.go）要在**同一个事务**里
// 一次性写多条绑定，所以插入逻辑必须能接受 tx，而不是只能走 *sql.DB。
type linkQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CreateLink 插入一条绑定。
//
// **本条不写 operation_log**（不是漏了）：建绑定永远发生在某个流程动作里
// （B2 的 CONFIG 完成写一批、或客户发起开通），由那个流程动作的审计覆盖
// （如 `COMPLETE_CONFIG` 里已含模型与来源明细）。逐条再写会把真正的动作淹掉，
// 追溯"这条绑定哪来的"用 `created_by + request_id + quote_id` 足够。
func (s *CustomerResourceLinkStore) CreateLink(ctx context.Context, in customer.CreateLinkRecord) (*customer.ResourceLink, error) {
	return s.createLinkWith(ctx, s.db, in)
}

// createLinkWith 是插入绑定的唯一实现（*sql.DB / *sql.Tx 共用）。
func (s *CustomerResourceLinkStore) createLinkWith(ctx context.Context, q linkQueryer, in customer.CreateLinkRecord) (*customer.ResourceLink, error) {
	query := fmt.Sprintf(`
INSERT INTO %s (customer_profile_id, standard_model_id, resource_id, quote_id, state,
                activation_note, activated_at, created_by, request_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, NULLIF($9, ''))
RETURNING %s`, s.table("customer_resource_link"), linkColumns)
	row := q.QueryRowContext(ctx, query,
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
// ApplyTransition 带 revision 乐观锁的状态迁移，**并在同一事务里写审计**。
//
// 为什么审计放在仓储而不是领域服务：状态迁移与审计必须同生共死——
// 服务层没有事务，先 UPDATE 再 INSERT 审计中间失败，会留下"状态变了但没有留痕"，
// 而下架/确认停用这类动作正是要追责的场景。放这里还有一个好处：
// Act（人工动作）与 MarkSupplyOffline/MarkSupplyRestored（事件驱动）**三条路径自动全覆盖**，
// 不会出现"HTTP 有审计、事件触发没有"的两套逻辑。
func (s *CustomerResourceLinkStore) ApplyTransition(ctx context.Context, in customer.TransitionRecord) (*customer.ResourceLink, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	defer func() { _ = tx.Rollback() }()

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
	link, err := scanLink(tx.QueryRowContext(ctx, query,
		in.ID, in.ExpectedRevision, in.To, in.StateBeforeOffline, in.Archived, in.Note, in.RequestID,
	))
	if err != nil {
		if errors.Is(mapLinkStorageError(err), customer.ErrLinkNotFound) {
			return nil, s.diagnoseTransition(ctx, in.ID, in.ExpectedRevision)
		}
		return nil, mapLinkStorageError(err)
	}
	if err := s.appendTransitionAudit(ctx, tx, link, in); err != nil {
		return nil, mapLinkStorageError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// appendTransitionAudit 写一条绑定迁移审计（与状态变更同事务）。
//
// operator_id 允许 NULL：事件驱动的迁移没有内部员工操作者（in.ActorID == nil），
// 而 operation_log.operator_id 有指向 internal_staff 的外键，写 0 会直接违反约束。
// 事件来源靠 request_id 前缀 "event:<id>" 反查，人工动作则是对应请求的 request_id。
func (s *CustomerResourceLinkStore) appendTransitionAudit(ctx context.Context, tx *sql.Tx, link *customer.ResourceLink, in customer.TransitionRecord) error {
	before, err := json.Marshal(map[string]any{"state": in.FromState})
	if err != nil {
		return err
	}
	after, err := json.Marshal(map[string]any{
		"state":                link.State,
		"archived":             link.Archived,
		"state_before_offline": link.StateBeforeOffline,
		"revision":             link.Revision,
	})
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
INSERT INTO %s (entity_type, entity_id, action, operator_id, operator_name_snapshot,
                "before", "after", reason, request_id, created_at)
VALUES ('customer_resource_link', $1, $2, $3,
        (SELECT name FROM %s WHERE id = $3), $4::jsonb, $5::jsonb, NULLIF($6, ''), NULLIF($7, ''), now())`,
		s.table("operation_log"), s.table("internal_staff"))
	_, err = tx.ExecContext(ctx, query, link.ID, in.Action, in.ActorID, string(before), string(after), in.Note, in.RequestID)
	return err
}

// ListLinksByResource 列出某条资源上未归档的客户绑定（供应域下架/上架事件联动用）。
// 只取未归档的：已归档意味着客户已经不用这个来源了，下架事件与它无关。
func (s *CustomerResourceLinkStore) ListLinksByResource(ctx context.Context, resourceID int64) ([]customer.ResourceLink, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE resource_id = $1 AND archived = FALSE ORDER BY id`,
		linkColumns, s.table("customer_resource_link"))
	rows, err := s.db.QueryContext(ctx, query, resourceID)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]customer.ResourceLink, 0, 4)
	for rows.Next() {
		link, err := scanLink(rows)
		if err != nil {
			return nil, mapLinkStorageError(err)
		}
		out = append(out, *link)
	}
	return out, rows.Err()
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
RETURNING id, resource_id, state`, s.table("customer_resource_link"))
	var archivedID, oldResourceID int64
	var oldState string
	if err := tx.QueryRowContext(ctx, archiveQuery, in.ArchiveID, in.ExpectedRevision, in.Note, in.RequestID).
		Scan(&archivedID, &oldResourceID, &oldState); err != nil {
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
	// 「重新配置来源」换的是"这个客户的这个模型由哪个货源供给"——比禁用/恢复更实质
	// （后者只是开关，前者换了供应商），所以必须留审计，且与两条写入同事务。
	// entity_id 记**新** link：审计要能顺着新绑定反查到这次换源的来源。
	if err := s.appendRebindAudit(ctx, tx, archivedID, oldResourceID, oldState, link, in); err != nil {
		return nil, mapLinkStorageError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, mapLinkStorageError(err)
	}
	return link, nil
}

// appendRebindAudit 写一条「重新配置来源」审计（与归档旧的 + 插入新的同事务）。
//
// before/after 同时带两个 link id，才能看出"从哪条换到哪条"；
// 只记新 id 的话，事后无法证明旧绑定是被这次操作归档的。
func (s *CustomerResourceLinkStore) appendRebindAudit(
	ctx context.Context, tx *sql.Tx,
	oldLinkID, oldResourceID int64, oldState string,
	link *customer.ResourceLink, in customer.RebindRecord,
) error {
	before, err := json.Marshal(map[string]any{
		"old_link_id":     oldLinkID,
		"old_resource_id": oldResourceID,
		"old_state":       oldState,
	})
	if err != nil {
		return err
	}
	after, err := json.Marshal(map[string]any{
		"new_link_id":       link.ID,
		"new_resource_id":   link.ResourceID,
		"standard_model_id": link.StandardModelID,
		"archived_old":      true,
	})
	if err != nil {
		return err
	}
	actor := in.Next.CreatedBy
	query := fmt.Sprintf(`
INSERT INTO %s (entity_type, entity_id, action, operator_id, operator_name_snapshot,
                "before", "after", reason, request_id, created_at)
VALUES ('customer_resource_link', $1, 'REBIND', $2,
        (SELECT name FROM %s WHERE id = $2), $3::jsonb, $4::jsonb, NULLIF($5, ''), NULLIF($6, ''), now())`,
		s.table("operation_log"), s.table("internal_staff"))
	_, err = tx.ExecContext(ctx, query, link.ID, actor, string(before), string(after), in.Note, in.RequestID)
	return err
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
           (array_agg(r.region ORDER BY (l.state = 'ACTIVE') DESC, l.id ASC))[1] AS region,
           (array_agg(r.coefficient ORDER BY (l.state = 'ACTIVE') DESC, l.id ASC))[1] AS coefficient
      FROM %s l
      JOIN %s r ON r.id = l.resource_id
     WHERE l.customer_profile_id = $1 AND l.archived = FALSE
     GROUP BY l.standard_model_id
)
SELECT b.standard_model_id,
       COALESCE(sm.name, '')        AS model_name,
       COALESCE(sm.official_id, '') AS official_id,
       COALESCE(sm.company, '')     AS company,
       COALESCE(sm.model_type, '')  AS model_type,
       COALESCE(b.region, '')       AS region,
       COALESCE(b.coefficient::text, '') AS coefficient,
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
			&row.StandardModelID, &row.ModelName, &row.OfficialID, &row.Company, &row.ModelType, &row.Region, &row.Coefficient, &primaryState,
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

// ListCustomerLinkDetails 列出某客户**未归档**的绑定明细（「接入模型」展开区）。
//
// 约束只有一条：**只看绑定表本身**。不 join 开通流程 / 不改用 onboarding 投影，
// 因为库里的绑定可能来自历史登记或供应域联动，不一定有对应的开通流程；
// 用流程去反查会让"没有流程的绑定"在界面上永远展不开。
//
// 资源/供应商用 LEFT JOIN：绑定的资源理论上不会丢（有外键），但供应商被删的情况下
// 这里退化成空名字，好过整页报错。
func (s *CustomerResourceLinkStore) ListCustomerLinkDetails(ctx context.Context, customerProfileID int64) ([]customer.CustomerLinkDetail, error) {
	if s == nil || s.db == nil || customerProfileID <= 0 {
		return nil, customer.ErrLinkCustomerIDOutOfRange
	}
	query := fmt.Sprintf(`
SELECT l.id,
       l.standard_model_id,
       l.resource_id,
       COALESCE(r.supplier_id, 0)  AS supplier_id,
       COALESCE(sp.name, '')       AS supplier_name,
       COALESCE(r.region, '')      AS region,
       COALESCE(r.state, '')       AS resource_state,
       l.state,
       l.revision,
       l.activated_at,
       l.created_at,
       l.updated_at
  FROM %s l
  LEFT JOIN %s r  ON r.id  = l.resource_id
  LEFT JOIN %s sp ON sp.id = r.supplier_id
 WHERE l.customer_profile_id = $1
   AND l.archived = FALSE
 ORDER BY l.standard_model_id ASC, l.id ASC`,
		s.table("customer_resource_link"), s.table("resource"), s.table("supplier"))

	rows, err := s.db.QueryContext(ctx, query, customerProfileID)
	if err != nil {
		return nil, mapLinkStorageError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]customer.CustomerLinkDetail, 0, 8)
	for rows.Next() {
		var (
			item      customer.CustomerLinkDetail
			activated sql.NullTime
		)
		if err := rows.Scan(
			&item.LinkID, &item.StandardModelID, &item.ResourceID,
			&item.SupplierID, &item.SupplierName, &item.Region, &item.ResourceState,
			&item.State, &item.Revision, &activated, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, mapLinkStorageError(err)
		}
		if activated.Valid {
			at := activated.Time
			item.ActivatedAt = &at
		}
		out = append(out, item)
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
