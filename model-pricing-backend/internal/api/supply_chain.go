package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	raw          *sql.DB
	urlPolicy    supply.FormalURLPolicy
	f12Readiness SupplyF12ReadinessReader
}

func NewSupplyChainRuntime(raw *sql.DB, policy supply.FormalURLPolicy, readiness ...SupplyF12ReadinessReader) *SupplyChainRuntime {
	if policy == nil {
		policy = devURLPolicy{}
	}
	runtime := &SupplyChainRuntime{raw: raw, urlPolicy: policy}
	if len(readiness) > 0 {
		runtime.f12Readiness = readiness[0]
	}
	return runtime
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
// Rejection shares the public idempotency transaction with the decision log.
func (r *SupplyChainRuntime) Reject(ctx context.Context, submissionID, expectedRevision, staffID int64, reason, requestID string) error {
	if submissionID <= 0 || expectedRevision <= 0 || staffID <= 0 || strings.TrimSpace(reason) == "" {
		return supply.ErrReadContract
	}
	tx, err := supplypg.GORMModelTxFromContext(ctx)
	owned := err != nil
	if owned {
		tx, err = r.raw.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}
	var qualified bool
	if err := tx.QueryRowContext(ctx, `SELECT can_approve_supplier AND status='ACTIVE' FROM public.internal_staff WHERE id=$1 FOR SHARE`, staffID).Scan(&qualified); err != nil || !qualified {
		return supply.ErrForbidden
	}

	var revision int64
	err = tx.QueryRowContext(ctx, `UPDATE public.submission
SET status='REJECTED', revision=revision+1, updated_at=now()
WHERE id=$1 AND revision=$2 AND status='PENDING_APPROVAL'
RETURNING revision`, submissionID, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return supply.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	after := fmt.Sprintf(`{"status":"REJECTED","revision":%d,"reason":%q}`, revision, strings.TrimSpace(reason))
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log
(entity_type, entity_id, action, "after", reason, request_id, created_at,operator_id,operator_name_snapshot)
VALUES ('submission', $1, 'REJECT', $2::jsonb, $3, $4, now(),$5,(SELECT name FROM public.internal_staff WHERE id=$5))`,
		fmt.Sprintf("%d", submissionID), after, strings.TrimSpace(reason), requestID, staffID); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
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
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	Aliases            []string `json:"aliases"`
	Region             string   `json:"region"`
	Phone              string   `json:"phone"`
	ResourceCount      int      `json:"resource_count"`
	CreditAmount       string   `json:"credit_amount"`
	CreditStatus       string   `json:"credit_status"`
	SettlementCurrency string   `json:"settlement_currency"`
	TaxRate            string   `json:"tax_rate"`
	ContractStatus     string   `json:"contract_status"`
	CooperationStage   string   `json:"cooperation_stage"`
	HandlingStatus     string   `json:"handling_status"`
}

type SupplierWorkspaceQuery struct {
	Keyword, Region, ContractStatus, CooperationStage, HandlingStatus string
	SortBy, SortOrder                                                 string
	Page, Size                                                        int
}

type SupplierWorkspacePage struct {
	List  []SupplierWorkspaceRow `json:"list"`
	Total int64                  `json:"total"`
	Page  int                    `json:"page"`
	Size  int                    `json:"size"`
}

func (r *SupplyChainRuntime) ListSuppliers(ctx context.Context) ([]SupplierWorkspaceRow, error) {
	var aliasTableAvailable bool
	if err := r.raw.QueryRowContext(ctx, `SELECT to_regclass('public.supplier_alias') IS NOT NULL`).Scan(&aliasTableAvailable); err != nil {
		return nil, err
	}
	rows, err := r.raw.QueryContext(ctx, supplierWorkspaceListQuery(aliasTableAvailable))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []SupplierWorkspaceRow{}
	for rows.Next() {
		var item SupplierWorkspaceRow
		var aliasesJSON string
		if err := rows.Scan(&item.ID, &item.Name, &aliasesJSON, &item.Region, &item.Phone, &item.ResourceCount, &item.CreditAmount, &item.CreditStatus, &item.SettlementCurrency, &item.TaxRate, &item.ContractStatus, &item.CooperationStage, &item.HandlingStatus); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliasesJSON), &item.Aliases); err != nil || item.Aliases == nil {
			if err != nil {
				return nil, err
			}
			item.Aliases = []string{}
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (r *SupplyChainRuntime) ListSuppliersPage(ctx context.Context, query SupplierWorkspaceQuery) (*SupplierWorkspacePage, error) {
	if r == nil || r.raw == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	countSQL, pageSQL, countArgs, pageArgs, err := supplierWorkspacePageQueries(false, query)
	if err != nil {
		return nil, err
	}
	tx, err := r.raw.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var aliasTableAvailable bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.supplier_alias') IS NOT NULL`).Scan(&aliasTableAvailable); err != nil {
		return nil, err
	}
	countSQL, pageSQL, countArgs, pageArgs, err = supplierWorkspacePageQueries(aliasTableAvailable, query)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := tx.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, pageSQL, pageArgs...)
	if err != nil {
		return nil, err
	}
	list := []SupplierWorkspaceRow{}
	for rows.Next() {
		var item SupplierWorkspaceRow
		var aliasesJSON string
		if err := rows.Scan(&item.ID, &item.Name, &aliasesJSON, &item.Region, &item.Phone, &item.ResourceCount, &item.CreditAmount, &item.CreditStatus, &item.SettlementCurrency, &item.TaxRate, &item.ContractStatus, &item.CooperationStage, &item.HandlingStatus); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliasesJSON), &item.Aliases); err != nil || item.Aliases == nil {
			_ = rows.Close()
			if err != nil {
				return nil, err
			}
			item.Aliases = []string{}
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SupplierWorkspacePage{List: list, Total: total, Page: query.Page, Size: query.Size}, nil
}

