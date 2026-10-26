package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
)

// SubmissionStore 是供应商申报（草稿/提交/撤回）的持久化层。
//
// 隔离纪律：所有查询都带 supplier_id 条件——供应商门户不挂 RequirePerm，
// 越权防护只能落在 SQL 上（见交接文档「约定与坑」）。
type SubmissionStore struct {
	db     *sql.DB
	schema string
}

// SubmissionHeader 是申报单头。
type SubmissionHeader struct {
	ID          int64
	SupplierID  int64
	Kind        string
	Region      string
	Status      string
	Reason      string
	Revision    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SubmittedAt *time.Time
}

// SubmissionRowRecord 是申报行及其测试产物（只读投影）。
type SubmissionRowRecord struct {
	ID                int64
	StandardModelID   int64
	StandardModelName string
	OfficialID        string
	SupplierModelName string
	Coefficient       string
	TestBaseURL       string
	RowStatus         string
	// 连通（baseline）与压力（stress）分开存放，互不覆盖。
	TestFingerprint     string
	ConnectivityStatus  *string
	ConnectivityVerdict *string
	ConnectionMS        *string
	StressStatus        *string
	StressVerdict       *string
	RPS                 *string
	TPS                 *string
	// Stale 表示该测试目标的指纹与发单时不一致（改了地址/Key/模型）→ 结果不可用。
	ConnectivityStale bool
	StressStale       bool
}

// SubmissionRowInput 是写入用的行。
type SubmissionRowInput struct {
	// ID > 0 表示更新已有行（保留 test_job_id / resource_id 等测试产物），0 表示新增。
	ID                int64
	StandardModelID   int64
	SupplierModelName string
	Coefficient       string
	TestBaseURL       string
	SealedAPIKey      string
}

// SubmissionModelOption 是申报可选标准模型。
type SubmissionModelOption struct {
	ID         int64
	Name       string
	OfficialID string
	Company    string
}

// NewSubmissionStore 构造存储。
func NewSubmissionStore(db *sql.DB, schema string) (*SubmissionStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("submission store")
	}
	return &SubmissionStore{db: db, schema: schema}, nil
}

func (s *SubmissionStore) table(name string) string { return s.schema + "." + name }

