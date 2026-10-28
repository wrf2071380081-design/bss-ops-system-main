package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
)

// TestReportReader 实现 A 侧模型广场要的 SupplyResourceReportReader。
//
// 一行"报告"对应一次测试任务（test_job），而不是一条 test_report：
// 任务在排队/运行中也要能在页面上看到，所以主体是 job，report 只是它的产物。
//
// 契约约束（见 internal/api/supply_resource_reports.go 的 validResourceReport）：
//   - Status 只能是 RUNNING / COMPLETED / FAILED
//   - 每个测试结论仅来自本任务；未测试的类别为 nil，不拼接其他任务结论
//   - CacheStats.Status ∈ PROVIDED / NOT_PROVIDED / NOT_APPLICABLE / NOT_TESTED
//
// 排队、运行及启动失败可没有 tested_at/finished_at，不伪造测试时间。
type TestReportReader struct {
	db        *sql.DB
	schema    string
	reportDir string
}

// NewTestReportReader 构造读取器。reportDir 必须与写入侧一致。
func NewTestReportReader(db *sql.DB, schema, reportDir string) (*TestReportReader, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("test report reader")
	}
	if strings.TrimSpace(reportDir) == "" {
		reportDir = filepath.Join("bin", "test-reports")
	}
	return &TestReportReader{db: db, schema: schema, reportDir: reportDir}, nil
}

func (r *TestReportReader) table(name string) string { return r.schema + "." + name }

// reportRow 是一次任务连同其最新产物。
type reportRow struct {
	jobID              string
	status             string
	verdict            sql.NullString
	category           string
	createdAt          time.Time
	finishedAt         sql.NullTime
	testedAt           sql.NullTime
	operatorName       string
	fingerprint        sql.NullString
	currentFingerprint sql.NullString
	source             string
	reportID           sql.NullString
	connMS             sql.NullString
	connPassed         sql.NullBool
	stressPass         sql.NullBool
	rps                sql.NullString
	tps                sql.NullString
	hitTokens          sql.NullInt64
	inTokens           sql.NullInt64
	ratio              sql.NullString
	ratioAvail         sql.NullBool
	scene              sql.NullString
	reportFile         sql.NullString
}

const reportSelect = `
SELECT j.id::text, j.status, j.verdict, j.category, j.created_at, j.finished_at,
       r.id::text, r.connection_ms::text, r.connection_passed, r.stress_passed,
       r.rps::text, r.tps::text, r.cache_hit_tokens, r.cache_input_tokens, r.scene, r.report_file,
       r.cache_hit_ratio::text, r.cache_ratio_available, r.tested_at,
       COALESCE(st.name,''), j.target_fingerprint,
       (SELECT sr.test_fingerprint FROM %s.submission_row sr WHERE sr.resource_id=j.resource_id ORDER BY sr.id DESC LIMIT 1), COALESCE(j.request_payload->>'source','')
FROM %s.test_job j
LEFT JOIN LATERAL (
  SELECT id, connection_ms, connection_passed, stress_passed, rps, tps,
         cache_hit_tokens, cache_input_tokens, scene, report_file, cache_hit_ratio, cache_ratio_available, tested_at, tested_by
  FROM %s.test_report WHERE test_job_id = j.id ORDER BY id DESC LIMIT 1
) r ON TRUE
LEFT JOIN %s.internal_staff st ON st.id=COALESCE(r.tested_by,j.created_by)`

func (r *TestReportReader) scanRows(rows *sql.Rows) ([]reportRow, error) {
	defer func() { _ = rows.Close() }()
	var result []reportRow
	for rows.Next() {
		var item reportRow
		if err := rows.Scan(&item.jobID, &item.status, &item.verdict, &item.category, &item.createdAt, &item.finishedAt,
			&item.reportID, &item.connMS, &item.connPassed, &item.stressPass, &item.rps, &item.tps,
			&item.hitTokens, &item.inTokens, &item.scene, &item.reportFile, &item.ratio, &item.ratioAvail,
			&item.testedAt, &item.operatorName, &item.fingerprint, &item.currentFingerprint, &item.source); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// List 分页列出某资源的测试记录（新的在前）。
func (r *TestReportReader) List(ctx context.Context, actorID, resourceID string, page, size int) (*supply.ResourceReportPage, error) {
	resource, err := parseID(resourceID)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	if _, err := parseID(actorID); err != nil {
		return nil, supply.ErrForbidden
	}
	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+r.table("test_job")+` WHERE resource_id=$1`, resource).Scan(&total); err != nil {
		return nil, err
	}
	offset := (page - 1) * size
	rows, err := r.db.QueryContext(ctx, strings.ReplaceAll(reportSelect, "%s", r.schema)+
		`
WHERE j.resource_id = $1 ORDER BY j.id DESC LIMIT $2 OFFSET $3`, resource, size, offset)
	if err != nil {
		return nil, err
	}
	items, err := r.scanRows(rows)
	if err != nil {
		return nil, err
	}
	result := &supply.ResourceReportPage{List: []supply.ResourceReportView{}, Page: page, Size: size, Total: total}
	for _, item := range items {
		view, err := buildReportView(item, resourceID, nil)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, *view)
	}
	return result, nil
}

