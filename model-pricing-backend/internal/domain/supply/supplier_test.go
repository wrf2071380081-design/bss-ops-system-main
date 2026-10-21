package supply

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func supplierProfileData() SupplierData {
	return SupplierData{Name: "示例供应商", Region: "CN", TaxRate: "0.06", SettlementCurrency: "CNY"}
}

func TestSupplierTaxRateUsesExactRatioAndExplicitCurrency(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"0", "0.00000000"}, {"0.06", "0.06000000"}, {"1", "1.00000000"}, {"0.00000001", "0.00000001"}, {"1.00000000", "1.00000000"}} {
		t.Run(tc.input, func(t *testing.T) {
			data := supplierProfileData()
			data.TaxRate = tc.input
			profile, err := NewSupplier("11", "7", data, modelNow)
			if err != nil || profile.Data.TaxRate != tc.want || profile.Data.SettlementCurrency != "CNY" || profile.Established || profile.Revision != 1 || profile.BuyerID != "7" {
				t.Fatalf("lost explicit profile facts: %+v %v", profile, err)
			}
		})
	}
}

func TestSupplierRejectsInvalidSuppliedDataWithoutRounding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SupplierData)
	}{
		{"missing tax", func(d *SupplierData) { d.TaxRate = "" }},
		{"percent instead of ratio", func(d *SupplierData) { d.TaxRate = "6" }},
		{"negative", func(d *SupplierData) { d.TaxRate = "-0.06" }},
		{"exponent", func(d *SupplierData) { d.TaxRate = "6e-2" }},
		{"over precision", func(d *SupplierData) { d.TaxRate = "0.060000001" }},
		{"over one", func(d *SupplierData) { d.TaxRate = "1.00000001" }},
		{"empty name", func(d *SupplierData) { d.Name = " " }},
		{"invalid region", func(d *SupplierData) { d.Region = "DOMESTIC" }},
		{"missing currency", func(d *SupplierData) { d.SettlementCurrency = "" }},
		{"currency case", func(d *SupplierData) { d.SettlementCurrency = "cny" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := supplierProfileData()
			tc.change(&data)
			profile, err := NewSupplier("11", "7", data, modelNow)
			if err == nil || profile != nil {
				t.Fatalf("invalid data accepted: %+v %v", profile, err)
			}
		})
	}
	data := supplierProfileData()
	data.Region = "OVERSEAS"
	data.SettlementCurrency = "USD"
	data.Phone = "+44 20 1234 5678"
	if _, err := NewSupplier("11", "7", data, modelNow); err != nil {
		t.Fatalf("international profile incorrectly restricted: %v", err)
	}
}

type supplierMemory struct {
	profiles   map[string]*Supplier
	usage      SupplierUsage
	usageErr   error
	currencies map[string]bool
	staff      map[string]bool
	operations []SupplierOperation
	failLog    bool
	next       int
}

func newSupplierMemory() *supplierMemory {
	return &supplierMemory{profiles: map[string]*Supplier{}, currencies: map[string]bool{"CNY": true, "USD": true}, staff: map[string]bool{"7": true, "8": true}}
}
func (db *supplierMemory) Within(ctx context.Context, run func(SupplierTransaction) error) error {
	work := *db
	work.profiles = map[string]*Supplier{}
	for id, profile := range db.profiles {
		work.profiles[id] = profile.Clone()
	}
	work.operations = append([]SupplierOperation(nil), db.operations...)
	if err := run(&work); err != nil {
		return err
	}
	*db = work
	return nil
}
func (db *supplierMemory) NextID(context.Context) (string, error) {
	db.next++
	return strconv.Itoa(100 + db.next), nil
}
func (db *supplierMemory) SupplierForUpdate(_ context.Context, id string) (*Supplier, error) {
	if db.profiles[id] == nil {
		return nil, ErrNotFound
	}
	return db.profiles[id].Clone(), nil
}
func (db *supplierMemory) CreateSupplier(_ context.Context, profile *Supplier) error {
	if db.profiles[profile.ID] != nil {
		return ErrIdentityConflict
	}
	db.profiles[profile.ID] = profile.Clone()
	return nil
}
func (db *supplierMemory) UpdateSupplier(_ context.Context, profile *Supplier, expected uint64) error {
	old := db.profiles[profile.ID]
	if old == nil {
		return ErrNotFound
	}
	if old.Revision != expected {
		return ErrRevisionConflict
	}
	db.profiles[profile.ID] = profile.Clone()
	return nil
}
func (db *supplierMemory) SupplierUsage(context.Context, string) (SupplierUsage, error) {
	return db.usage, db.usageErr
}
func (db *supplierMemory) CurrencyEnabled(_ context.Context, code string) (bool, error) {
	return db.currencies[code], nil
}
func (db *supplierMemory) StaffActive(_ context.Context, id string) (bool, error) {
	return db.staff[id], nil
}
func (db *supplierMemory) AppendSupplierOperation(_ context.Context, op SupplierOperation) error {
	if db.failLog {
		return errOperationLog
	}
	db.operations = append(db.operations, op)
	return nil
}
func supplierService(db *supplierMemory) *SupplierService {
	return NewSupplierService(db, func() time.Time { return modelNow.Add(time.Hour) })
}
func existingSupplier(t *testing.T, db *supplierMemory) *Supplier {
	t.Helper()
	profile, err := NewSupplier("11", "7", supplierProfileData(), modelNow)
	if err != nil {
		t.Fatal(err)
	}
	profile.Established = true
	db.profiles[profile.ID] = profile.Clone()
	return profile
}

