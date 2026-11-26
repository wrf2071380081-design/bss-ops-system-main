package supply

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ResourceListQuery struct {
	View, Keyword, Company, ModelType, SupplierID, StandardModelID, Region, State, SortBy, Order string
	Companies, ModelTypes, SupplierIDs, Regions, States                                          []string
	CoefficientMin, CoefficientMax, ConnectionMin, ConnectionMax, RPSMin, RPSMax, TPSMin, TPSMax string
	Page, Size                                                                                   int
	NullsLast                                                                                    bool
	TieBreakBy                                                                                   string
}

// RegionCoefficient 是某部署节点内的采购系数区间。原型在列表里按节点分列展示
// （如「国内：0.531–0.693；海外：0.486–0.8」），单一全局区间会被最便宜的节点带偏。
type RegionCoefficient struct {
	Region string `json:"region"`
	Min    string `json:"min"`
	Max    string `json:"max"`
}
type ResourceGroup struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	OfficialID         string              `json:"official_id"`
	Company            string              `json:"company"`
	ModelType          string              `json:"model_type"`
	SupplierCount      int                 `json:"supplier_count"`
	ResourceCount      int                 `json:"resource_count"`
	AvailableCount     int                 `json:"available_count"`
	CoefficientMin     string              `json:"coefficient_min"`
	CoefficientMax     string              `json:"coefficient_max"`
	Regions            []string            `json:"regions"`
	RegionCoefficients []RegionCoefficient `json:"region_coefficients"`
	StateCounts        map[string]int      `json:"state_counts"`
	// Issues 是"这个模型下有哪些资源读不出来"的展示信息。
	// 问题行不计入上面的供应商数/资源数/可报价数/系数区间，避免把没接入完成的数据算成货源；
	// 但它必须显示出来——隐藏起来用户只会看到模型凭空少了货源，无从排查（2026-11-26 事故）。
	Issues []ResourceIssue `json:"issues,omitempty"`
}

// ResourceGroupRows 是一个分组的成员资源。Model 只在没有可统计资源时提供模型元信息
// ——模型广场同时是招商清单，没有资源的模型也要占一行（供应来源 0 家、系数「—」）。
type ResourceGroupRows struct {
	Model   *ResourceGroup
	Records []*ResourceRecord
	// Issues 是该模型下读不出来、未计入 Records 的资源问题。
	Issues []ResourceIssue
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
	ListResourceGroups(context.Context, ResourceListQuery) ([]ResourceGroupRows, int64, error)
	ListResourceRows(context.Context, ResourceListQuery) ([]*ResourceRecord, int64, error)
	GetResource(context.Context, string) (*ResourceRecord, error)
	// ConfigGroupOf 取资源所属的准入配置组（旁路查询，**不参与**控制快照的投影比对）。
	ConfigGroupOf(context.Context, string) (ResourceConfigGroup, error)
}

// ResourceConfigGroup 是资源与准入配置组的关系（正式配置按组复用）。
type ResourceConfigGroup struct {
	GroupID   string `json:"config_group_id"`
	GroupName string `json:"config_group_name"`
	BaseURL   string `json:"base_url"`
	HasKey    bool   `json:"has_api_key"`
}
type ResourceReader struct{ store ResourceReadStore }

func NewResourceReader(store ResourceReadStore) *ResourceReader { return &ResourceReader{store: store} }

