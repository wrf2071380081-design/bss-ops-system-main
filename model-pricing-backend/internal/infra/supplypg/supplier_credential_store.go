package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	tpdomain "model_bss/internal/domain/testplatform"
)

// SupplierCredentialStore 是供应商**主体级**准入测试凭据的持久化层（000044）。
//
// 为什么放在主体而不是申报行：原型申报页把 URL/Key 收在表单顶部，
// 一句「仅供本主体准入测试、供申报模型共用」；行级凭据会让同一主体
// 出现多套地址，测试范围与指纹口径都跟着发虚。
//
// 隔离纪律与 SubmissionStore 一致：所有查询都带 supplier_id 条件——
// 供应商门户不挂 RequirePerm，越权防护只能落在 SQL 上。
type SupplierCredentialStore struct {
	db     *sql.DB
	schema string
}

// SupplierTestCredential 是主体级测试凭据。
type SupplierTestCredential struct {
	SupplierID int64
	BaseURL    string
	APIKey     string
	UpdatedAt  time.Time
	UpdatedBy  *int64
}

// NewSupplierCredentialStore 构造存储。
func NewSupplierCredentialStore(db *sql.DB, schema string) (*SupplierCredentialStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("supplier credential store")
	}
	return &SupplierCredentialStore{db: db, schema: schema}, nil
}

func (s *SupplierCredentialStore) table(name string) string { return s.schema + "." + name }

// Get 读取主体凭据；**未登记时返回 (nil, nil)**——调用方需要区分
// 「没登记」和「查询失败」，所以这里不用 ErrNotFound。
func (s *SupplierCredentialStore) Get(ctx context.Context, supplierID int64) (*SupplierTestCredential, error) {
	if supplierID <= 0 {
		return nil, nil
	}
	var (
		item    SupplierTestCredential
		apiKey  sql.NullString
		updated sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
SELECT supplier_id, base_url, COALESCE(api_key_enc,''), updated_at, updated_by
FROM `+s.table("supplier_test_credential")+`
WHERE supplier_id = $1`, supplierID).
		Scan(&item.SupplierID, &item.BaseURL, &apiKey, &item.UpdatedAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.APIKey = apiKey.String
	if updated.Valid {
		value := updated.Int64
		item.UpdatedBy = &value
	}
	return &item, nil
}

// Upsert 写入主体凭据并返回最新值。
//
// apiKey 传 nil 表示「留空保留原值」（原型「测试 API Key（留空保留已保存值）」）；
// 传指向空串的指针表示显式清空。baseURL 为空由调用方拦截，这里不做业务校验。
func (s *SupplierCredentialStore) Upsert(ctx context.Context, supplierID int64, baseURL string, apiKey *string, updatedBy int64) (*SupplierTestCredential, error) {
	if supplierID <= 0 {
		return nil, mappingError("supplier credential supplier")
	}
	var updatedByValue any
	if updatedBy > 0 {
		updatedByValue = updatedBy
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO `+s.table("supplier_test_credential")+` (supplier_id, base_url, api_key_enc, updated_at, updated_by)
VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (supplier_id) DO UPDATE
SET base_url    = EXCLUDED.base_url,
    api_key_enc = COALESCE($3::varchar, `+s.table("supplier_test_credential")+`.api_key_enc),
    updated_at  = now(),
    updated_by  = EXCLUDED.updated_by`,
		supplierID, strings.TrimSpace(baseURL), nullableCredentialKey(apiKey), updatedByValue); err != nil {
		return nil, err
	}
	return s.Get(ctx, supplierID)
}

// nullableCredentialKey 把「留空保留」的语义翻译成 SQL NULL。
func nullableCredentialKey(apiKey *string) any {
	if apiKey == nil {
		return nil
	}
	return strings.TrimSpace(*apiKey)
}

// RefreshOpenRowSnapshots 把主体凭据刷到**未提交**申报行的快照列与指纹上。
//
// 只处理 DRAFT / REJECTED / WITHDRAWN：这三种状态下的申报还能改、还能重测，
// 凭据一变旧结果就必须失效。已提交（PENDING_APPROVAL / PENDING_TEST / PROCESSING）
// 的记录是只读快照，不动它——真要用新凭据，得先撤回或重提。
//
// 指纹在 Go 侧算：`tpdomain.TargetFingerprint` 的分隔符与大小写规整
// 没法在 SQL 里等价复刻，硬写在 SQL 里迟早和发单口径漂移。
func (s *SupplierCredentialStore) RefreshOpenRowSnapshots(ctx context.Context, supplierID int64) error {
	cred, err := s.Get(ctx, supplierID)
	if err != nil || cred == nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.standard_model_id, r.supplier_model_name
FROM `+s.table("submission_row")+` r
JOIN `+s.table("submission")+` s ON s.id = r.submission_id
WHERE s.supplier_id = $1 AND s.status IN ('DRAFT','REJECTED','WITHDRAWN')`, supplierID)
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
		fingerprint := tpdomain.TargetFingerprint(item.modelID, item.modelName, cred.BaseURL, cred.APIKey)
		if _, err := stmt.ExecContext(ctx, item.id, cred.BaseURL, cred.APIKey, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit()
}
