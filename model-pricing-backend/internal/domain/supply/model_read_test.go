package supply

import (
	"context"
	"errors"
	"testing"
)

type modelReadFixture struct {
	current *StandardModel
	old     *StandardModel
	query   ModelListQuery
}

func (f *modelReadFixture) ListModels(_ context.Context, q ModelListQuery) ([]*StandardModel, int64, error) {
	f.query = q
	return []*StandardModel{f.current}, 1, nil
}
func (f *modelReadFixture) GetModel(_ context.Context, id string) (*StandardModel, error) {
	if f.current == nil || f.current.ID != id {
		return nil, ErrNotFound
	}
	return f.current, nil
}
func (f *modelReadFixture) ListModelVersions(_ context.Context, id string, page, size int) ([]*StandardModel, int64, error) {
	if _, err := f.GetModel(context.Background(), id); err != nil {
		return nil, 0, err
	}
	return []*StandardModel{f.old}, 1, nil
}
func (f *modelReadFixture) GetModelVersion(_ context.Context, id string, version uint64) (*StandardModel, error) {
	if f.old != nil && f.old.ID == id && f.old.VersionNo == version {
		return f.old, nil
	}
	return nil, ErrNotFound
}

func TestModelReaderNormalizesSafeGlobalQuery(t *testing.T) {
	m, _ := NewStandardModel("9007199254740993", "7", completeModelData(), modelNow)
	store := &modelReadFixture{current: m}
	reader := NewModelReader(store)
	page, err := reader.List(context.Background(), ModelListQuery{Keyword: " Example "})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.Size != 20 || page.Total != 1 || page.List[0].ID != "9007199254740993" {
		t.Fatalf("invalid list contract: %+v", page)
	}
	if store.query.Keyword != "Example" || store.query.SortBy != "updated_at" || store.query.Order != "desc" || !store.query.NullsLast || store.query.TieBreakBy != "id" {
		t.Fatalf("unsafe or unstable query: %+v", store.query)
	}
	page.List[0].Data.PriceItems[0].Amount = "99"
	if store.current.Data.PriceItems[0].Amount != "2.00000000" {
		t.Fatal("reader output aliases repository state")
	}
}

func TestModelReaderRejectsInvalidQueriesBeforeStore(t *testing.T) {
	validModel, _ := NewStandardModel("1", "7", completeModelData(), modelNow)
	reader := NewModelReader(&modelReadFixture{current: validModel})
	for _, query := range []ModelListQuery{{Size: 101}, {Page: -1}, {SortBy: "name; DROP TABLE standard_model"}, {Order: "random"}, {ModelType: "UNKNOWN"}, {Status: "DRAFT"}, {VerifyStatus: "APPROVED"}} {
		if _, err := reader.List(context.Background(), query); err == nil {
			t.Fatalf("invalid query accepted: %+v", query)
		}
	}
}

func TestModelReaderHistoricalSnapshotPreservesPriceAndBlocksNewQuotes(t *testing.T) {
	old, _ := NewStandardModel("1", "7", completeModelData(), modelNow)
	old, _, _ = old.verified("7", 1, "2026-10-20", modelNow)
	changed := cloneData(old.Data)
	changed.PriceItems[0].Amount = "3.00000000"
	current, _, _ := old.Updated("7", 2, changed, modelNow)
	reader := NewModelReader(&modelReadFixture{current: current, old: old})
	history, err := reader.Snapshot(context.Background(), "1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if history.IsCurrent || history.CurrentVersionNo != 3 || history.Model.Data.PriceItems[0].Amount != "2.00000000" || history.QuoteReadiness.Ready {
		t.Fatalf("history repriced or eligible for new quotes: %+v", history)
	}
	snapshot, err := reader.Snapshot(context.Background(), "1", 0)
	if err != nil || !snapshot.IsCurrent || snapshot.Model.Data.PriceItems[0].Amount != "3.00000000" {
		t.Fatalf("current snapshot incorrect: %v %+v", err, snapshot)
	}
	versions, err := reader.Versions(context.Background(), "1", 1, 20)
	if err != nil || len(versions.List) != 1 || versions.List[0].VersionNo != 2 {
		t.Fatalf("version read failed: %v", err)
	}
}

func TestModelReaderMissingDependencyInvalidIDAndUnknownVersion(t *testing.T) {
	ctx := context.Background()
	if _, err := NewModelReader(nil).Get(ctx, "1"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing store accepted: %v", err)
	}
	m, _ := NewStandardModel("1", "7", completeModelData(), modelNow)
	reader := NewModelReader(&modelReadFixture{current: m})
	if _, err := reader.Get(ctx, "0"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err := reader.Get(ctx, "2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := reader.Snapshot(ctx, "1", 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("future snapshot returned: %v", err)
	}
}
