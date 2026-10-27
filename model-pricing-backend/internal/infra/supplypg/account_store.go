package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 供应商门户「账户设置」持久化层（2026-10-27，M2）。
//
// 隔离纪律：供应商门户不挂 RequirePerm，越权防护只能落在 SQL 上——
// 所有读写都必须带 supplier_id，而 supplier_id 只允许来自登录态解析（ResolveSupplierID），
// 绝不接受请求参数指定。
//
// 身份链路（000043 打通后）：
//   - account.owner_type='SUPPLIER' → account.owner_id 就是 supplier.id（手机号登录/注册的新链路）
//   - account.owner_type='OPERATOR' → account.owner_id 是 subject_operator.id
//     → subject_operator.subject_id → supplier.subject_id（老链路，靠 000043 回填）
//
// 两种账号的 OperatorID 语义不同，所以解析入口必须看 account.owner_type，不能只看 OperatorID。

var (
	// ErrAccountUnresolved 登录态解析不到供应商主体（账号没绑主体 / 主体还没建）。
	ErrAccountUnresolved = errors.New("supplier account unresolved")
	// ErrRegionLocked 主体属性已锁定（有资源 / 有申报 / 曾上架），不允许修改。
	ErrRegionLocked = errors.New("supplier region locked")
	// ErrAccountMissing 供应商行不存在。
	ErrAccountMissing = errors.New("supplier account missing")
)

// AccountStore 是账户设置的持久化层。
type AccountStore struct {
	db     *sql.DB
	schema string
}

// NewAccountStore 构造。
func NewAccountStore(db *sql.DB, schema string) (*AccountStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("account store")
	}
	return &AccountStore{db: db, schema: schema}, nil
}

func (s *AccountStore) table(name string) string { return s.schema + "." + name }

// AccountRecord 是账户设置的完整读模型（一次查询取齐，供 GET/PUT 复用）。
type AccountRecord struct {
	ID                int64
	Name              string
	Region            string
	RegistrationNo    string
	Contact           string
	Phone             string
	Status            string
	SubjectID         int64
	UpdatedAt         time.Time
	ResourceCount     int64
	SubmissionCount   int64
	EverAdmitted      bool
	LicenseName       string
	LicenseRef        string
	LicenseUploadedAt *time.Time
}

// AccountUpdate 是 PUT 的可写字段。刻意不含 phone / subject_id：
// 手机号是登录键，主体归属不可由门户自助修改。
type AccountUpdate struct {
	Name           string
	RegistrationNo string
	Contact        string
	Region         string
}

// AccountLogRow 是一条账户留痕。
type AccountLogRow struct {
	ID        int64
	Action    string
	Operator  string
	CreatedAt time.Time
	Payload   map[string]any
}

// ResolveSupplierID 把登录态解析成 supplier.id（0 表示解析不到，由 handler 转 404）。
//
// 优先 accountID：只有它能区分 owner_type，而两种账号的 owner_id 语义不同。
// 退回 operatorID 仅限「账号行读不到」这一种残缺情况；**已知 owner_type 时绝不退回**，
// 否则 owner_id 是 subject_operator.id 的账号可能撞上同 id 的 supplier，读成别家的数据。
func (s *AccountStore) ResolveSupplierID(ctx context.Context, accountID, operatorID int64) (int64, error) {
	if accountID > 0 {
		var ownerType string
		var ownerID int64
		err := s.db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT owner_type, owner_id FROM %s WHERE id = $1`, s.table("account")),
			accountID).Scan(&ownerType, &ownerID)
		switch {
		case err == nil:
			if ownerType == "SUPPLIER" {
				return s.supplierByID(ctx, ownerID)
			}
			// 门户老账号：owner_id 是 subject_operator.id，经 subject_id 收敛到 supplier。
			return s.supplierBySubjectOperator(ctx, ownerID)
		case !errors.Is(err, sql.ErrNoRows):
			return 0, fmt.Errorf("load account %d: %w", accountID, err)
		}
	}
	if operatorID > 0 {
		return s.supplierByID(ctx, operatorID)
	}
	return 0, nil
}

func (s *AccountStore) supplierByID(ctx context.Context, supplierID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE id = $1`, s.table("supplier")), supplierID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find supplier %d: %w", supplierID, err)
	}
	return id, nil
}

