package supply

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Multi-value dimensions use OR within each dimension and AND across dimensions.
// SupplierIDs and Regions MUST match the same resource in one EXISTS predicate.
// Normalized queries use plural fields; empty relation filters keep models with no resources.
func normalizeModelFilters(q ModelListQuery) (ModelListQuery, error) {
	if (q.Company != "" && len(q.Companies) > 0) || (q.ModelType != "" && len(q.ModelTypes) > 0) {
		return q, invalid("filters", "singular and plural filters cannot be combined")
	}
	if q.Company != "" {
		q.Companies = []string{q.Company}
	}
	if q.ModelType != "" {
		q.ModelTypes = []string{q.ModelType}
	}
	var err error
	q.Companies, err = normalizeModelValues("companies", q.Companies, func(v string) bool { return utf8.RuneCountInString(v) <= 200 })
	if err != nil {
		return q, err
	}
	q.ModelTypes, err = normalizeModelValues("model_types", q.ModelTypes, modelTypeKnown)
	if err != nil {
		return q, err
	}
	q.SupplierIDs, err = normalizeModelValues("supplier_ids", q.SupplierIDs, validID)
	if err != nil {
		return q, err
	}
	q.Regions, err = normalizeModelValues("regions", q.Regions, func(v string) bool { return v == "CN" || v == "OVERSEAS" })
	q.Company = ""
	q.ModelType = ""
	return q, err
}
func normalizeModelValues(field string, values []string, valid func(string) bool) ([]string, error) {
	if len(values) > 50 {
		return nil, invalid(field, "at most 50 distinct values")
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] || !valid(value) {
			return nil, invalid(field, "expected distinct supported values")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

type ModelSupplierOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
}
type ModelFilterOptions struct {
	Companies []string              `json:"companies"`
	Suppliers []ModelSupplierOption `json:"suppliers"`
	HasMore   bool                  `json:"has_more"`
}
type ModelOptionsQuery struct {
	Regions []string
	Keyword string
	Limit   int
}

// Companies come from all standard models, including models with no resources.
// Suppliers come from related resources and their supplier identities, filtered by Regions.
// Apply keyword matching and stable name/id sorting before the per-kind limit.
type ModelFilterOptionsStore interface {
	ModelFilterOptions(context.Context, ModelOptionsQuery) (ModelFilterOptions, error)
}

func (r *ModelReader) Options(ctx context.Context, regions []string, keyword string) (*ModelFilterOptions, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	store, ok := r.store.(ModelFilterOptionsStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	normalized, err := normalizeModelValues("regions", regions, func(v string) bool { return v == "CN" || v == "OVERSEAS" })
	if err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	if utf8.RuneCountInString(keyword) > 200 {
		return nil, invalid("keyword", "at most 200 characters")
	}
	result, err := store.ModelFilterOptions(ctx, ModelOptionsQuery{Regions: normalized, Keyword: keyword, Limit: 50})
	if err != nil {
		return nil, err
	}
	if len(result.Companies) > 50 || len(result.Suppliers) > 50 {
		return nil, ErrReadContract
	}
	companies, err := normalizeModelValues("companies", result.Companies, func(v string) bool { return utf8.RuneCountInString(v) <= 200 })
	if err != nil {
		return nil, ErrReadContract
	}
	suppliers := make([]ModelSupplierOption, 0, len(result.Suppliers))
	seen := map[string]bool{}
	for _, supplier := range result.Suppliers {
		if !validID(supplier.ID) || strings.TrimSpace(supplier.Name) == "" || seen[supplier.ID] || (supplier.Region != "CN" && supplier.Region != "OVERSEAS") {
			return nil, ErrReadContract
		}
		if len(normalized) > 0 && !modelFilterContains(normalized, supplier.Region) {
			return nil, ErrReadContract
		}
		seen[supplier.ID] = true
		suppliers = append(suppliers, supplier)
	}
	return &ModelFilterOptions{Companies: companies, Suppliers: suppliers, HasMore: result.HasMore}, nil
}
func modelFilterContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
