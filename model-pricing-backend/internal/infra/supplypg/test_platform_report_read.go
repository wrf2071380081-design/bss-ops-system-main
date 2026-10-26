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
	tpdomain "model_bss/internal/domain/testplatform"
)

// TestReportReader 实现 A 侧模型广场要的 SupplyResourceReportReader。
//
// 一行"报告"对应一次测试任务（test_job），而不是一条 test_report：
// 任务在排队/运行中也要能在页面上看到，所以主体是 job，report 只是它的产物。
//
// 契约约束（见 internal/api/supply_resource_reports.go 的 validResourceReport）：
//   - Status 只能是 RUNNING / COMPLETED / FAILED
//   - COMPLETED 时 ConnectivityPassed 与 PressurePassed 必须非 nil
//   - CacheStats.Status ∈ PROVIDED / NOT_PROVIDED / NOT_APPLICABLE / NOT_TESTED
//
// 注意：一期把"未测过"和"没通过"都落成 false（契约只接受布尔）。
// M2 引入三态后改成指针语义，这里不需要调用方改动。
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
	jobID      string
	status     string
	verdict    sql.NullString
	category   string
	createdAt  time.Time
	finishedAt sql.NullTime
	reportID   sql.NullString
	connMS     sql.NullString
	connPassed sql.NullBool
	stressPass sql.NullBool
	rps        sql.NullString
	tps        sql.NullString
	hitTokens  sql.NullInt64
	inTokens   sql.NullInt64
	ratio      sql.NullString
	ratioAvail sql.NullBool
	scene      sql.NullString
	reportFile sql.NullString
}

const reportSelect = `
SELECT j.id::text, j.status, j.verdict, j.category, j.created_at, j.finished_at,
       r.id::text, r.connection_ms::text, r.connection_passed, r.stress_passed,
       r.rps::text, r.tps::text, r.cache_hit_tokens, r.cache_input_tokens, r.scene, r.report_file,
       r.cache_hit_ratio::text, r.cache_ratio_available
FROM %s.test_job j
LEFT JOIN LATERAL (
  SELECT id, connection_ms, connection_passed, stress_passed, rps, tps,
         cache_hit_tokens, cache_input_tokens, scene, report_file, cache_hit_ratio, cache_ratio_available
  FROM %s.test_report WHERE test_job_id = j.id ORDER BY id DESC LIMIT 1
) r ON TRUE`

func (r *TestReportReader) scanRows(rows *sql.Rows) ([]reportRow, error) {
	defer func() { _ = rows.Close() }()
	var result []reportRow
	for rows.Next() {
		var item reportRow
		if err := rows.Scan(&item.jobID, &item.status, &item.verdict, &item.category, &item.createdAt, &item.finishedAt,
			&item.reportID, &item.connMS, &item.connPassed, &item.stressPass, &item.rps, &item.tps,
			&item.hitTokens, &item.inTokens, &item.scene, &item.reportFile, &item.ratio, &item.ratioAvail); err != nil {
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
	flags, err := r.categoryFlags(ctx, resource)
	if err != nil {
		return nil, err
	}
	result := &supply.ResourceReportPage{Page: page, Size: size, Total: total}
	for _, item := range items {
		view, err := buildReportView(item, resourceID, flags)
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
	flags, err := r.categoryFlags(ctx, resource)
	if err != nil {
		return nil, err
	}
	view, err := buildReportView(items[0], resourceID, flags)
	if err != nil {
		return nil, err
	}
	detail := &supply.ResourceReportDetail{ResourceReportView: *view, Files: []supply.ResourceReportFile{}}
	if items[0].reportFile.Valid && strings.TrimSpace(items[0].reportFile.String) != "" {
		detail.Files = append(detail.Files, supply.ResourceReportFile{
			ID: reportID, Name: items[0].reportFile.String, CanDownload: true,
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

// categoryFlags 取该资源各类测试的最新结论，供"本次只测了一类"时补齐另一类。
func (r *TestReportReader) categoryFlags(ctx context.Context, resource int64) (map[string]string, error) {
	flags := map[string]string{}
	rows, err := r.db.QueryContext(ctx, `
SELECT j.category, r.verdict FROM `+r.table("test_job")+` j
JOIN LATERAL (
  SELECT verdict FROM `+r.table("test_report")+` WHERE test_job_id = j.id ORDER BY id DESC LIMIT 1
) r ON TRUE
WHERE j.resource_id = $1
ORDER BY j.category, j.id DESC`, resource)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var category string
		var verdict sql.NullString
		if err := rows.Scan(&category, &verdict); err != nil {
			return nil, err
		}
		// 每个 category 只保留最新一条（ORDER BY 已保证），先到先得。
		if _, exists := flags[category]; !exists {
			flags[category] = verdict.String
		}
	}
	return flags, rows.Err()
}

// buildReportView 把一行数据翻译成 A 侧契约要求的投影。
func buildReportView(row reportRow, resourceID string, flags map[string]string) (*supply.ResourceReportView, error) {
	if _, err := parseID(row.jobID); err != nil {
		return nil, supply.ErrReadContract
	}
	view := &supply.ResourceReportView{
		ID: row.jobID, ResourceID: resourceID, CreatedAt: row.createdAt,
		CacheStats: supply.ResourceCacheStats{ReportID: row.jobID},
	}
	matches := true
	view.TargetMatches = &matches

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
		if view.Status != "RUNNING" {
			finished := row.createdAt
			view.FinishedAt = &finished
		}
	} else {
		finished := row.finishedAt.Time
		view.FinishedAt = &finished
	}

	connectivity := boolOrFalse(row.connPassed)
	if row.category != string(tpdomain.CategoryBaseline) && flags[string(tpdomain.CategoryBaseline)] != "" {
		connectivity = verdictPassed(flags[string(tpdomain.CategoryBaseline)])
	}
	pressure := boolOrFalse(row.stressPass)
	if row.category != string(tpdomain.CategoryStress) && flags[string(tpdomain.CategoryStress)] != "" {
		pressure = verdictPassed(flags[string(tpdomain.CategoryStress)])
	}
	view.ConnectivityPassed = &connectivity
	view.PressurePassed = &pressure

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

func boolOrFalse(value sql.NullBool) bool { return value.Valid && value.Bool }

func verdictPassed(verdict string) bool { return verdict == "pass" || verdict == "pass_with_warnings" }

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
