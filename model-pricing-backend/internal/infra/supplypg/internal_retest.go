package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
)

// InternalRetestResource verifies current B handling facts. Being the buyer,
// administrator or selected workbench viewer does not grant test execution.
// No supplier URL or Key is loaded by this query.
func (s *TestPlatformStore) InternalRetestResource(ctx context.Context, actorID, resourceID string) (*ResourceTestTarget, error) {
	if _, err := parseID(actorID); err != nil {
		return nil, supply.ErrForbidden
	}
	if _, err := parseID(resourceID); err != nil {
		return nil, supply.ErrNotFound
	}
	tx, err := GORMModelTxFromContext(ctx)
	if err != nil {
		tx, err = s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
	}
	state, err := NewResourceStateStore(s.schema, nil)
	if err != nil {
		return nil, err
	}
	resource, err := state.Current(ctx, tx, resourceID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.assignee_id::text
 FROM `+s.table("submission_row")+` sr
 JOIN `+s.table("submission")+` sub ON sub.id=sr.submission_id
 JOIN `+s.table("onboarding_flow")+` f ON f.owner_id=sub.supplier_id AND f.kind='SUPPLIER' AND f.status='PROCESSING'
 JOIN `+s.table("onboarding_node")+` n ON n.flow_id=f.id AND n.node_key='CONFIG' AND n.status='IN_PROGRESS' AND n.enabled
 WHERE sr.id=$1 AND sr.resource_id=$2 AND sr.standard_model_id=$3
 AND sub.supplier_id=$4 AND sub.region=$5 AND sub.status='PROCESSING' AND sr.row_status<>'CANCELED'
 FOR SHARE OF sr,sub,f,n`, resource.OriginRowID, resource.ID, resource.StandardModelID, resource.SupplierID, resource.Region)
	if err != nil {
		return nil, err
	}
	assignees := []string{}
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		assignees = append(assignees, id.String)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(assignees) != 1 || assignees[0] != actorID {
		return nil, supply.ErrForbidden
	}
	result := &ResourceTestTarget{StandardModelID: resource.StandardModelID, SupplierID: resource.SupplierID}
	err = tx.QueryRowContext(ctx, `SELECT m.official_id,s.name FROM `+s.table("standard_model")+` m JOIN `+s.table("supplier")+` s ON s.id=$2 AND s.status='ACTIVE' WHERE m.id=$1`, resource.StandardModelID, resource.SupplierID).Scan(&result.Model, &result.SupplierName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if result.Model == "" {
		return nil, supply.ErrDependencyUnavailable
	}
	return result, nil
}