func supplierWorkspaceListQuery(aliasTableAvailable bool) string {
	aliases := `'[]'::text`
	if aliasTableAvailable {
		aliases = `COALESCE((SELECT json_agg(sa.alias_name ORDER BY sa.id)::text FROM public.supplier_alias sa WHERE sa.supplier_id=s.id),'[]')`
	}
	return `SELECT s.id,s.name,` + aliases + `,s.region,COALESCE(s.phone,''),
       (SELECT count(*) FROM public.resource res WHERE res.supplier_id=s.id),
       COALESCE(current_credit.amount,''),current_credit.status,COALESCE(s.settlement_currency,''),COALESCE(s.tax_rate::text,''),
       CASE
         WHEN EXISTS (SELECT 1 FROM public.onboarding_flow f JOIN public.onboarding_node n ON n.flow_id=f.id
                      WHERE f.kind='SUPPLIER' AND f.owner_id=s.id AND n.node_key='CONTRACT' AND n.enabled
                        AND n.status IN ('IN_PROGRESS','WAITING_PREDECESSOR')) THEN '办理中'
         WHEN EXISTS (SELECT 1 FROM public.contract c WHERE c.subject_type='SUPPLIER' AND c.subject_id=s.id AND c.status='PROCESSING') THEN '办理中'
         WHEN EXISTS (SELECT 1 FROM public.onboarding_flow f JOIN public.onboarding_node n ON n.flow_id=f.id
                      WHERE f.kind='SUPPLIER' AND f.owner_id=s.id AND n.node_key='CONTRACT' AND n.enabled AND n.status='DONE') THEN '已归档'
         WHEN EXISTS (SELECT 1 FROM public.contract c WHERE c.subject_type='SUPPLIER' AND c.subject_id=s.id AND c.status='ARCHIVED') THEN '已归档'
         ELSE '未登记'
       END,
       CASE WHEN s.established THEN '已有' ELSE '潜在' END,
       CASE WHEN EXISTS (SELECT 1 FROM public.submission sub WHERE sub.supplier_id=s.id AND sub.status IN ('PENDING_APPROVAL','PROCESSING'))
                   OR EXISTS (SELECT 1 FROM public.onboarding_flow f WHERE f.kind='SUPPLIER' AND f.owner_id=s.id AND f.status='PROCESSING')
                   OR EXISTS (SELECT 1 FROM public.plan p JOIN public.resource res ON res.id=p.resource_id WHERE res.supplier_id=s.id AND p.kind='PRICE_CHANGE' AND p.status IN ('PENDING','EXECUTING','BLOCKED'))
            THEN '有未完成事项' ELSE '无未完成事项' END
FROM public.supplier s
LEFT JOIN LATERAL (
  SELECT CASE WHEN count(*)=1 AND min(c.amount)>0 THEN min(c.amount)::text ELSE '' END AS amount,
         CASE WHEN count(*)=0 THEN 'NOT_RECORDED'
              WHEN count(*)>1 THEN 'AMBIGUOUS_VERSION'
              WHEN min(c.amount)<=0 THEN 'INVALID_LEGACY_AMOUNT'
              ELSE 'AVAILABLE' END AS status
  FROM public.credit_line c
  WHERE c.subject_type='SUPPLIER' AND c.subject_id=s.id AND c.direction='SUPPLIER_TO_US'
    AND c.currency=NULLIF(s.settlement_currency,'')
    AND c.version=(SELECT max(latest.version) FROM public.credit_line latest
                   WHERE latest.subject_type='SUPPLIER' AND latest.subject_id=s.id
                     AND latest.direction='SUPPLIER_TO_US' AND latest.currency=NULLIF(s.settlement_currency,''))
) current_credit ON TRUE
ORDER BY s.id`
}

