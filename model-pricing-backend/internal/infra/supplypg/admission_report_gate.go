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
// 这里补三道检查，任一不满足就拒绝：
//  1. 资源上有报告，且连通、压力两项都通过
//  2. 该报告已被选为证据（operation_log 里有 SELECTED/NOOP），即走过正式评审
//  3. 测试目标指纹未变化（改了地址/Key/模型后旧报告不得再用）
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

// Check 校验资源是否具备可用测试证据。
func (g *AdmissionReportGate) Check(ctx context.Context, resourceID string) error {
	id, err := strconv.ParseInt(strings.TrimSpace(resourceID), 10, 64)
	if err != nil || id <= 0 {
		return supply.ErrNotFound
	}
	var reportID sql.NullInt64
	var connectionPassed, stressPassed sql.NullBool
	err = g.db.QueryRowContext(ctx, `
SELECT r.latest_report_id, tr.connection_passed, tr.stress_passed
FROM `+g.table("resource")+` r
LEFT JOIN `+g.table("test_report")+` tr ON tr.id = r.latest_report_id
WHERE r.id = $1`, id).Scan(&reportID, &connectionPassed, &stressPassed)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrNotFound
	}
	if err != nil {
		return err
	}
	// 没有报告，或任一项未通过，都不允许接入。
	if !reportID.Valid || !connectionPassed.Valid || !connectionPassed.Bool || !stressPassed.Valid || !stressPassed.Bool {
		return supply.ErrTestEvidenceRequired
	}
	// 必须被正式选为证据（A 侧 TestEvidenceService 写的 SELECTED/NOOP）。
	var selected int
	err = g.db.QueryRowContext(ctx, `
SELECT 1 FROM `+g.table("operation_log")+`
WHERE entity_type = 'resource' AND entity_id = $1 AND action = $2
  AND "after"->'test_evidence'->>'report_id' = $3
  AND "after"->'test_evidence'->>'outcome' IN ('SELECTED','NOOP')
LIMIT 1`, strconv.FormatInt(id, 10), resourceRevisionAction, strconv.FormatInt(reportID.Int64, 10)).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrTestEvidenceRequired
	}
	if err != nil {
		return err
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
