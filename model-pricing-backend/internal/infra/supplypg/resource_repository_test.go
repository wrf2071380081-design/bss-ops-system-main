package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestCombinedResourceRepositoryPreservesSharedReadFailures(t *testing.T) {
	if repo, err := NewResourceRepository(`public;SELECT 1`, nil, nil); repo != nil || err == nil {
		t.Fatal("invalid schema accepted")
	}
	failure := errors.New("resource read runner failed")
	calls := 0
	facts := func(context.Context, *sql.Tx, ResourceProjection) (*ResourceReadFacts, error) { return nil, nil }
	repo, err := NewResourceRepository("public", func(context.Context, func(*sql.Tx) error) error { calls++; return failure }, facts)
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewResourceReader(repo)
	ctx := context.Background()
	for _, check := range []func() error{
		func() error { _, err := reader.List(ctx, supply.ResourceListQuery{}); return err },
		func() error { _, err := reader.List(ctx, supply.ResourceListQuery{View: "flat"}); return err },
		func() error { _, err := reader.Get(ctx, "1", ""); return err },
		func() error { _, err := reader.Options(ctx, "CN", ""); return err },
	} {
		if err := check(); !errors.Is(err, failure) {
			t.Fatalf("read error hidden %v", err)
		}
	}
	if calls != 4 {
		t.Fatalf("shared runner calls %d", calls)
	}
	empty, err := NewResourceRepository("public", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := supply.NewResourceReader(empty).List(ctx, supply.ResourceListQuery{}); !errors.Is(err, supply.ErrDependencyUnavailable) || got != nil {
		t.Fatal("missing dependencies hidden", err)
	}
}
