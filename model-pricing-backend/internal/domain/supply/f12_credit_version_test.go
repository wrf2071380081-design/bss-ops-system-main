package supply

import (
	"errors"
	"testing"
	"time"
)

func f12CreditVersionFixture(id, amount string, version uint64) *F12CreditVersion {
	return &F12CreditVersion{
		ID: "" + id, SubjectType: "SUPPLIER", SubjectID: "10", Direction: "SUPPLIER_TO_US",
		Amount: amount, Currency: "CNY", DocumentName: "供应商授信", DocumentNo: "S-10",
		FileID: "file-s-10", Reason: "旧版", CreatedBy: "7", Version: version,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
}

func TestAppendF12CreditVersionUsesLatestAmountAndKeepsHistory(t *testing.T) {
	first := f12CreditVersionFixture("101", "100.00", 1)
	command := F12CreditWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", Direction: "SUPPLIER_TO_US", Amount: "80",
		Currency: "CNY", DocumentName: "供应商授信", DocumentNo: "S-10-v2", FileID: "file-s-10-v2", Reason: "调整额度",
	}
	createdAt := time.Date(2026, 10, 24, 8, 30, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))

	got, err := AppendF12CreditVersion([]*F12CreditVersion{first}, 1, "102", "7", command, createdAt)
	if err != nil {
		t.Fatalf("AppendF12CreditVersion() error = %v", err)
	}
	if got.Version != 2 || got.Amount != "80.00" || got.ID != "102" || got.CreatedBy != "7" {
		t.Fatalf("new version = %#v, want version 2 with current amount 80.00", got)
	}
	if first.Version != 1 || first.Amount != "100.00" || first.DocumentNo != "S-10" {
		t.Fatalf("appending a credit version mutated history: %#v", first)
	}
	if !got.CreatedAt.Equal(time.Date(2026, 10, 24, 0, 30, 0, 123456000, time.UTC)) {
		t.Fatalf("created_at = %s, want UTC microsecond timestamp", got.CreatedAt)
	}
}

func TestAppendF12CreditVersionRejectsStaleAndAmbiguousLatest(t *testing.T) {
	command := F12CreditWriteCommand{
		SubjectType: "CUSTOMER", SubjectID: "10", Direction: "US_TO_CUSTOMER", Amount: "80",
		Currency: "CNY", DocumentName: "客户授信", DocumentNo: "C-10-v2", FileID: "file-c-10-v2", Reason: "调整额度",
	}
	first := f12CreditVersionFixture("201", "100.00", 1)
	first.SubjectType, first.Direction = "CUSTOMER", "US_TO_CUSTOMER"
	first.DocumentName, first.DocumentNo, first.FileID = "客户授信", "C-10", "file-c-10"
	createdAt := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

	if _, err := AppendF12CreditVersion([]*F12CreditVersion{first}, 0, "202", "7", command, createdAt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale expected version error = %v, want ErrRevisionConflict", err)
	}
	duplicate := f12CreditVersionFixture("203", "90.00", 1)
	duplicate.SubjectType, duplicate.Direction = "CUSTOMER", "US_TO_CUSTOMER"
	duplicate.DocumentName, duplicate.DocumentNo, duplicate.FileID = "客户授信", "C-10", "file-c-10"
	if _, err := AppendF12CreditVersion([]*F12CreditVersion{first, duplicate}, 1, "204", "7", command, createdAt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("ambiguous latest version error = %v, want ErrRevisionConflict", err)
	}
}
