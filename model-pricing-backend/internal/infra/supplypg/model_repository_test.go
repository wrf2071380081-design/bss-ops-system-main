package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestCombinedModelRepositoryKeepsOptionsAndReadFailuresVisible(t *testing.T) {
	if repo, err := NewModelRepository(`public;SELECT 1`, nil); err == nil || repo != nil {
		t.Fatal("unsafe schema accepted")
	}
	failure := errors.New("test read runner unavailable")
	calls := 0
	repo, err := NewModelRepository("public", func(context.Context, func(*sql.Tx) error) error { calls++; return failure })
	if err != nil {
		t.Fatal(err)
	}
	reader := supply.NewModelReader(repo)
	ctx := context.Background()
	checks := []func() error{
		func() error { _, err := reader.List(ctx, supply.ModelListQuery{}); return err },
		func() error { _, err := reader.Get(ctx, "1"); return err },
		func() error { _, _, err := repo.ListModelVersions(ctx, "1", 1, 20); return err },
		func() error { _, err := repo.GetModelVersion(ctx, "1", 1); return err },
		func() error { _, err := reader.Options(ctx, nil, ""); return err },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, failure) {
			t.Fatalf("read failure hidden %v", err)
		}
	}
	if calls != 5 {
		t.Fatalf("inconsistent runner composition %d", calls)
	}
	empty, err := NewModelRepository("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supply.NewModelReader(empty).Options(ctx, nil, ""); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("options silently fell back", err)
	}
}
