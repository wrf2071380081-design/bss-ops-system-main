package supply

import (
	"context"
	"strings"
)

type SupplierReader struct {
	store              SupplierReadStore
	qualificationStore SupplierQualificationReadStore
}

// SupplierAliasCapabilityStore reports whether former-name search and writes
// are available in the deployed schema. It is optional so older test/read
// adapters fail closed to "unsupported" instead of claiming empty aliases.
type SupplierAliasCapabilityStore interface {
	SupplierAliasesSupported(context.Context) (bool, error)
}

func NewSupplierReader(store SupplierReadStore) *SupplierReader {
	qualificationStore, _ := store.(SupplierQualificationReadStore)
	return &SupplierReader{store: store, qualificationStore: qualificationStore}
}

func (r *SupplierReader) AliasesSupported(ctx context.Context) (bool, error) {
	if r == nil || r.store == nil {
		return false, ErrDependencyUnavailable
	}
	capability, ok := r.store.(SupplierAliasCapabilityStore)
	if !ok {
		return false, nil
	}
	return capability.SupplierAliasesSupported(ctx)
}

func (r *SupplierReader) ListQualifications(ctx context.Context, id string, page, size int) (*SupplierQualificationPage, error) {
	if r == nil || r.store == nil || r.qualificationStore == nil {
		return nil, ErrDependencyUnavailable
	}
	profile, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.ID != id {
		return nil, ErrReadContract
	}
	page, size, err = readPagination(page, size)
	if err != nil {
		return nil, err
	}
	result, err := r.qualificationStore.ListQualifications(ctx, id, SupplierQualificationQuery{Page: page, Size: size})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Page != page || result.Size != size || result.Total < 0 || len(result.List) > size || (result.Total > 0 && len(result.List) == 0 && int64((page-1)*size) < result.Total) {
		return nil, ErrReadContract
	}
	seen := make(map[string]struct{}, len(result.List))
	for i := range result.List {
		row := &result.List[i]
		if !validID(row.ID) || row.SupplierID != id || strings.TrimSpace(row.Name) == "" || len([]rune(row.Name)) > 128 || row.UploadedAt.IsZero() {
			return nil, ErrReadContract
		}
		if _, exists := seen[row.ID]; exists {
			return nil, ErrReadContract
		}
		seen[row.ID] = struct{}{}
	}
	if result.List == nil {
		result.List = []SupplierQualification{}
	}
	return result, nil
}

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
