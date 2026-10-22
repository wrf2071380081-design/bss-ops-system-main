package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type f12ReadFixture struct {
	contractQuery F12ContractQuery
	creditQuery   F12CreditQuery
	contracts     []*F12ContractRecord
	contractTotal int64
	credits       []*F12CreditRecord
	creditTotal   int64
	latest        []*F12CreditRecord
}

func (f *f12ReadFixture) ListSupplierContracts(_ context.Context, q F12ContractQuery) ([]*F12ContractRecord, int64, error) {
	f.contractQuery = q
	return f.contracts, f.contractTotal, nil
}
func (f *f12ReadFixture) ListSupplierCredits(_ context.Context, q F12CreditQuery) ([]*F12CreditRecord, int64, []*F12CreditRecord, error) {
	f.creditQuery = q
	return f.credits, f.creditTotal, f.latest, nil
}

var f12ReadAt = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)

func TestF12ReadUsesSafeSupplierProjectionAndCurrentCreditByVersion(t *testing.T) {
	contract := &F12ContractRecord{ID: "8", SubjectType: "SUPPLIER", SubjectID: "21", Status: "PROCESSING", Version: 2, HasAttachment: true, CreatedAt: f12ReadAt, UpdatedAt: f12ReadAt}
	credit := &F12CreditRecord{ID: "9", SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Amount: "80.00", Currency: "CNY", Version: 2, CreatedAt: f12ReadAt}
	reader := NewF12Reader(&f12ReadFixture{contracts: []*F12ContractRecord{contract}, contractTotal: 1, credits: []*F12CreditRecord{credit}, creditTotal: 1, latest: []*F12CreditRecord{credit}})

	contracts, err := reader.ListContracts(context.Background(), F12ContractQuery{SubjectType: "SUPPLIER", SubjectID: "21"})
	if err != nil || contracts.Page != 1 || contracts.Size != 20 || len(contracts.List) != 1 || !contracts.List[0].HasAttachment {
		t.Fatalf("contract projection invalid: %+v %v", contracts, err)
	}
	credits, err := reader.ListCredits(context.Background(), F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21"})
	if err != nil || credits.Page != 1 || credits.Size != 20 || len(credits.List) != 1 || len(credits.Current) != 1 {
		t.Fatalf("credit projection invalid: %+v %v", credits, err)
	}
	if credits.Current[0].Status != "AVAILABLE" || credits.Current[0].Record == nil || credits.Current[0].Record.Amount != "80.00" {
		t.Fatalf("current credit not selected by version: %+v", credits.Current)
	}
}

func TestF12ReadDoesNotGuessAmongTiedLatestCreditVersions(t *testing.T) {
	a := &F12CreditRecord{ID: "10", SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Amount: "80.00", Currency: "CNY", Version: 3, CreatedAt: f12ReadAt}
	b := *a
	b.ID, b.Amount = "11", "90.00"
	reader := NewF12Reader(&f12ReadFixture{latest: []*F12CreditRecord{a, &b}})
	page, err := reader.ListCredits(context.Background(), F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21"})
	if err != nil || len(page.Current) != 1 || page.Current[0].Status != "AMBIGUOUS_VERSION" || page.Current[0].Record != nil {
		t.Fatalf("tied latest versions must not produce a guessed amount: %+v %v", page, err)
	}
}

func TestF12ReadRejectsUnsupportedOrUnsafeQueriesBeforeStore(t *testing.T) {
	reader := NewF12Reader(&f12ReadFixture{})
	for _, q := range []F12ContractQuery{{SubjectType: "CUSTOMER", SubjectID: "21"}, {SubjectType: "SUPPLIER", SubjectID: "021"}, {SubjectType: "SUPPLIER", SubjectID: "21", Status: "DELETED"}, {SubjectType: "SUPPLIER", SubjectID: "21", Page: 1, Size: 101}} {
		if _, err := reader.ListContracts(context.Background(), q); err == nil {
			t.Fatalf("unsafe contract query accepted: %+v", q)
		}
	}
	for _, q := range []F12CreditQuery{{SubjectType: "SUPPLIER", SubjectID: "21", Direction: "US_TO_CUSTOMER"}, {SubjectType: "SUPPLIER", SubjectID: "21", Currency: "usd"}, {SubjectType: "SUPPLIER", SubjectID: "21", Size: 101}} {
		if _, err := reader.ListCredits(context.Background(), q); err == nil {
			t.Fatalf("unsafe credit query accepted: %+v", q)
		}
	}
}

func TestF12ReadMissingDependencyAndInvalidLegacyAmount(t *testing.T) {
	if _, err := NewF12Reader(nil).ListCredits(context.Background(), F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing reader dependency: %v", err)
	}
	zero := &F12CreditRecord{ID: "12", SubjectType: "SUPPLIER", SubjectID: "21", Direction: "SUPPLIER_TO_US", Amount: "0.00", Currency: "CNY", Version: 1, CreatedAt: f12ReadAt}
	page, err := NewF12Reader(&f12ReadFixture{latest: []*F12CreditRecord{zero}}).ListCredits(context.Background(), F12CreditQuery{SubjectType: "SUPPLIER", SubjectID: "21"})
	if err != nil || len(page.Current) != 1 || page.Current[0].Status != "INVALID_LEGACY_AMOUNT" || page.Current[0].Record != nil {
		t.Fatalf("zero legacy amount must not be shown as current credit: %+v %v", page, err)
	}
}
