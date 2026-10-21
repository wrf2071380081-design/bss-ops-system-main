package supply

import (
	"context"
	"slices"
	"strings"
)

type ResourceListQuery struct {
	View, Keyword, Company, ModelType, SupplierID, StandardModelID, Region, State, SortBy, Order string
	Page, Size                                                                                   int
	NullsLast                                                                                    bool
	TieBreakBy                                                                                   string
}
type ResourceGroup struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	OfficialID     string         `json:"official_id"`
	Company        string         `json:"company"`
	ModelType      string         `json:"model_type"`
	SupplierCount  int            `json:"supplier_count"`
	ResourceCount  int            `json:"resource_count"`
	AvailableCount int            `json:"available_count"`
	CoefficientMin string         `json:"coefficient_min"`
	CoefficientMax string         `json:"coefficient_max"`
	Regions        []string       `json:"regions"`
	StateCounts    map[string]int `json:"state_counts"`
}
type ResourceListResult struct {
	View       string
	Groups     []*ResourceGroup
	Resources  []*ResourceSnapshot
	Total      int64
	Page, Size int
}
type ResourceReadStore interface {
	// Page distinct filtered model IDs first, then return complete members for
	// those IDs. Domain filtering below protects aggregates from extra regions,
	// but cannot repair missing members or an incorrect SQL total/order.
	ListResourceGroups(context.Context, ResourceListQuery) ([][]*ResourceRecord, int64, error)
	ListResourceRows(context.Context, ResourceListQuery) ([]*ResourceRecord, int64, error)
	GetResource(context.Context, string) (*ResourceRecord, error)
}
type ResourceReader struct{ store ResourceReadStore }

