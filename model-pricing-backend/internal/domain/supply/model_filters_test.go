package supply

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestModelMultiFiltersNormalizeWithoutLosingCompanyCommas(t *testing.T) {
	companies := []string{" Company, LLC ", "OpenAI"}
	q, err := normalizeModelQuery(ModelListQuery{Companies: companies, ModelTypes: []string{"TEXT", "IMAGE"}, SupplierIDs: []string{"9007199254740993", "21"}, Regions: []string{"CN", "OVERSEAS"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q.Companies, []string{"Company, LLC", "OpenAI"}) || len(q.SupplierIDs) != 2 || len(q.Regions) != 2 || !q.NullsLast {
		t.Fatalf("lost filters: %+v", q)
	}
	q.Companies[0] = "changed"
	if companies[0] != " Company, LLC " {
		t.Fatal("query aliases caller")
	}
	legacy, err := normalizeModelQuery(ModelListQuery{Company: " OpenAI ", ModelType: "TEXT"})
	if err != nil || !reflect.DeepEqual(legacy.Companies, []string{"OpenAI"}) || !reflect.DeepEqual(legacy.ModelTypes, []string{"TEXT"}) {
		t.Fatalf("legacy lost: %+v %v", legacy, err)
	}
}
func TestModelMultiFiltersRejectAmbiguityAndInvalidValues(t *testing.T) {
	many := make([]string, 51)
	for _, q := range []ModelListQuery{
		{Company: "A", Companies: []string{"B"}}, {ModelType: "TEXT", ModelTypes: []string{"IMAGE"}},
		{Companies: []string{"A", " A "}}, {Companies: []string{""}}, {Companies: many},
		{ModelTypes: []string{"UNKNOWN"}}, {SupplierIDs: []string{"01"}}, {Regions: []string{"cn"}},
	} {
		if _, err := normalizeModelQuery(q); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}

type modelOptionsFixture struct {
	modelReadFixture
	options      ModelFilterOptions
	optionsQuery ModelOptionsQuery
}

func (s *modelOptionsFixture) ModelFilterOptions(_ context.Context, q ModelOptionsQuery) (ModelFilterOptions, error) {
	s.optionsQuery = q
	return s.options, nil
}
func TestModelOptionsUseDedicatedDirectoryAndReturnCopies(t *testing.T) {
	store := &modelOptionsFixture{options: ModelFilterOptions{Companies: []string{"Company, LLC", "No resources company"}, Suppliers: []ModelSupplierOption{{ID: "9007199254740993", Name: "供应商甲", Region: "CN"}}, HasMore: true}}
	result, err := NewModelReader(store).Options(context.Background(), []string{"CN"}, " 甲 ")
	if err != nil {
		t.Fatal(err)
	}
	if store.optionsQuery.Keyword != "甲" || store.optionsQuery.Limit != 50 || !reflect.DeepEqual(store.optionsQuery.Regions, []string{"CN"}) || !result.HasMore {
		t.Fatalf("bad options %+v %+v", result, store.optionsQuery)
	}
	result.Companies[0] = "changed"
	result.Suppliers[0].Name = "changed"
	if store.options.Companies[0] != "Company, LLC" || store.options.Suppliers[0].Name != "供应商甲" {
		t.Fatal("aliased store")
	}
	if _, err := NewModelReader(&modelReadFixture{}).Options(context.Background(), nil, ""); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing capability: %v", err)
	}
}
func TestModelOptionsRejectInconsistentRowsAndRegion(t *testing.T) {
	for _, options := range []ModelFilterOptions{
		{Companies: []string{"A", "A"}}, {Suppliers: []ModelSupplierOption{{ID: "01", Name: "甲", Region: "CN"}}},
		{Suppliers: []ModelSupplierOption{{ID: "1", Name: "甲", Region: "OVERSEAS"}}},
		{Suppliers: []ModelSupplierOption{{ID: "1", Name: "甲", Region: "CN"}, {ID: "1", Name: "乙", Region: "CN"}}},
	} {
		if _, err := NewModelReader(&modelOptionsFixture{options: options}).Options(context.Background(), []string{"CN"}, ""); !errors.Is(err, ErrReadContract) {
			t.Fatalf("accepted %+v: %v", options, err)
		}
	}
}
