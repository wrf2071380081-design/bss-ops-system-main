package supply

import (
	"errors"
	"math"
	"testing"
	"time"
)

func admissionFixture(t *testing.T) (*ResourceRecord, *AdmissionProof, ResourceAdmissionCommand) {
	r := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	r.Resource.State = "PENDING_ACCESS"
	r.Resource.EverAdmitted = false
	p := &AdmissionProof{FlowID: "81", NodeID: "82", FlowRevision: 3, SupplierID: "21", ResourceID: "11", Region: "CN", OriginRowID: r.Resource.OriginRowID, OriginRowVersion: 1, TargetVersion: 1, AssigneeID: "8", FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}
	return r, p, ResourceAdmissionCommand{ResourceID: "11", ExpectedRevision: 1, ExternalConfirmed: true, CompletionNote: "已在国内平台完成实际配置"}
}
func TestAdmissionRequiresAssignedExecutorNotCurrentBuyer(t *testing.T) {
	r, p, cmd := admissionFixture(t)
	now := modelNow.Add(time.Hour)
	after, err := ProposeResourceAdmission(r, p, "8", cmd, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "ADMITTED" || !after.EverAdmitted || after.Revision != 2 || !after.UpdatedAt.Equal(now) || after.BuyerID != "7" || after.LatestReportID != r.Resource.LatestReportID || after.TargetVersion != 1 || after.OriginRowVersion != 1 || r.Resource.State != "PENDING_ACCESS" || r.Resource.EverAdmitted {
		t.Fatalf("invalid candidate or input mutation %+v %+v", after, r.Resource)
	}
	if _, err := ProposeResourceAdmission(r, p, "7", cmd, now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer bypassed assignee: %v", err)
	}
}
func TestAdmissionRejectsWrongFlowRowVersionAndUnreadyPrerequisites(t *testing.T) {
	for _, change := range []func(*AdmissionProof){func(p *AdmissionProof) { p.ResourceID = "12" }, func(p *AdmissionProof) { p.SupplierID = "22" }, func(p *AdmissionProof) { p.Region = "OVERSEAS" }, func(p *AdmissionProof) { p.OriginRowID = "999" }, func(p *AdmissionProof) { p.OriginRowVersion = 2 }, func(p *AdmissionProof) { p.TargetVersion = 2 }, func(p *AdmissionProof) { p.FlowID = "" }, func(p *AdmissionProof) { p.NodeID = "" }, func(p *AdmissionProof) { p.FlowRevision = 0 }, func(p *AdmissionProof) { p.FlowInProgress = false }, func(p *AdmissionProof) { p.NodeInProgress = false }, func(p *AdmissionProof) { p.PrerequisitesReady = false }} {
		r, p, cmd := admissionFixture(t)
		change(p)
		if _, err := ProposeResourceAdmission(r, p, "8", cmd, modelNow); err == nil {
			t.Fatalf("bad proof admitted %+v", p)
		}
	}
}
func TestAdmissionUsesOwnCurrentTestAndConfirmedFormalConfig(t *testing.T) {
	for _, change := range []func(*ResourceRecord){func(r *ResourceRecord) { r.Report.ConnectivityPassed = false }, func(r *ResourceRecord) { r.Report.PressurePassed = false }, func(r *ResourceRecord) { r.Report.Valid = false }, func(r *ResourceRecord) { r.Report.TargetVersion = 2 }, func(r *ResourceRecord) { r.Report.OriginRowVersion = 2 }, func(r *ResourceRecord) { r.Report.ResourceID = "12" }, func(r *ResourceRecord) { r.Report = nil }, func(r *ResourceRecord) { r.Config = nil }, func(r *ResourceRecord) { r.Config.ConfirmedRevision = 0 }, func(r *ResourceRecord) { r.Config.HasAPIKey = false }, func(r *ResourceRecord) { r.Config.Region = "OVERSEAS" }, func(r *ResourceRecord) { r.Supplier.Status = "FROZEN" }} {
		r, p, cmd := admissionFixture(t)
		change(r)
		if _, err := ProposeResourceAdmission(r, p, "8", cmd, modelNow); err == nil {
			t.Fatal("unready resource admitted")
		}
		if r.Resource.State != "PENDING_ACCESS" {
			t.Fatal("failed admission mutated state")
		}
	}
	// No cache statistics does not add a new admission threshold.
	r, p, cmd := admissionFixture(t)
	r.Report.Cache = nil
	if _, err := ProposeResourceAdmission(r, p, "8", cmd, modelNow); err != nil {
		t.Fatalf("missing cache blocked admission: %v", err)
	}
}
func TestAdmissionKeepsModelVerificationAsSeparateQuoteGate(t *testing.T) {
	r, p, cmd := admissionFixture(t)
	r.Model.VerifyStatus = "UNVERIFIED"
	r.Model.CheckedBy = ""
	after, err := ProposeResourceAdmission(r, p, "8", cmd, modelNow)
	if err != nil {
		t.Fatal(err)
	}
	copy := *r
	copy.Resource = after
	snapshot, err := BuildResourceSnapshot(&copy)
	if err != nil || snapshot.QuoteReadiness.Eligible {
		t.Fatalf("unverified model became quotable %+v %v", snapshot, err)
	}
}
func TestAdmissionRequiresPendingFirstAdmissionRevisionAndActualEvidence(t *testing.T) {
	for _, change := range []func(*ResourceRecord, *ResourceAdmissionCommand){func(r *ResourceRecord, c *ResourceAdmissionCommand) { r.Resource.State = "PENDING_TEST" }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { r.Resource.State = "ADMITTED" }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { r.Resource.EverAdmitted = true }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { c.ExpectedRevision = 2 }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { c.ExternalConfirmed = false }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { c.CompletionNote = " " }, func(r *ResourceRecord, c *ResourceAdmissionCommand) { c.ResourceID = "12" }, func(r *ResourceRecord, c *ResourceAdmissionCommand) {
		r.Resource.Revision = math.MaxInt64
		c.ExpectedRevision = math.MaxInt64
	}} {
		r, p, c := admissionFixture(t)
		change(r, &c)
		if _, err := ProposeResourceAdmission(r, p, "8", c, modelNow); err == nil {
			t.Fatal("invalid admission accepted")
		}
	}
}
