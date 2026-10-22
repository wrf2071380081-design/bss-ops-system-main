package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/domain/supply"
)

// Use the existing read-only snapshot runner for a consistent count and page.
func (s *ModelReadStore) ListSourceOperations(ctx context.Context, q supply.SourceOperationQuery) ([]supply.SourceOperation, int64, error) {
	if !modelIdentity(q.ModelID) || (q.ResourceID != "" && !modelIdentity(q.ResourceID)) || !modelPageValid(q.Page, q.Size) {
		return nil, 0, supply.ErrReadContract
	}
	result := []supply.SourceOperation{}
	var total int64
	err := s.within(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("standard_model")+` WHERE id=$1)`, q.ModelID).Scan(&exists); err != nil {
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
		from := ` FROM ` + s.table("operation_log") + ` l LEFT JOIN ` + s.table("plan") + ` p ON l.entity_type='plan' AND p.id=l.entity_id JOIN ` + s.table("resource") + ` r ON (l.entity_type='resource' AND l.entity_id=r.id) OR (l.entity_type='plan' AND p.resource_id=r.id) JOIN ` + s.table("supplier") + ` s ON s.id=r.supplier_id WHERE r.standard_model_id=$1 AND ($2::bigint IS NULL OR r.id=$2::bigint)`
		var resourceID any
		if q.ResourceID != "" {
			resourceID = q.ResourceID
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+from, q.ModelID, resourceID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT l.id::text,l.entity_type,l.entity_id::text,r.id::text,r.supplier_id::text,s.name,r.supplier_model_name,r.region,CASE WHEN l.entity_type='plan' AND l.action='PLAN_REVISION' THEN coalesce(l."after"->>'operation',l.action) ELSE l.action END,coalesce(l.operator_name_snapshot,''),coalesce(l.reason,''),l.created_at`+from+` ORDER BY l.created_at DESC,l.id DESC LIMIT $3 OFFSET $4`, q.ModelID, resourceID, q.Size, (q.Page-1)*q.Size)
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
