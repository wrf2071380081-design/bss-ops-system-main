package api

import (
	"errors"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// SupplyChainRuntime 是 Stage 2 的最小"申报→审批→测试→接入"编排。
// 它直接复用 A 的 ResourceCreation / TestEvidence / ResourceAdmission 服务，
// 并在同一事务里补齐 B 侧 hook（submission_row / onboarding / test_report / outbox）。
type SupplyChainRuntime struct {
	raw       *sql.DB
	urlPolicy supply.FormalURLPolicy
}

func NewSupplyChainRuntime(raw *sql.DB, policy supply.FormalURLPolicy) *SupplyChainRuntime {
	if policy == nil {
		policy = devURLPolicy{}
	}
	return &SupplyChainRuntime{raw: raw, urlPolicy: policy}
}

type devURLPolicy struct{}

func (devURLPolicy) ValidateFormalURL(context.Context, string) error { return nil }

// StandardModelOption 供应商申报可选的模型。
type StandardModelOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Company    string `json:"company"`
	ModelType  string `json:"model_type"`
	OfficialID string `json:"official_id"`
}

func (r *SupplyChainRuntime) ListStandardModels(ctx context.Context) ([]StandardModelOption, error) {
	rows, err := r.raw.QueryContext(ctx, `SELECT id::text,name,company,model_type,official_id
FROM public.standard_model WHERE status='ACTIVE' ORDER BY company,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []StandardModelOption{}
	for rows.Next() {
		var item StandardModelOption
		if err := rows.Scan(&item.ID, &item.Name, &item.Company, &item.ModelType, &item.OfficialID); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}


// ApprovalRow 审批列表行。
type ApprovalRow struct {
	ID           int64  `json:"id"`
	SupplierID   int64  `json:"supplier_id"`
	SupplierName string `json:"supplier_name"`
	Region       string `json:"region"`
	Status       string `json:"status"`
	RowCount     int    `json:"row_count"`
	FirstModel   bool   `json:"first_model"`
	SubmittedAt  string `json:"submitted_at"`
	Submitter    string `json:"submitter"`
	Kind         string `json:"kind"`
	// Revision 用于驳回时的乐观锁。
	Revision int64 `json:"revision"`
}

// Reject 把申报打回（原型「不通过」）。供应商随后可「修改重提」。
//
// 这是本期唯一的内部写状态入口：原 approve/simulate 已下线，
// 办理节点推进留给 B 线 /internal/flows/*（未实现）。
func (r *SupplyChainRuntime) Reject(ctx context.Context, submissionID, expectedRevision, staffID int64, reason, requestID string) error {
	if submissionID <= 0 || expectedRevision <= 0 || staffID <= 0 || strings.TrimSpace(reason) == "" {
		return supply.ErrReadContract
	}
	tx, err := r.raw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var revision int64
	err = tx.QueryRowContext(ctx, `UPDATE public.submission
SET status='REJECTED', revision=revision+1, updated_at=now()
WHERE id=$1 AND revision=$2 AND status IN ('PENDING_APPROVAL','PROCESSING','PENDING_TEST')
RETURNING revision`, submissionID, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	after := fmt.Sprintf(`{"status":"REJECTED","revision":%d,"reason":%q}`, revision, strings.TrimSpace(reason))
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log
(entity_type, entity_id, action, "after", reason, request_id, created_at)
VALUES ('submission', $1, 'REJECT', $2::jsonb, $3, $4, now())`,
		fmt.Sprintf("%d", submissionID), after, strings.TrimSpace(reason), requestID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplyChainRuntime) ListApprovals(ctx context.Context) ([]ApprovalRow, error) {
	rows, err := r.raw.QueryContext(ctx, `SELECT s.id,s.supplier_id,sp.name,s.region,s.status,s.kind,
       (SELECT count(*) FROM public.submission_row r WHERE r.submission_id=s.id),
       NOT EXISTS (SELECT 1 FROM public.resource res WHERE res.supplier_id=s.supplier_id AND res.ever_admitted=true),
       to_char(s.submitted_at,'YYYY-MM-DD"T"HH24:MI:SSOF'),
       COALESCE(NULLIF(sp.contact,''),sp.phone,'—'), s.revision
FROM public.submission s JOIN public.supplier sp ON sp.id=s.supplier_id
WHERE s.status IN ('PENDING_APPROVAL','PROCESSING','PENDING_TEST') ORDER BY s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ApprovalRow{}
	for rows.Next() {
		var item ApprovalRow
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.SupplierName, &item.Region, &item.Status, &item.Kind, &item.RowCount, &item.FirstModel, &item.SubmittedAt, &item.Submitter, &item.Revision); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// ApprovalHistoryRow 审批记录行。
type ApprovalHistoryRow struct {
	ID           int64  `json:"id"`
	SupplierID   int64  `json:"supplier_id"`
	SupplierName string `json:"supplier_name"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Region       string `json:"region"`
	RowCount     int    `json:"row_count"`
	Approver     string `json:"approver"`
	ApprovedAt   string `json:"approved_at"`
	Result       string `json:"result"`
	Note         string `json:"note"`
}

func (r *SupplyChainRuntime) ListApprovalHistory(ctx context.Context) ([]ApprovalHistoryRow, error) {
	rows, err := r.raw.QueryContext(ctx, `SELECT s.id,s.supplier_id,sp.name,s.kind,s.status,s.region,
       (SELECT count(*) FROM public.submission_row r WHERE r.submission_id=s.id),
       COALESCE((SELECT operator_name_snapshot FROM public.operation_log l
         WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT') ORDER BY l.id DESC LIMIT 1),''),
       COALESCE(to_char((SELECT created_at FROM public.operation_log l
         WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT') ORDER BY l.id DESC LIMIT 1),'YYYY-MM-DD"T"HH24:MI:SSOF'),''),
       COALESCE((SELECT action FROM public.operation_log l
         WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT') ORDER BY l.id DESC LIMIT 1),''),
       COALESCE((SELECT reason FROM public.operation_log l
         WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT') ORDER BY l.id DESC LIMIT 1),'')
FROM public.submission s JOIN public.supplier sp ON sp.id=s.supplier_id
WHERE s.status IN ('PROCESSING','COMPLETED','REJECTED') ORDER BY s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ApprovalHistoryRow{}
	for rows.Next() {
		var item ApprovalHistoryRow
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.SupplierName, &item.Kind, &item.Status, &item.Region, &item.RowCount, &item.Approver, &item.ApprovedAt, &item.Result, &item.Note); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// SupplierWorkspaceRow 供应商管理列表行。
type SupplierWorkspaceRow struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Region             string `json:"region"`
	Phone              string `json:"phone"`
	ResourceCount      int    `json:"resource_count"`
	CreditAmount       string `json:"credit_amount"`
	SettlementCurrency string `json:"settlement_currency"`
	TaxRate            string `json:"tax_rate"`
	ContractStatus     string `json:"contract_status"`
	CooperationStage   string `json:"cooperation_stage"`
	HandlingStatus     string `json:"handling_status"`
}

func (r *SupplyChainRuntime) ListSuppliers(ctx context.Context) ([]SupplierWorkspaceRow, error) {
	rows, err := r.raw.QueryContext(ctx, `SELECT s.id,s.name,s.region,COALESCE(s.phone,''),
       (SELECT count(*) FROM public.resource r WHERE r.supplier_id=s.id),
       COALESCE(s.credit_amount::text,''), COALESCE(s.settlement_currency,''), COALESCE(s.tax_rate::text,''),
       CASE WHEN EXISTS (SELECT 1 FROM public.supplier_formal_config c WHERE c.supplier_id=s.id) THEN '已归档' ELSE '办理中' END,
       CASE WHEN EXISTS (SELECT 1 FROM public.resource r WHERE r.supplier_id=s.id) THEN '已有' ELSE '待接入' END,
       CASE WHEN EXISTS (SELECT 1 FROM public.submission sub WHERE sub.supplier_id=s.id AND sub.status IN ('PENDING_APPROVAL','PROCESSING','PENDING_TEST')) THEN '有未完成事项' ELSE '无未完成事项' END
FROM public.supplier s ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []SupplierWorkspaceRow{}
	for rows.Next() {
		var item SupplierWorkspaceRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Region, &item.Phone, &item.ResourceCount, &item.CreditAmount, &item.SettlementCurrency, &item.TaxRate, &item.ContractStatus, &item.CooperationStage, &item.HandlingStatus); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (r *SupplyChainRuntime) isApprover(ctx context.Context, staffID int64) (bool, error) {
	var ok bool
	if err := r.raw.QueryRowContext(ctx, `SELECT can_approve_supplier FROM public.internal_staff WHERE id=$1`, staffID).Scan(&ok); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}

// Approve 领导通过申报并分配测试/合同办理人。
func (r *SupplyChainRuntime) Approve(ctx context.Context, submissionID, actorID, testAssignee, contractAssignee int64, reason string) error {
	ok, err := r.isApprover(ctx, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return supply.ErrForbidden
	}
	if testAssignee <= 0 {
		return fmt.Errorf("test assignee is required")
	}
	tx, err := r.raw.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var supplierID int64
	var status, region string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,status,region FROM public.submission WHERE id=$1 FOR UPDATE`, submissionID).Scan(&supplierID, &status, &region); err != nil {
		if err == sql.ErrNoRows {
			return supply.ErrNotFound
		}
		return err
	}
	if status != "PENDING_APPROVAL" {
		return supply.ErrRevisionConflict
	}
	var firstModel bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM public.resource WHERE supplier_id=$1 AND ever_admitted=true)`, supplierID).Scan(&firstModel); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.submission SET status='PROCESSING' WHERE id=$1`, submissionID); err != nil {
		return err
	}
	var flowID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.onboarding_flow(kind,owner_id,status,current_node)
VALUES('SUPPLIER',$1,'PROCESSING','CONFIG') RETURNING id`, supplierID).Scan(&flowID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.onboarding_node(flow_id,node_key,status,assignee_id)
VALUES($1,'CONFIG','IN_PROGRESS',$2)`, flowID, testAssignee); err != nil {
		return err
	}
	if firstModel && contractAssignee > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO public.onboarding_node(flow_id,node_key,status,assignee_id)
VALUES($1,'CONTRACT','WAITING_PREDECESSOR',$2)`, flowID, contractAssignee); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.assignment_log(entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by)
SELECT 'supplier',$1,'buyer_id',NULL,$2,$3
WHERE NOT EXISTS (SELECT 1 FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id')`, supplierID, testAssignee, actorID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log
(entity_type,entity_id,action,operator_id,operator_name_snapshot,reason,request_id,created_at)
VALUES('submission',$1,'APPROVE',$2,(SELECT name FROM public.internal_staff WHERE id=$2),$3,$4,now())`,
		submissionID, actorID, strings.TrimSpace(reason), fmt.Sprintf("stage2-approve-%d", submissionID)); err != nil {
		return err
	}
	return tx.Commit()
}

type stageReportRefs struct {
	m map[string]*supplypg.ReportEvidenceRef
}

func (s *stageReportRefs) put(ref *supplypg.ReportEvidenceRef) { s.m[ref.ReportID] = ref }

func (s *stageReportRefs) get(id string) *supplypg.ReportEvidenceRef {
	if s == nil {
		return nil
	}
	return s.m[id]
}

// SimulateAndAdmit 由办理人触发：mock 测试 + 创建资源 + 绑定正式配置 + 登记接入。
func (r *SupplyChainRuntime) SimulateAndAdmit(ctx context.Context, submissionID, actorID int64) (*supply.Resource, error) {
	var supplierID, rowID, modelID int64
	var supplierModel, coefficient, rowStatus string
	if err := r.raw.QueryRowContext(ctx, `SELECT s.supplier_id,r.id,r.standard_model_id,r.supplier_model_name,r.coefficient::text,r.row_status
FROM public.submission s JOIN public.submission_row r ON r.submission_id=s.id
WHERE s.id=$1 ORDER BY r.id LIMIT 1`, submissionID).Scan(&supplierID, &rowID, &modelID, &supplierModel, &coefficient, &rowStatus); err != nil {
		if err == sql.ErrNoRows {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	if rowStatus == "ADMITTED" {
		return nil, supply.ErrRevisionConflict
	}
	var flowID, nodeID int64
	var assignee string
	if err := r.raw.QueryRowContext(ctx, `SELECT f.id,n.id,n.assignee_id::text
FROM public.onboarding_flow f JOIN public.onboarding_node n ON n.flow_id=f.id
WHERE f.kind='SUPPLIER' AND f.owner_id=$1 AND n.node_key='CONFIG' AND n.status='IN_PROGRESS'
ORDER BY f.id DESC, n.id DESC LIMIT 1`, supplierID).Scan(&flowID, &nodeID, &assignee); err != nil {
		if err == sql.ErrNoRows {
			return nil, supply.ErrRevisionConflict
		}
		return nil, err
	}
	if assignee != fmt.Sprint(actorID) {
		return nil, supply.ErrForbidden
	}

	rowIDStr := fmt.Sprint(rowID)
	flowIDStr := fmt.Sprint(flowID)
	nodeIDStr := fmt.Sprint(nodeID)
	supplierStr := fmt.Sprint(supplierID)
	actorStr := fmt.Sprint(actorID)
	rowVersion := uint64(2)
	targetVersion := uint64(1)
	flowRevision := uint64(1)

	tx, err := r.raw.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	logFn := func(ctx context.Context, t *sql.Tx, e supplypg.ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := t.ExecContext(ctx, `INSERT INTO public.operation_log
(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`,
			e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt.UTC())
		return err
	}
	state, err := supplypg.NewResourceStateStore("public", logFn)
	if err != nil {
		return nil, err
	}
	configs, err := supplypg.NewFormalConfigStore("public", func(ctx context.Context, t *sql.Tx, e supplypg.FormalConfigLog) error {
		return logFn(ctx, t, supplypg.ResourceStateLog(e))
	})
	if err != nil {
		return nil, err
	}

	creationWork := supplypg.NewResourceCreationWork(state, resolve, supplypg.ResourceCreationHooks{
		LockSource: func(ctx context.Context, t *sql.Tx, row string, version uint64) (*supply.ResourceCreationContext, error) {
			var mid, vendor, coef string
			if err := t.QueryRowContext(ctx, `SELECT standard_model_id::text,supplier_model_name,coefficient::text
FROM public.submission_row WHERE id=$1 FOR UPDATE`, row).Scan(&mid, &vendor, &coef); err != nil {
				return nil, err
			}
			return &supply.ResourceCreationContext{Proof: &supply.SubmissionResourceProof{
				RowID: row, RowVersion: rowVersion, TargetVersion: targetVersion, SupplierID: supplierStr,
				StandardModelID: mid, Region: supplierRegionOf(ctx, r.raw, supplierID), SupplierModelID: vendor,
				Coefficient: coef, BuyerID: actorStr, FlowID: flowIDStr, FlowRevision: flowRevision,
				AuthorizedActorID: actorStr, ApprovedAndAssigned: true, EnteredHandling: true,
			}}, nil
		},
		AttachRow: func(ctx context.Context, t *sql.Tx, e supply.ResourceCreationEffect) error {
			res, err := t.ExecContext(ctx, `UPDATE public.submission_row SET resource_id=$1 WHERE id=$2`, e.Resource.ID, e.RowID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return supply.ErrRevisionConflict
			}
			return nil
		},
	})
	creation := supply.NewResourceCreationService(creationWork, time.Now)
	created, err := creation.CreateFromSubmission(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "办理人创建资源", RequestID: fmt.Sprintf("stage2-create-%d", submissionID)}, supply.ResourceCreationCommand{RowID: rowIDStr, RowVersion: rowVersion})
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	resource := created.Resource

	refs := &stageReportRefs{m: map[string]*supplypg.ReportEvidenceRef{}}
	addReport := func(sequence uint64, connection, pressure bool) (string, error) {
		var job, report string
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.test_job(resource_id,category,status,verdict,created_by,finished_at)
VALUES($1,'stress','SUCCEEDED','PASS',$2,now()) RETURNING id::text`, resource.ID, actorID).Scan(&job); err != nil {
			return "", err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.test_report
(test_job_id,resource_id,connection_ms,connection_passed,stress_passed,rps,tps,availability,error_rate,ttft_ms,tpot_ms,sample_count,verdict,report_url,report_format,scene,tested_by,tested_at,cache_hit_tokens,cache_input_tokens)
VALUES($1,$2,86,$3,$4,24.5,19750,99.9,0.1,320,28,100,'PASS','https://example.com/report/stage2','JSON','stage2',$5,now(),300,1000)
RETURNING id::text`, job, resource.ID, connection, pressure, actorID).Scan(&report); err != nil {
			return "", err
		}
		refs.put(&supplypg.ReportEvidenceRef{ReportID: report, JobID: job, ResourceID: resource.ID, SupplierID: resource.SupplierID,
			StandardModelID: resource.StandardModelID, Region: resource.Region, OriginRowID: resource.OriginRowID,
			OriginRowVersion: resource.OriginRowVersion, TargetVersion: resource.TargetVersion, Sequence: sequence, Valid: true})
		return report, nil
	}
	authority := func(ctx context.Context, t *sql.Tx, res *supply.Resource, report string) (*supplypg.ReportEvidenceRef, bool, error) {
		ref := refs.get(report)
		if ref == nil {
			return nil, true, supply.ErrNotFound
		}
		copy := *ref
		return &copy, true, nil
	}
	evidence := supply.NewTestEvidenceService(supplypg.NewTestEvidenceWork(state, resolve, authority), time.Now)
	failed, err := addReport(1, true, false)
	if err != nil {
		return nil, err
	}
	if _, err := evidence.AttachTestEvidence(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "mock 失败测试", RequestID: fmt.Sprintf("stage2-fail-%d", submissionID)}, supply.TestEvidenceCommand{ResourceID: resource.ID, ReportID: failed}); err != nil {
		return nil, fmt.Errorf("evidence-failed: %w", err)
	}
	passed, err := addReport(2, true, true)
	if err != nil {
		return nil, err
	}
	selected, err := evidence.AttachTestEvidence(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "mock 通过测试", RequestID: fmt.Sprintf("stage2-pass-%d", submissionID)}, supply.TestEvidenceCommand{ResourceID: resource.ID, ReportID: passed})
	if err != nil {
		return nil, fmt.Errorf("evidence-passed: %w", err)
	}

	formalID, formalRevision, err := r.ensureFormalConfig(ctx, tx, supplierID, actorID, configs)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	proof := func(ctx context.Context, t *sql.Tx, res *supply.Resource) (*supply.AdmissionProof, error) {
		var assigneeID, nodeStatus, flowStatus string
		if err := t.QueryRowContext(ctx, `SELECT n.assignee_id::text,n.status,f.status
FROM public.onboarding_node n JOIN public.onboarding_flow f ON f.id=n.flow_id
WHERE n.id=$1 AND f.id=$2 FOR UPDATE OF n,f`, nodeIDStr, flowIDStr).Scan(&assigneeID, &nodeStatus, &flowStatus); err != nil {
			return nil, err
		}
		return &supply.AdmissionProof{FlowID: flowIDStr, NodeID: nodeIDStr, FlowRevision: flowRevision, ResourceID: res.ID,
			SupplierID: res.SupplierID, Region: res.Region, OriginRowID: res.OriginRowID, OriginRowVersion: res.OriginRowVersion,
			TargetVersion: res.TargetVersion, AssigneeID: assigneeID, FlowInProgress: flowStatus == "PROCESSING",
			NodeInProgress: nodeStatus == "IN_PROGRESS", PrerequisitesReady: true}, nil
	}
	readRef := func(ctx context.Context, t *sql.Tx, res *supply.Resource) (*supplypg.ReportEvidenceRef, error) {
		ref := refs.get(res.LatestReportID)
		if ref == nil {
			return nil, supply.ErrNotFound
		}
		copy := *ref
		return &copy, nil
	}
	binding := supply.NewResourceConfigBindingService(supplypg.NewResourceBindingWork(state, configs, resolve, proof), time.Now)
	bound, err := binding.BindFormalConfig(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "绑定正式配置", RequestID: fmt.Sprintf("stage2-bind-%d", submissionID)}, supply.ResourceConfigBindingCommand{
		ResourceID: resource.ID, ConfigID: formalID, ExpectedRevision: selected.Resource.Revision, ExpectedConfigRevision: formalRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("bind: %w", err)
	}
	admission := supply.NewResourceAdmissionService(supplypg.NewResourceAdmissionWork(state, configs, resolve, supplypg.ResourceAdmissionHooks{
		Proof:  proof,
		Report: readRef,
		CompleteRow: func(ctx context.Context, t *sql.Tx, e supply.ResourceAdmissionEffect) error {
			res, err := t.ExecContext(ctx, `UPDATE public.submission_row SET row_status='ADMITTED' WHERE id=$1 AND resource_id=$2`, e.After.OriginRowID, e.After.ID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return supply.ErrRevisionConflict
			}
			return nil
		},
		Outbox: func(ctx context.Context, t *sql.Tx, e supplypg.AdmissionOutboxEntry) (string, error) {
			var id string
			err := t.QueryRowContext(ctx, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by,updated_by)
VALUES($1,$2::jsonb,$3,$4,$4) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID).Scan(&id)
			return id, err
		},
	}), time.Now)
	admitted, err := admission.Admit(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "登记接入", RequestID: fmt.Sprintf("stage2-admit-%d", submissionID)}, supply.ResourceAdmissionCommand{
		ResourceID: resource.ID, ExpectedRevision: bound.Resource.Revision, ExternalConfirmed: true, CompletionNote: "mock 测试通过，已登记接入",
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.onboarding_node SET status='DONE',completed_at=now() WHERE id=$1`, nodeID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.submission SET status='COMPLETED',completed_at=now() WHERE id=$1`, submissionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return admitted, nil
}

func supplierRegionOf(ctx context.Context, db *sql.DB, supplierID int64) string {
	var region string
	_ = db.QueryRowContext(ctx, `SELECT region FROM public.supplier WHERE id=$1`, supplierID).Scan(&region)
	return region
}

func (r *SupplyChainRuntime) ensureFormalConfig(ctx context.Context, tx *sql.Tx, supplierID, actorID int64, configs *supplypg.FormalConfigStore) (string, uint64, error) {
	buyerID := actorID
	var buyer sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT new_staff_id::text FROM public.assignment_log
WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id' ORDER BY id DESC LIMIT 1`, supplierID).Scan(&buyer); err == nil && buyer.Valid {
		if n, err := strconv.ParseInt(buyer.String, 10, 64); err == nil && n > 0 {
			buyerID = n
		}
	}
	svc := supply.NewFormalConfigService(supplypg.NewFormalConfigWork(configs, func(context.Context) (*sql.Tx, error) { return tx, nil }), r.urlPolicy, time.Now)
	secret := "stage2-local-secret"
	view, err := svc.Create(ctx, supply.WriteMeta{ActorID: fmt.Sprint(buyerID), Reason: "Stage2 正式配置", RequestID: fmt.Sprintf("stage2-config-%d", supplierID)}, fmt.Sprint(supplierID), supply.FormalConfigInput{
		Scope: "全部模型", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true,
	})
	if err != nil {
		return "", 0, err
	}
	return view.ID, view.Revision, nil
}

// SupplyChainHandler Stage 2 HTTP 入口。
type SupplyChainHandler struct{ rt *SupplyChainRuntime }

func NewSupplyChainHandler(rt *SupplyChainRuntime) *SupplyChainHandler {
	return &SupplyChainHandler{rt: rt}
}

func (h *SupplyChainHandler) ListStandardModels(c *gin.Context) {
	list, err := h.rt.ListStandardModels(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list})
}



func (h *SupplyChainHandler) ListApprovals(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	ok, err := h.rt.isApprover(c.Request.Context(), op.StaffID)
	if err != nil || !ok {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	list, err := h.rt.ListApprovals(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list})
}

type approveRequest struct {
	TestAssigneeID     int64  `json:"test_assignee_id"`
	ContractAssigneeID int64  `json:"contract_assignee_id"`
	Reason             string `json:"reason"`
}

func (h *SupplyChainHandler) Approve(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var req approveRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TestAssigneeID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	if err := h.rt.Approve(c.Request.Context(), id, op.StaffID, req.TestAssigneeID, req.ContractAssigneeID, req.Reason); err != nil {
		if err == supply.ErrForbidden {
			response.Error(c, apperr.ErrForbidden)
			return
		}
		if err == supply.ErrNotFound {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	response.Success(c, gin.H{"submission_id": id, "status": "PROCESSING"})
}

// Reject 是内部驳回入口。
func (h *SupplyChainHandler) Reject(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	ok, err := h.rt.isApprover(c.Request.Context(), op.StaffID)
	if err != nil || !ok {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	parsedID, err := planPositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	id := int64(parsedID)
	var body struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ExpectedRevision <= 0 || strings.TrimSpace(body.Reason) == "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "expected_revision 与 reason 必填", 400))
		return
	}
	requestID := c.GetString("request_id")
	if err := h.rt.Reject(c.Request.Context(), id, body.ExpectedRevision, op.StaffID, body.Reason, requestID); err != nil {
		if errors.Is(err, supply.ErrRevisionConflict) {
			response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "申报已变化，请刷新后重试", 409))
			return
		}
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"id": strconv.FormatInt(id, 10)})
}

func (h *SupplyChainHandler) ListSuppliers(c *gin.Context) {
	if middleware.OperatorFrom(c) == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	list, err := h.rt.ListSuppliers(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list})
}

func (h *SupplyChainHandler) ListApprovalHistory(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	list, err := h.rt.ListApprovalHistory(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{"list": list})
}

func (h *SupplyChainHandler) Simulate(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	res, err := h.rt.SimulateAndAdmit(c.Request.Context(), id, op.StaffID)
	if err != nil {
		fmt.Printf("stage2 simulate error: %v\n", err)
		if err == supply.ErrForbidden {
			response.Error(c, apperr.ErrForbidden)
			return
		}
		if err == supply.ErrNotFound {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	response.Success(c, gin.H{"resource_id": res.ID, "state": res.State, "revision": res.Revision})
}
