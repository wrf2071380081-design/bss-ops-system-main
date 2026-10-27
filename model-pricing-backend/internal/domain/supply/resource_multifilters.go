package supply

import (
	"github.com/shopspring/decimal"
	"regexp"
	"slices"
)

var resourceRangeNumber = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

func normalizeResourceFilters(q ResourceListQuery) (ResourceListQuery, error) {
	for _, pair := range []struct {
		single string
		multi  []string
	}{{q.Company, q.Companies}, {q.ModelType, q.ModelTypes}, {q.SupplierID, q.SupplierIDs}, {q.Region, q.Regions}, {q.State, q.States}} {
		if pair.single != "" && len(pair.multi) > 0 {
			return q, invalid("filters", "singular and plural filters cannot be combined")
		}
	}
	specs := []struct {
		name   string
		values *[]string
		valid  func(string) bool
	}{
		{"companies", &q.Companies, func(v string) bool { return resourceSearchText("company", v, 128) == nil }},
		{"model_types", &q.ModelTypes, modelTypeKnown}, {"supplier_ids", &q.SupplierIDs, validID},
		{"regions", &q.Regions, func(v string) bool { return v == "CN" || v == "OVERSEAS" }}, {"states", &q.States, resourceStateKnown},
	}
	for _, spec := range specs {
		values, err := normalizeModelValues(spec.name, *spec.values, spec.valid)
		if err != nil {
			return q, err
		}
		*spec.values = values
	}
	for _, r := range []struct{ key, min, max string }{{"coefficient", q.CoefficientMin, q.CoefficientMax}, {"connection", q.ConnectionMin, q.ConnectionMax}, {"rps", q.RPSMin, q.RPSMax}, {"tps", q.TPSMin, q.TPSMax}} {
		for _, value := range []string{r.min, r.max} {
			if value == "" {
				continue
			}
			if len(value) > 64 || !resourceRangeNumber.MatchString(value) {
				return q, invalid(r.key, "expected a nonnegative decimal range")
			}
			n, err := decimal.NewFromString(value)
			if err != nil || (r.key == "coefficient" && n.GreaterThan(decimal.NewFromInt(1))) {
				return q, invalid(r.key, "invalid numeric range")
			}
		}
		if r.min != "" && r.max != "" {
			min, _ := decimal.NewFromString(r.min)
			max, _ := decimal.NewFromString(r.max)
			if min.GreaterThan(max) {
				return q, invalid(r.key, "range minimum exceeds maximum")
			}
		}
	}
	return q, nil
}
func resourceExtraMatches(row *ResourceSnapshot, q ResourceListQuery) bool {
	for _, f := range []struct {
		values []string
		value  string
	}{{q.Companies, row.Company}, {q.ModelTypes, row.ModelType}, {q.SupplierIDs, row.SupplierID}, {q.Regions, row.Region}, {q.States, row.State}} {
		if len(f.values) > 0 && !slices.Contains(f.values, f.value) {
			return false
		}
	}
	coefficient := row.Coefficient
	for _, r := range []struct {
		min, max string
		value    *string
		metric   bool
	}{{q.CoefficientMin, q.CoefficientMax, &coefficient, false}, {q.ConnectionMin, q.ConnectionMax, row.Test.Metrics.ConnectionMS, true}, {q.RPSMin, q.RPSMax, row.Test.Metrics.RPS, true}, {q.TPSMin, q.TPSMax, row.Test.Metrics.TPS, true}} {
		if r.min == "" && r.max == "" {
			continue
		}
		if r.value == nil || (r.metric && row.Test.Status != "COMPLETED") {
			return false
		}
		n, err := decimal.NewFromString(*r.value)
		if err != nil {
			return false
		}
		if r.min != "" {
			min, _ := decimal.NewFromString(r.min)
			if n.LessThan(min) {
				return false
			}
		}
		if r.max != "" {
			max, _ := decimal.NewFromString(r.max)
			if n.GreaterThan(max) {
				return false
			}
		}
	}
	return true
}
func NormalizeResourceListQuery(q ResourceListQuery) (ResourceListQuery, error) {
	return normalizeResourceQuery(q)
}
func ResourceMatchesQuery(row *ResourceSnapshot, q ResourceListQuery) bool {
	return resourceMatches(row, q)
}
