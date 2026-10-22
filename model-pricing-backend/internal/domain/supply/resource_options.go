package supply

import (
	"context"
	"strings"
)

type ResourceSupplierOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
}
type ResourceFilterOptions struct {
	Companies []string                 `json:"companies"`
	Suppliers []ResourceSupplierOption `json:"suppliers"`
	HasMore   bool                     `json:"has_more"`
}
type ResourceOptionsQuery struct {
	Region, Keyword string
	Limit           int
}

// Optional capability: options are queried from the complete resource relation,
// not the current list page. At most Limit matches per kind, with HasMore explicit.
type ResourceFilterOptionsStore interface {
	ResourceFilterOptions(context.Context, ResourceOptionsQuery) (ResourceFilterOptions, error)
}

func (r *ResourceReader) Options(ctx context.Context, region, keyword string) (*ResourceFilterOptions, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if region != "" && region != "CN" && region != "OVERSEAS" {
		return nil, invalid("region", "expected CN or OVERSEAS")
	}
	store, ok := r.store.(ResourceFilterOptionsStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	keyword = strings.TrimSpace(keyword)
	if err := resourceSearchText("keyword", keyword, 200); err != nil {
		return nil, err
	}
	query := ResourceOptionsQuery{Region: region, Keyword: keyword, Limit: 50}
	options, err := store.ResourceFilterOptions(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(options.Companies) > query.Limit || len(options.Suppliers) > query.Limit {
		return nil, ErrReadContract
	}
	seen := map[string]bool{}
	matches := func(value string) bool {
		return strings.TrimSpace(value) != "" && strings.Contains(strings.ToLower(value), strings.ToLower(query.Keyword))
	}
	for _, company := range options.Companies {
		if seen[company] || !matches(company) {
			return nil, ErrReadContract
		}
		seen[company] = true
	}
	seen = map[string]bool{}
	for _, supplier := range options.Suppliers {
		if !validID(supplier.ID) || seen[supplier.ID] || !matches(supplier.Name) || (supplier.Region != "CN" && supplier.Region != "OVERSEAS") || (region != "" && supplier.Region != region) {
			return nil, ErrReadContract
		}
		seen[supplier.ID] = true
	}
	result := &ResourceFilterOptions{Companies: append([]string{}, options.Companies...), Suppliers: append([]ResourceSupplierOption{}, options.Suppliers...), HasMore: options.HasMore}
	return result, nil
}