func supplierWorkspacePageQueries(aliasTableAvailable bool, query SupplierWorkspaceQuery) (string, string, []any, []any, error) {
	if query.Page < 1 || query.Size < 1 || query.Size > 100 || query.Page > int(^uint(0)>>1)/query.Size {
		return "", "", nil, nil, fmt.Errorf("invalid supplier workspace pagination")
	}
	sortColumns := map[string]string{
		"id": "p.id", "name": "lower(p.name)", "region": "p.region", "resource_count": "p.resource_count",
		"contract_status": "p.contract_status", "cooperation_stage": "p.cooperation_stage", "handling_status": "p.handling_status",
	}
	sortColumn, ok := sortColumns[query.SortBy]
	if !ok {
		return "", "", nil, nil, fmt.Errorf("invalid supplier workspace sort field")
	}
	sortOrder := strings.ToUpper(query.SortOrder)
	if sortOrder != "ASC" && sortOrder != "DESC" {
		return "", "", nil, nil, fmt.Errorf("invalid supplier workspace sort order")
	}
	projection := strings.TrimSuffix(supplierWorkspaceListQuery(aliasTableAvailable), "\nORDER BY s.id")
	keywordMatch := `strpos(lower(p.name),lower($1))>0`
	if aliasTableAvailable {
		keywordMatch += ` OR EXISTS (SELECT 1 FROM public.supplier_alias sa WHERE sa.supplier_id=p.id AND strpos(lower(sa.alias_name),lower($1))>0)`
	}
	where := `($1='' OR (` + keywordMatch + `))
 AND ($2='' OR p.region=$2)
 AND ($3='' OR p.contract_status=$3)
 AND ($4='' OR p.cooperation_stage=$4)
 AND ($5='' OR p.handling_status=$5)`
	cte := `WITH projected(id,name,aliases,region,phone,resource_count,credit_amount,credit_status,settlement_currency,tax_rate,contract_status,cooperation_stage,handling_status) AS (` + projection + `) `
	countSQL := cte + `SELECT count(*) FROM projected p WHERE ` + where
	offset := (query.Page - 1) * query.Size
	pageSQL := cte + `SELECT p.id,p.name,p.aliases,p.region,p.phone,p.resource_count,p.credit_amount,p.credit_status,p.settlement_currency,p.tax_rate,p.contract_status,p.cooperation_stage,p.handling_status
FROM projected p WHERE ` + where + fmt.Sprintf(` ORDER BY %s %s,p.id ASC LIMIT $6 OFFSET $7`, sortColumn, sortOrder)
	countArgs := []any{query.Keyword, query.Region, query.ContractStatus, query.CooperationStage, query.HandlingStatus}
	pageArgs := append(append([]any{}, countArgs...), query.Size, offset)
	return countSQL, pageSQL, countArgs, pageArgs, nil
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
	return r.approve(ctx, submissionID, 0, actorID, testAssignee, contractAssignee, reason, fmt.Sprintf("approve-%d", submissionID))
}

