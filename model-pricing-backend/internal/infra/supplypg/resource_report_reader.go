package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"strings"
	"time"
)

// ResourceReportReader projects B's native test_report/test_job facts into A's
// safe report-history contract. It never returns report URLs or storage paths.
type ResourceReportReader struct {
	schema string
	read   ModelReadRunner
}

func NewResourceReportReader(schema string, read ModelReadRunner) (*ResourceReportReader, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	if read == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return &ResourceReportReader{schema: schema, read: read}, nil
}

func (r *ResourceReportReader) table(name string) string {
	return `"` + r.schema + `"."` + name + `"`
}

func (r *ResourceReportReader) List(ctx context.Context, actorID, resourceID string, page, size int) (*supply.ResourceReportPage, error) {
	if r == nil || r.read == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if !modelIdentity(actorID) || !modelIdentity(resourceID) || !modelPageValid(page, size) {
		return nil, supply.ErrReadContract
	}
	result := &supply.ResourceReportPage{List: []supply.ResourceReportView{}, Page: page, Size: size}
	err := r.read(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+r.table("test_report")+` p JOIN `+r.table("test_job")+` j ON j.id=p.test_job_id AND j.resource_id=p.resource_id WHERE p.resource_id=$1::bigint`, resourceID).Scan(&result.Total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, r.selectSQL()+` WHERE p.resource_id=$1::bigint ORDER BY p.tested_at DESC,p.id DESC LIMIT $2 OFFSET $3`, resourceID, size, (page-1)*size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			view, _, _, err := scanResourceReport(rows)
			if err != nil {
				return err
			}
			result.List = append(result.List, view)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *ResourceReportReader) Get(ctx context.Context, actorID, resourceID, reportID string) (*supply.ResourceReportDetail, error) {
	if r == nil || r.read == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if !modelIdentity(actorID) || !modelIdentity(resourceID) || !modelIdentity(reportID) {
		return nil, supply.ErrReadContract
	}
	var detail *supply.ResourceReportDetail
	err := r.read(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		row := tx.QueryRowContext(ctx, r.selectSQL()+` WHERE p.resource_id=$1::bigint AND p.id=$2::bigint`, resourceID, reportID)
		view, hasFile, format, err := scanResourceReport(row)
		if errors.Is(err, sql.ErrNoRows) {
			return supply.ErrNotFound
		}
		if err != nil {
			return err
		}
		files := []supply.ResourceReportFile{}
		if hasFile {
			files = append(files, supply.ResourceReportFile{
				ID: reportID, Name: reportDisplayName(format), CanDownload: false,
				UnavailableReason: "当前部署尚未接入受控报告文件服务",
			})
		}
		detail = &supply.ResourceReportDetail{ResourceReportView: view, Files: files}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

func (r *ResourceReportReader) OpenFile(ctx context.Context, actorID, resourceID, reportID, fileID string) (*supply.ResourceReportFileContent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !modelIdentity(actorID) || !modelIdentity(resourceID) || !modelIdentity(reportID) || !modelIdentity(fileID) {
		return nil, supply.ErrReadContract
	}
	// test_report.report_file/report_url are untrusted location strings. Until
	// an authorized storage adapter exists, do not read local paths or fetch URLs.
	return nil, supply.ErrDependencyUnavailable
}

func (r *ResourceReportReader) selectSQL() string {
	report, job := r.table("test_report"), r.table("test_job")
	resource, logs, staff := r.table("resource"), r.table("operation_log"), r.table("internal_staff")
	return `SELECT p.id::text,p.resource_id::text,j.status,j.created_at,j.finished_at,p.tested_at,j.category,COALESCE(st.name,''),
p.connection_passed,p.stress_passed,p.connection_ms::text,p.rps::text,p.tps::text,
p.cache_hit_tokens,p.cache_input_tokens,p.scene,p.report_format,
EXISTS (SELECT 1 FROM ` + logs + ` e
  WHERE e.entity_type='resource' AND e.entity_id=p.resource_id AND e.action='RESOURCE_REVISION'
    AND e."after"->'test_evidence'->>'report_id'=p.id::text
    AND e."after"->'test_evidence'->>'outcome' IN ('SELECTED','NOOP')
    AND e."after"->'test_evidence'->>'valid'='true'
    AND e."after"->'test_evidence'->>'target_version'=latest.control->>'target_version'
    AND e."after"->'test_evidence'->>'origin_row_version'=latest.control->>'origin_row_version') AS target_matches,
(NULLIF(BTRIM(p.report_file),'') IS NOT NULL OR NULLIF(BTRIM(p.report_url),'') IS NOT NULL) AS has_file
FROM ` + report + ` p JOIN ` + job + ` j ON j.id=p.test_job_id AND j.resource_id=p.resource_id
JOIN ` + resource + ` r ON r.id=p.resource_id
LEFT JOIN ` + staff + ` st ON st.id::text=p.tested_by::text
LEFT JOIN LATERAL (SELECT l."after"->'resource' AS control FROM ` + logs + ` l
  WHERE l.entity_type='resource' AND l.entity_id=r.id AND l.action='RESOURCE_REVISION'
  ORDER BY l.id DESC LIMIT 1) latest ON TRUE`
}

type reportRowScanner interface {
	Scan(...any) error
}

func scanResourceReport(row reportRowScanner) (supply.ResourceReportView, bool, string, error) {
	var view supply.ResourceReportView
	var jobStatus string
	var testItem, operatorName string
	var createdAt, testedAt time.Time
	var finishedAt sql.NullTime
	var connectivity, pressure sql.NullBool
	var connectionMS, rps, tps sql.NullString
	var cacheHit, cacheInput sql.NullInt64
	var scene, format sql.NullString
	var targetMatches, hasFile bool
	err := row.Scan(&view.ID, &view.ResourceID, &jobStatus, &createdAt, &finishedAt, &testedAt, &testItem, &operatorName,
		&connectivity, &pressure, &connectionMS, &rps, &tps,
		&cacheHit, &cacheInput, &scene, &format, &targetMatches, &hasFile)
	if err != nil {
		return view, false, "", err
	}
	view.Status, err = reportStatus(jobStatus)
	if err != nil {
		return view, false, "", err
	}
	view.CreatedAt = createdAt.UTC()
	tested := testedAt.UTC()
	view.TestedAt = &tested
	view.TestItem = strings.TrimSpace(testItem)
	view.OperatorName = strings.TrimSpace(operatorName)
	view.Source = "" // v36 stores no trusted supplier/internal provenance for this report.
	view.Scene = strings.TrimSpace(scene.String)
	if finishedAt.Valid {
		finished := finishedAt.Time.UTC()
		view.FinishedAt = &finished
	} else if view.Status != "RUNNING" {
		// tested_at is the persisted report completion timestamp for legacy jobs
		// whose test_job.finished_at was not populated.
		finished := testedAt.UTC()
		view.FinishedAt = &finished
	}
	view.TargetMatches = &targetMatches
	if connectivity.Valid {
		value := connectivity.Bool
		view.ConnectivityPassed = &value
	}
	if pressure.Valid {
		value := pressure.Bool
		view.PressurePassed = &value
	}
	view.Metrics = supply.ResourceReportMetrics{ConnectionMS: nullableMetric(connectionMS), RPS: nullableMetric(rps), TPS: nullableMetric(tps)}
	cache := ReportCacheRow{CacheHitTokens: cacheHit, CacheInputTokens: cacheInput, Scene: scene}
	view.CacheStats, err = supply.ProjectResourceCacheStats(view.ID, cache.DomainCache())
	if err != nil {
		return view, false, "", err
	}
	return view, hasFile, strings.ToLower(strings.TrimSpace(format.String)), nil
}

func nullableMetric(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	metric := strings.TrimSpace(value.String)
	return &metric
}

func reportStatus(status string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "QUEUED", "SCHEDULED", "RUNNING", "PAUSED":
		return "RUNNING", nil
	case "SUCCEEDED":
		return "COMPLETED", nil
	case "FAILED", "ABORTED", "CANCELED":
		return "FAILED", nil
	default:
		return "", fmt.Errorf("%w: unknown test job status", supply.ErrReadContract)
	}
}

func reportDisplayName(format string) string {
	switch format {
	case "pdf":
		return "测试报告.pdf"
	case "json":
		return "测试报告.json"
	case "md", "markdown":
		return "测试报告.md"
	case "csv":
		return "测试报告.csv"
	case "xlsx":
		return "测试报告.xlsx"
	default:
		return "测试报告附件"
	}
}
