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
	if _, _, err := store.ListF12Contracts(context.Background(), supply.F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 20}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing read runner: %v", err)
	}
	if _, _, _, err := store.ListF12Credits(context.Background(), supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 20}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatalf("missing read runner: %v", err)
	}
}

func TestF12CustomerSubjectUsesCustomerProfileIdentity(t *testing.T) {
	store, err := NewF12ReadStore("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	table, err := store.subjectTable("CUSTOMER")
	if err != nil || table != `"public"."customer_profile"` {
		t.Fatalf("CUSTOMER subject id must resolve through the current customer profile table, got %q, %v", table, err)
	}
	if _, err := store.subjectTable("BROKER"); !errors.Is(err, supply.ErrReadContract) {
		t.Fatalf("unknown subject type accepted: %v", err)
	}
}

func TestF12ReadStoreValidatesQueryAndUsesStableParameters(t *testing.T) {
	runner := ModelReadRunner(func(context.Context, func(*sql.Tx) error) error { return nil })
	store, err := NewF12ReadStore("public", runner)
	if err != nil {
		t.Fatal(err)
	}
	if rows, total, err := store.ListF12Contracts(context.Background(), supply.F12ContractQuery{SubjectType: "CUSTOMER", SubjectID: "21", Page: 1, Size: 20}); err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("customer subject query must be accepted: %+v %d %v", rows, total, err)
	}
	if rows, total, _, err := store.ListF12Credits(context.Background(), supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "021", Page: 1, Size: 20}); rows != nil || total != 0 || !errors.Is(err, supply.ErrReadContract) {
		t.Fatalf("invalid subject ID accepted: %+v %d %v", rows, total, err)
	}
	cutoff := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	q := supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Currency: "CNY", AsOf: &cutoff}
	where, args := f12CreditWhere(q)
	if where != "c.subject_type=$1 AND c.subject_id=$2 AND c.direction=$3 AND c.currency=$4 AND c.created_at <= $5" || !reflect.DeepEqual(args, []any{"SUPPLIER", "21", "SUPPLIER_TO_US", "CNY", cutoff}) {
		t.Fatalf("unsafe credit filter: %s %#v", where, args)
	}
	customerQuery := supply.F12ContractQuery{SubjectType: "CUSTOMER", SubjectID: "21"}
	customerWhere, customerArgs := f12ContractWhere(customerQuery)
	if customerWhere != "c.subject_type=$1 AND c.subject_id=$2" || !reflect.DeepEqual(customerArgs, []any{"CUSTOMER", "21"}) {
		t.Fatalf("customer contracts must filter by both type and id: %s %#v", customerWhere, customerArgs)
	}
	customerCredit := supply.F12CreditQuery{SubjectType: "CUSTOMER", SubjectID: "21", Direction: "US_TO_CUSTOMER", Currency: "CNY"}
	customerCreditWhere, customerCreditArgs := f12CreditWhere(customerCredit)
	if customerCreditWhere != "c.subject_type=$1 AND c.subject_id=$2 AND c.direction=$3 AND c.currency=$4" || !reflect.DeepEqual(customerCreditArgs, []any{"CUSTOMER", "21", "US_TO_CUSTOMER", "CNY"}) {
		t.Fatalf("customer credits must filter by type, id, direction, and currency: %s %#v", customerCreditWhere, customerCreditArgs)
	}
}

func TestF12LatestCreditQueryReusesFilterBindings(t *testing.T) {
	cutoff := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	q := supply.F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Currency: "CNY", AsOf: &cutoff}
	where, args := f12CreditWhere(q)
	query, gotArgs := f12LatestCreditQuery(`"public"."credit_line"`, where, args, "")
	wantQuery := `SELECT c.id::text,c.subject_type,c.subject_id::text,c.direction,c.amount::text,c.currency,c.version,c.effective_at,c.created_at
FROM "public"."credit_line" c JOIN (
 SELECT direction,currency,MAX(version) AS version FROM "public"."credit_line" c WHERE c.subject_type=$1 AND c.subject_id=$2 AND c.direction=$3 AND c.currency=$4 AND c.created_at <= $5 GROUP BY direction,currency
) current ON current.direction=c.direction AND current.currency=c.currency AND current.version=c.version
WHERE c.subject_type=$1 AND c.subject_id=$2 AND c.direction=$3 AND c.currency=$4 AND c.created_at <= $5 ORDER BY c.direction,c.currency,c.id`
	if query != wantQuery {
		t.Fatalf("latest credit query changed unexpectedly:\n%s", query)
	}
	if !reflect.DeepEqual(gotArgs, []any{"SUPPLIER", "21", "SUPPLIER_TO_US", "CNY", cutoff}) {
		t.Fatalf("repeated predicate must reuse the four placeholders, got args %#v", gotArgs)
	}
}

func TestF12ContractKeyFilterRetainsSubjectScope(t *testing.T) {
	where, args := f12ContractWhere(supply.F12ContractQuery{SubjectType: "CUSTOMER", SubjectID: "21", ContractKey: "18"})
	if where != "c.subject_type=$1 AND c.subject_id=$2 AND c.contract_key=$3" || !reflect.DeepEqual(args, []any{"CUSTOMER", "21", "18"}) {
		t.Fatalf("archive lookup must retain both subject filters: %s %#v", where, args)
	}
}
