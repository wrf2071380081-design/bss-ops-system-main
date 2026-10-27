package supplypg

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"model_bss/internal/domain/supply"
)

func TestMapSupplierStorageErrorPreservesDomainSemantics(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "missing row", err: sql.ErrNoRows, want: supply.ErrNotFound},
		{name: "duplicate identity", err: &pgconn.PgError{Code: "23505"}, want: supply.ErrIdentityConflict},
		{name: "serialization conflict", err: &pgconn.PgError{Code: "40001"}, want: supply.ErrRevisionConflict},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: supply.ErrForbidden},
		{name: "missing column", err: &pgconn.PgError{Code: "42703"}, want: supply.ErrDependencyUnavailable},
		{name: "missing table", err: &pgconn.PgError{Code: "42P01"}, want: supply.ErrDependencyUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mapSupplierStorageError(test.err); !errors.Is(got, test.want) {
				t.Fatalf("mapSupplierStorageError(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestMapSupplierStorageErrorLeavesUnknownErrorsIntact(t *testing.T) {
	want := errors.New("temporary database failure")
	if got := mapSupplierStorageError(want); got != want {
		t.Fatalf("unknown error was replaced: got %v, want original %v", got, want)
	}
	if got := mapSupplierStorageError(nil); got != nil {
		t.Fatalf("nil error mapped to %v", got)
	}
}

func TestSupplierAliasTableCapabilityRejectsMissingTransaction(t *testing.T) {
	store := &SupplierStore{schema: "public"}
	if available, err := store.supplierAliasTableAvailable(t.Context(), nil); available || !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing transaction returned available=%t, err=%v", available, err)
	}
}
