package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
)

var _ supply.ModelOperationStore = (*ModelReadStore)(nil)

const modelOperationColumns = `l.id::text,l.entity_id::text,coalesce(nullif(l."after"#>>'{data,name}',''),nullif(l."before"#>>'{data,name}',''),''),l.action,coalesce(l.operator_name_snapshot,''),coalesce(l.reason,''),l.created_at`

func (s *ModelReadStore) ListModelOperations(ctx context.Context, q supply.ModelOperationQuery) ([]supply.ModelOperationItem, int64, error) {
	var err error
	q, err = supply.NormalizeModelOperationQuery(q)
	if err != nil {
		return nil, 0, err
	}
	columns := map[string]string{"id": "l.id", "action": "l.action", "operator_name": "l.operator_name_snapshot", "created_at": "l.created_at"}
	from := ` FROM ` + s.table("operation_log") + ` l WHERE l.entity_type='standard_model' AND ($1::bigint IS NULL OR l.entity_id=$1::bigint)`
	var modelID any
	if q.ModelID != "" {
		modelID = q.ModelID
	}
	result := []supply.ModelOperationItem{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*)`+from, modelID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+modelOperationColumns+from+` ORDER BY `+columns[q.SortBy]+` `+q.Order+` NULLS LAST,l.id DESC LIMIT $2 OFFSET $3`, modelID, q.Size, (q.Page-1)*q.Size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row supply.ModelOperationItem
			if err := rows.Scan(&row.ID, &row.ModelID, &row.ModelName, &row.Action, &row.OperatorName, &row.Reason, &row.CreatedAt); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	return result, total, err
}

// Decode only the known model schema; raw operation JSON is never returned.
func modelOperationSnapshot(raw []byte, id string) *supply.ModelOperationSnapshot {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var model supply.StandardModel
	if json.Unmarshal(raw, &model) != nil || model.ID != id || validateStoredModel(&model) != nil {
		return nil
	}
	return &supply.ModelOperationSnapshot{ID: model.ID, Data: model.Clone().Data, VersionNo: model.VersionNo, Revision: model.Revision, VerifyStatus: model.VerifyStatus}
}
func (s *ModelReadStore) GetModelOperation(ctx context.Context, modelID, id string) (*supply.ModelOperationDetail, error) {
	if !modelIdentity(modelID) || !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	var result supply.ModelOperationDetail
	err := s.within(ctx, func(tx *sql.Tx) error {
		var before, after []byte
		err := tx.QueryRowContext(ctx, `SELECT `+modelOperationColumns+`,l."before",l."after" FROM `+s.table("operation_log")+` l WHERE l.entity_type='standard_model' AND l.entity_id=$1 AND l.id=$2`, modelID, id).Scan(&result.ID, &result.ModelID, &result.ModelName, &result.Action, &result.OperatorName, &result.Reason, &result.CreatedAt, &before, &after)
		if errors.Is(err, sql.ErrNoRows) {
			return supply.ErrNotFound
		}
		if err != nil {
			return err
		}
		result.Before = modelOperationSnapshot(before, modelID)
		result.After = modelOperationSnapshot(after, modelID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
