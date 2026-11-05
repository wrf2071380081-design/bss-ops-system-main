package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
	"strconv"
	"strings"
	"time"
)

type nativeFlowRow struct {
	ID             string `json:"id"`
	ModelID        string `json:"model_id"`
	ModelName      string `json:"model_name"`
	InterfaceModel string `json:"interface_model_id"`
	Coefficient    string `json:"coefficient"`
	Status         string `json:"status"`
	ResourceID     string `json:"resource_id"`
	Decision       string `json:"decision"`
	// Company / ModelType 给审批页的「模型公司 / 模型类型」筛选（原型审批页有这两个筛选）。
	Company   string `json:"company"`
	ModelType string `json:"model_type"`
	// 本次申报对应的供应资源最近一次测试结果（原型审批页的连通/RPS/TPS/缓存四列）。
	// 没有报告就是 NOT_TESTED，前端显示「未测试」；缓存另外区分「未提供」。
	TestStatus     string `json:"test_status"`
	ConnectionMS   string `json:"connection_ms"`
	RPS            string `json:"rps"`
	TPS            string `json:"tps"`
	CacheHitRate   string `json:"cache_hit_rate"`
	CacheAvailable bool   `json:"cache_available"`
	// 最近一次测试的两项结论与时间：审批页据此提示「压力测试未通过」，
	// 否则四列有数值、结论却是未通过，审批人会以为只是没测。
	// 用指针表达三态：true 通过 / false 不通过 / nil 该项还没测过。
	ConnectionPassed *bool  `json:"connection_passed"`
	StressPassed     *bool  `json:"stress_passed"`
	TestedAt         string `json:"tested_at"`
	ReportCount      int    `json:"report_count"`
}
type nativeFlowView struct {
	ID                  string           `json:"id"`
	SupplierID          string           `json:"supplier_id"`
	SupplierName        string           `json:"supplier_name"`
	Revision            string           `json:"revision"`
	Status              string           `json:"status"`
	Kind                string           `json:"kind"`
	Region              string           `json:"region"`
	FirstModel          bool             `json:"first_model"`
	CanApprove          bool             `json:"can_approve"`
	CanHandle           bool             `json:"can_handle"`
	Rows                []nativeFlowRow  `json:"rows"`
	Nodes               []nativeFlowNode `json:"nodes"`
	FlowID              string           `json:"flow_id"`
	FlowRevision        string           `json:"flow_revision"`
	SelectedNodeID      string           `json:"selected_node_id"`
	CanHandleContract   bool             `json:"can_handle_contract"`
	CanCompleteContract bool             `json:"can_complete_contract"`
	CreatedAt           time.Time        `json:"created_at"`
	SubmittedAt         *time.Time       `json:"submitted_at"`
	// Submitter 是申报人（供应商侧联系人）。历史单据可能没记，前端显示「未记录」。
	Submitter string `json:"submitter"`
	// 审批结论时间与审批人：取 operation_log 的 APPROVE/REJECT 记录，
	// 只在供应商申报历史里填充，供「审批记录」表展示。
	ApprovedAt          *time.Time       `json:"approved_at,omitempty"`
	Approver            string           `json:"approver,omitempty"`
	// ApprovalResult 是逐模型决定的派生结论：通过 / 部分通过 / 不通过（未决定为空）。
	// 原型列表直接显示「部分通过」，而 submission.status 只能表达 APPROVED/PROCESSING。
	ApprovalResult string `json:"approval_result,omitempty"`
}
type nativeFlowNode struct {
	ID           string `json:"id"`
	Key          string `json:"key"`
	Status       string `json:"status"`
	AssigneeID   string `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`
}

