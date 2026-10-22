package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestModelUnitOfWorkRequiresSafeSchemaAndCallerTransaction(t *testing.T) {
	for _, schema := range []string{"", "public; drop table x", "public.standard_model", `public"`, "UPPER"} {
		if w, err := NewModelUnitOfWork(schema, nil, nil); err == nil || w != nil {
			t.Fatalf("unsafe schema accepted %q", schema)
		}
	}
	w, err := NewModelUnitOfWork("public", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Within(context.Background(), func(supply.ModelTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal(err)
	}
	calls := 0
	w, err = NewModelUnitOfWork("public", func(context.Context) (*sql.Tx, error) { calls++; return nil, nil }, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Within(ctx, func(supply.ModelTransaction) error { return nil }); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("canceled request reached resolver")
	}
	if err := w.Within(context.Background(), func(supply.ModelTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) || calls != 1 {
		t.Fatal("nil transaction accepted")
	}
}