// supplierBySubjectOperator 走 subject_operator.id → subject_id → supplier.subject_id。
// supplier.subject_id 为空（000043 没回填到）时返回 0，不猜。
func (s *AccountStore) supplierBySubjectOperator(ctx context.Context, operatorID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT s.id
  FROM %s so
  JOIN %s s ON s.subject_id = so.subject_id
 WHERE so.id = $1 AND s.subject_id IS NOT NULL`, s.table("subject_operator"), s.table("supplier")),
		operatorID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("resolve supplier by subject operator %d: %w", operatorID, err)
	}
	return id, nil
}

// accountSelect 是账户读模型的主查询；最后一段 LEFT JOIN LATERAL 取最新一份企业证明。
//
// 锁定口径（决策 D3）：有资源 / 有申报 / 曾上架，任一成立即锁定。
// resource / submission 都按 supplier_id 过滤，与 A/B 线的写入口径一致。
const accountSelect = `
SELECT s.id, s.name, s.region,
       COALESCE(s.registration_no, ''), COALESCE(s.contact, ''), COALESCE(s.phone, ''),
       s.status, COALESCE(s.subject_id, 0), s.updated_at,
       (SELECT count(*) FROM %[1]s.resource r WHERE r.supplier_id = s.id),
       (SELECT count(*) FROM %[1]s.submission sub WHERE sub.supplier_id = s.id),
       EXISTS (SELECT 1 FROM %[1]s.resource ra WHERE ra.supplier_id = s.id AND ra.ever_admitted),
       COALESCE(q.name, ''), COALESCE(q.file_ref, ''), q.uploaded_at
  FROM %[1]s.supplier s
  LEFT JOIN LATERAL (
        SELECT sq.name, sq.file_ref, sq.uploaded_at
          FROM %[1]s.supplier_qualification sq
         WHERE sq.supplier_id = s.id
         ORDER BY sq.uploaded_at DESC, sq.id DESC
         LIMIT 1
  ) q ON true
 WHERE s.id = $1`

// Load 按 supplier.id 取账户（不存在返回 ErrAccountMissing）。
func (s *AccountStore) Load(ctx context.Context, supplierID int64) (*AccountRecord, error) {
	return s.load(ctx, s.db, supplierID)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *AccountStore) load(ctx context.Context, q rowQuerier, supplierID int64) (*AccountRecord, error) {
	var rec AccountRecord
	var uploadedAt sql.NullTime
	err := q.QueryRowContext(ctx, fmt.Sprintf(accountSelect, s.schema), supplierID).Scan(
		&rec.ID, &rec.Name, &rec.Region, &rec.RegistrationNo, &rec.Contact, &rec.Phone,
		&rec.Status, &rec.SubjectID, &rec.UpdatedAt,
		&rec.ResourceCount, &rec.SubmissionCount, &rec.EverAdmitted,
		&rec.LicenseName, &rec.LicenseRef, &uploadedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountMissing
	}
	if err != nil {
		return nil, fmt.Errorf("load supplier account %d: %w", supplierID, err)
	}
	if uploadedAt.Valid {
		t := uploadedAt.Time
		rec.LicenseUploadedAt = &t
	}
	return &rec, nil
}

// Update 在一个事务里做「锁定校验 → 写回 supplier → 落留痕」。
//
// 先 SELECT ... FOR UPDATE 锁住 supplier 行，避免两个并发保存各自看到旧的 region。
// 锁定计数用单独查询取：Postgres 不允许 FOR UPDATE 与聚合出现在同一层查询里。
// 并发口径：region 守卫挂在被锁的行上，resource/submission 只增不减，读到即视为已锁定。
func (s *AccountStore) Update(ctx context.Context, supplierID int64, in AccountUpdate, operatorName, requestID string) (*AccountRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current AccountRecord
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT id, name, region, COALESCE(registration_no, ''), COALESCE(contact, ''), COALESCE(phone, '')
  FROM %s.supplier WHERE id = $1 FOR UPDATE`, s.schema), supplierID).Scan(
		&current.ID, &current.Name, &current.Region, &current.RegistrationNo, &current.Contact, &current.Phone,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountMissing
	}
	if err != nil {
		return nil, fmt.Errorf("lock supplier account %d: %w", supplierID, err)
	}

	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT
  (SELECT count(*) FROM %[1]s.resource r WHERE r.supplier_id = $1),
  (SELECT count(*) FROM %[1]s.submission sub WHERE sub.supplier_id = $1),
  EXISTS (SELECT 1 FROM %[1]s.resource ra WHERE ra.supplier_id = $1 AND ra.ever_admitted)`,
		s.schema), supplierID).Scan(
		&current.ResourceCount, &current.SubmissionCount, &current.EverAdmitted,
	); err != nil {
		return nil, fmt.Errorf("load supplier lock state %d: %w", supplierID, err)
	}

	if CurrentRegionLocked(&current) && in.Region != current.Region {
		return nil, ErrRegionLocked
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s.supplier
   SET name = $2, registration_no = $3, contact = $4, region = $5, updated_at = now()
 WHERE id = $1`, s.schema),
		supplierID, in.Name, in.RegistrationNo, in.Contact, in.Region); err != nil {
		return nil, fmt.Errorf("update supplier %d: %w", supplierID, err)
	}

	before, _ := json.Marshal(map[string]any{
		"name": current.Name, "registrationNo": current.RegistrationNo,
		"contact": current.Contact, "region": current.Region,
	})
	changed := make([]string, 0, 4)
	if in.Name != current.Name {
		changed = append(changed, "name")
	}
	if in.RegistrationNo != current.RegistrationNo {
		changed = append(changed, "registrationNo")
	}
	if in.Contact != current.Contact {
		changed = append(changed, "contact")
	}
	if in.Region != current.Region {
		changed = append(changed, "region")
	}
	after, _ := json.Marshal(map[string]any{
		"changed": changed, "name": in.Name, "registrationNo": in.RegistrationNo,
		"contact": in.Contact, "region": in.Region,
	})
	// operation_log.operator_id 外键指向 internal_staff，供应商侧没有 staff id，
	// 因此留空、只落 operator_name_snapshot（前端「操作人」列取的就是这个快照）。
	//
	// operator 兜底：对接负责人可能没填（新注册主体、或用户先点保存再补人），
	// 退到登录手机号，再退到「—」。**绝不能落空串**——前端契约要求 operator 非空，
	// 一条空串会把整个操作记录列表判为不合法（2026-10-27 用户实测踩到）。
	logOperator := firstNonEmpty(operatorName, in.Contact, current.Contact, current.Phone)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s.operation_log (entity_type, entity_id, action, operator_name_snapshot, before, after, request_id, created_at)
