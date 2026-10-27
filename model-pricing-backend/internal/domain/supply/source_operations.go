package supply

import (
	"context"
	"time"
)

type SourceOperationQuery struct {
	ModelID, SupplierID, ResourceID string
	EntityType                     string
	Page, Size                      int
}

// SourceOperation contains display fields only, never operation snapshots.
type SourceOperation struct {
	ID              string    `json:"id"`
	EntityType      string    `json:"entity_type"`
	EntityID        string    `json:"entity_id"`
	ResourceID      string    `json:"resource_id"`
	SupplierID      string    `json:"supplier_id"`
	SupplierName    string    `json:"supplier_name"`
	SupplierModelID string    `json:"supplier_model_id"`
	Region          string    `json:"region"`
	Action          string    `json:"action"`
	OperatorName    string    `json:"operator_name"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}
type SourceOperationPage struct {
	List  []SourceOperation `json:"list"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}
type SourceOperationStore interface {
	ListSourceOperations(context.Context, SourceOperationQuery) ([]SourceOperation, int64, error)
}
type SourceOperationReader struct{ store SourceOperationStore }

func sourceOperationTypeAllowed(entityType string, supplierScope bool) bool {
	switch entityType {
	case "resource", "plan":
		return true
	case "supplier", "supplier_qualification", "supplier_formal_config", "contract", "credit_line":
		return supplierScope
	default:
		return false
	}
}

func NewSourceOperationReader(store SourceOperationStore) *SourceOperationReader {
	return &SourceOperationReader{store: store}
}
func (r *SourceOperationReader) List(ctx context.Context, q SourceOperationQuery) (*SourceOperationPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if (validID(q.ModelID) == validID(q.SupplierID)) || (q.ModelID != "" && !validID(q.ModelID)) || (q.SupplierID != "" && !validID(q.SupplierID)) || (q.ResourceID != "" && (q.ModelID == "" || !validID(q.ResourceID))) {
		return nil, invalid("id", "exactly one model or supplier scope is required")
	}
	if q.EntityType != "" && (q.SupplierID == "" || !sourceOperationTypeAllowed(q.EntityType, true)) {
		return nil, invalid("entity_type", "unsupported supplier operation category")
	}
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return nil, err
	}
	rows, total, err := r.store.ListSourceOperations(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < int64(len(rows)) || len(rows) > q.Size {
		return nil, ErrReadContract
	}
	seen := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		resourceOperation := row.EntityType == "resource" || row.EntityType == "plan"
		if !validID(row.ID) || !validID(row.EntityID) || !sourceOperationTypeAllowed(row.EntityType, q.SupplierID != "") ||
			(row.EntityType == "resource" && row.EntityID != row.ResourceID) || (resourceOperation && !validID(row.ResourceID)) ||
			(!resourceOperation && (row.ResourceID != "" || row.SupplierModelID != "")) || (row.EntityType == "supplier" && row.EntityID != row.SupplierID) ||
			!validID(row.SupplierID) || seen[row.ID] || (q.EntityType != "" && row.EntityType != q.EntityType) || (q.ResourceID != "" && row.ResourceID != q.ResourceID) || (q.SupplierID != "" && row.SupplierID != q.SupplierID) || (row.Region != "CN" && row.Region != "OVERSEAS") || row.Action == "" || row.CreatedAt.IsZero() {
			return nil, ErrReadContract
		}
		seen[row.ID] = true
		row.CreatedAt = row.CreatedAt.UTC()
	}
	if rows == nil {
		rows = []SourceOperation{}
	}
	return &SourceOperationPage{List: rows, Total: total, Page: q.Page, Size: q.Size}, nil
}
