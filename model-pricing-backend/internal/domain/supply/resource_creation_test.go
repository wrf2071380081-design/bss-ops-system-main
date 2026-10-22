package supply

import (
	"errors"
	"testing"
)

func creationFixture() (*SubmissionResourceProof, ResourceSupplier) {
	p := &SubmissionResourceProof{RowID: "71", RowVersion: 2, TargetVersion: 3, SupplierID: "21", StandardModelID: "31", Region: "CN", SupplierModelID: "provider-model-id", Coefficient: "0.68", BuyerID: "7", FlowID: "81", FlowRevision: 4, AuthorizedActorID: "8", ApprovedAndAssigned: true, EnteredHandling: true}
	return p, ResourceSupplier{ID: "21", Name: "供应商甲", Region: "CN", Status: "ACTIVE"}
}
func creationReport(p *SubmissionResourceProof) *ResourceReportSummary {
	return &ResourceReportSummary{ID: "51", Sequence: 1, SupplierID: p.SupplierID, StandardModelID: p.StandardModelID, Region: p.Region, OriginRowID: p.RowID, OriginRowVersion: p.RowVersion, TargetVersion: p.TargetVersion, Valid: true, ConnectivityPassed: true, PressurePassed: true}
}
func TestResourceCreationDefersAdmissionAndFormalConfig(t *testing.T) {
	p, s := creationFixture()
	r, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "PENDING_TEST" || r.EverAdmitted || r.Revision != 1 || r.OriginRowID != "71" || r.OriginRowVersion != 2 || r.TargetVersion != 3 || r.Coefficient != "0.6800" || r.BuyerID != "7" || r.FormalConfigID != "" || r.LatestReportID != "" || r.SupplierModelID != "provider-model-id" {
		t.Fatalf("wrong resource %+v", r)
	}
	if p.Coefficient != "0.68" {
		t.Fatal("mutated submission")
	}
}
func TestResourceCreationUsesOwnRowTestsForInitialState(t *testing.T) {
	for _, kind := range []string{"pass", "connection failed", "pressure failed", "invalid"} {
		p, s := creationFixture()
		p.Report = creationReport(p)
		switch kind {
		case "connection failed":
			p.Report.ConnectivityPassed = false
		case "pressure failed":
			p.Report.PressurePassed = false
		case "invalid":
			p.Report.Valid = false
		}
		r, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow)
		if err != nil {
			t.Fatal(err)
		}
		want := "PENDING_TEST"
		if kind == "pass" {
			want = "PENDING_ACCESS"
		}
		if r.State != want || r.LatestReportID != "51" || r.LatestReportSequence != 1 || r.EverAdmitted {
			t.Fatalf("%s: %+v", kind, r)
		}
		if p.Report.ResourceID != "" {
			t.Fatal("draft report was rewritten")
		}
	}
}

func TestResourceCreationRefusesReportWithoutStableTaskOrder(t *testing.T) {
	p, s := creationFixture()
	p.Report = creationReport(p)
	p.Report.Sequence = 0
	if _, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow); err == nil {
		t.Fatal("missing report order accepted")
	}
}
func TestResourceCreationRejectsDraftUnassignedAndUnauthorizedSources(t *testing.T) {
	for _, change := range []func(*SubmissionResourceProof){func(p *SubmissionResourceProof) { p.ApprovedAndAssigned = false }, func(p *SubmissionResourceProof) { p.EnteredHandling = false }, func(p *SubmissionResourceProof) { p.FlowRevision = 0 }, func(p *SubmissionResourceProof) { p.FlowID = "" }, func(p *SubmissionResourceProof) { p.RowVersion = 0 }, func(p *SubmissionResourceProof) { p.TargetVersion = 0 }, func(p *SubmissionResourceProof) { p.BuyerID = "" }, func(p *SubmissionResourceProof) { p.SupplierModelID = " " }, func(p *SubmissionResourceProof) { p.Coefficient = "0" }, func(p *SubmissionResourceProof) { p.Coefficient = "0.12345" }} {
		p, s := creationFixture()
		change(p)
		if _, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow); err == nil {
			t.Fatalf("invalid source accepted %+v", p)
		}
	}
	p, s := creationFixture()
	if _, err := NewResourceFromSubmission("11", "7", p, s, "31", modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer bypassed source authorization %v", err)
	}
	s.Region = "OVERSEAS"
	if _, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow); err == nil {
		t.Fatal("supplier region mismatch")
	}
}
func TestResourceCreationRejectsBorrowedOrStaleReports(t *testing.T) {
	for _, change := range []func(*ResourceReportSummary){func(r *ResourceReportSummary) { r.ID = "0" }, func(r *ResourceReportSummary) { r.ResourceID = "12" }, func(r *ResourceReportSummary) { r.SupplierID = "22" }, func(r *ResourceReportSummary) { r.StandardModelID = "32" }, func(r *ResourceReportSummary) { r.Region = "OVERSEAS" }, func(r *ResourceReportSummary) { r.OriginRowID = "72" }, func(r *ResourceReportSummary) { r.OriginRowVersion = 1 }, func(r *ResourceReportSummary) { r.TargetVersion = 2 }} {
		p, s := creationFixture()
		p.Report = creationReport(p)
		change(p.Report)
		if _, err := NewResourceFromSubmission("11", "8", p, s, "31", modelNow); err == nil {
			t.Fatal("foreign/stale report attached")
		}
	}
}
