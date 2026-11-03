package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	tpdomain "model_bss/internal/domain/testplatform"
)

// 供应商准入测试「配置组」（000052）。
//
// 背景：000046 把测试 URL/Key 收成**主体级一条**（supplier_test_credential 主键就是
// supplier_id），但原型申报页是多组：卡片式列出「原单组配置」等若干组，每行模型挂一组，
// 门槛是「每组至少一个连通成功 + 整批至少一次有效压测」。
//
// 三条纪律：
//  1. **永不回显 Key**：List/Get 只给 HasAPIKey；只有 GetForTest/ListCredentials 这类
//     内部发单路径才带明文 Key，且不经过任何 JSON 序列化。
//  2. **停用不删除**：Disable 只把 enabled 置 false，历史和已挂行都不受影响；
//     停用组仍允许发起测试（在途申报不能被卡死），由调用方决定是否提示。
//  3. **越权防护落在 SQL**：供应商门户不挂 RequirePerm，所有读方法都必须带 supplier_id 条件。

var (
	// ErrTestConfigGroupNameTaken 同一主体下组名重复（唯一索引 uk_stcg_supplier_name）。
	ErrTestConfigGroupNameTaken = errors.New("该配置组名称已存在，请换一个名字")
	// ErrTestConfigGroupNotFound 组不存在，或不属于该主体（对外统一按不存在处理，避免探测）。
	ErrTestConfigGroupNotFound = errors.New("配置组不存在")
	// ErrTestConfigGroupLastEnabled 停用后该主体将没有任何启用组。
	ErrTestConfigGroupLastEnabled = errors.New("至少需要保留一个启用的配置组")
)

// TestConfigGroup 是对外的配置组（不含 Key）。
type TestConfigGroup struct {
	ID         int64
	SupplierID int64
	Name       string
	BaseURL    string
	HasAPIKey  bool
	Enabled    bool
	SortOrder  int
	UpdatedAt  time.Time
}

// TestConfigGroupCredential 仅供内部发单/算指纹使用，**不要**把它序列化出去。
type TestConfigGroupCredential struct {
	TestConfigGroup
	APIKey string
}

// TestConfigGroupPatch 是 PATCH 的入参：nil 表示该字段不改。
type TestConfigGroupPatch struct {
	Name    *string
	BaseURL *string
	// APIKey 三态：nil = 不改；指针指向空串 = 显式清空；其它 = 覆盖。
	APIKey  *string
	Enabled *bool
}

// TestConfigGroupStore 配置组持久化。
type TestConfigGroupStore struct {
	db     *sql.DB
	schema string
}

// NewTestConfigGroupStore 构造。
func NewTestConfigGroupStore(db *sql.DB, schema string) (*TestConfigGroupStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("supplier test config group store")
	}
	return &TestConfigGroupStore{db: db, schema: schema}, nil
}

func (s *TestConfigGroupStore) table(name string) string { return s.schema + "." + name }

const testConfigGroupColumns = `id, supplier_id, name, base_url, api_key_enc IS NOT NULL, enabled, sort_order, updated_at`

func scanTestConfigGroup(row interface{ Scan(...any) error }) (*TestConfigGroup, error) {
	var group TestConfigGroup
	if err := row.Scan(&group.ID, &group.SupplierID, &group.Name, &group.BaseURL,
		&group.HasAPIKey, &group.Enabled, &group.SortOrder, &group.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTestConfigGroupNotFound
		}
		return nil, err
	}
	return &group, nil
}

// List 列出主体的全部组（含已停用，前端要能展示并重新启用）。
// 排序：sort_order, id —— 与前端卡片顺序、门槛文案里的「第 N 组」保持一致。
func (s *TestConfigGroupStore) List(ctx context.Context, supplierID int64) ([]TestConfigGroup, error) {
	if supplierID <= 0 {
		return nil, ErrTestConfigGroupNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+testConfigGroupColumns+` FROM `+s.table("supplier_test_config_group")+`
WHERE supplier_id = $1 ORDER BY sort_order, id`, supplierID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]TestConfigGroup, 0, 4)
	for rows.Next() {
		group, scanErr := scanTestConfigGroup(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *group)
	}
	return out, rows.Err()
}

