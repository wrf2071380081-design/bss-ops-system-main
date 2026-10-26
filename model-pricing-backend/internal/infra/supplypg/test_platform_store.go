package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
)

// TestPlatformStore 落库测试任务与报告。
//
// 边界：只写 test_job / test_report 两张表，报告正文按 Q12 立即落本地副本
// （平台会按 report_retention_days 归档，归档后下载接口返回 404）。
// 本实现刻意不改 resource 表：最新报告由读取侧按时间取，避免扰动 A 侧的资源不变量。
type TestPlatformStore struct {
	db        *sql.DB
	schema    string
	reportDir string
}

var _ tpdomain.Store = (*TestPlatformStore)(nil)

// NewTestPlatformStore 构造存储。reportDir 为空时用 bin/test-reports。
func NewTestPlatformStore(db *sql.DB, schema, reportDir string) (*TestPlatformStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("test platform store")
	}
	if strings.TrimSpace(reportDir) == "" {
		reportDir = filepath.Join("bin", "test-reports")
	}
	return &TestPlatformStore{db: db, schema: schema, reportDir: reportDir}, nil
}

func (s *TestPlatformStore) table(name string) string { return s.schema + "." + name }

// CreateJob 建本地任务行。带幂等键时先查后插，保证同键不重复建单。
func (s *TestPlatformStore) CreateJob(ctx context.Context, rec tpdomain.NewJobRecord) (string, error) {
	resourceID, err := parseID(rec.ResourceID)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(rec.IdempotencyKey)
	if key != "" {
		var existing string
		err := s.db.QueryRowContext(ctx,
			`SELECT id::text FROM `+s.table("test_job")+` WHERE idempotency_key=$1 ORDER BY id DESC LIMIT 1`, key).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	payload := "null"
	if len(rec.RequestPayload) > 0 && json.Valid(rec.RequestPayload) {
		payload = string(rec.RequestPayload)
	}
	var actor any
	if id, err := strconv.ParseInt(strings.TrimSpace(rec.ActorID), 10, 64); err == nil && id > 0 {
		actor = id
	}
	var reviewOf any
	if id, err := strconv.ParseInt(strings.TrimSpace(rec.ReviewOf), 10, 64); err == nil && id > 0 {
		reviewOf = id
	}
	status := rec.Status
	if status == "" {
		status = "QUEUED"
	}
	var id string
	err = s.db.QueryRowContext(ctx, `
INSERT INTO `+s.table("test_job")+`(resource_id,category,idempotency_key,status,request_payload,review_of_job_id,created_by,created_at)
VALUES ($1,$2,NULLIF($3,''),$4,$5::jsonb,$6,$7,$8) RETURNING id::text`,
		resourceID, string(rec.Category), key, status, payload, reviewOf, actor, time.Now().UTC()).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// AttachExternalJob 回填平台任务号与最新状态。
func (s *TestPlatformStore) AttachExternalJob(ctx context.Context, localID, externalID, status string) error {
	id, err := parseID(localID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(externalID) == "" {
		return errors.New("test platform: empty external job id")
	}
	if status == "" {
		status = "QUEUED"
	}
	_, err = s.db.ExecContext(ctx, `UPDATE `+s.table("test_job")+`
SET external_job_id=$2, status=$3 WHERE id=$1`, id, strings.TrimSpace(externalID), status)
	return err
}

// PendingJobs 取未终态且已有平台任务号的任务。
func (s *TestPlatformStore) PendingJobs(ctx context.Context, limit int) ([]tpdomain.PendingJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text, resource_id::text, external_job_id, category, status, created_at
FROM `+s.table("test_job")+`
WHERE status IN ('QUEUED','RUNNING') AND external_job_id IS NOT NULL
ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []tpdomain.PendingJob
	for rows.Next() {
		var item tpdomain.PendingJob
		var category string
		if err := rows.Scan(&item.ID, &item.ResourceID, &item.ExternalJobID, &category, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Category = tpdomain.Category(category)
		result = append(result, item)
	}
	return result, rows.Err()
}

// CompleteJob 收口一个任务：更新任务行，成功时写报告行与本地副本。
func (s *TestPlatformStore) CompleteJob(ctx context.Context, done tpdomain.Completion) (string, error) {
	jobID, err := parseID(done.JobID)
	if err != nil {
		return "", err
	}
	resourceID, err := parseID(done.ResourceID)
	if err != nil {
		return "", err
	}
	summary := "null"
	if len(done.ResponseSummary) > 0 && json.Valid(done.ResponseSummary) {
		summary = string(done.ResponseSummary)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("test_job")+`
SET status=$2, verdict=NULLIF($3,''), response_summary=$4::jsonb, finished_at=$5 WHERE id=$1`,
		jobID, done.FinalStatus, done.Verdict, summary, done.FinishedAt.UTC()); err != nil {
		return "", err
	}
	if done.FinalStatus != "SUCCEEDED" || len(done.ReportJSON) == 0 {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "", nil
	}

	var testedBy any
	if id, err := strconv.ParseInt(strings.TrimSpace(done.TestedBy), 10, 64); err == nil && id > 0 {
		testedBy = id
	}
	var reportID int64
	// 报告已存在时按任务号复用，避免重复收口写出两行。
	err = tx.QueryRowContext(ctx, `SELECT id FROM `+s.table("test_report")+` WHERE test_job_id=$1 ORDER BY id DESC LIMIT 1`, jobID).Scan(&reportID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		outcome := done.Outcome
		err = tx.QueryRowContext(ctx, `
INSERT INTO `+s.table("test_report")+`(
  test_job_id, resource_id, connection_ms, connection_passed, stress_passed, rps, tps,
  verdict, report_format, report_file, scene, tested_by, tested_at, created_at,
  cache_hit_tokens, cache_input_tokens, cache_hit_ratio, cache_ratio_available)
VALUES ($1,$2,$3::numeric,$4,$5,$6::numeric,$7::numeric,$8,$9,$10,$11,$12,$13,$13,$14,$15,$16::numeric,$17)
RETURNING id`,
			jobID, resourceID, outcomeValue(outcome.ConnectionMS), outcome.ConnectivityPassed, outcome.PressurePassed,
			outcomeValue(outcome.RPS), outcomeValue(outcome.TPS), done.Verdict, "json", "", outcome.Scene,
			testedBy, done.FinishedAt.UTC(), outcome.CacheHitTokens, outcome.CacheInputTokens,
			outcomeValue(outcome.CacheRatioPercent), outcome.CacheRatioProvided).Scan(&reportID)
		if err != nil {
			return "", err
		}
	}

	fileName, fileErr := s.writeReportFiles(reportID, done.ReportJSON, done.ReportMD)
	if fileErr != nil {
		return "", fileErr
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("test_report")+` SET report_file=$2 WHERE id=$1`, reportID, fileName); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return strconv.FormatInt(reportID, 10), nil
}

// writeReportFiles 把报告正文落到本地目录，返回登记用的文件名。
// 写文件失败必须回滚整笔收口，否则页面会出现"有报告行但打不开"。
func (s *TestPlatformStore) writeReportFiles(reportID int64, reportJSON, reportMD []byte) (string, error) {
	if err := os.MkdirAll(s.reportDir, 0o750); err != nil {
		return "", err
	}
	jsonName := strconv.FormatInt(reportID, 10) + ".json"
	if err := os.WriteFile(filepath.Join(s.reportDir, jsonName), reportJSON, 0o640); err != nil {
		return "", err
	}
	if len(reportMD) > 0 {
		mdName := strconv.FormatInt(reportID, 10) + ".md"
		if err := os.WriteFile(filepath.Join(s.reportDir, mdName), reportMD, 0o640); err != nil {
			return "", err
		}
	}
	return jsonName, nil
}

// ClaimEvent 登记一次回调投递。event_id 是主键，冲突即"已处理过"。
func (s *TestPlatformStore) ClaimEvent(ctx context.Context, eventID, externalJobID string, payload []byte) (bool, error) {
	body := "{}"
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return false, errors.New("test platform: callback payload is not valid json")
		}
		body = string(payload)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO `+s.table("webhook_event")+`
(event_id, job_id, payload, received_at) VALUES ($1,$2,$3::jsonb,$4)
ON CONFLICT (event_id) DO NOTHING`, eventID, externalJobID, body, time.Now().UTC())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// JobByExternalID 按平台任务号取本地任务；不存在返回 ErrNotFound。
func (s *TestPlatformStore) JobByExternalID(ctx context.Context, externalJobID string) (*tpdomain.PendingJob, error) {
	var item tpdomain.PendingJob
	var category string
	err := s.db.QueryRowContext(ctx, `
SELECT id::text, resource_id::text, external_job_id, category, status, created_at
FROM `+s.table("test_job")+` WHERE external_job_id = $1 ORDER BY id DESC LIMIT 1`, externalJobID).
		Scan(&item.ID, &item.ResourceID, &item.ExternalJobID, &category, &item.Status, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tpdomain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Category = tpdomain.Category(category)
	return &item, nil
}

// outcomeValue 把可空十进制字符串转成可空 SQL 值。
func outcomeValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// parseID 校验并转换正整数标识。
func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, mappingError("id must be a positive integer")
	}
	return id, nil
}

// ResourceTestTarget 是资源自身的测试目标配置（内部复测用，T-10）。
type ResourceTestTarget struct {
	Model        string
	BaseURL      string
	SealedAPIKey string
	SupplierID   string
	SupplierName string
}

// ResourceTestConfig 读取资源上登记的测试目标；内部复测不要求人工再填一遍。
func (s *TestPlatformStore) ResourceTestConfig(ctx context.Context, resourceID string) (*ResourceTestTarget, error) {
	id, err := parseID(resourceID)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	var target ResourceTestTarget
	err = s.db.QueryRowContext(ctx, `
SELECT r.supplier_model_name, COALESCE(r.test_base_url,''), COALESCE(r.test_api_key_enc,''),
       r.supplier_id::text, sp.name
FROM `+s.table("resource")+` r
JOIN `+s.table("supplier")+` sp ON sp.id = r.supplier_id
WHERE r.id = $1`, id).
		Scan(&target.Model, &target.BaseURL, &target.SealedAPIKey, &target.SupplierID, &target.SupplierName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}