func TestSupplierUpdatePreservesBindingAndDerivedFacts(t *testing.T) {
	db := newSupplierMemory()
	old := existingSupplier(t, db)
	svc := supplierService(db)
	data := old.Data
	data.Name = "新主体名称"
	data.Contact = "新联系人"
	data.SettlementCurrency = "USD"
	updated, err := svc.Update(context.Background(), writeMeta("7"), "11", 1, data)
	if err != nil || updated.Revision != 2 || updated.BuyerID != "7" || !updated.Established || updated.Data.TaxRate != "0.06000000" || updated.Data.SettlementCurrency != "USD" || !updated.UpdatedAt.Equal(modelNow.Add(time.Hour)) {
		t.Fatalf("profile update changed binding or derived facts: %+v %v", updated, err)
	}
	if len(db.operations) != 1 || db.operations[0].Action != "UPDATE" || db.operations[0].Before.Data.Name != "示例供应商" || db.operations[0].After.Data.Name != "新主体名称" || db.operations[0].RequestID != "request-test" || db.operations[0].ActorID != "7" {
		t.Fatal("profile lost trace")
	}
	updated.Data.Name = "caller mutation"
	if db.profiles["11"].Data.Name != "新主体名称" || db.operations[0].After.Data.Name != "新主体名称" {
		t.Fatal("profile aliases returned result")
	}
}

func TestSupplierRegionLocksOnAnyHistoricalUse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage SupplierUsage
	}{{"submission", SupplierUsage{HasSubmissions: true}}, {"resource", SupplierUsage{HasResources: true}}, {"contract", SupplierUsage{HasContracts: true}}} {
		t.Run(tc.name, func(t *testing.T) {
			db := newSupplierMemory()
			old := existingSupplier(t, db)
			db.usage = tc.usage
			data := old.Data
			data.Region = "OVERSEAS"
			result, err := supplierService(db).Update(context.Background(), writeMeta("7"), "11", 1, data)
			if result != nil || !errors.Is(err, ErrSupplierRegionLocked) || db.profiles["11"].Data.Region != "CN" || len(db.operations) != 0 {
				t.Fatalf("used supplier region changed: %+v %v", result, err)
			}
		})
	}
	db := newSupplierMemory()
	old := existingSupplier(t, db)
	data := old.Data
	data.Region = "OVERSEAS"
	updated, err := supplierService(db).Update(context.Background(), writeMeta("7"), "11", 1, data)
	if err != nil || updated.Data.Region != "OVERSEAS" || updated.Revision != 2 {
		t.Fatalf("unused supplier region change rejected: %+v %v", updated, err)
	}
}

func TestSupplierRejectsUnboundStaleAndUnknownCurrency(t *testing.T) {
	for _, tc := range []struct {
		name, actor, currency string
		revision              uint64
		want                  error
	}{{"unbound", "8", "CNY", 1, ErrForbidden}, {"stale", "7", "CNY", 2, ErrRevisionConflict}, {"zero", "7", "CNY", 0, ErrRevisionConflict}, {"unknown currency", "7", "ZZZ", 1, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			db := newSupplierMemory()
			old := existingSupplier(t, db)
			data := old.Data
			data.SettlementCurrency = tc.currency
			result, err := supplierService(db).Update(context.Background(), writeMeta(tc.actor), "11", tc.revision, data)
			if err == nil || result != nil || (tc.want != nil && !errors.Is(err, tc.want)) || len(db.operations) != 0 || db.profiles["11"].Revision != 1 {
				t.Fatalf("invalid write accepted: %+v %v", result, err)
			}
		})
	}
}

func TestSupplierNoOpPreservesRevisionAndWritesAudit(t *testing.T) {
	db := newSupplierMemory()
	old := existingSupplier(t, db)
	data := old.Data
	data.TaxRate = "0.06"
	same, err := supplierService(db).Update(context.Background(), writeMeta("7"), "11", 1, data)
	if err != nil || same.Revision != 1 || !same.UpdatedAt.Equal(old.UpdatedAt) || len(db.operations) != 1 || db.operations[0].Action != "UPDATE_NOOP" || !db.operations[0].OccurredAt.Equal(modelNow.Add(time.Hour)) {
		t.Fatalf("no-op changed version or lost intent: %+v %v", same, err)
	}
}

func TestSupplierWriteFailuresLeaveNoPartialState(t *testing.T) {
	for _, mode := range []string{"log", "noop log", "usage unavailable", "currency unavailable", "inactive staff"} {
		t.Run(mode, func(t *testing.T) {
			db := newSupplierMemory()
			old := existingSupplier(t, db)
			data := old.Data
			data.Name = "changed"
			want := errOperationLog
			switch mode {
			case "log":
				db.failLog = true
			case "noop log":
				db.failLog = true
				data = old.Data
			case "usage unavailable":
				db.usageErr = ErrDependencyUnavailable
				data.Region = "OVERSEAS"
				want = ErrDependencyUnavailable
			case "currency unavailable":
				db.currencies = nil
				want = nil
			case "inactive staff":
				db.staff["7"] = false
				want = ErrForbidden
			}
			result, err := supplierService(db).Update(context.Background(), writeMeta("7"), "11", 1, data)
			if result != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("failure returned success: %+v %v", result, err)
			}
			if db.profiles["11"].Data.Name != "示例供应商" || db.profiles["11"].Revision != 1 || len(db.operations) != 0 {
				t.Fatal("failed update partly committed")
			}
		})
	}
}
