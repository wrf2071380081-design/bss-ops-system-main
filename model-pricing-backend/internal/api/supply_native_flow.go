package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
	"strconv"
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
	err := r.raw.QueryRowContext(ctx, `SELECT s.id::text,s.supplier_id::text,sp.name,s.revision::text,s.status,s.kind,s.region,NOT EXISTS(SELECT 1 FROM public.resource r WHERE r.supplier_id=s.supplier_id AND r.ever_admitted),s.created_at,s.submitted_at FROM public.submission s JOIN public.supplier sp ON sp.id=s.supplier_id WHERE s.id=$1`, id).Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.Revision, &v.Status, &v.Kind, &v.Region, &v.FirstModel, &v.CreatedAt, &v.SubmittedAt)
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
	rows, err := r.raw.QueryContext(ctx, `SELECT sr.id::text,sr.standard_model_id::text,m.name,sr.supplier_model_name,sr.coefficient::text,sr.row_status,coalesce(sr.resource_id::text,'') FROM public.submission_row sr JOIN public.standard_model m ON m.id=sr.standard_model_id WHERE sr.submission_id=$1 ORDER BY sr.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.Rows = []nativeFlowRow{}
	for rows.Next() {
		var row nativeFlowRow
		if err := rows.Scan(&row.ID, &row.ModelID, &row.ModelName, &row.InterfaceModel, &row.Coefficient, &row.Status, &row.ResourceID); err != nil {
			return nil, err
		}
		v.Rows = append(v.Rows, row)
	}
	return &v, rows.Err()
}
func mustFlowInt(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

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
		if err := decodeSupplyRequest(c, &body, []string{"expected_revision", "reason"}); err != nil {
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
		}
		if err := decodeSupplyRequest(c, &body, []string{"expected_revision", "test_assignee_id", "reason"}); err != nil {
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
		meta, err := supplyWriteMeta(c, body.Reason)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		err = runtime.ApproveVersioned(c.Request.Context(), int64(id), int64(expected), mustFlowInt(meta.ActorID), int64(test), int64(contract), meta.Reason, meta.RequestID)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
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
		receipt := gin.H{"submission_id": c.Param("id"), "revision": revision, "status": "PROCESSING"}
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
