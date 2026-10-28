package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/domain/supply"
)

// Use the existing read-only snapshot runner for a consistent count and page.
func (s *ModelReadStore) ListSourceOperations(ctx context.Context, q supply.SourceOperationQuery) ([]supply.SourceOperation, int64, error) {
	if (modelIdentity(q.ModelID) == modelIdentity(q.SupplierID)) || (q.ModelID != "" && !modelIdentity(q.ModelID)) || (q.SupplierID != "" && !modelIdentity(q.SupplierID)) || (q.ResourceID != "" && (q.ModelID == "" || !modelIdentity(q.ResourceID))) || !modelPageValid(q.Page, q.Size) {
		return nil, 0, supply.ErrReadContract
	}
	if q.SupplierID != "" {
		return s.listSupplierOperations(ctx, q)
	}
	if q.EntityType != "" {
		return nil, 0, supply.ErrReadContract
	}
	result := []supply.SourceOperation{}
	var total int64
	err := s.within(ctx, func(tx *sql.Tx) error {
		var exists bool
		scopeID, scopeColumn := q.ModelID, "r.standard_model_id"
		if q.SupplierID != "" {
			scopeID, scopeColumn = q.SupplierID, "r.supplier_id"
		}
		existenceTable := "standard_model"
		if q.SupplierID != "" {
			existenceTable = "supplier"
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table(existenceTable)+` WHERE id=$1)`, scopeID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return supply.ErrNotFound
		}
		if q.ResourceID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("resource")+` WHERE id=$1 AND standard_model_id=$2)`, q.ResourceID, q.ModelID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return supply.ErrNotFound
			}
		}
		from := ` FROM ` + s.table("operation_log") + ` l LEFT JOIN ` + s.table("plan") + ` p ON l.entity_type='plan' AND p.id=l.entity_id JOIN ` + s.table("resource") + ` r ON (l.entity_type='resource' AND l.entity_id=r.id) OR (l.entity_type='plan' AND p.resource_id=r.id) JOIN ` + s.table("supplier") + ` s ON s.id=r.supplier_id WHERE ` + scopeColumn + `=$1 AND ($2::bigint IS NULL OR r.id=$2::bigint)`
		var resourceID any
		if q.ResourceID != "" {
			resourceID = q.ResourceID
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+from, scopeID, resourceID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT l.id::text,l.entity_type,l.entity_id::text,r.id::text,r.supplier_id::text,s.name,r.supplier_model_name,r.region,CASE WHEN l.entity_type='plan' AND l.action='PLAN_REVISION' THEN coalesce(l."after"->>'operation',l.action) ELSE l.action END,coalesce(l.operator_name_snapshot,''),coalesce(l.reason,''),l.created_at`+from+` ORDER BY l.created_at DESC,l.id DESC LIMIT $3 OFFSET $4`, scopeID, resourceID, q.Size, (q.Page-1)*q.Size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row supply.SourceOperation
			if err := rows.Scan(&row.ID, &row.EntityType, &row.EntityID, &row.ResourceID, &row.SupplierID, &row.SupplierName, &row.SupplierModelID, &row.Region, &row.Action, &row.OperatorName, &row.Reason, &row.CreatedAt); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

// Supplier history includes subject-level records even when there are no
// resources. Ownership comes from business rows, never from log snapshots.
func (s *ModelReadStore) listSupplierOperations(ctx context.Context, q supply.SourceOperationQuery) ([]supply.SourceOperation, int64, error) {
	switch q.EntityType {
	case "", "supplier", "supplier_qualification", "supplier_formal_config", "contract", "credit_line", "resource", "plan":
	default:
		return nil, 0, supply.ErrReadContract
	}
	result := []supply.SourceOperation{}
	var total int64
	err := s.within(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("supplier")+` WHERE id=$1)`, q.SupplierID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return supply.ErrNotFound
		}
		projection := `SELECT l.id,l.entity_type,l.entity_id,r.id::text AS resource_id,r.supplier_id::text AS supplier_id,s.name AS supplier_name,r.supplier_model_name AS supplier_model_id,r.region,
CASE WHEN (l.entity_type='plan' AND l.action='PLAN_REVISION') OR (l.entity_type='resource' AND l.action='RESOURCE_REVISION') THEN coalesce(l."after"->>'operation',l.action) ELSE l.action END AS action,
coalesce(l.operator_name_snapshot,'') AS operator_name,coalesce(l.reason,'') AS reason,l.created_at
FROM ` + s.table("operation_log") + ` l LEFT JOIN ` + s.table("plan") + ` p ON l.entity_type='plan' AND p.id=l.entity_id
JOIN ` + s.table("resource") + ` r ON (l.entity_type='resource' AND l.entity_id=r.id) OR (l.entity_type='plan' AND p.resource_id=r.id)
JOIN ` + s.table("supplier") + ` s ON s.id=r.supplier_id WHERE r.supplier_id=$1
UNION ALL
SELECT l.id,l.entity_type,l.entity_id,''::text,s.id::text,s.name,''::text,s.region,
CASE WHEN l.entity_type='supplier_formal_config' AND l.action='FORMAL_CONFIG_REVISION' THEN coalesce(l."after"->>'operation',l.action) ELSE l.action END,
coalesce(l.operator_name_snapshot,''),coalesce(l.reason,''),l.created_at
FROM ` + s.table("operation_log") + ` l JOIN ` + s.table("supplier") + ` s ON s.id=$1
WHERE (l.entity_type='supplier' AND l.entity_id=s.id)
 OR (l.entity_type='supplier_qualification' AND EXISTS(SELECT 1 FROM ` + s.table("supplier_qualification") + ` q WHERE q.id=l.entity_id AND q.supplier_id=s.id))
 OR (l.entity_type='supplier_formal_config' AND EXISTS(SELECT 1 FROM ` + s.table("supplier_formal_config") + ` f WHERE f.id=l.entity_id AND f.supplier_id=s.id))
 OR (l.entity_type='contract' AND EXISTS(SELECT 1 FROM ` + s.table("contract") + ` c WHERE c.id=l.entity_id AND c.subject_type='SUPPLIER' AND c.subject_id=s.id))
 OR (l.entity_type='credit_line' AND EXISTS(SELECT 1 FROM ` + s.table("credit_line") + ` c WHERE c.id=l.entity_id AND c.subject_type='SUPPLIER' AND c.subject_id=s.id))`
		from := ` FROM (` + projection + `) op WHERE ($2::text='' OR op.entity_type=$2)`
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+from, q.SupplierID, q.EntityType).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT op.id::text,op.entity_type,op.entity_id::text,op.resource_id,op.supplier_id,op.supplier_name,op.supplier_model_id,op.region,op.action,op.operator_name,op.reason,op.created_at`+from+` ORDER BY op.created_at DESC,op.id DESC LIMIT $3 OFFSET $4`, q.SupplierID, q.EntityType, q.Size, (q.Page-1)*q.Size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row supply.SourceOperation
			if err := rows.Scan(&row.ID, &row.EntityType, &row.EntityID, &row.ResourceID, &row.SupplierID, &row.SupplierName, &row.SupplierModelID, &row.Region, &row.Action, &row.OperatorName, &row.Reason, &row.CreatedAt); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
