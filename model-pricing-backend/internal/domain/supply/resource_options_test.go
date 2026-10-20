package supply

import (
	"context"
	"errors"
	"testing"
)

type resourceOptionsMemory struct {
	resourceReadMemory
	options ResourceFilterOptions
	query   ResourceOptionsQuery
}

func (s *resourceOptionsMemory) ResourceFilterOptions(_ context.Context, q ResourceOptionsQuery) (ResourceFilterOptions, error) {
	s.query = q
	return s.options, nil
}
func TestResourceOptionsAreRemoteBoundedAndUseSafeSupplierIdentity(t *testing.T) {
	store := &resourceOptionsMemory{options: ResourceFilterOptions{Companies: []string{"示例公司"}, Suppliers: []ResourceSupplierOption{{ID: "9007199254740993", Name: "示例供应商", Region: "CN"}}, HasMore: true}}
	result, err := NewResourceReader(store).Options(context.Background(), "CN", "示例")
	if err != nil || result.Suppliers[0].ID != "9007199254740993" || !result.HasMore || store.query.Limit != 50 || store.query.Region != "CN" {
		t.Fatalf("wrong option query: %+v %v", result, err)
	}
	result.Suppliers[0].Name = "caller mutation"
	if store.options.Suppliers[0].Name != "示例供应商" {
		t.Fatal("returned options alias adapter")
	}
	store.options.Suppliers[0].Region = "OVERSEAS"
	if _, err := NewResourceReader(store).Options(context.Background(), "CN", ""); !errors.Is(err, ErrReadContract) {
		t.Fatalf("options leaked excluded region: %v", err)
	}
	if _, err := NewResourceReader(&resourceReadMemory{}).Options(context.Background(), "", ""); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing option adapter guessed values: %v", err)
	}
}
