package supply

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type modelOperationsFake struct {
	ModelReadStore
	rows   []ModelOperationItem
	detail *ModelOperationDetail
	query  ModelOperationQuery
}

func (s *modelOperationsFake) ListModelOperations(_ context.Context, q ModelOperationQuery) ([]ModelOperationItem, int64, error) {
	s.query = q
	return s.rows, int64(len(s.rows)), nil
}
func (s *modelOperationsFake) GetModelOperation(_ context.Context, modelID, id string) (*ModelOperationDetail, error) {
	if s.detail == nil {
		return nil, ErrNotFound
	}
	return s.detail, nil
}
func TestModelOperationsReadStringIdentityAndScope(t *testing.T) {
	row := ModelOperationItem{ID: "9007199254740993", ModelID: "11", ModelName: "历史名称", Action: "MANUAL_SAVE", OperatorName: "维护人", CreatedAt: modelNow}
	store := &modelOperationsFake{rows: []ModelOperationItem{row}, detail: &ModelOperationDetail{ModelOperationItem: row}}
	reader := NewModelReader(store)
	page, err := reader.ListOperations(context.Background(), ModelOperationQuery{ModelID: "11"})
	if err != nil || page.List[0].ModelName != "历史名称" || store.query.Size != 20 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	raw, _ := json.Marshal(page)
	if !strings.Contains(string(raw), `"id":"9007199254740993"`) {
		t.Fatal(string(raw))
	}
	if _, err := reader.GetOperation(context.Background(), "12", row.ID); !errors.Is(err, ErrReadContract) {
		t.Fatalf("cross-model detail accepted: %v", err)
	}
	for _, q := range []ModelOperationQuery{{ModelID: "0"}, {Size: 101}, {SortBy: "before"}, {Order: "desc;SELECT 1"}} {
		if _, err := reader.ListOperations(context.Background(), q); err == nil {
			t.Fatalf("invalid query accepted: %+v", q)
		}
	}
	store.rows[0].ModelID = "12"
	if _, err := reader.ListOperations(context.Background(), ModelOperationQuery{ModelID: "11"}); !errors.Is(err, ErrReadContract) {
		t.Fatalf("cross-model page accepted: %v", err)
	}
}
