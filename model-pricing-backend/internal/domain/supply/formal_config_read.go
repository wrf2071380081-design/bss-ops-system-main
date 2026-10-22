package supply

import (
	"context"
	"math"
	"strings"
)

type FormalConfigReadStore interface {
	ListFormalConfigs(context.Context, string) ([]*FormalConfigView, error)
}

type FormalConfigReader struct{ store FormalConfigReadStore }

func NewFormalConfigReader(store FormalConfigReadStore) *FormalConfigReader {
	return &FormalConfigReader{store: store}
}

func (r *FormalConfigReader) List(ctx context.Context, supplierID string) ([]*FormalConfigView, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(supplierID) {
		return nil, invalid("supplier_id", "expected positive bigint string")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	views, err := r.store.ListFormalConfigs(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	result := make([]*FormalConfigView, 0, len(views))
	seen := make(map[string]struct{}, len(views))
	for _, view := range views {
		if view == nil || !validID(view.ID) || view.SupplierID != supplierID || view.Revision == 0 || view.Revision > math.MaxInt64 || view.ConfirmedRevision == 0 || view.ConfirmedRevision > view.Revision || (view.Region != "CN" && view.Region != "OVERSEAS") || strings.TrimSpace(view.Scope) == "" || strings.TrimSpace(view.BaseURL) == "" || (view.Status != "ACTIVE" && view.Status != "INACTIVE") || !validID(view.CreatedBy) || !validID(view.UpdatedBy) || !validID(view.ConfirmedBy) || view.CreatedAt.IsZero() || view.UpdatedAt.Before(view.CreatedAt) || view.ConfirmedAt.Before(view.CreatedAt) || view.ConfirmedAt.After(view.UpdatedAt) || view.HasAPIKey != (view.APIKeyMask == "********") {
			return nil, ErrReadContract
		}
		if view.Status == "ACTIVE" && view.ConfirmedRevision != view.Revision {
			return nil, ErrReadContract
		}
		if _, ok := seen[view.ID]; ok {
			return nil, ErrReadContract
		}
		seen[view.ID] = struct{}{}
		copy := *view
		result = append(result, &copy)
	}
	return result, nil
}