// Get 取单次测试记录详情（含可下载文件清单）。
func (r *TestReportReader) Get(ctx context.Context, actorID, resourceID, reportID string) (*supply.ResourceReportDetail, error) {
	resource, err := parseID(resourceID)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	jobID, err := parseID(reportID)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	if _, err := parseID(actorID); err != nil {
		return nil, supply.ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, strings.ReplaceAll(reportSelect, "%s", r.schema)+
		`
WHERE j.resource_id = $1 AND j.id = $2`, resource, jobID)
	if err != nil {
		return nil, err
	}
	items, err := r.scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, supply.ErrNotFound
	}
	view, err := buildReportView(items[0], resourceID, nil)
	if err != nil {
		return nil, err
	}
	detail := &supply.ResourceReportDetail{ResourceReportView: *view, Files: []supply.ResourceReportFile{}}
	if items[0].reportFile.Valid && strings.TrimSpace(items[0].reportFile.String) != "" {
		detail.Files = append(detail.Files, supply.ResourceReportFile{
			ID: items[0].reportID.String, Name: items[0].reportFile.String, CanDownload: true,
		})
	}
	return detail, nil
}

// OpenFile 读取本地报告副本。报告正文在收口时已落盘，不依赖平台可达性。
func (r *TestReportReader) OpenFile(ctx context.Context, actorID, resourceID, reportID, fileID string) (*supply.ResourceReportFileContent, error) {
	detail, err := r.Get(ctx, actorID, resourceID, reportID)
	if err != nil {
		return nil, err
	}
	var name string
	for _, file := range detail.Files {
		if file.ID == fileID {
			name = file.Name
			break
		}
	}
	if name == "" {
		return nil, supply.ErrForbidden
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return nil, supply.ErrReadContract
	}
	handle, err := os.Open(filepath.Join(r.reportDir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	info, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return nil, err
	}
	return &supply.ResourceReportFileContent{
		Name: name, ContentType: "text/plain", Size: info.Size(), Body: io.NopCloser(handle),
	}, nil
}

// buildReportView 把一行数据翻译成 A 侧契约要求的投影。
func buildReportView(row reportRow, resourceID string, _ map[string]string) (*supply.ResourceReportView, error) {
	if _, err := parseID(row.jobID); err != nil {
		return nil, supply.ErrReadContract
	}
	view := &supply.ResourceReportView{
		ID: row.jobID, ResourceID: resourceID, CreatedAt: row.createdAt,
		TestJobID: row.jobID, TestReportID: row.reportID.String,
		TestItem: row.category, OperatorName: row.operatorName, Scene: strings.TrimSpace(row.scene.String),
		CacheStats: supply.ResourceCacheStats{ReportID: row.jobID},
	}
	matches := row.fingerprint.Valid && row.currentFingerprint.Valid && strings.TrimSpace(row.fingerprint.String) != "" && row.fingerprint.String == row.currentFingerprint.String
	view.TargetMatches = &matches
	if row.source == "INTERNAL" || row.source == "SUPPLIER" {
		view.Source = row.source
	}
	if row.testedAt.Valid {
		tested := row.testedAt.Time.UTC()
		view.TestedAt = &tested
	}

	switch row.status {
	case "QUEUED", "RUNNING":
		view.Status = "RUNNING"
		view.CacheStats.Status = "NOT_TESTED"
		return view, nil
	case "SUCCEEDED":
		view.Status = "COMPLETED"
	case "FAILED", "ABORTED", "CANCELED":
		view.Status = "FAILED"
	default:
		view.Status = "FAILED"
	}
	if !row.finishedAt.Valid {
		view.FinishedAt = nil
		if row.testedAt.Valid {
			finished := row.testedAt.Time.UTC()
			view.FinishedAt = &finished
		}
	} else {
		finished := row.finishedAt.Time
		view.FinishedAt = &finished
	}

	if row.connPassed.Valid {
		value := row.connPassed.Bool
		view.ConnectivityPassed = &value
	}
	if row.stressPass.Valid {
		value := row.stressPass.Bool
		view.PressurePassed = &value
	}

	view.Metrics = supply.ResourceReportMetrics{
		ConnectionMS: decimalPtr(row.connMS),
		RPS:          decimalPtr(row.rps),
		TPS:          decimalPtr(row.tps),
	}
	view.CacheStats.Status = "NOT_PROVIDED"
	if row.scene.Valid && strings.TrimSpace(row.scene.String) != "" {
		view.CacheStats.Scene = strings.TrimSpace(row.scene.String)
	}
	switch {
	case row.hitTokens.Valid && row.inTokens.Valid && row.inTokens.Int64 > 0 &&
		row.hitTokens.Int64 >= 0 && row.hitTokens.Int64 <= row.inTokens.Int64:
		// 有 Token 明细：按 token 口径给，比率由 A 侧反算。
		view.CacheStats.Status = "PROVIDED"
		view.CacheStats.HitTokens = strconv.FormatInt(row.hitTokens.Int64, 10)
		view.CacheStats.InputTokens = strconv.FormatInt(row.inTokens.Int64, 10)
	case row.ratioAvail.Valid && row.ratioAvail.Bool && row.ratio.Valid:
		// 只有平台上报的比率（Q4）：直接给比率，不反算 Token。
		if percent := decimalPtr(row.ratio); percent != nil {
			view.CacheStats.Status = "PROVIDED"
			view.CacheStats.HitPercent = *percent
		}
	}
	return view, nil
}

// reportMetricPattern 限制指标为无符号十进制，负值与科学计数法一律视为缺失。
var reportMetricPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// decimalPtr 把数值列转成契约要求的十进制字符串；非法值视为缺失。
func decimalPtr(value sql.NullString) *string {
	if !value.Valid || !reportMetricPattern.MatchString(value.String) {
		return nil
	}
	trimmed := value.String
	return &trimmed
}
