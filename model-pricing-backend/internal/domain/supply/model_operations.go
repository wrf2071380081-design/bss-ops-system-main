package supply

import (
	"context"
	"slices"
	"strings"
	"time"
)

// A-owned read projection of the shared operation_log; no second audit store.
type ModelOperationQuery struct {
	ModelID, SortBy, Order string
	Page, Size             int
}
type ModelOperationItem struct {
	ID           string    `json:"id"`
	ModelID      string    `json:"entity_id"`
	ModelName    string    `json:"model_name"`
	Action       string    `json:"action"`
	OperatorName string    `json:"operator_name"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}
type ModelOperationSnapshot struct {
	ID           string    `json:"id"`
	Data         ModelData `json:"data"`
	VersionNo    uint64    `json:"version_no,string"`
	Revision     uint64    `json:"revision,string"`
	VerifyStatus string    `json:"verify_status"`
}
type ModelOperationDetail struct {
	ModelOperationItem
	Before *ModelOperationSnapshot `json:"before"`
	After  *ModelOperationSnapshot `json:"after"`
}
type ModelOperationPage struct {
	List  []ModelOperationItem `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}
type ModelOperationStore interface {
	ListModelOperations(context.Context, ModelOperationQuery) ([]ModelOperationItem, int64, error)
	GetModelOperation(context.Context, string, string) (*ModelOperationDetail, error)
}

func NormalizeModelOperationQuery(q ModelOperationQuery) (ModelOperationQuery, error) {
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return q, err
	}
	if q.ModelID != "" && !validID(q.ModelID) {
		return q, invalid("id", "expected a positive bigint string")
	}
	if q.SortBy == "" {
		q.SortBy = "created_at"
	}
	if q.Order == "" {
		q.Order = "desc"
	}
	q.Order = strings.ToLower(q.Order)
	if !slices.Contains([]string{"id", "action", "operator_name", "created_at"}, q.SortBy) || !slices.Contains([]string{"asc", "desc"}, q.Order) {
		return q, invalid("sort", "unsupported model operation sort")
	}
	return q, nil
}
func validModelOperation(row ModelOperationItem, modelID string) bool {
	return validID(row.ID) && validID(row.ModelID) && (modelID == "" || row.ModelID == modelID) && row.Action != "" && !row.CreatedAt.IsZero()
}
func (r *ModelReader) ListOperations(ctx context.Context, q ModelOperationQuery) (*ModelOperationPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	store, ok := r.store.(ModelOperationStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	q, err := NormalizeModelOperationQuery(q)
	if err != nil {
		return nil, err
	}
	rows, total, err := store.ListModelOperations(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < int64(len(rows)) || len(rows) > q.Size {
		return nil, ErrReadContract
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if !validModelOperation(row, q.ModelID) || seen[row.ID] {
			return nil, ErrReadContract
		}
		seen[row.ID] = true
	}
	if rows == nil {
		rows = []ModelOperationItem{}
	}
	return &ModelOperationPage{rows, total, q.Page, q.Size}, nil
}
func (r *ModelReader) GetOperation(ctx context.Context, modelID, id string) (*ModelOperationDetail, error) {
	if !validID(modelID) || !validID(id) {
		return nil, invalid("id", "expected positive bigint strings")
	}
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	store, ok := r.store.(ModelOperationStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	row, err := store.GetModelOperation(ctx, modelID, id)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ID != id || !validModelOperation(row.ModelOperationItem, modelID) {
		return nil, ErrReadContract
	}
	for _, snapshot := range []*ModelOperationSnapshot{row.Before, row.After} {
		if snapshot != nil && (snapshot.ID != modelID || snapshot.VersionNo == 0 || snapshot.Revision == 0) {
			return nil, ErrReadContract
		}
	}
	return row, nil
}
