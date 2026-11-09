package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"model_bss/internal/domain/supply"
)

// AdmissionReportGate 是「登记接入」前的测试证据闸门（T-13）。
//
// 背景：准入链路本身不校验测试报告——A 侧 ProposeResourceAdmission 只看配置确认，
// 所以一个没测过、没通过、或报告已失效的资源也能被登记接入。
//
// 判定口径（2026-11-05 调整）：压测平台只负责把数字测准，**不给结论**——
// stress 门类的 verdict 全平台返回空。旧实现把 stress_passed=false 当硬条件，
// 于是「平台没给结论」被当成「不通过」，任何资源都过不了准入。
// 现在改为按 verdict 判定：
//  1. 该资源至少有一份测试报告（没测过仍然拦）；
//  2. 连通、压力任一门类**没有被明确判为失败**（fail / abort / canceled 才拦，
//     空 verdict 视为未判定，放行）；
//  3. 测试目标指纹未变化（改了地址/Key/模型后旧报告不得再用）。
//
// 「报告是否被正式选为证据」只做一致性校验：有 SELECTED/NOOP 记录时必须指向当前报告；
// 历史数据从未选过证据的，以最近一次实测报告为准，不再单独拦截。
type AdmissionReportGate struct {
	db     *sql.DB
	schema string
}

// NewAdmissionReportGate 构造闸门。
func NewAdmissionReportGate(db *sql.DB, schema string) (*AdmissionReportGate, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("admission report gate")
	}
	return &AdmissionReportGate{db: db, schema: schema}, nil
}

func (g *AdmissionReportGate) table(name string) string { return g.schema + "." + name }

// failedVerdict 是平台明确判负的取值。空字符串是「未判定」，不在此列。
func failedVerdict(verdict string) bool {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "fail", "failed", "abort", "aborted", "canceled", "cancelled":
		return true
	}
	return false
}

// Check 校验资源是否具备可用测试证据。
func (g *AdmissionReportGate) Check(ctx context.Context, resourceID string) error {
	id, err := strconv.ParseInt(strings.TrimSpace(resourceID), 10, 64)
	if err != nil || id <= 0 {
		return supply.ErrNotFound
	}

	// 当前证据报告：优先用资源上的 latest_test_report_id；为空时退化为最近一次实测报告。
	var reportID sql.NullInt64
	err = g.db.QueryRowContext(ctx, `
SELECT COALESCE(r.latest_test_report_id, (SELECT max(tr.id) FROM `+g.table("test_report")+` tr WHERE tr.resource_id = r.id))
FROM `+g.table("resource")+` r
WHERE r.id = $1`, id).Scan(&reportID)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrNotFound
	}
	if err != nil {
		return err
	}
	// 一份报告都没有 = 没测过，仍然不允许接入。
	if !reportID.Valid {
		return supply.ErrTestEvidenceRequired
	}

	// 逐门类取最近一条报告看结论：连通与压力是两个独立 test_job，
	// 只看单条报告会漏掉另一门类的失败判定。
	rows, err := g.db.QueryContext(ctx, `
SELECT DISTINCT ON (j.category) j.category, COALESCE(tr.verdict, '')
FROM `+g.table("test_report")+` tr
JOIN `+g.table("test_job")+` j ON j.id = tr.test_job_id
WHERE tr.resource_id = $1
ORDER BY j.category, tr.tested_at DESC NULLS LAST, tr.id DESC`, id)
	if err != nil {
		return err
	}
	failed := false
	for rows.Next() {
		var category, verdict string
		if err := rows.Scan(&category, &verdict); err != nil {
			rows.Close()
			return err
		}
		if failedVerdict(verdict) {
			failed = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// 平台明确判负才拦；没给结论（空 verdict）按未判定放行。
	if failed {
		return supply.ErrTestEvidenceRequired
	}

	// 证据选择一致性：有选中记录时必须指向当前报告。
	// 从未选过（历史数据、或只跑过测试没走评审）不作为拦截条件，以最近一次实测为准。
	var selectedReport sql.NullInt64
	err = g.db.QueryRowContext(ctx, `
SELECT ("after"->'test_evidence'->>'report_id')::bigint
FROM `+g.table("operation_log")+`
WHERE entity_type = 'resource' AND entity_id = $1 AND action = $2
  AND "after"->'test_evidence'->>'outcome' IN ('SELECTED','NOOP')
ORDER BY id DESC
LIMIT 1`, strconv.FormatInt(id, 10), resourceRevisionAction).Scan(&selectedReport)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if selectedReport.Valid && selectedReport.Int64 != reportID.Int64 {
		return supply.ErrTestEvidenceRequired
	}

	// 指纹：测试目标变了，旧报告不再作为接入依据。
	var rowFingerprint, jobFingerprint sql.NullString
	err = g.db.QueryRowContext(ctx, `
SELECT sr.test_fingerprint, j.target_fingerprint
FROM `+g.table("test_report")+` tr
JOIN `+g.table("test_job")+` j ON j.id = tr.test_job_id
LEFT JOIN `+g.table("submission_row")+` sr ON sr.resource_id = tr.resource_id
WHERE tr.id = $1
ORDER BY sr.id DESC NULLS LAST
LIMIT 1`, reportID.Int64).Scan(&rowFingerprint, &jobFingerprint)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if rowFingerprint.Valid && jobFingerprint.Valid && rowFingerprint.String != jobFingerprint.String {
		return supply.ErrTestEvidenceRequired
	}
	return nil
}