// ConfigGroup 取该资源所属的准入配置组。
// 单独一条只读查询，不走投影校验：正式配置要按"组"复用（2026-11-20）。
func (r *ResourceReader) ConfigGroup(ctx context.Context, resourceID string) (ResourceConfigGroup, error) {
	if r == nil || r.store == nil {
		return ResourceConfigGroup{}, ErrDependencyUnavailable
	}
	if id, err := strconv.ParseInt(resourceID, 10, 64); err != nil || id <= 0 {
		return ResourceConfigGroup{}, invalid("id", "expected positive id")
	}
	return r.store.ConfigGroupOf(ctx, resourceID)
}
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
	q, err = normalizeResourceFilters(q)
	if err != nil {
		return q, err
	}
	if err := resourceSearchText("company", q.Company, 128); err != nil {
		return q, err
	}
	if err := resourceSearchText("keyword", q.Keyword, 200); err != nil {
		return q, err
	}
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
		allowed = []string{"id", "supplier_name", "region", "coefficient", "state", "updated_at", "admitted_at", "connection_ms", "rps", "tps", "cache_hit_rate"}
	}
	if !slices.Contains(allowed, q.SortBy) {
		return q, invalid("sort_by", "unsupported field for selected view")
	}
	q.NullsLast = true
	q.TieBreakBy = "id"
	return q, nil
}
func resourceSearchText(field, value string, limit int) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > limit {
		return invalid(field, "invalid search text or character limit exceeded")
	}
	return nil
}
func resourceMatches(row *ResourceSnapshot, q ResourceListQuery) bool {
	return resourceExtraMatches(row, q) && (q.Region == "" || row.Region == q.Region) && (q.SupplierID == "" || row.SupplierID == q.SupplierID) && (q.StandardModelID == "" || row.StandardModelID == q.StandardModelID) && (q.Company == "" || row.Company == q.Company) && (q.ModelType == "" || row.ModelType == q.ModelType) && (q.State == "" || row.State == q.State) && (q.Keyword == "" || strings.Contains(strings.ToLower(row.ModelName+" "+row.OfficialID+" "+row.SupplierName+" "+row.SupplierModelID), strings.ToLower(q.Keyword)))
}
func buildResourceGroup(records []*ResourceRecord, q ResourceListQuery) (*ResourceGroup, error) {
	group := &ResourceGroup{Regions: []string{}, StateCounts: map[string]int{}, RegionCoefficients: []RegionCoefficient{}}
	suppliers := map[string]bool{}
	ids := map[string]bool{}
	byRegion := map[string]*RegionCoefficient{}
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
		value, err := resourceCoefficient(row.Coefficient)
		if err != nil {
			return nil, err
		}
		if group.CoefficientMin == "" {
			group.CoefficientMin = row.Coefficient
			group.CoefficientMax = row.Coefficient
		} else {
			minimum, err := resourceCoefficient(group.CoefficientMin)
			if err != nil {
				return nil, err
			}
			maximum, err := resourceCoefficient(group.CoefficientMax)
			if err != nil {
				return nil, err
			}
			if value.LessThan(minimum) {
				group.CoefficientMin = row.Coefficient
			}
			if value.GreaterThan(maximum) {
				group.CoefficientMax = row.Coefficient
			}
		}
		regional := byRegion[row.Region]
		if regional == nil {
			byRegion[row.Region] = &RegionCoefficient{Region: row.Region, Min: row.Coefficient, Max: row.Coefficient}
			continue
		}
		minimum, err := resourceCoefficient(regional.Min)
		if err != nil {
			return nil, err
		}
		maximum, err := resourceCoefficient(regional.Max)
		if err != nil {
			return nil, err
		}
		if value.LessThan(minimum) {
			regional.Min = row.Coefficient
		}
		if value.GreaterThan(maximum) {
			regional.Max = row.Coefficient
		}
	}
	if group.ID == "" {
		return nil, ErrReadContract
	}
	group.SupplierCount = len(suppliers)
	slices.Sort(group.Regions)
	// 固定 CN 在前，与原型「国内：…；海外：…」的读法一致。
	for _, region := range []string{"CN", "OVERSEAS"} {
		if regional, ok := byRegion[region]; ok {
			group.RegionCoefficients = append(group.RegionCoefficients, *regional)
		}
	}
	return group, nil
}

// buildEmptyResourceGroup 把"没有货源的标准模型"变成一个正常的空分组：
// 供应来源 0 家、系数留空、状态为空。模型广场因此同时充当招商清单。
func buildEmptyResourceGroup(model *ResourceGroup) (*ResourceGroup, error) {
	if model == nil || !validID(model.ID) || model.Name == "" || model.OfficialID == "" || model.Company == "" || model.ModelType == "" || model.ResourceCount != 0 || model.SupplierCount != 0 || model.AvailableCount != 0 || model.CoefficientMin != "" || model.CoefficientMax != "" || len(model.RegionCoefficients) != 0 || len(model.StateCounts) != 0 {
		return nil, ErrReadContract
	}
	return &ResourceGroup{ID: model.ID, Name: model.Name, OfficialID: model.OfficialID, Company: model.Company, ModelType: model.ModelType, Regions: []string{}, RegionCoefficients: []RegionCoefficient{}, StateCounts: map[string]int{}}, nil
}
// resourceGroupFallback 在某组数据构建失败时，尽量用模型元信息拼一个"只有问题、没有统计"的行，
// 让前端仍能看到这个模型，而不是整页报错或静默消失。
func resourceGroupFallback(row ResourceGroupRows) *ResourceGroup {
	meta := row.Model
	if meta == nil && len(row.Records) > 0 {
		if m := row.Records[0].Model; m != nil {
			meta = &ResourceGroup{ID: m.ID, Name: m.Data.Name, OfficialID: m.Data.OfficialID, Company: m.Data.Company, ModelType: m.Data.ModelType}
		}
	}
	if meta == nil {
		return nil
	}
	return &ResourceGroup{ID: meta.ID, Name: meta.Name, OfficialID: meta.OfficialID, Company: meta.Company, ModelType: meta.ModelType, Regions: []string{}, RegionCoefficients: []RegionCoefficient{}, StateCounts: map[string]int{}}
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
		for _, row := range groups {
			var group *ResourceGroup
			var buildErr error
			switch {
			case len(row.Records) == 0 && row.Model != nil:
				group, buildErr = buildEmptyResourceGroup(row.Model)
			case len(row.Records) > 0:
				group, buildErr = buildResourceGroup(row.Records, q)
			default:
				buildErr = ErrReadContract
			}
			if buildErr != nil || group == nil {
				// 一组数据不合契约不再打挂整页：降级成一条带原因的问题行。
				group = resourceGroupFallback(row)
				if group == nil {
					continue
				}
				group.Issues = append(group.Issues, ResourceIssue{Code: "RESOURCE_GROUP_INVALID", Message: "该模型下的资源数据不一致，已从统计中剔除"})
			}
			if len(row.Issues) > 0 {
				group.Issues = append(group.Issues, row.Issues...)
			}
			if group.ID == "" || seen[group.ID] {
				continue
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
				// flat 视图只服务"挑一条资源"的场景，没有地方挂问题标签：数据问题的行跳过，
				// 由模型广场（grouped）在模型行上展示原因。
				if _, dataProblem := ResourceIssueOf(err); dataProblem {
					continue
				}
				return nil, err
			}
			if seen[row.ID] || !resourceMatches(row, q) {
				continue
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
