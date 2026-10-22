package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
)

func TestSelectedReportRejectsInvalidReferenceBeforeSQL(t *testing.T) {
	ref := ReportEvidenceRef{ReportID: "51", JobID: "41", ResourceID: "11", SupplierID: "21", StandardModelID: "31", Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, Sequence: 4, Valid: true}
	if _, err := ReadSelectedReport(context.Background(), nil, "public", ref); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal(err)
	}
	for _, change := range []func(*ReportEvidenceRef){func(r *ReportEvidenceRef) { r.ReportID = "1 OR TRUE" }, func(r *ReportEvidenceRef) { r.Sequence = 0 }, func(r *ReportEvidenceRef) { r.TargetVersion = 0 }, func(r *ReportEvidenceRef) { r.OriginRowID = "" }, func(r *ReportEvidenceRef) { r.Region = "ALL" }} {
		wrong := ref
		change(&wrong)
		if _, err := ReadSelectedReport(context.Background(), nil, "public", wrong); !errors.Is(err, supply.ErrReadContract) {
			t.Fatal("invalid reference reached SQL", err)
		}
	}
	if _, err := ReadSelectedReport(context.Background(), nil, "public;SELECT 1", ref); err == nil || errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("unsafe schema accepted")
	}
}
