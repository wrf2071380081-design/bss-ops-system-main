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
	ID           int64
	SubmissionNo string
	SubmitterName string
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
	// ParticipateTest 是压力测试勾选（原型「压力测试」列）。
	ParticipateTest bool
	Company         string
	ModelType       string
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
	// ParticipateTest 表示该行参与压力测试。
	ParticipateTest bool
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

// SubmissionFilter 是申报列表的筛选条件（对应原型 6 维筛选）。
type SubmissionFilter struct {
	Status    string
	Kind      string
	Company   string
	ModelType string
	Region    string
	Submitter string
}

// Page 按供应商分页列出申报单（新的在前）。
func (s *SubmissionStore) Page(ctx context.Context, supplierID int64, f SubmissionFilter, page, size int) ([]SubmissionHeader, int64, error) {
	where := []string{"supplier_id = $1"}
	args := []any{supplierID}
	add := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		args = append(args, strings.TrimSpace(value))
		where = append(where, column+" = $"+itoa(len(args)))
	}
	add("status", f.Status)
	add("kind", f.Kind)
	add("region", f.Region)
	add("submitter_name", f.Submitter)
	// 模型公司/类型挂在行上，用 EXISTS 过滤，避免 JOIN 造成重复行。
	if v := strings.TrimSpace(f.Company); v != "" {
		args = append(args, v)
		where = append(where, `EXISTS (SELECT 1 FROM `+s.table("submission_row")+` r JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id WHERE r.submission_id = `+s.table("submission")+`.id AND m.company = $`+itoa(len(args))+`)`)
	}
	if v := strings.TrimSpace(f.ModelType); v != "" {
		args = append(args, v)
		where = append(where, `EXISTS (SELECT 1 FROM `+s.table("submission_row")+` r JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id WHERE r.submission_id = `+s.table("submission")+`.id AND m.model_type = $`+itoa(len(args))+`)`)
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("submission")+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT id, COALESCE(submission_no,''), COALESCE(submitter_name,''), supplier_id, kind, region, status, COALESCE(reason,''), revision, created_at, updated_at, submitted_at
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
		if err := rows.Scan(&item.ID, &item.SubmissionNo, &item.SubmitterName, &item.SupplierID, &item.Kind, &item.Region, &item.Status, &item.Reason,
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
	err := s.db.QueryRowContext(ctx, `SELECT id, COALESCE(submission_no,''), COALESCE(submitter_name,''), supplier_id, kind, region, status, COALESCE(reason,''), revision,
created_at, updated_at, submitted_at FROM `+s.table("submission")+` WHERE id=$1 AND supplier_id=$2`, id, supplierID).
		Scan(&item.ID, &item.SubmissionNo, &item.SubmitterName, &item.SupplierID, &item.Kind, &item.Region, &item.Status, &item.Reason,
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
       r.coefficient::text, COALESCE(r.test_base_url,''), r.row_status, r.participate_test,
       COALESCE(m.company,''), COALESCE(m.model_type,''),
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
			&item.ParticipateTest, &item.Company, &item.ModelType,
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

// editableStatuses 是允许直接编辑的申报状态。
// 含 REJECTED / WITHDRAWN / PENDING_TEST：原型「修改重提」就是原地编辑再提交，不另开接口。
var editableStatuses = []string{"DRAFT", "REJECTED", "WITHDRAWN", "PENDING_TEST"}

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
WHERE id=$2 AND supplier_id=$3 AND status = ANY($5) AND revision=$4 RETURNING revision`,
		reason, id, supplierID, expectedRevision, editableStatuses).Scan(&revision)
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
func (s *SubmissionStore) Transition(ctx context.Context, supplierID, id, expectedRevision int64, fromStatuses []string, to string, markSubmitted bool, submitterName string, submitterAccountID int64) (int64, error) {
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
		query += `, submitted_at=now(), submitter_name=NULLIF($` + itoa(len(args)+1) + `,''), submitter_account_id=NULLIF($` + itoa(len(args)+2) + `,0)`
		args = append(args, strings.TrimSpace(submitterName), submitterAccountID)
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
    test_fingerprint=NULLIF($8,''), participate_test=$9
WHERE id=$1 AND submission_id=$2`,
				row.ID, submissionID, row.StandardModelID, row.SupplierModelName,
				row.Coefficient, row.TestBaseURL, row.SealedAPIKey, fingerprint, row.ParticipateTest)
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
VALUES ($1,$2,$3,$4::numeric,NULLIF($5,''),NULLIF($6,''),$8,'PENDING',$7) RETURNING id`,
			submissionID, row.StandardModelID, row.SupplierModelName, row.Coefficient, row.TestBaseURL, row.SealedAPIKey,
			tpdomain.TargetFingerprint(row.StandardModelID, row.SupplierModelName, row.TestBaseURL, row.SealedAPIKey),
			row.ParticipateTest).Scan(&newID); err != nil {
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
VALUES ($1,$2,$3,$4::numeric,NULLIF($5,''),NULLIF($6,''),$7,'PENDING')`,
			submissionID, row.StandardModelID, row.SupplierModelName, row.Coefficient, row.TestBaseURL, row.SealedAPIKey,
			row.ParticipateTest); err != nil {
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
	OfficialID        string
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
	// 下面四组用于判断「能否跳过重测」：状态 + 发单时指纹 + 是否已有报告结论。
	TestJobStatus      string
	TestJobFingerprint string
	TestJobVerdict     string
	StressJobStatus      string
	StressJobFingerprint string
	StressJobVerdict     string
	SubmissionStatus  string
}

// TestRow 取某一行（限定供应商，防越权）。
func (s *SubmissionStore) TestRow(ctx context.Context, supplierID, submissionID, rowID int64) (*TestRow, error) {
	var item TestRow
	var resourceID, jobID, stressJobID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT r.id, r.submission_id, r.standard_model_id, COALESCE(m.official_id,''), r.supplier_model_name, COALESCE(r.test_base_url,''),
       COALESCE(r.test_api_key_enc,''), r.coefficient::text, sub.region, sub.supplier_id, sp.name,
       COALESCE(r.test_fingerprint,''), r.resource_id, r.test_job_id, r.stress_job_id, sub.status,
       COALESCE(cj.status,''), COALESCE(cj.target_fingerprint,''),
       COALESCE((SELECT tr.verdict FROM `+s.table("test_report")+` tr WHERE tr.test_job_id = cj.id ORDER BY tr.id DESC LIMIT 1),''),
       COALESCE(sj.status,''), COALESCE(sj.target_fingerprint,''),
       COALESCE((SELECT tr.verdict FROM `+s.table("test_report")+` tr WHERE tr.test_job_id = sj.id ORDER BY tr.id DESC LIMIT 1),'')
FROM `+s.table("submission_row")+` r
JOIN `+s.table("standard_model")+` m ON m.id = r.standard_model_id
JOIN `+s.table("submission")+` sub ON sub.id = r.submission_id
JOIN `+s.table("supplier")+` sp ON sp.id = sub.supplier_id
LEFT JOIN `+s.table("test_job")+` cj ON cj.id = r.test_job_id
LEFT JOIN `+s.table("test_job")+` sj ON sj.id = r.stress_job_id
WHERE r.id=$1 AND r.submission_id=$2 AND sub.supplier_id=$3`, rowID, submissionID, supplierID).
		Scan(&item.RowID, &item.SubmissionID, &item.StandardModelID, &item.OfficialID, &item.SupplierModelName, &item.TestBaseURL,
			&item.SealedAPIKey, &item.Coefficient, &item.Region, &item.SupplierID, &item.SupplierName,
			&item.TestFingerprint, &resourceID, &jobID, &stressJobID, &item.SubmissionStatus,
			&item.TestJobStatus, &item.TestJobFingerprint, &item.TestJobVerdict,
			&item.StressJobStatus, &item.StressJobFingerprint, &item.StressJobVerdict)
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
	// resource 上有唯一约束 (supplier_id, standard_model_id, region)：
	// 同一主体对同一模型只允许一个资源，重复发起测试或同一模型多行申报都会撞上。
	// 因此先 ON CONFLICT DO NOTHING，再回查取 id——已存在则复用，不再报 500。
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("resource")+`
(standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot, region, coefficient, state)
VALUES ($1,$2,$3,$4,$5,$6::numeric,'PENDING_TEST')
ON CONFLICT (supplier_id, standard_model_id, region) DO NOTHING`,
		row.StandardModelID, row.SupplierID, row.SupplierModelName, modelName, row.Region, row.Coefficient); err != nil {
		return 0, err
	}
	var resourceID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM `+s.table("resource")+`
WHERE supplier_id=$1 AND standard_model_id=$2 AND region=$3`,
		row.SupplierID, row.StandardModelID, row.Region).Scan(&resourceID); err != nil {
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
	// 只有压力测试才把 participate_test 置 true：它表示「压力测试勾选」，
	// 由连通测试顺手打开会让勾选语义失效（旧实现即如此）。
	if category == "stress" {
		column = "stress_job_id"
	}
	flag := column == "stress_job_id"
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.table("submission_row")+`
SET `+column+`=$2, participate_test=CASE WHEN $3 THEN true ELSE participate_test END WHERE id=$1`, rowID, jobID, flag)
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

// SubmitterName 解析提交人显示名：优先联系人，退到供应商名称。
// 账户体系与主体未打通（本地 supplier.contact 为空），这里只做尽力而为的回退。
func (s *SubmissionStore) SubmitterName(ctx context.Context, supplierID int64) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(NULLIF(sp.contact,''), NULLIF(sp.name,''), '')
FROM `+s.table("supplier")+` sp WHERE sp.id = $1`, supplierID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}
