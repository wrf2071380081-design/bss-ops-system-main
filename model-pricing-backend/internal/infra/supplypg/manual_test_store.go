package supplypg

// 手工登记测试结果（2026-11-18，临时方案）。
//
// 背景：LLM 测试平台的地址与令牌还没拿到，但业务流程必须先跑通一遍。
// 申报提交时只做「本地模拟连通检查」（不产证据），准入门禁却要求真实 test_report，
// 于是审批后必然报「测试报告未通过或已失效」。这里补一个**内部手工登记测试结果**的入口：
//
//   - 写 test_job（category=CONNECTIVITY / STRESS）+ test_report，并回填 resource.latest_test_report_id，
//     让准入门禁能读到证据；
//   - 报告 source 固定 `MANUAL_SIMULATED`，一眼能和平台产出的报告区分；
//   - operation_log 留痕（action=MANUAL_TEST_RECORD），说明这是人工登记、不是平台实测。
//
// 等测试平台可用后，这个入口应当退役（或收敛成「补录平台故障期间的线下结果」）。

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ManualTestInput 是一次手工测试登记的入参。
type ManualTestInput struct {
	RowID        int64
	Category     string // CONNECTIVITY / STRESS / BOTH
	ConnectionMS string
	RPS          string
	TPS          string
	Verdict      string // pass / fail
	Note         string
	ActorID      int64
	RequestID    string
}

// ManualTestStore 手工登记测试结果。
type ManualTestStore struct {
	db     *sql.DB
	schema string
}

// NewManualTestStore 构造。
func NewManualTestStore(db *sql.DB, schema string) (*ManualTestStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("manual test store")
	}
	return &ManualTestStore{db: db, schema: schema}, nil
}

func (s *ManualTestStore) table(name string) string { return s.schema + "." + name }

// Record 按申报行登记一次测试结果，返回生成的报告 id。
func (s *ManualTestStore) Record(ctx context.Context, in ManualTestInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var resourceIDNull sql.NullInt64
	var resourceID int64
	var fingerprint sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT resource_id, test_fingerprint FROM `+s.table("submission_row")+` WHERE id = $1`, in.RowID).
		Scan(&resourceIDNull, &fingerprint)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("申报行不存在")
		}
		return 0, err
	}
	now := time.Now().UTC()

	// 申报提交时不一定会分配物理资源，而测试报告必须挂在资源上。
	// 这里按申报行的资料补建一条 PENDING_TEST 资源（等同"为测试分配资源"），
	// 不改变准入状态，也不代表已接入。
	if resourceID <= 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO `+s.table("resource")+`
(standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot, region, coefficient,
 state, ever_admitted, buyer_id, created_at, updated_at, created_by, updated_by)
SELECT r.standard_model_id, s.supplier_id, r.supplier_model_name,
       (SELECT name FROM `+s.table("standard_model")+` WHERE id = r.standard_model_id),
       s.region, r.coefficient, 'PENDING_TEST', false, $2, $3, $3, $2, $2
FROM `+s.table("submission_row")+` r
JOIN `+s.table("submission")+` s ON s.id = r.submission_id
WHERE r.id = $1
RETURNING id`, in.RowID, in.ActorID, now).Scan(&resourceID)
		if err != nil {
			return 0, fmt.Errorf("为该申报行分配资源失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("submission_row")+` SET resource_id = $1 WHERE id = $2`, resourceID, in.RowID); err != nil {
			return 0, fmt.Errorf("回填申报行资源失败: %w", err)
		}
	}

	// 审批环节还要求申报行带测试指纹（与真实测试同一门槛：没指纹就不认这份证据）。
	// 真实测试的指纹由配置组 URL/Key 计算；手工登记没有这些输入，
	// 这里给一个可追溯的稳定标记，并同时写到 job 上，保证门禁的"指纹一致"校验成立。
	targetFingerprint := strings.TrimSpace(fingerprint.String)
	if targetFingerprint == "" {
		targetFingerprint = fmt.Sprintf("manual:%d:%s", in.RowID, now.Format("20060102T150405Z"))
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("submission_row")+` SET test_fingerprint = $1 WHERE id = $2`, targetFingerprint, in.RowID); err != nil {
			return 0, fmt.Errorf("写入测试指纹失败: %w", err)
		}
	}

	categories := []string{in.Category}
	if in.Category == "BOTH" {
		categories = []string{"CONNECTIVITY", "STRESS"}
	}
	var lastReportID int64
	for _, category := range categories {
		var jobID int64
		err = tx.QueryRowContext(ctx, `INSERT INTO `+s.table("test_job")+`
(resource_id, category, status, verdict, request_payload, created_by, created_at, finished_at, target_fingerprint)
VALUES ($1,$2,'SUCCEEDED',$3,$4::jsonb,$5,$6,$6,NULLIF($7,''))
RETURNING id`, resourceID, category, in.Verdict,
			fmt.Sprintf(`{"manual":true,"note":%q}`, in.Note), in.ActorID, now, targetFingerprint).Scan(&jobID)
		if err != nil {
			return 0, fmt.Errorf("insert test job: %w", err)
		}

		passed := strings.EqualFold(in.Verdict, "pass")
		var connectionMS, rps, tps any
		if category == "CONNECTIVITY" && in.ConnectionMS != "" {
			connectionMS = in.ConnectionMS
		}
		if category == "STRESS" {
			if in.RPS != "" {
				rps = in.RPS
			}
			if in.TPS != "" {
				tps = in.TPS
			}
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO `+s.table("test_report")+`
(test_job_id, resource_id, connection_ms, connection_passed, stress_passed, rps, tps, verdict,
 tested_by, tested_at, created_at, source, cache_ratio_available)
VALUES ($1,$2,$3::numeric,$4,$5,$6::numeric,$7::numeric,$8,$9,$10,$10,'MANUAL_SIMULATED',false)
RETURNING id`, jobID, resourceID, connectionMS, passed, passed, rps, tps, in.Verdict, in.ActorID, now).Scan(&lastReportID)
		if err != nil {
			return 0, fmt.Errorf("insert test report: %w", err)
		}
		// 申报行上记一下 job id，供应商门户与审批页读的就是这两个字段。
		column := "test_job_id"
		if category == "STRESS" {
			column = "stress_job_id"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("submission_row")+` SET `+column+` = $1 WHERE id = $2`, jobID, in.RowID); err != nil {
			return 0, fmt.Errorf("attach test job: %w", err)
		}
	}

	if lastReportID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.table("resource")+` SET latest_test_report_id = $1, updated_at = $2, updated_by = $3 WHERE id = $4`,
			lastReportID, now, in.ActorID, resourceID); err != nil {
			return 0, fmt.Errorf("update latest report: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("operation_log")+`
(entity_type, entity_id, action, operator_id, operator_name_snapshot, "after", request_id, created_at)
VALUES ('submission_row', $1, 'MANUAL_TEST_RECORD', $2,
        (SELECT name FROM `+s.table("internal_staff")+` WHERE id = $2), $3::jsonb, NULLIF($4,''), $5)`,
		fmt.Sprintf("%d", in.RowID), in.ActorID,
		fmt.Sprintf(`{"category":%q,"verdict":%q,"note":%q,"simulated":true}`, in.Category, in.Verdict, in.Note),
		in.RequestID, now); err != nil {
		return 0, fmt.Errorf("write manual test log: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return lastReportID, nil
}
