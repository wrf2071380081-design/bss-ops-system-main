package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type supplierMergePreviewStoreFake struct {
	facts              *SupplierMergeFacts
	err                error
	sourceID, targetID string
	calls              int
}

func (f *supplierMergePreviewStoreFake) LoadSupplierMergeFacts(_ context.Context, sourceID, targetID string) (*SupplierMergeFacts, error) {
	f.calls++
	f.sourceID, f.targetID = sourceID, targetID
	return f.facts, f.err
}

func validMergeFacts() *SupplierMergeFacts {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	zero := int64(0)
	return &SupplierMergeFacts{
		Source:                SupplierMergePartySummary{ID: "21", Name: "供应商甲", Region: "CN", BuyerID: "7", SettlementCurrency: "CNY", Revision: 2, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now},
		Target:                SupplierMergePartySummary{ID: "22", Name: "供应商乙", Region: "OVERSEAS", BuyerID: "8", SettlementCurrency: "USD", Revision: 3, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now},
		SourceReferences:      SupplierMergeReferenceCounts{Resources: 1, Submissions: 2, PendingSubmissions: 1, OnboardingFlows: 1, Contracts: 1, CreditLines: 1, FormalConfigs: 1, Qualifications: 1, CustomerRelations: 2, TestCredentialRecords: 1, WebsiteAccountConfigured: true, Aliases: &zero},
		TargetReferences:      SupplierMergeReferenceCounts{Resources: 2, CreditLines: 1, Aliases: &zero},
		DuplicateResourceKeys: 1,
	}
}

func TestSupplierMergePreviewListsRisksWithoutClaimingMergePermission(t *testing.T) {
	fake := &supplierMergePreviewStoreFake{facts: validMergeFacts()}
	reader := NewSupplierMergePreviewReader(fake)
	got, err := reader.Preview(context.Background(), "21", "22")
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || fake.sourceID != "21" || fake.targetID != "22" {
		t.Fatalf("preview did not load both selected suppliers: %+v", fake)
	}
	if got.Source.ID != "21" || got.Target.ID != "22" || got.Source.Revision != 2 || got.Target.Revision != 3 || got.DuplicateResourceKeys != 1 {
		t.Fatalf("preview lost party revisions or duplicate resource conflicts: %+v", got)
	}
	wantCodes := []string{"REGION_MISMATCH", "DIFFERENT_PROCUREMENT_OWNERS", "DUPLICATE_RESOURCE_KEY", "IN_PROGRESS_SUBMISSIONS", "IN_PROGRESS_ONBOARDING", "FORMAL_CONFIGS_PRESENT", "TEST_CREDENTIALS_PRESENT", "WEBSITE_ACCOUNT_PRESENT", "CONTRACT_CREDIT_HISTORY", "QUALIFICATION_HISTORY", "CUSTOMER_RELATIONS"}
	if len(got.ReviewItems) != len(wantCodes) {
		t.Fatalf("unexpected unresolved merge risks: %+v", got.ReviewItems)
	}
	for i, want := range wantCodes {
		if got.ReviewItems[i].Code != want {
			t.Fatalf("risk %d = %q; want %q", i, got.ReviewItems[i].Code, want)
		}
	}
}

func TestSupplierMergePreviewRejectsSameSupplierBeforeReading(t *testing.T) {
	fake := &supplierMergePreviewStoreFake{facts: validMergeFacts()}
	_, err := NewSupplierMergePreviewReader(fake).Preview(context.Background(), "21", "21")
	var field *FieldError
	if !errors.As(err, &field) || fake.calls != 0 {
		t.Fatalf("self merge must be rejected without reading: calls=%d err=%v", fake.calls, err)
	}
}

func TestSupplierMergePreviewRejectsMismatchedOrUnsafeFacts(t *testing.T) {
	facts := validMergeFacts()
	facts.Target.ID = "23"
	facts.SourceReferences.Resources = -1
	_, err := NewSupplierMergePreviewReader(&supplierMergePreviewStoreFake{facts: facts}).Preview(context.Background(), "21", "22")
	if !errors.Is(err, ErrReadContract) {
		t.Fatalf("malformed reference count or mismatched target accepted: %v", err)
	}
}

func TestSupplierMergePreviewPropagatesStoreFailure(t *testing.T) {
	want := errors.New("snapshot unavailable")
	_, err := NewSupplierMergePreviewReader(&supplierMergePreviewStoreFake{err: want}).Preview(context.Background(), "21", "22")
	if !errors.Is(err, want) {
		t.Fatalf("store failure lost: %v", err)
	}
}
