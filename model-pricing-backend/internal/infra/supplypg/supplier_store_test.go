package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"model_bss/internal/domain/supply"
)

func supplierTaxRateValue(value string) *string { return &value }

func TestSupplierSnapshotComparisonUsesTaxValueNotPointerIdentity(t *testing.T) {
	leftRate, rightRate := "0.06000000", "0.06000000"
	left := supply.SupplierData{Name: "supplier", Region: "CN", TaxRate: &leftRate, SettlementCurrency: "CNY"}
	right := supply.SupplierData{Name: "supplier", Region: "CN", TaxRate: &rightRate, SettlementCurrency: "CNY"}
	if !supplierDataEqual(left, right) {
		t.Fatal("equal supplier snapshots with independently decoded tax pointers must compare equal")
	}
}

func TestSupplierTaxRateStorageUsesPercentAndRejectsLoss(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"0", "0.00"},
		{"0.06", "6.00"},
		{"0.0625", "6.25"},
		{"1", "100.00"},
	} {
		got, err := supplierTaxRateToDB(supplierTaxRateValue(tc.input))
		if err != nil || got != tc.want {
			t.Fatalf("supplierTaxRateToDB(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	if _, err := supplierTaxRateToDB(supplierTaxRateValue("0.00000001")); err == nil {
		t.Fatal("tax rate finer than the native numeric(6,2) percentage column was accepted")
	}
	if got, err := supplierTaxRateToDB(nil); err != nil || got != nil {
		t.Fatalf("missing tax rate must map to SQL NULL: %v, %v", got, err)
	}
}

func TestSupplierTaxRateReadConvertsStoredPercentToRatio(t *testing.T) {
	got, err := supplierTaxRateFromDB("6.25")
	if err != nil || got == nil || *got != "0.06250000" {
		t.Fatalf("supplierTaxRateFromDB returned %v, %v", got, err)
	}
}

func TestSupplierQualificationMetadataAdapterRejectsInvalidIDsBeforeResolvingSQL(t *testing.T) {
	tests := []struct {
		name            string
		meta            supply.WriteMeta
		supplierID      string
		qualificationID string
		revision        uint64
		wantField       string
	}{
		{name: "actor", meta: supply.WriteMeta{ActorID: "0"}, supplierID: "21", qualificationID: "81", revision: 1, wantField: "actor_id"},
		{name: "supplier", meta: supply.WriteMeta{ActorID: "7"}, supplierID: "021", qualificationID: "81", revision: 1, wantField: "supplier_id"},
		{name: "qualification", meta: supply.WriteMeta{ActorID: "7"}, supplierID: "21", qualificationID: "0", revision: 1, wantField: "qualification_id"},
		{name: "zero revision", meta: supply.WriteMeta{ActorID: "7"}, supplierID: "21", qualificationID: "81", revision: 0, wantField: "expected_revision"},
		{name: "revision exceeds storage limit", meta: supply.WriteMeta{ActorID: "7"}, supplierID: "21", qualificationID: "81", revision: uint64(math.MaxInt32), wantField: "expected_revision"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolveCalls := 0
			store := &SupplierStore{
				schema: "public",
				resolve: func(context.Context) (*sql.Tx, error) {
					resolveCalls++
					return nil, nil
				},
				log: func(context.Context, *sql.Tx, ResourceStateLog) error { return nil },
			}
			_, err := store.UpdateQualificationMetadata(context.Background(), test.meta, test.supplierID, test.qualificationID, test.revision, "营业执照", supply.SupplierQualificationMetadata{Type: "证照"}, time.Now())
			var fieldErr *supply.FieldError
			if !errors.As(err, &fieldErr) || fieldErr.Path != test.wantField || resolveCalls != 0 {
				t.Fatalf("adapter error=%v field=%+v resolver calls=%d, want field %q before SQL", err, fieldErr, resolveCalls, test.wantField)
			}
		})
	}
}

func TestSupplierAuditTimestampAllowsNoopWithoutChangingUpdatedAt(t *testing.T) {
	created := time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	updated := created.Add(time.Minute)
	logged := updated.Add(time.Minute)
	if !validSupplierAuditTimestamp("UPDATE_NOOP", created, updated, logged) {
		t.Fatal("no-op audit time should advance while supplier updated_at stays unchanged")
	}
	if validSupplierAuditTimestamp("UPDATE", created, updated, logged) {
		t.Fatal("a data-changing update must keep operation time and updated_at aligned")
	}
}