// ApproveVersioned joins the public idempotency transaction. The decision and
// task assignment are the same implementation used by native A handling.
func (r *SupplyChainRuntime) ApproveVersioned(ctx context.Context, submissionID, expected, actorID, testAssignee, contractAssignee int64, reason, requestID string) error {
	if expected <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return supply.ErrReadContract
	}
	return r.approve(ctx, submissionID, expected, actorID, testAssignee, contractAssignee, reason, requestID)
}
func (r *SupplyChainRuntime) approve(ctx context.Context, submissionID, expected, actorID, testAssignee, contractAssignee int64, reason, requestID string) error {
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
	tx, txErr := supplypg.GORMModelTxFromContext(ctx)
	owned := txErr != nil
	if owned {
		tx, err = r.raw.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}
	var stillQualified bool
	if err := tx.QueryRowContext(ctx, `SELECT can_approve_supplier AND status='ACTIVE' FROM public.internal_staff WHERE id=$1 FOR SHARE`, actorID).Scan(&stillQualified); err != nil {
		return supply.ErrForbidden
	}
	if !stillQualified {
		return supply.ErrForbidden
	}
	var supplierID int64
	var status, region string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM public.submission WHERE id=$1`, submissionID).Scan(&supplierID); err != nil {
		return err
	}
	// Coordinate with A writes: supplier -> resource/configuration -> B rows.
	var lockedSupplier int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM public.supplier WHERE id=$1 FOR UPDATE`, supplierID).Scan(&lockedSupplier); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,status,region FROM public.submission WHERE id=$1 FOR UPDATE`, submissionID).Scan(&supplierID, &status, &region); err != nil {
		if err == sql.ErrNoRows {
			return supply.ErrNotFound
		}
		return err
	}
	if status != "PENDING_APPROVAL" {
		return supply.ErrRevisionConflict
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM public.submission WHERE id=$1`, submissionID).Scan(&revision); err != nil {
		return err
	}
	if expected > 0 && revision != expected {
		return supply.ErrRevisionConflict
	}
	for _, staff := range []int64{testAssignee, contractAssignee} {
		if staff <= 0 {
			continue
		}
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT status='ACTIVE' FROM public.internal_staff WHERE id=$1 FOR SHARE`, staff).Scan(&active); err != nil {
			return supply.ErrForbidden
		}
		if !active {
			return supply.ErrForbidden
		}
	}
	var firstModel bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM public.resource WHERE supplier_id=$1 AND ever_admitted=true)`, supplierID).Scan(&firstModel); err != nil {
		return err
	}
	if expected > 0 && firstModel && contractAssignee <= 0 {
		return &supply.FieldError{Path: "contract_assignee_id", Reason: "first model requires a contract handler"}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.submission SET status='PROCESSING',revision=revision+1,updated_at=now() WHERE id=$1`, submissionID); err != nil {
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
	state, err := supplypg.NewResourceStateStore("public", func(c context.Context, t *sql.Tx, e supplypg.ResourceStateLog) error {
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := t.ExecContext(c, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	})
	if err != nil {
		return err
	}
	if err := supplypg.RecordNativeApproval(ctx, tx, state, fmt.Sprint(submissionID), fmt.Sprint(flowID), fmt.Sprint(actorID), strings.TrimSpace(reason), requestID); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
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
	values, err := supplyQuery(c, "page", "size", "keyword", "region", "contract_status", "cooperation_stage", "handling_status", "sort_by", "sort_order")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	query, err := parseSupplierWorkspaceQuery(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if h == nil || h.rt == nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	page, err := h.rt.ListSuppliersPage(c.Request.Context(), query)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, page)
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