func (r *SupplyChainRuntime) nativeFlowView(ctx context.Context, id, actor string, node bool) (*nativeFlowView, error) {
	selected := ""
	if node {
		selected = id
		var sub string
		err := r.raw.QueryRowContext(ctx, `SELECT l.entity_id::text FROM public.operation_log l JOIN public.onboarding_node n ON n.flow_id::text=l."after"->>'flow_id' WHERE l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND n.id::text=$1 AND n.node_key IN ('CONFIG','CONTRACT') ORDER BY l.id DESC LIMIT 1`, id).Scan(&sub)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		id = sub
	}
	var v nativeFlowView
	err := r.raw.QueryRowContext(ctx, `SELECT s.id::text,s.supplier_id::text,sp.name,s.revision::text,s.status,s.kind,s.region,NOT EXISTS(SELECT 1 FROM public.resource r WHERE r.supplier_id=s.supplier_id AND r.ever_admitted),s.created_at,s.submitted_at,coalesce(s.submitter_name,'') FROM public.submission s JOIN public.supplier sp ON sp.id=s.supplier_id WHERE s.id=$1`, id).Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.Revision, &v.Status, &v.Kind, &v.Region, &v.FirstModel, &v.CreatedAt, &v.SubmittedAt, &v.Submitter)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	approver, err := r.isApprover(ctx, mustFlowInt(actor))
	if err != nil {
		return nil, err
	}
	v.CanApprove = approver && v.Status == "PENDING_APPROVAL"
	v.SelectedNodeID = selected
	v.Nodes = []nativeFlowNode{}
	var flowID, flowRevision string
	err = r.raw.QueryRowContext(ctx, `SELECT "after"->>'flow_id',id::text FROM public.operation_log WHERE entity_type='submission' AND entity_id=$1 AND action='APPROVE' AND "after"->>'contract'='supply.handling_approval' ORDER BY id DESC LIMIT 1`, id).Scan(&flowID, &flowRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		v.FlowID, v.FlowRevision = flowID, flowRevision
		nodes, err := r.raw.QueryContext(ctx, `SELECT n.id::text,n.node_key,n.status,coalesce(n.assignee_id::text,''),coalesce(st.name,'') FROM public.onboarding_node n LEFT JOIN public.internal_staff st ON st.id=n.assignee_id WHERE n.flow_id=$1 AND n.enabled ORDER BY n.id`, flowID)
		if err != nil {
			return nil, err
		}
		contractActive := false
		for nodes.Next() {
			var item nativeFlowNode
			if err := nodes.Scan(&item.ID, &item.Key, &item.Status, &item.AssigneeID, &item.AssigneeName); err != nil {
				nodes.Close()
				return nil, err
			}
			v.Nodes = append(v.Nodes, item)
			if item.Key == "CONTRACT" && (item.Status == "IN_PROGRESS" || item.Status == "WAITING_PREDECESSOR") && item.AssigneeID == actor && v.Status == "PROCESSING" {
				v.CanHandleContract = true
				contractActive = item.Status == "IN_PROGRESS"
			}
		}
		if err := nodes.Err(); err != nil {
			nodes.Close()
			return nil, err
		}
		nodes.Close()
		v.CanCompleteContract = v.CanHandleContract && contractActive && r.f12Readiness != nil
	}
	if err := r.raw.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.operation_log l JOIN public.onboarding_node n ON n.id::text=l."after"->>'node_id' JOIN public.onboarding_flow f ON f.id=n.flow_id WHERE l.entity_type='submission' AND l.entity_id=$1 AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND n.enabled AND n.status='IN_PROGRESS' AND n.assignee_id=$2 AND f.status='PROCESSING' AND f.current_node='CONFIG')`, id, actor).Scan(&v.CanHandle); err != nil {
		return nil, err
	}
	// 指标取「该资源最近一次实际测试」。
	// 不能用 r.latest_test_report_id：那是「当前生效、可作为准入依据」的报告指针，
	// 压力测试没过（或办理重开）时它会被清空，于是测过的也会显示成「未测试」。
	// 审批人要看的恰恰是「这轮测出来什么、过没过」，所以按 resource 取最近一份
	// baseline（连通）与 stress（压力）的实测值——两类报告是两个 test_job，不能只取一行。
	rows, err := r.raw.QueryContext(ctx, `SELECT sr.id::text,sr.standard_model_id::text,m.name,sr.supplier_model_name,sr.coefficient::text,sr.row_status,coalesce(sr.resource_id::text,''),sr.decision,coalesce(m.company,''),coalesce(m.model_type,''),
		 -- 结论只看平台明确判负的 verdict：压测平台对 stress 不返回结论（verdict 空），
		 -- 旧口径把 stress_passed=false 当不通过，会把「未判定」显示成「没通过」。
		 CASE WHEN tr.last_report_id IS NULL THEN 'NOT_TESTED'
		      WHEN tr.connection_failed OR tr.stress_failed THEN 'FAILED'
		      WHEN tr.connection_passed IS TRUE THEN 'PASSED'
		      ELSE 'INCOMPLETE' END,
		 coalesce(tr.connection_ms::text,''),coalesce(tr.rps::text,''),coalesce(tr.tps::text,''),
		 coalesce(tr.cache_hit_ratio::text,''),coalesce(tr.cache_available,false),
		 tr.connection_passed,tr.stress_passed,
		 coalesce(tr.tested_at::text,''),coalesce(tr.report_count,0)
