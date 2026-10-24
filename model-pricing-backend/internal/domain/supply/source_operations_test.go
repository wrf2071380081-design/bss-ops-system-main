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
