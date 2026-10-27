package supply

import (
	"context"
	"testing"
	"time"
)

type sourceOperationFake struct {
	called bool
	query  SourceOperationQuery
	rows   []SourceOperation
}

func (s *sourceOperationFake) ListSourceOperations(_ context.Context, q SourceOperationQuery) ([]SourceOperation, int64, error) {
	s.called = true
	s.query = q
	return s.rows, int64(len(s.rows)), nil
}

func TestSourceOperationsCanBeScopedToSupplier(t *testing.T) {
	store := &sourceOperationFake{rows: []SourceOperation{{ID: "9", EntityType: "plan", EntityID: "8", ResourceID: "2", SupplierID: "3", Region: "CN", Action: "CREATE", CreatedAt: time.Now()}}}
	reader := NewSourceOperationReader(store)
	page, err := reader.List(context.Background(), SourceOperationQuery{SupplierID: "3", Page: 1, Size: 20})
	if err != nil || page.Total != 1 || len(page.List) != 1 || store.query.SupplierID != "3" {
		t.Fatalf("supplier operation query failed: page=%+v query=%+v err=%v", page, store.query, err)
	}
	store.called = false
	if _, err := reader.List(context.Background(), SourceOperationQuery{SupplierID: "3 OR 1=1", Page: 1, Size: 20}); err == nil || store.called {
		t.Fatal("invalid supplier scope reached persistence")
	}
}

func TestSourceOperationsValidateScopeAndProjection(t *testing.T) {
	store := &sourceOperationFake{rows: []SourceOperation{{ID: "9", EntityType: "plan", EntityID: "8", ResourceID: "2", SupplierID: "3", Region: "CN", Action: "CREATE", CreatedAt: time.Now()}}}
	reader := NewSourceOperationReader(store)
	if _, err := reader.List(context.Background(), SourceOperationQuery{ModelID: "1", ResourceID: "2", Page: 1, Size: 20}); err != nil {
		t.Fatal(err)
	}
	store.called = false
	if _, err := reader.List(context.Background(), SourceOperationQuery{ModelID: "1 OR 1=1", Page: 1, Size: 20}); err == nil || store.called {
		t.Fatal("invalid model reached persistence")
	}
	if _, err := reader.List(context.Background(), SourceOperationQuery{ModelID: "1", ResourceID: "4", Page: 1, Size: 20}); err != ErrReadContract {
		t.Fatalf("wrong resource accepted: %v", err)
	}
}

func TestSupplierOperationsAcceptSubjectRecordsWithoutResource(t *testing.T) {
	for _, entityType := range []string{"supplier", "supplier_qualification", "supplier_formal_config", "contract", "credit_line"} {
		entityID := "8"
		if entityType == "supplier" { entityID = "3" }
		store := &sourceOperationFake{rows: []SourceOperation{{ID: "9", EntityType: entityType, EntityID: entityID, SupplierID: "3", Region: "CN", Action: "UPDATE", CreatedAt: time.Now()}}}
		reader := NewSourceOperationReader(store)
		if _, err := reader.List(context.Background(), SourceOperationQuery{SupplierID: "3", EntityType: entityType}); err != nil {
			t.Fatalf("subject-level %s must not require a resource: %v", entityType, err)
		}
		if _, err := reader.List(context.Background(), SourceOperationQuery{ModelID: "1"}); err != ErrReadContract {
			t.Fatalf("model scope accepted supplier-level history: %v", err)
		}
		store.rows[0].SupplierID = "4"
		if _, err := reader.List(context.Background(), SourceOperationQuery{SupplierID: "3"}); err != ErrReadContract {
			t.Fatalf("cross-supplier history accepted: %v", err)
		}
	}
	store := &sourceOperationFake{}
	if _, err := NewSourceOperationReader(store).List(context.Background(), SourceOperationQuery{SupplierID: "3", EntityType: "account"}); err == nil || store.called {
		t.Fatal("unknown history category reached persistence")
	}
}