// ListCredentials 取主体的「组 → 凭据」映射，用于批量算指纹（避免逐行查库）。
//
// 返回的 map 含明文 Key，只允许在 api 层的指纹/发单路径里用，禁止进 DTO。
func (s *TestConfigGroupStore) ListCredentials(ctx context.Context, supplierID int64) (map[int64]TestConfigGroupCredential, error) {
	if supplierID <= 0 {
		return map[int64]TestConfigGroupCredential{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+testConfigGroupColumns+`, COALESCE(api_key_enc,'')
FROM `+s.table("supplier_test_config_group")+` WHERE supplier_id = $1 ORDER BY sort_order, id`, supplierID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64]TestConfigGroupCredential, 4)
	for rows.Next() {
		var item TestConfigGroupCredential
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.Name, &item.BaseURL, &item.HasAPIKey,
			&item.Enabled, &item.SortOrder, &item.UpdatedAt, &item.APIKey); err != nil {
			return nil, err
		}
		out[item.ID] = item
	}
	return out, rows.Err()
}

// Create 新建一组。名称重复返回 ErrTestConfigGroupNameTaken（API 映射 409）。
func (s *TestConfigGroupStore) Create(ctx context.Context, supplierID int64, name, baseURL string, apiKey *string, updatedBy int64) (*TestConfigGroup, error) {
	if supplierID <= 0 {
		return nil, ErrTestConfigGroupNotFound
	}
	// 新组排到最后：sort_order 取当前最大值 +1（前端卡片按它排列）。
	var next int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM `+s.table("supplier_test_config_group")+` WHERE supplier_id = $1`,
		supplierID).Scan(&next); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `INSERT INTO `+s.table("supplier_test_config_group")+`
(supplier_id, name, base_url, api_key_enc, sort_order, updated_by)
VALUES ($1,$2,$3,NULLIF($4,''),$5,$6)
RETURNING `+testConfigGroupColumns,
		supplierID, strings.TrimSpace(name), strings.TrimSpace(baseURL), strings.TrimSpace(stringOrEmpty(apiKey)), next, updatedBy)
	group, err := scanTestConfigGroup(row)
	if err != nil {
		if isTestConfigGroupNameConflict(err) {
			return nil, ErrTestConfigGroupNameTaken
		}
		return nil, err
	}
	return group, nil
}

// Update 改名 / 换 URL / 换 Key / 启停。
//
// APIKey 为 nil 表示「不动已保存的 Key」——这是最容易做错的一处：
// 前端编辑卡片时不回显 Key，提交时也不会带 key，所以必须能表达「保持不变」。
func (s *TestConfigGroupStore) Update(ctx context.Context, supplierID, id int64, patch TestConfigGroupPatch, updatedBy int64) (*TestConfigGroup, error) {
	if supplierID <= 0 || id <= 0 {
		return nil, ErrTestConfigGroupNotFound
	}
	sets := []string{"updated_at = now()", "updated_by = $3"}
	args := []any{id, supplierID, updatedBy}
	add := func(expr string, value any) {
		args = append(args, value)
		sets = append(sets, expr+" = $"+itoa(len(args)))
	}
	if patch.Name != nil {
		add("name", strings.TrimSpace(*patch.Name))
	}
	if patch.BaseURL != nil {
		add("base_url", strings.TrimSpace(*patch.BaseURL))
	}
	if patch.APIKey != nil {
		// 空串 = 显式清空 Key（NULL）
		add("api_key_enc", nullableTestConfigKey(strings.TrimSpace(*patch.APIKey)))
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	query := `UPDATE ` + s.table("supplier_test_config_group") + ` SET ` + strings.Join(sets, ", ") +
		` WHERE id = $1 AND supplier_id = $2 RETURNING ` + testConfigGroupColumns
	group, err := scanTestConfigGroup(s.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if isTestConfigGroupNameConflict(err) {
			return nil, ErrTestConfigGroupNameTaken
		}
		return nil, err
	}
	return group, nil
}

// Disable 只停用，不做物理删除：已挂在该组上的申报行、历史测试报告都要继续可读。
//
// **不拦「停用最后一个组」**：停用是配置动作，「先停旧组再建新组」是正常操作序列，
// 硬拦会让这个顺序做不成。兜底放在提交门槛（没有可用组就提交不了）。
func (s *TestConfigGroupStore) Disable(ctx context.Context, supplierID, id int64, updatedBy int64) (*TestConfigGroup, error) {
	if supplierID <= 0 || id <= 0 {
		return nil, ErrTestConfigGroupNotFound
	}
	return scanTestConfigGroup(s.db.QueryRowContext(ctx, `UPDATE `+s.table("supplier_test_config_group")+`
SET enabled = false, updated_at = now(), updated_by = $3
WHERE id = $1 AND supplier_id = $2
RETURNING `+testConfigGroupColumns, id, supplierID, updatedBy))
}

// GetForTest 取发单用的组凭据：校验归属，**不校验 enabled**。
//
// 为什么不校验 enabled：组被停用后在途申报仍挂着它，卡死在这里等于让供应商
// 既不能提交也不能测。由调用方（api 层）在 DTO 里标注「该组已停用」，让供应商
// 自己决定是继续测还是把行改到别的组。
func (s *TestConfigGroupStore) GetForTest(ctx context.Context, supplierID, id int64) (*TestConfigGroupCredential, error) {
	if supplierID <= 0 || id <= 0 {
		return nil, ErrTestConfigGroupNotFound
	}
	var item TestConfigGroupCredential
	err := s.db.QueryRowContext(ctx, `SELECT `+testConfigGroupColumns+`, COALESCE(api_key_enc,'')
FROM `+s.table("supplier_test_config_group")+` WHERE id = $1 AND supplier_id = $2`, id, supplierID).
		Scan(&item.ID, &item.SupplierID, &item.Name, &item.BaseURL, &item.HasAPIKey,
			&item.Enabled, &item.SortOrder, &item.UpdatedAt, &item.APIKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTestConfigGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// RefreshOpenRowSnapshotsByGroup 把**某一组**的 URL/Key 刷到挂在它上面的未提交行。
//
// 这是多配置组最核心的价值之一：改 A 组的 Key 不该让 B 组模型的测试结果失效。
// ⚠️ 不要"顺手"改回按主体全量刷新（000046 的旧语义）——那会让改一个组把别的组
// 全部判红，供应商会以为自己的测试白跑了。
//
// 只处理 DRAFT / REJECTED / WITHDRAWN：这三种状态还能改、还能重测；
// 已提交（PENDING_APPROVAL / PENDING_TEST / PROCESSING）是只读快照，不动它。
//
// 指纹在 Go 侧算（TargetFingerprint 的分隔符与大小写规整无法在 SQL 里等价复刻）。
func (s *TestConfigGroupStore) RefreshOpenRowSnapshotsByGroup(ctx context.Context, supplierID, groupID int64) error {
	if supplierID <= 0 || groupID <= 0 {
		return ErrTestConfigGroupNotFound
	}
	group, err := s.GetForTest(ctx, supplierID, groupID)
	if err != nil {
		return err
	}
	baseURL, apiKey := CredentialForTest(&SupplierTestCredential{BaseURL: group.BaseURL, APIKey: group.APIKey})
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.standard_model_id, r.supplier_model_name
FROM `+s.table("submission_row")+` r
JOIN `+s.table("submission")+` sub ON sub.id = r.submission_id
WHERE sub.supplier_id = $1 AND r.test_config_group_id = $2
  AND sub.status IN ('DRAFT','REJECTED','WITHDRAWN')`, supplierID, groupID)
	if err != nil {
		return err
	}
	type target struct {
		id        int64
		modelID   int64
		modelName string
	}
	targets := make([]target, 0, 8)
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.id, &item.modelID, &item.modelName); err != nil {
			_ = rows.Close()
			return err
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if len(targets) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `UPDATE `+s.table("submission_row")+`
SET test_base_url = $2, test_api_key_enc = NULLIF($3,''), test_fingerprint = NULLIF($4,'')
WHERE id = $1`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, item := range targets {
		fingerprint := tpdomain.TargetFingerprint(item.modelID, item.modelName, baseURL, apiKey)
		if _, err := stmt.ExecContext(ctx, item.id, baseURL, apiKey, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// nullableTestConfigKey / stringOrEmpty 是 SQL NULL 与 Go 指针之间的转换小工具。
// （不要复用 submission_store.go 的 nullableString：那是 sql.NullString 版本。）
func nullableTestConfigKey(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// isTestConfigGroupNameConflict 判断是否撞了唯一索引 uk_stcg_supplier_name（组名重复）。
// 不用 `isUniqueViolation` 这个名字：测试文件里同名函数签名不同（带约束名），会编译冲突。
func isTestConfigGroupNameConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