FROM public.submission_row sr
JOIN public.standard_model m ON m.id=sr.standard_model_id
LEFT JOIN public.resource r ON r.id=sr.resource_id
LEFT JOIN LATERAL (
	SELECT
		max(t.id) AS last_report_id,
		count(*) AS report_count,
		max(t.tested_at) AS tested_at,
		-- 连通与压力是两个独立 test_job（category=baseline / stress），必须分开取：
		-- stress 报告同样会写 connection_passed=false，混在一起取「最近非空」会把连通结论取成 false。
		(SELECT x.connection_ms FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='baseline' AND x.connection_ms IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS connection_ms,
		(SELECT x.connection_passed FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='baseline' AND x.connection_passed IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS connection_passed,
		-- 平台明确判负（fail/abort/canceled）才算失败；空 verdict 是「未判定」，不参与判定。
		(SELECT lower(coalesce(x.verdict,'')) IN ('fail','failed','abort','aborted','canceled','cancelled')
			FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='baseline'
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS connection_failed,
		(SELECT lower(coalesce(x.verdict,'')) IN ('fail','failed','abort','aborted','canceled','cancelled')
			FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress'
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS stress_failed,
		(SELECT x.rps FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress' AND x.rps IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS rps,
		(SELECT x.tps FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress' AND x.tps IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS tps,
		(SELECT x.cache_hit_ratio FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress' AND x.cache_hit_ratio IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS cache_hit_ratio,
		(SELECT x.cache_ratio_available FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress'
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS cache_available,
		(SELECT x.stress_passed FROM public.test_report x JOIN public.test_job j ON j.id=x.test_job_id
			WHERE x.resource_id=r.id AND j.category='stress' AND x.stress_passed IS NOT NULL
			ORDER BY x.tested_at DESC NULLS LAST, x.id DESC LIMIT 1) AS stress_passed
	FROM public.test_report t WHERE t.resource_id=r.id
) tr ON TRUE
WHERE sr.submission_id=$1 ORDER BY sr.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.Rows = []nativeFlowRow{}
	for rows.Next() {
		var row nativeFlowRow
		var connectionPassed, stressPassed sql.NullBool
		if err := rows.Scan(&row.ID, &row.ModelID, &row.ModelName, &row.InterfaceModel, &row.Coefficient, &row.Status, &row.ResourceID, &row.Decision, &row.Company, &row.ModelType,
			&row.TestStatus, &row.ConnectionMS, &row.RPS, &row.TPS, &row.CacheHitRate, &row.CacheAvailable,
			&connectionPassed, &stressPassed, &row.TestedAt, &row.ReportCount); err != nil {
			return nil, err
		}
		if connectionPassed.Valid {
			row.ConnectionPassed = &connectionPassed.Bool
		}
		if stressPassed.Valid {
			row.StressPassed = &stressPassed.Bool
		}
		v.Rows = append(v.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	v.ApprovalResult = approvalResultOf(v.Status, v.Rows)
	return &v, nil
}
func mustFlowInt(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// approvalResultOf 把逐模型决定折成一句结论。
// 与原型一致：只要有一个不通过就是「部分通过」（全不通过才叫「不通过」）。
// 整单驳回时行上不会写逐模型决定，所以要看单据状态兜底成「不通过」。
func approvalResultOf(status string, rows []nativeFlowRow) string {
	approved, rejected := 0, 0
	for _, row := range rows {
		switch row.Decision {
		case "APPROVED":
			approved++
		case "REJECTED":
			rejected++
		}
	}
	if approved+rejected == 0 {
		if status == "REJECTED" {
			return "不通过"
		}
		return ""
	}
	switch {
	case rejected == 0:
		return "通过"
	case approved == 0:
		return "不通过"
	default:
		return "部分通过"
	}
}

// One registered entry for real B decisions. Old stage approve/simulate routes
// remain absent. Writes are authenticated and share public idempotency commit.
func RegisterNativeSupplyFlowRoutes(internal *gin.RouterGroup, runtime *SupplyChainRuntime, store middleware.IdempotencyStore) {
	group := internal.Group("/flows")
	group.Use(requireSupplyInternalStaff)
	registerSupplierActivityRoutes(group, runtime)
	get := func(node bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			if runtime == nil {
				supplyHTTPError(c, supply.ErrDependencyUnavailable)
				return
			}
			if _, err := planPositiveID(c.Param("id")); err != nil {
				supplyHTTPError(c, err)
				return
			}
			v, err := runtime.nativeFlowView(c.Request.Context(), c.Param("id"), supplyActor(c), node)
			if err != nil {
				supplyHTTPError(c, err)
				return
			}
			response.Success(c, v)
		}
	}
	group.GET("/submissions/:id", get(false))
	group.GET("/nodes/:id", get(true))
	idem := func(c *gin.Context) { supplyHTTPError(c, supply.ErrDependencyUnavailable); c.Abort() }
	if store != nil {
		idem = middleware.Idempotency(store, middleware.DefaultBizKey)
	}
	group.POST("/submissions/:id/reject", idem, func(c *gin.Context) {
		if runtime == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		id, err := planPositiveID(c.Param("id"))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var body struct {
			Expected string `json:"expected_revision"`
			Reason   string `json:"reason"`
		}
		if err := decodeSupplyRequest(c, &body, []string{"expected_revision"}); err != nil {
			supplyHTTPError(c, err)
			return
		}
		expected, err := planPositiveID(body.Expected)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		meta, err := supplyWriteMetaOptional(c, body.Reason, "领导驳回")
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		if err := runtime.Reject(c.Request.Context(), int64(id), int64(expected), mustFlowInt(meta.ActorID), meta.Reason, meta.RequestID); err != nil {
			supplyHTTPError(c, err)
			return
		}
		nativeDecisionReceipt(c, id, "REJECTED")
	})
	group.POST("/nodes/:id/complete-contract", idem, func(c *gin.Context) {
		if runtime == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		id, err := planPositiveID(c.Param("id"))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var body struct {
			Expected string `json:"expected_flow_revision"`
			Reason   string `json:"reason"`
		}
		if err := decodeSupplyRequest(c, &body, []string{"expected_flow_revision", "reason"}); err != nil {
			supplyHTTPError(c, err)
			return
		}
		expected, err := planPositiveID(body.Expected)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		meta, err := supplyWriteMeta(c, body.Reason)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		if err := runtime.completeNativeContract(c.Request.Context(), meta, id, expected); err != nil {
			supplyHTTPError(c, err)
			return
		}
		receipt := gin.H{"node_id": c.Param("id"), "status": "COMPLETED"}
		middleware.SetIdempotencyResult(c, receipt)
		response.Success(c, receipt)
	})
	group.POST("/submissions/:id/approve", idem, func(c *gin.Context) {
		if runtime == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		id, err := planPositiveID(c.Param("id"))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var body struct {
			Expected string `json:"expected_revision"`
			Test     string `json:"test_assignee_id"`
			Contract string `json:"contract_assignee_id"`
			Reason   string `json:"reason"`
			// Rows 是逐模型决策（原型「决定」勾选）：不传则该行按不通过处理；
			// 整个字段缺省时按整单通过处理，兼容尚未升级的调用方。
			Rows []struct {
				ID       string `json:"id"`
				Decision string `json:"decision"`
			} `json:"rows"`
		}
		if err := decodeSupplyRequest(c, &body, []string{"expected_revision", "test_assignee_id"}); err != nil {
			supplyHTTPError(c, err)
			return
		}
		expected, err := planPositiveID(body.Expected)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		test, err := planPositiveID(body.Test)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var contract uint64
		if body.Contract != "" {
			contract, err = planPositiveID(body.Contract)
			if err != nil {
				supplyHTTPError(c, err)
				return
			}
		}
		meta, err := supplyWriteMetaOptional(c, body.Reason, "领导审批")
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var decisions map[int64]bool
		if body.Rows != nil {
			decisions = make(map[int64]bool, len(body.Rows))
			for _, row := range body.Rows {
				rowID, err := planPositiveID(row.ID)
				if err != nil {
					supplyHTTPError(c, err)
					return
				}
				switch row.Decision {
				case "APPROVED":
					decisions[int64(rowID)] = true
				case "REJECTED":
					decisions[int64(rowID)] = false
				default:
					supplyHTTPError(c, supplyRequestError("rows", "decision must be APPROVED or REJECTED"))
					return
				}
			}
		}
		err = runtime.ApproveVersioned(c.Request.Context(), int64(id), int64(expected), mustFlowInt(meta.ActorID), int64(test), int64(contract), meta.Reason, meta.RequestID, decisions)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		tx, err := supplypg.GORMModelTxFromContext(c.Request.Context())
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var revision, status string
		if err := tx.QueryRowContext(c.Request.Context(), `SELECT revision::text,status FROM public.submission WHERE id=$1`, id).Scan(&revision, &status); err != nil {
			supplyHTTPError(c, err)
			return
		}
		receipt := gin.H{"submission_id": c.Param("id"), "revision": revision, "status": status}
		middleware.SetIdempotencyResult(c, receipt)
		response.Success(c, receipt)
	})
}

func nativeDecisionReceipt(c *gin.Context, id uint64, status string) {
	tx, err := supplypg.GORMModelTxFromContext(c.Request.Context())
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	var revision string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT revision::text FROM public.submission WHERE id=$1`, id).Scan(&revision); err != nil {
		supplyHTTPError(c, err)
		return
	}
	receipt := gin.H{"submission_id": c.Param("id"), "revision": revision, "status": status}
	middleware.SetIdempotencyResult(c, receipt)
	response.Success(c, receipt)
}

// supplyWriteMetaOptional 与 supplyWriteMeta 相同，但理由留空时用 fallback 兜底。
// 原型里审批/驳回都不强制填写意见（勾选即结论），故审批类动作用这个变体；
// 标准模型维护等"必须说明理由"的写操作仍走 supplyWriteMeta，不放宽。
func supplyWriteMetaOptional(c *gin.Context, reason, fallback string) (supply.WriteMeta, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = fallback
	}
	if len(reason) > 2000 {
		return supply.WriteMeta{}, supplyRequestError("reason", "reason is too long")
	}
	requestID := infra.FromContext(c)
	if requestID == "" {
		return supply.WriteMeta{}, supply.ErrDependencyUnavailable
	}
	return supply.WriteMeta{ActorID: supplyActor(c), Reason: reason, RequestID: requestID, CanMaintain: supplyCanMaintain(c)}, nil
}