// Page 按供应商分页列出申报单（新的在前）。
func (s *SubmissionStore) Page(ctx context.Context, supplierID int64, status, kind string, page, size int) ([]SubmissionHeader, int64, error) {
	where := []string{"supplier_id = $1"}
	args := []any{supplierID}
	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		where = append(where, "status = $"+itoa(len(args)))
	}
	if strings.TrimSpace(kind) != "" {
		args = append(args, strings.TrimSpace(kind))
		where = append(where, "kind = $"+itoa(len(args)))
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("submission")+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT id, supplier_id, kind, region, status, COALESCE(reason,''), revision, created_at, updated_at, submitted_at
FROM ` + s.table("submission") + ` WHERE ` + clause + `
ORDER BY created_at DESC, id DESC LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	list := make([]SubmissionHeader, 0, size)
	for rows.Next() {
		var item SubmissionHeader
		var submitted sql.NullTime
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.Kind, &item.Region, &item.Status, &item.Reason,
			&item.Revision, &item.CreatedAt, &item.UpdatedAt, &submitted); err != nil {
			return nil, 0, err
		}
		if submitted.Valid {
			item.SubmittedAt = &submitted.Time
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

// Get 取单张申报单（限定供应商）。
func (s *SubmissionStore) Get(ctx context.Context, supplierID, id int64) (*SubmissionHeader, error) {
	var item SubmissionHeader
	var submitted sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id, supplier_id, kind, region, status, COALESCE(reason,''), revision,
created_at, updated_at, submitted_at FROM `+s.table("submission")+` WHERE id=$1 AND supplier_id=$2`, id, supplierID).
		Scan(&item.ID, &item.SupplierID, &item.Kind, &item.Region, &item.Status, &item.Reason,
			&item.Revision, &item.CreatedAt, &item.UpdatedAt, &submitted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if submitted.Valid {
		item.SubmittedAt = &submitted.Time
	}
	return &item, nil
}

// Rows 批量取多张申报单的行（避免列表页 N+1）。
func (s *SubmissionStore) Rows(ctx context.Context, submissionIDs []int64) (map[int64][]SubmissionRowRecord, error) {
	result := map[int64][]SubmissionRowRecord{}
	if len(submissionIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, 0, len(submissionIDs))
	args := make([]any, 0, len(submissionIDs))
	for i, id := range submissionIDs {
		args = append(args, id)
		placeholders = append(placeholders, "$"+itoa(i+1))
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.submission_id, r.id, r.standard_model_id, m.name, COALESCE(m.official_id,''), r.supplier_model_name,
       r.coefficient::text, COALESCE(r.test_base_url,''), r.row_status,
       r.test_fingerprint, cj.status, crep.verdict, crep.connection_ms::text, cj.target_fingerprint,
       sj.status, srep.verdict, srep.rps::text, srep.tps::text, sj.target_fingerprint
FROM `+s.table("submission_row")+` r
JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id
LEFT JOIN `+s.table("test_job")+` cj ON cj.id = r.test_job_id
LEFT JOIN LATERAL (
  SELECT verdict, connection_ms FROM `+s.table("test_report")+`
  WHERE test_job_id = cj.id ORDER BY id DESC LIMIT 1
) crep ON TRUE
LEFT JOIN `+s.table("test_job")+` sj ON sj.id = r.stress_job_id
LEFT JOIN LATERAL (
  SELECT verdict, rps, tps FROM `+s.table("test_report")+`
  WHERE test_job_id = sj.id ORDER BY id DESC LIMIT 1
) srep ON TRUE
WHERE r.submission_id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY r.submission_id, r.id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var submissionID int64
		var item SubmissionRowRecord
		var connStatus, connVerdict, conn, stressStatus, stressVerdict, rps, tps sql.NullString
		var rowFP, connFP, stressFP sql.NullString
		if err := rows.Scan(&submissionID, &item.ID, &item.StandardModelID, &item.StandardModelName, &item.OfficialID,
			&item.SupplierModelName, &item.Coefficient, &item.TestBaseURL, &item.RowStatus,
			&rowFP, &connStatus, &connVerdict, &conn, &connFP,
			&stressStatus, &stressVerdict, &rps, &tps, &stressFP); err != nil {
			return nil, err
		}
		item.TestFingerprint = rowFP.String
		fingerprintChanged := func(jobFP sql.NullString) bool {
			// 老数据可能没有指纹：只有两者都存在且不等才算失效，不做无依据的误判。
			return rowFP.Valid && jobFP.Valid && rowFP.String != jobFP.String
		}
		item.ConnectivityStale = fingerprintChanged(connFP)
		item.StressStale = fingerprintChanged(stressFP)
		item.ConnectivityStatus = nullableString(connStatus)
		item.ConnectivityVerdict = nullableString(connVerdict)
		item.ConnectionMS = nullableString(conn)
		item.StressStatus = nullableString(stressStatus)
		item.StressVerdict = nullableString(stressVerdict)
		item.RPS = nullableString(rps)
		item.TPS = nullableString(tps)
		result[submissionID] = append(result[submissionID], item)
	}
	return result, rows.Err()
}

// CreateDraft 新建草稿（含行）。region 由调用方按供应商主体取。
func (s *SubmissionStore) CreateDraft(ctx context.Context, supplierID int64, kind, region, reason string, rows []SubmissionRowInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO `+s.table("submission")+`
(supplier_id, kind, region, status, reason, revision, created_at, updated_at)
VALUES ($1,$2,$3,'DRAFT',$4,1,now(),now()) RETURNING id`, supplierID, kind, region, reason).Scan(&id); err != nil {
		return 0, err
	}
	if err := insertRows(ctx, tx, s.table("submission_row"), id, rows); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateDraft 保存草稿（乐观锁）。返回新 revision；冲突返回 supply.ErrRevisionConflict。
func (s *SubmissionStore) UpdateDraft(ctx context.Context, supplierID, id, expectedRevision int64, reason string, rows []SubmissionRowInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var revision int64
	err = tx.QueryRowContext(ctx, `UPDATE `+s.table("submission")+`
SET reason=$1, revision=revision+1, updated_at=now()
WHERE id=$2 AND supplier_id=$3 AND status='DRAFT' AND revision=$4 RETURNING revision`,
		reason, id, supplierID, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, supply.ErrRevisionConflict
	}
	if err != nil {
		return 0, err
	}
	if err := s.ensureNoRunningTests(ctx, tx, id); err != nil {
		return 0, err
	}
	if err := syncRows(ctx, tx, s.table("submission_row"), id, rows); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

// ensureNoRunningTests 是测试期写锁（T-21）：
// 还有任务排队/运行时，不允许改测试目标或撤回，否则结果落库后立刻失效、白跑一轮。
func (s *SubmissionStore) ensureNoRunningTests(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, submissionID int64) error {
	var running int
	if err := q.QueryRowContext(ctx, `
SELECT count(*) FROM `+s.table("submission_row")+` r
JOIN `+s.table("test_job")+` j ON j.id = r.test_job_id OR j.id = r.stress_job_id
WHERE r.submission_id = $1 AND j.status IN ('QUEUED','RUNNING')`, submissionID).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return supply.ErrTestInProgress
	}
	return nil
}

// Transition 做状态流转（乐观锁）。fromStatuses 为空表示不限来源状态。
func (s *SubmissionStore) Transition(ctx context.Context, supplierID, id, expectedRevision int64, fromStatuses []string, to string, markSubmitted bool) (int64, error) {
	args := []any{to, id, supplierID, expectedRevision}
	clause := "id=$2 AND supplier_id=$3 AND revision=$4"
	if to == "WITHDRAWN" {
		// 测试期写锁：还在跑的时候撤回，结果会落在一条已撤回的单子上。
		if err := s.ensureNoRunningTests(ctx, s.db, id); err != nil {
			return 0, err
		}
	}
	if len(fromStatuses) > 0 {
		placeholders := make([]string, 0, len(fromStatuses))
		for _, status := range fromStatuses {
			args = append(args, status)
			placeholders = append(placeholders, "$"+itoa(len(args)))
		}
		clause += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	if markSubmitted {
		clause += " AND EXISTS (SELECT 1 FROM " + s.table("submission_row") + " r WHERE r.submission_id = " + s.table("submission") + ".id)"
	}
	var revision int64
	query := `UPDATE ` + s.table("submission") + ` SET status=$1, revision=revision+1, updated_at=now()`
	if markSubmitted {
		query += `, submitted_at=now()`
	}
	query += ` WHERE ` + clause + ` RETURNING revision`
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, supply.ErrRevisionConflict
	}
	if err != nil {
		return 0, err
	}
	return revision, nil
}

// SupplierRegion 取供应商主体区域（申报单的 region 由主体决定）。
func (s *SubmissionStore) SupplierRegion(ctx context.Context, supplierID int64) (string, error) {
	var region string
	err := s.db.QueryRowContext(ctx, `SELECT region FROM `+s.table("supplier")+` WHERE id=$1`, supplierID).Scan(&region)
	if errors.Is(err, sql.ErrNoRows) {
		return "", supply.ErrNotFound
	}
	return region, err
}

// ModelOptions 分页搜索可选标准模型（仅 ACTIVE）。
func (s *SubmissionStore) ModelOptions(ctx context.Context, keyword string, page, size int) ([]SubmissionModelOption, int64, error) {
	args := []any{}
	clause := "status='ACTIVE'"
	if kw := strings.TrimSpace(keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		clause += " AND (name ILIKE $1 OR official_id ILIKE $1 OR company ILIKE $1)"
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("standard_model")+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT id, name, COALESCE(official_id,''), COALESCE(company,'') FROM ` + s.table("standard_model") +
		` WHERE ` + clause + ` ORDER BY company, name LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	list := make([]SubmissionModelOption, 0, size)
	for rows.Next() {
		var item SubmissionModelOption
		if err := rows.Scan(&item.ID, &item.Name, &item.OfficialID, &item.Company); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

// syncRows 按行 id 做增量同步：带 id 的更新、不带的新增、未出现的删除。
//
// 为什么不能"删了重插"：test_job_id / resource_id 挂在行 id 上，
// 删掉重插会让已完成的测试结果全部丢失（前端流程是"先测再提交"，中途还会保存草稿）。
// 另外 test_api_key 传空表示"沿用原值"——接口从不回传密钥，前端拿不到旧值。
func syncRows(ctx context.Context, tx *sql.Tx, table string, submissionID int64, rows []SubmissionRowInput) error {
	kept := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			var existingKey string
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(test_api_key_enc,'') FROM `+table+`
WHERE id=$1 AND submission_id=$2`, row.ID, submissionID).Scan(&existingKey); err != nil {
				return err
			}
			effectiveKey := row.SealedAPIKey
			if strings.TrimSpace(effectiveKey) == "" {
				effectiveKey = existingKey // 前端不回传密钥：留空表示沿用
			}
			fingerprint := tpdomain.TargetFingerprint(row.StandardModelID, row.SupplierModelName, row.TestBaseURL, effectiveKey)
			result, err := tx.ExecContext(ctx, `UPDATE `+table+`
SET standard_model_id=$3, supplier_model_name=$4, coefficient=$5::numeric,
    test_base_url=NULLIF($6,''), test_api_key_enc=COALESCE(NULLIF($7,''), test_api_key_enc),
    test_fingerprint=NULLIF($8,'')
WHERE id=$1 AND submission_id=$2`,
				row.ID, submissionID, row.StandardModelID, row.SupplierModelName,
				row.Coefficient, row.TestBaseURL, row.SealedAPIKey, fingerprint)
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return err
			} else if affected == 0 {
				return supply.ErrRevisionConflict
			}
			kept = append(kept, row.ID)
			continue
		}
		var newID int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO `+table+`
(submission_id, standard_model_id, supplier_model_name, coefficient, test_base_url, test_api_key_enc, participate_test, row_status, test_fingerprint)
VALUES ($1,$2,$3,$4::numeric,NULLIF($5,''),NULLIF($6,''),false,'PENDING',$7) RETURNING id`,
			submissionID, row.StandardModelID, row.SupplierModelName, row.Coefficient, row.TestBaseURL, row.SealedAPIKey,
			tpdomain.TargetFingerprint(row.StandardModelID, row.SupplierModelName, row.TestBaseURL, row.SealedAPIKey)).Scan(&newID); err != nil {
			return err
		}
		kept = append(kept, newID)
	}
	if len(kept) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE submission_id=$1`, submissionID)
		return err
	}
	placeholders := make([]string, 0, len(kept)+1)
	args := []any{submissionID}
	for i, rowID := range kept {
		args = append(args, rowID)
		placeholders = append(placeholders, "$"+itoa(i+2))
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE submission_id=$1 AND id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func insertRows(ctx context.Context, tx *sql.Tx, table string, submissionID int64, rows []SubmissionRowInput) error {
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+`
(submission_id, standard_model_id, supplier_model_name, coefficient, test_base_url, test_api_key_enc, participate_test, row_status)
VALUES ($1,$2,$3,$4::numeric,NULLIF($5,''),NULLIF($6,''),false,'PENDING')`,
			submissionID, row.StandardModelID, row.SupplierModelName, row.Coefficient, row.TestBaseURL, row.SealedAPIKey); err != nil {
			return err
		}
	}
	return nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	trimmed := value.String
	return &trimmed
}

// itoa 避免为一个占位符引入 strconv 依赖链噪声。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestRow 是发起测试所需的一行信息（含供应商主体名，平台要求 owner）。
type TestRow struct {
	RowID             int64
	SubmissionID      int64
	StandardModelID   int64
	SupplierModelName string
	TestBaseURL       string
	SealedAPIKey      string
	Coefficient       string
	Region            string
	SupplierID        int64
	SupplierName      string
	TestFingerprint   string
	ResourceID        *int64
	TestJobID         *int64
	StressJobID       *int64
	SubmissionStatus  string
}

// TestRow 取某一行（限定供应商，防越权）。
func (s *SubmissionStore) TestRow(ctx context.Context, supplierID, submissionID, rowID int64) (*TestRow, error) {
	var item TestRow
	var resourceID, jobID, stressJobID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT r.id, r.submission_id, r.standard_model_id, r.supplier_model_name, COALESCE(r.test_base_url,''),
       COALESCE(r.test_api_key_enc,''), r.coefficient::text, sub.region, sub.supplier_id, sp.name,
       COALESCE(r.test_fingerprint,''), r.resource_id, r.test_job_id, r.stress_job_id, sub.status
FROM `+s.table("submission_row")+` r
JOIN `+s.table("submission")+` sub ON sub.id = r.submission_id
JOIN `+s.table("supplier")+` sp ON sp.id = sub.supplier_id
WHERE r.id=$1 AND r.submission_id=$2 AND sub.supplier_id=$3`, rowID, submissionID, supplierID).
		Scan(&item.RowID, &item.SubmissionID, &item.StandardModelID, &item.SupplierModelName, &item.TestBaseURL,
			&item.SealedAPIKey, &item.Coefficient, &item.Region, &item.SupplierID, &item.SupplierName,
			&item.TestFingerprint, &resourceID, &jobID, &stressJobID, &item.SubmissionStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if resourceID.Valid {
		item.ResourceID = &resourceID.Int64
	}
	if jobID.Valid {
		item.TestJobID = &jobID.Int64
	}
	if stressJobID.Valid {
		item.StressJobID = &stressJobID.Int64
	}
	return &item, nil
}

// EnsureTestResource 保证该行有一个可挂靠的资源（Q13：测试前先建 PENDING_TEST 资源）。
// 已有 resource_id 直接复用；否则建一条并回写。
func (s *SubmissionStore) EnsureTestResource(ctx context.Context, row *TestRow) (int64, error) {
	if row == nil {
		return 0, supply.ErrNotFound
	}
	if row.ResourceID != nil {
		return *row.ResourceID, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var modelName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM `+s.table("standard_model")+` WHERE id=$1`, row.StandardModelID).Scan(&modelName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, supply.ErrNotFound
		}
		return 0, err
	}
	var resourceID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO `+s.table("resource")+`
(standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot, region, coefficient, state)
VALUES ($1,$2,$3,$4,$5,$6::numeric,'PENDING_TEST') RETURNING id`,
		row.StandardModelID, row.SupplierID, row.SupplierModelName, modelName, row.Region, row.Coefficient).Scan(&resourceID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("submission_row")+` SET resource_id=$2 WHERE id=$1`, row.RowID, resourceID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return resourceID, nil
}

// AttachTestJob 把测试任务回写到申报行（打通的关键一步）。
// category 为 baseline 写 test_job_id，stress 写 stress_job_id——两个槽位互不覆盖。
func (s *SubmissionStore) AttachTestJob(ctx context.Context, rowID, jobID int64, category string) error {
	column := "test_job_id"
	if category == "stress" {
		column = "stress_job_id"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.table("submission_row")+`
SET `+column+`=$2, participate_test=true WHERE id=$1`, rowID, jobID)
	return err
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	digits := make([]byte, 0, 20)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
