package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
)

func TestF12ReadStoreConstructionAndMissingRunner(t *testing.T) {
	if store, err := NewF12ReadStore("public;DROP", nil); store != nil || err == nil {
		t.Fatal("unsafe schema accepted")
	}
	store, err := NewF12ReadStore("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ListSupplierContracts(context.Background(), supply.F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 20}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing read runner: %v", err)
	}
	if _, _, _, err := store.ListSupplierCredits(context.Background(), supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 20}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing read runner: %v", err)
	}
}

func TestF12ReadStoreValidatesQueryAndUsesStableParameters(t *testing.T) {
	runner := ModelReadRunner(func(context.Context, func(*sql.Tx) error) error { return nil })
	store, err := NewF12ReadStore("public", runner)
	if err != nil {
		t.Fatal(err)
	}
	if rows, total, err := store.ListSupplierContracts(context.Background(), supply.F12ContractQuery{SubjectType: "CUSTOMER", SubjectID: "21", Page: 1, Size: 20}); rows != nil || total != 0 || !errors.Is(err, supply.ErrReadContract) {
		t.Fatalf("unsupported subject accepted: %+v %d %v", rows, total, err)
	}
	if rows, total, _, err := store.ListSupplierCredits(context.Background(), supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "021", Page: 1, Size: 20}); rows != nil || total != 0 || !errors.Is(err, supply.ErrReadContract) {
		t.Fatalf("invalid subject ID accepted: %+v %d %v", rows, total, err)
	}
	cutoff := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	q := supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Currency: "CNY", AsOf: &cutoff}
	where, args := f12CreditWhere(q)
	if where != "c.subject_type='SUPPLIER' AND c.subject_id=$1 AND c.direction=$2 AND c.currency=$3 AND c.created_at <= $4" || !reflect.DeepEqual(args, []any{"21", "SUPPLIER_TO_US", "CNY", cutoff}) {
		t.Fatalf("unsafe credit filter: %s %#v", where, args)
	}
}
