package api

import (
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

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
		_, page, size, err := f12ReadQuery(c, "page", "size")
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		var total int
		if err = runtime.raw.QueryRowContext(c.Request.Context(), `SELECT count(*) FROM submission WHERE supplier_id=$1`, c.Param("id")).Scan(&total); err != nil {
			supplyHTTPError(c, err)
			return
		}
		rows, err := runtime.raw.QueryContext(c.Request.Context(), `SELECT id::text FROM submission WHERE supplier_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, c.Param("id"), size, (page-1)*size)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				supplyHTTPError(c, err)
				return
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		list := []*nativeFlowView{}
		for _, id := range ids {
			v, err := runtime.nativeFlowView(c.Request.Context(), id, supplyActor(c), false)
			if err != nil {
				supplyHTTPError(c, err)
				return
			}
			list = append(list, v)
		}
		response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
	})
}
