package supply

import (
	"context"
	"errors"
	"testing"
)

type supplierAliasCapabilityBase struct{}

func (supplierAliasCapabilityBase) Get(context.Context, string) (*Supplier, error) {
	return nil, ErrNotFound
}
func (supplierAliasCapabilityBase) List(context.Context, SupplierListQuery) (*SupplierPage, error) {
	return &SupplierPage{}, nil
}

type supplierAliasCapabilityStore struct {
	supplierAliasCapabilityBase
	supported bool
	err       error
}

func (s supplierAliasCapabilityStore) SupplierAliasesSupported(context.Context) (bool, error) {
	return s.supported, s.err
}

func TestSupplierAliasCapabilityFailsClosedForLegacyReadStore(t *testing.T) {
	reader := NewSupplierReader(supplierAliasCapabilityBase{})
	supported, err := reader.AliasesSupported(context.Background())
	if err != nil || supported {
		t.Fatalf("legacy store must report optional aliases unsupported, got %t, %v", supported, err)
	}
}

func TestSupplierAliasCapabilityUsesProviderAndPropagatesErrors(t *testing.T) {
	reader := NewSupplierReader(supplierAliasCapabilityStore{supported: true})
	supported, err := reader.AliasesSupported(context.Background())
	if err != nil || !supported {
		t.Fatalf("provided alias capability was lost, got %t, %v", supported, err)
	}

	want := errors.New("schema probe failed")
	reader = NewSupplierReader(supplierAliasCapabilityStore{err: want})
	if _, err := reader.AliasesSupported(context.Background()); !errors.Is(err, want) {
		t.Fatalf("schema probe error must not be presented as unsupported, got %v", err)
	}
}
