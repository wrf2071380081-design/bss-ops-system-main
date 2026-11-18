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
	// 这里刻意用**窄查询**读资源，不走 A 线 ResourceStateStore.Current：
	// 那套契约要求「申报单来源 + facts 对齐」，运营手工登记的资源（无申报行）会被判
	// ErrReadContract，导致这类资源永远不能自测（2026-11-18 实测）。
	// 复测只需要 4 个字段，直接查即可（申报行另查，可能没有）。
	var standardModelID, supplierID, region string
	var originRow sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT standard_model_id::text, supplier_id::text, region FROM `+s.table("resource")+` WHERE id=$1`, resourceID).Scan(&standardModelID, &supplierID, &region); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		return nil, err
	}
	// 资源对应的申报行（手工登记的资源没有申报行，保持 NULL 即可）
	if err := tx.QueryRowContext(ctx, `SELECT id FROM `+s.table("submission_row")+` WHERE resource_id=$1 ORDER BY id DESC LIMIT 1`, resourceID).Scan(&originRow); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.assignee_id::text
 FROM `+s.table("submission_row")+` sr
 JOIN `+s.table("submission")+` sub ON sub.id=sr.submission_id
 JOIN `+s.table("onboarding_flow")+` f ON f.owner_id=sub.supplier_id AND f.kind='SUPPLIER' AND f.status='PROCESSING'
 JOIN `+s.table("onboarding_node")+` n ON n.flow_id=f.id AND n.node_key='CONFIG' AND n.status='IN_PROGRESS' AND n.enabled
 WHERE sr.id=$1 AND sr.resource_id=$2 AND sr.standard_model_id=$3
 AND sub.supplier_id=$4 AND sub.region=$5 AND sub.status='PROCESSING' AND sr.row_status<>'CANCELED'
 FOR SHARE OF sr,sub,f,n`, originRow.Int64, resourceID, standardModelID, supplierID, region)
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
		if id.Valid {
			assignees = append(assignees, id.String)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// 授权口径：
	//
	//   ① 资源来自供应商申报：必须是该申报单 CONFIG 节点的办理人（原口径，保持严格）；
	//   ② 资源是运营手工登记的（没有申报行）：允许**该资源的登记人（buyer）**自测。
	//      手工登记的用途就是"先把货源记下来、再补测试"，如果这里一刀切禁掉，
	//      这条通道就永远测不了（2026-11-18 实测卡点）。
	if len(assignees) == 0 {
		var buyer sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT buyer_id::text FROM `+s.table("resource")+` WHERE id=$1`, resourceID).Scan(&buyer); err != nil {
			return nil, err
		}
		if !buyer.Valid || buyer.String != actorID {
			return nil, supply.ErrForbidden
		}
	} else {
		// 同一供应商可能残留多条 PROCESSING 流程（历史脏数据），会把同一办理人查出多次；
		// 按去重后的集合判断：当前人在办理人里就放行，不再要求“恰好一条”。
		allowed := false
		for _, id := range assignees {
			if id == actorID {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, supply.ErrForbidden
		}
	}
	result := &ResourceTestTarget{StandardModelID: standardModelID, SupplierID: supplierID}
	err = tx.QueryRowContext(ctx, `SELECT m.official_id,s.name FROM `+s.table("standard_model")+` m JOIN `+s.table("supplier")+` s ON s.id=$2 AND s.status='ACTIVE' WHERE m.id=$1`, standardModelID, supplierID).Scan(&result.Model, &result.SupplierName)
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