VALUES ('supplier_account', $1, 'UPDATE_PROFILE', $2, $3::jsonb, $4::jsonb, NULLIF($5, ''), now())`, s.schema),
		supplierID, logOperator, string(before), string(after), requestID); err != nil {
		return nil, fmt.Errorf("write account operation log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account update: %w", err)
	}
	return s.Load(ctx, supplierID)
}

// CurrentRegionLocked 是锁定判定的唯一口径（service 层也调它，避免两处逻辑漂移）。
func CurrentRegionLocked(rec *AccountRecord) bool {
	if rec == nil {
		return false
	}
	return rec.EverAdmitted || rec.ResourceCount > 0 || rec.SubmissionCount > 0
}

// SaveLicense 登记一份企业证明（替换语义：旧记录保留，取最新一条作为当前证明）。
func (s *AccountStore) SaveLicense(ctx context.Context, supplierID int64, name, fileRef, operatorName, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin license save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 上传企业证明是独立动作（可能发生在用户填对接负责人之前），
	// 所以这里也要兜底操作人：表单传的 → 库里的负责人 → 登录手机号 → 「—」。
	var contact, phone string
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT COALESCE(contact, ''), COALESCE(phone, '') FROM %s.supplier WHERE id = $1`, s.schema), supplierID).
		Scan(&contact, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountMissing
	}
	if err != nil {
		return fmt.Errorf("load supplier %d for license log: %w", supplierID, err)
	}
	logOperator := firstNonEmpty(operatorName, contact, phone)

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s.supplier_qualification (supplier_id, name, file_ref, uploaded_at)
VALUES ($1, $2, $3, now())`, s.schema), supplierID, name, fileRef); err != nil {
		return fmt.Errorf("insert supplier qualification: %w", err)
	}
	after, _ := json.Marshal(map[string]any{"licenseName": name, "fileRef": fileRef})
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s.operation_log (entity_type, entity_id, action, operator_name_snapshot, after, request_id, created_at)
VALUES ('supplier_account', $1, 'UPDATE_LICENSE', $2, $3::jsonb, NULLIF($4, ''), now())`, s.schema),
		supplierID, logOperator, string(after), requestID); err != nil {
		return fmt.Errorf("write license operation log: %w", err)
	}
	return tx.Commit()
}

// firstNonEmpty 返回第一个 trim 后非空的候选值；全空时返回「—」。
//
// 用途只有一个：operation_log.operator_name_snapshot 是给页面「操作人」列展示的快照，
// 空串会让前端契约校验判整页不合法，所以写入侧必须保证非空。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "—"
}

// ListLogs 分页读账户留痕（一期只取 entity_type='supplier_account'，决策 D4）。
func (s *AccountStore) ListLogs(ctx context.Context, supplierID int64, page, size int) ([]AccountLogRow, int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT count(*) FROM %s.operation_log WHERE entity_type = 'supplier_account' AND entity_id = $1`, s.schema),
		supplierID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count account logs: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, action, COALESCE(operator_name_snapshot, ''), created_at, COALESCE(after, '{}'::jsonb)
  FROM %s.operation_log
 WHERE entity_type = 'supplier_account' AND entity_id = $1
 ORDER BY created_at DESC, id DESC
 LIMIT $2 OFFSET $3`, s.schema), supplierID, size, (page-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list account logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	list := make([]AccountLogRow, 0, size)
	for rows.Next() {
		var item AccountLogRow
		var payload []byte
		if err := rows.Scan(&item.ID, &item.Action, &item.Operator, &item.CreatedAt, &payload); err != nil {
			return nil, 0, fmt.Errorf("scan account log: %w", err)
		}
		item.Payload = map[string]any{}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &item.Payload)
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate account logs: %w", err)
	}
	return list, total, nil
}
