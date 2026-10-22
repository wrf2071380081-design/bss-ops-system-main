package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func TestModelReadGuardsBeforeTransaction(t *testing.T) {
	if s, err := NewModelReadStore(`public;SELECT 1`, nil); err == nil || s != nil {
		t.Fatal("unsafe schema")
	}
	calls := 0
	s, err := NewModelReadStore("public", func(context.Context, func(*sql.Tx) error) error { calls++; return supply.ErrDependencyUnavailable })
	if err != nil {
		t.Fatal(err)
	}
	base := supply.ModelListQuery{Page: 1, Size: 20, SortBy: "id", Order: "asc", NullsLast: true, TieBreakBy: "id"}
	for _, change := range []func(*supply.ModelListQuery){
		func(q *supply.ModelListQuery) { q.SortBy = "id;DROP TABLE x" },
		func(q *supply.ModelListQuery) { q.Order = "asc;SELECT 1" },
		func(q *supply.ModelListQuery) { q.Page = 0 },
		func(q *supply.ModelListQuery) { q.SupplierIDs = []string{"1 OR TRUE"}; q.Regions = []string{"CN"} },
		func(q *supply.ModelListQuery) { q.Keyword = "\x00" },
		func(q *supply.ModelListQuery) { q.Regions = []string{"CN", "CN"} },
	} {
		q := base
		change(&q)
		if _, _, err := s.ListModels(context.Background(), q); err == nil || errors.Is(err, supply.ErrDependencyUnavailable) {
			t.Fatalf("invalid query reached transaction %+v %v", q, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid SQL parameters reached runner")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetModel(ctx, "1"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("canceled read reached runner")
	}
}

func TestModelSnapshotStrictEnvelopeAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	model, err := supply.NewStandardModel("9007199254740993", "7", supply.ModelData{Name: "草稿"}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"contract": "supply.standard_model", "schema_version": 1, "model": model})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeModelSnapshot(raw, model.ID, 1); err != nil || got.ID != model.ID {
		t.Fatalf("bigint string lost %+v %v", got, err)
	}
	for _, bad := range []string{`{}`, `null`, `[]`, string(raw) + ` {}`, strings.Replace(string(raw), `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(string(raw), `"revision":"1"`, `"revision":1`, 1), strings.Replace(string(raw), `"maintainer_id":"7"`, `"maintainer_id":""`, 1)} {
		if _, err := decodeModelSnapshot([]byte(bad), model.ID, 1); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("invalid snapshot accepted %s %v", bad, err)
		}
	}
	if _, err := decodeModelSnapshot(raw, "2", 1); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("model ID mismatch")
	}
	if _, err := decodeModelSnapshot(raw, model.ID, 2); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("version mismatch")
	}
}