func NewResourceReader(store ResourceReadStore) *ResourceReader { return &ResourceReader{store: store} }
func normalizeResourceQuery(q ResourceListQuery) (ResourceListQuery, error) {
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return q, err
	}
	if q.View == "" {
		q.View = "grouped"
	}
	if q.View != "grouped" && q.View != "flat" {
		return q, invalid("view", "expected grouped or flat")
	}
	if q.Region != "" && q.Region != "CN" && q.Region != "OVERSEAS" {
		return q, invalid("region", "expected CN or OVERSEAS")
	}
	if q.ModelType != "" && !modelTypeKnown(q.ModelType) {
		return q, invalid("model_type", "unsupported model type")
	}
	if q.State != "" && !resourceStateKnown(q.State) {
		return q, invalid("state", "unsupported resource state")
	}
	for _, field := range []struct{ key, value string }{{"supplier_id", q.SupplierID}, {"standard_model_id", q.StandardModelID}} {
		if field.value != "" && !validID(field.value) {
			return q, invalid(field.key, "expected bigint string")
		}
	}
	q.Company = strings.TrimSpace(q.Company)
	q.Keyword = strings.TrimSpace(q.Keyword)
	if q.SortBy == "" {
		if q.View == "grouped" {
			q.SortBy = "name"
		} else {
			q.SortBy = "coefficient"
		}
	}
	if q.Order == "" {
		q.Order = "asc"
	}
	q.Order = strings.ToLower(q.Order)
	if q.Order != "asc" && q.Order != "desc" {
		return q, invalid("order", "expected asc or desc")
	}
	allowed := []string{"id", "name", "company", "model_type", "supplier_count", "resource_count", "available_count", "coefficient_min", "coefficient_max"}
	if q.View == "flat" {
		allowed = []string{"id", "supplier_name", "region", "coefficient", "state", "updated_at", "connection_ms", "rps", "tps", "cache_hit_rate"}
	}
	if !slices.Contains(allowed, q.SortBy) {
		return q, invalid("sort_by", "unsupported field for selected view")
	}
	q.NullsLast = true
	q.TieBreakBy = "id"
	return q, nil
}
func resourceMatches(row *ResourceSnapshot, q ResourceListQuery) bool {
	return (q.Region == "" || row.Region == q.Region) && (q.SupplierID == "" || row.SupplierID == q.SupplierID) && (q.StandardModelID == "" || row.StandardModelID == q.StandardModelID) && (q.Company == "" || row.Company == q.Company) && (q.ModelType == "" || row.ModelType == q.ModelType) && (q.State == "" || row.State == q.State) && (q.Keyword == "" || strings.Contains(strings.ToLower(row.ModelName+" "+row.OfficialID+" "+row.SupplierName+" "+row.SupplierModelID), strings.ToLower(q.Keyword)))
}
func buildResourceGroup(records []*ResourceRecord, q ResourceListQuery) (*ResourceGroup, error) {
	group := &ResourceGroup{Regions: []string{}, StateCounts: map[string]int{}}
	suppliers := map[string]bool{}
	ids := map[string]bool{}
	for _, record := range records {
		row, err := BuildResourceSnapshot(record)
		if err != nil {
			return nil, err
		}
		if !resourceMatches(row, q) {
			continue
		}
		if ids[row.ID] {
			return nil, ErrReadContract
		}
		ids[row.ID] = true
		if group.ID == "" {
			group.ID = row.StandardModelID
			group.Name = row.ModelName
			group.OfficialID = row.OfficialID
			group.Company = row.Company
			group.ModelType = row.ModelType
		}
		if group.ID != row.StandardModelID || group.Name != row.ModelName || group.Company != row.Company || group.OfficialID != row.OfficialID || group.ModelType != row.ModelType {
			return nil, ErrReadContract
		}
		suppliers[row.SupplierID] = true
		group.ResourceCount++
		group.StateCounts[row.State]++
		if row.QuoteReadiness.Eligible {
			group.AvailableCount++
		}
		if !slices.Contains(group.Regions, row.Region) {
			group.Regions = append(group.Regions, row.Region)
		}
		value, _ := resourceCoefficient(row.Coefficient)
		if group.CoefficientMin == "" {
			group.CoefficientMin = row.Coefficient
			group.CoefficientMax = row.Coefficient
		} else {
			minimum, _ := resourceCoefficient(group.CoefficientMin)
			maximum, _ := resourceCoefficient(group.CoefficientMax)
			if value.LessThan(minimum) {
				group.CoefficientMin = row.Coefficient
			}
			if value.GreaterThan(maximum) {
				group.CoefficientMax = row.Coefficient
			}
		}
	}
	if group.ID == "" {
		return nil, ErrReadContract
	}
	group.SupplierCount = len(suppliers)
	slices.Sort(group.Regions)
	return group, nil
}
func (r *ResourceReader) List(ctx context.Context, q ResourceListQuery) (*ResourceListResult, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	q, err := normalizeResourceQuery(q)
	if err != nil {
		return nil, err
	}
	result := &ResourceListResult{View: q.View, Page: q.Page, Size: q.Size, Groups: []*ResourceGroup{}, Resources: []*ResourceSnapshot{}}
	if q.View == "grouped" {
		groups, total, err := r.store.ListResourceGroups(ctx, q)
		if err != nil {
			return nil, err
		}
		if total < int64(len(groups)) || len(groups) > q.Size {
			return nil, ErrReadContract
		}
		result.Total = total
		seen := map[string]bool{}
		for _, records := range groups {
			group, err := buildResourceGroup(records, q)
			if err != nil {
				return nil, err
			}
			if seen[group.ID] {
				return nil, ErrReadContract
			}
			seen[group.ID] = true
			result.Groups = append(result.Groups, group)
		}
	} else {
		rows, total, err := r.store.ListResourceRows(ctx, q)
		if err != nil {
			return nil, err
		}
		if total < int64(len(rows)) || len(rows) > q.Size {
			return nil, ErrReadContract
		}
		result.Total = total
		seen := map[string]bool{}
		for _, record := range rows {
			row, err := BuildResourceSnapshot(record)
			if err != nil {
				return nil, err
			}
			if seen[row.ID] || !resourceMatches(row, q) {
				return nil, ErrReadContract
			}
			seen[row.ID] = true
			result.Resources = append(result.Resources, row)
		}
	}
	return result, nil
}
func (r *ResourceReader) Get(ctx context.Context, id, region string) (*ResourceSnapshot, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(id) {
		return nil, invalid("id", "expected bigint string")
	}
	if region != "" && region != "CN" && region != "OVERSEAS" {
		return nil, invalid("region", "expected CN or OVERSEAS")
	}
	record, err := r.store.GetResource(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := BuildResourceSnapshot(record)
	if err != nil {
		return nil, err
	}
	if row.ID != id {
		return nil, ErrReadContract
	}
	if region != "" && row.Region != region {
		return nil, ErrNotFound
	}
	return row, nil
}
