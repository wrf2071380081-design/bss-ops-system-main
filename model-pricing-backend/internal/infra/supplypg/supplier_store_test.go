package supplypg

import (
	"testing"
	"time"
)

func TestSupplierTaxRateStorageUsesPercentAndRejectsLoss(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"0", "0.00"},
		{"0.06", "6.00"},
		{"0.0625", "6.25"},
		{"1", "100.00"},
	} {
		got, err := supplierTaxRateToDB(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("supplierTaxRateToDB(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	if _, err := supplierTaxRateToDB("0.00000001"); err == nil {
		t.Fatal("tax rate finer than the native numeric(6,2) percentage column was accepted")
	}
}

func TestSupplierTaxRateReadConvertsStoredPercentToRatio(t *testing.T) {
	got, err := supplierTaxRateFromDB("6.25")
	if err != nil || got != "0.06250000" {
		t.Fatalf("supplierTaxRateFromDB returned %q, %v", got, err)
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
