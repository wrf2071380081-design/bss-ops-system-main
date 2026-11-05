package api

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

// submissionStatuses 是申报单的合法状态，用于校验筛选参数。
var submissionStatuses = map[string]bool{
	"DRAFT": true, "PENDING_APPROVAL": true, "APPROVED": true, "REJECTED": true,
	"PROCESSING": true, "COMPLETED": true, "WITHDRAWN": true,
}

type supplierProgressNode struct {
	ResourceID   string `json:"resource_id"`
	SubmissionID string `json:"submission_id"`
	NodeID       string `json:"node_id"`
	Key          string `json:"key"`
	Status       string `json:"status"`
	AssigneeID   string `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`
	RowStatus    string `json:"row_status"`
}

// Reads native supplier/submission identities, never legacy supplier_profile IDs.
func registerSupplierActivityRoutes(group *gin.RouterGroup, runtime *SupplyChainRuntime) {
	ready := func(c *gin.Context) bool {
		if runtime == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return false
		}
		if _, err := planPositiveID(c.Param("id")); err != nil {
			supplyHTTPError(c, err)
			return false
		}
		var id string
		err := runtime.raw.QueryRowContext(c.Request.Context(), `SELECT id::text FROM supplier WHERE id=$1`, c.Param("id")).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			supplyHTTPError(c, supply.ErrNotFound)
			return false
		}
		if err != nil {
			supplyHTTPError(c, err)
			return false
		}
		return true
	}
	group.GET("/suppliers/:id/progress", func(c *gin.Context) {
		if !ready(c) {
			return
		}
		if _, err := supplyQuery(c); err != nil {
			supplyHTTPError(c, err)
			return
		}
		rows, err := runtime.raw.QueryContext(c.Request.Context(), `SELECT DISTINCT ON(sr.resource_id,n.node_key) sr.resource_id::text,s.id::text,n.id::text,n.node_key,n.status,coalesce(n.assignee_id::text,''),coalesce(st.name,''),sr.row_status
  FROM submission s JOIN submission_row sr ON sr.submission_id=s.id
  JOIN operation_log l ON l.entity_type='submission' AND l.entity_id=s.id AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'supplier_id'=s.supplier_id::text
  JOIN onboarding_flow f ON f.id::text=l."after"->>'flow_id' AND f.kind='SUPPLIER' AND f.owner_id=s.supplier_id
  JOIN onboarding_node n ON n.flow_id=f.id AND n.enabled LEFT JOIN internal_staff st ON st.id=n.assignee_id
  WHERE s.supplier_id=$1 AND sr.resource_id IS NOT NULL ORDER BY sr.resource_id,n.node_key,l.id DESC`, c.Param("id"))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		defer rows.Close()
		list := []supplierProgressNode{}
		for rows.Next() {
			var n supplierProgressNode
			if err = rows.Scan(&n.ResourceID, &n.SubmissionID, &n.NodeID, &n.Key, &n.Status, &n.AssigneeID, &n.AssigneeName, &n.RowStatus); err != nil {
				supplyHTTPError(c, err)
				return
			}
			list = append(list, n)
		}
		if err = rows.Err(); err != nil {
			supplyHTTPError(c, err)
			return
		}
		response.Success(c, gin.H{"supplier_id": c.Param("id"), "list": list})
	})
	group.GET("/suppliers/:id/submissions", func(c *gin.Context) {
		if !ready(c) {
			return
		}
		_, page, size, err := f12ReadQuery(c, "page", "size", "status", "exclude_status")
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		// status / exclude_status：页面把「待审核」与「审批记录」拆成两张表，
		// 各自要独立的过滤与计数，否则分页会把待审批单挤到第二页以外看不见。
		status := strings.TrimSpace(c.Query("status"))
		exclude := strings.TrimSpace(c.Query("exclude_status"))
		for name, value := range map[string]string{"status": status, "exclude_status": exclude} {
			if value != "" && !submissionStatuses[value] {
				supplyHTTPError(c, &supply.FieldError{Path: name, Reason: "未知的申报状态"})
				return
			}
		}
		// 单状态清单（待审核）不分页：这类是待办，必须一次看全。
		unbounded := status != ""
		var total int
		if err = runtime.raw.QueryRowContext(c.Request.Context(), `SELECT count(*) FROM submission
 WHERE supplier_id=$1 AND ($2='' OR status=$2) AND ($3='' OR status<>$3)`, c.Param("id"), status, exclude).Scan(&total); err != nil {
			supplyHTTPError(c, err)
			return
		}
		// 审批时间与审批人来自 operation_log 的 APPROVE/REJECT 记录；
		// 一次查询带出来，避免逐条再查。
		rows, err := runtime.raw.QueryContext(c.Request.Context(), `SELECT s.id::text,
 (SELECT max(l.created_at) FROM operation_log l WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT')),
 COALESCE((SELECT l.operator_name_snapshot FROM operation_log l WHERE l.entity_type='submission' AND l.entity_id=s.id AND l.action IN ('APPROVE','REJECT') ORDER BY l.id DESC LIMIT 1),'')
 FROM submission s
 WHERE s.supplier_id=$1 AND ($2='' OR s.status=$2) AND ($3='' OR s.status<>$3)
 ORDER BY s.id DESC
 LIMIT (CASE WHEN $6 THEN NULL ELSE $4::bigint END) OFFSET $5`, c.Param("id"), status, exclude, size, (page-1)*size, unbounded)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		type approvalMeta struct {
			at sql.NullTime
			by string
		}
		ids := []string{}
		metas := []approvalMeta{}
		for rows.Next() {
			var id string
			var meta approvalMeta
			if err = rows.Scan(&id, &meta.at, &meta.by); err != nil {
				rows.Close()
				supplyHTTPError(c, err)
				return
			}
			ids = append(ids, id)
			metas = append(metas, meta)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		list := []*nativeFlowView{}
		for i, id := range ids {
			v, err := runtime.nativeFlowView(c.Request.Context(), id, supplyActor(c), false)
			if err != nil {
				supplyHTTPError(c, err)
				return
			}
			if i < len(metas) && metas[i].at.Valid {
				at := metas[i].at.Time.UTC()
				v.ApprovedAt = &at
				v.Approver = metas[i].by
			}
			list = append(list, v)
		}
		response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
	})
}
