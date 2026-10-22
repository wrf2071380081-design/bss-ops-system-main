package supply

import (
	"context"
	"strings"
)

type SupplierReader struct{ store SupplierReadStore }

func NewSupplierReader(store SupplierReadStore) *SupplierReader { return &SupplierReader{store: store} }

func (r *SupplierReader) List(ctx context.Context, query SupplierListQuery) (*SupplierPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	var err error
	query.Page, query.Size, err = readPagination(query.Page, query.Size)
	if err != nil {
		return nil, err
	}
	query.Keyword = strings.TrimSpace(query.Keyword)
	if err := resourceSearchText("keyword", query.Keyword, 200); err != nil {
		return nil, err
	}
	if query.Region != "" && query.Region != "CN" && query.Region != "OVERSEAS" {
		return nil, invalid("region", "expected CN or OVERSEAS")
	}
	return r.store.List(ctx, query)
}

func (r *SupplierReader) Get(ctx context.Context, id string) (*Supplier, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(id) {
		return nil, invalid("id", "expected positive bigint string")
	}
	profile, err := r.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.ID != id || !validID(profile.BuyerID) || profile.Revision == 0 || profile.CreatedAt.IsZero() || profile.UpdatedAt.Before(profile.CreatedAt) {
		return nil, ErrReadContract
	}
	if _, err := canonicalSupplierData(profile.Data); err != nil {
		return nil, ErrReadContract
	}
	return profile.Clone(), nil
}
