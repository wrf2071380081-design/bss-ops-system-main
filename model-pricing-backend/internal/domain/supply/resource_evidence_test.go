package supply

import (
	"testing"
	"time"
)

func evidenceFixture(t *testing.T) (*Resource, *ResourceReportSummary) {
	record, _, _ := admissionFixture(t)
	r := record.Resource
	r.State = "PENDING_TEST"
	r.LatestReportID = ""
	r.LatestReportSequence = 0
	report := record.Report
	report.Sequence = 2
	return r, report
}
func TestEvidenceSelectionPreparesOwnResourceWithoutAdmitting(t *testing.T) {
	r, report := evidenceFixture(t)
	now := modelNow.Add(time.Hour)
	change, err := ProposeTestEvidence(r, report, now)
	if err != nil {
		t.Fatal(err)
	}
	if !change.Changed || change.Outcome != "SELECTED" || change.Resource.State != "PENDING_ACCESS" || change.Resource.EverAdmitted || change.Resource.Revision != 2 || change.Resource.LatestReportID != report.ID || change.Resource.LatestReportSequence != 2 || !change.Resource.UpdatedAt.Equal(now) || r.LatestReportID != "" || r.State != "PENDING_TEST" {
		t.Fatalf("bad proposal %+v", change)
	}
}
func TestEvidenceOlderAndDuplicateCallbacksDoNotOverwriteCurrent(t *testing.T) {
	for _, kind := range []string{"older", "duplicate", "stale target", "stale row"} {
		r, report := evidenceFixture(t)
		r.LatestReportID = "99"
		r.LatestReportSequence = 5
		r.Revision = 7
		want := "IGNORED_OLDER"
		switch kind {
		case "duplicate":
			report.ID = "99"
			report.Sequence = 5
			want = "NOOP"
		case "stale target":
			r.TargetVersion = 2
			report.Sequence = 8
			want = "IGNORED_STALE_TARGET"
		case "stale row":
			r.OriginRowVersion = 2
			report.Sequence = 8
			want = "IGNORED_STALE_TARGET"
		}
		change, err := ProposeTestEvidence(r, report, modelNow.Add(time.Hour))
		if err != nil || change.Changed || change.Outcome != want || *change.Resource != *r {
			t.Fatalf("%s overwrote current %+v %v", kind, change, err)
		}
	}
}
func TestEvidenceRejectsAmbiguousOrderAndForeignOrFutureIdentity(t *testing.T) {
	for _, kind := range []string{"equal other report", "same id new sequence", "missing prior order", "zero sequence", "future target", "future row", "other resource", "other supplier", "other model", "other region", "other row"} {
		r, report := evidenceFixture(t)
		r.LatestReportID = "99"
		r.LatestReportSequence = 5
		switch kind {
		case "equal other report":
			report.Sequence = 5
		case "same id new sequence":
			report.ID = "99"
			report.Sequence = 6
		case "missing prior order":
			r.LatestReportSequence = 0
		case "zero sequence":
			report.Sequence = 0
		case "future target":
			report.TargetVersion = 2
		case "future row":
			report.OriginRowVersion = 2
		case "other resource":
			report.ResourceID = "12"
		case "other supplier":
			report.SupplierID = "22"
		case "other model":
			report.StandardModelID = "32"
		case "other region":
			report.Region = "OVERSEAS"
		case "other row":
			report.OriginRowID = "999"
		}
		if _, err := ProposeTestEvidence(r, report, modelNow); err == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}
func TestEvidenceFailedRetestPreservesActualAdmittedAndOfflineState(t *testing.T) {
	for _, state := range []string{"ADMITTED", "OFFLINE", "PENDING_ACCESS"} {
		r, report := evidenceFixture(t)
		r.State = state
		r.EverAdmitted = state != "PENDING_ACCESS"
		report.PressurePassed = false
		change, err := ProposeTestEvidence(r, report, modelNow)
		if err != nil {
			t.Fatal(err)
		}
		want := state
		if state == "PENDING_ACCESS" {
			want = "PENDING_TEST"
		}
		if change.Resource.State != want || change.Resource.EverAdmitted != r.EverAdmitted || change.Resource.Revision != 2 {
			t.Fatalf("failed retest altered actual state %+v", change)
		}
	}
}
