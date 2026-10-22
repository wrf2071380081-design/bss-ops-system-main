package supply

import (
	"math"
	"time"
)

type ResourceEvidenceChange struct {
	Resource *Resource
	Outcome  string
	Changed  bool
}

func ProposeTestEvidence(r *Resource, report *ResourceReportSummary, now time.Time) (*ResourceEvidenceChange, error) {
	if r == nil {
		return nil, ErrNotFound
	}
	if report == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(r.ID) || !validID(r.SupplierID) || !validID(r.StandardModelID) || !validID(r.OriginRowID) || (r.Region != "CN" && r.Region != "OVERSEAS") || !resourceStateKnown(r.State) || r.EverAdmitted != (r.State == "ADMITTED" || r.State == "OFFLINE") || now.IsZero() {
		return nil, ErrReadContract
	}
	for _, version := range []uint64{r.Revision, r.OriginRowVersion, r.TargetVersion, report.Sequence, report.OriginRowVersion, report.TargetVersion} {
		if version == 0 || version > math.MaxInt64 {
			return nil, ErrReadContract
		}
	}
	if (r.LatestReportID == "") != (r.LatestReportSequence == 0) || r.LatestReportSequence > math.MaxInt64 || (r.LatestReportID != "" && !validID(r.LatestReportID)) {
		return nil, ErrReadContract
	}
	if !validID(report.ID) || (report.ResourceID != "" && report.ResourceID != r.ID) || report.SupplierID != r.SupplierID || report.StandardModelID != r.StandardModelID || report.Region != r.Region || report.OriginRowID != r.OriginRowID {
		return nil, ErrReadContract
	}
	if report.TargetVersion > r.TargetVersion || report.OriginRowVersion > r.OriginRowVersion {
		return nil, ErrRevisionConflict
	}
	copy := *r
	result := &ResourceEvidenceChange{Resource: &copy}
	if report.ID == r.LatestReportID && report.Sequence != r.LatestReportSequence {
		return nil, ErrReadContract
	}
	if report.Sequence == r.LatestReportSequence && report.ID != r.LatestReportID {
		return nil, ErrReadContract
	}
	if report.TargetVersion < r.TargetVersion || report.OriginRowVersion < r.OriginRowVersion {
		result.Outcome = "IGNORED_STALE_TARGET"
		return result, nil
	}
	if report.Sequence < r.LatestReportSequence {
		result.Outcome = "IGNORED_OLDER"
		return result, nil
	}
	if report.ID == r.LatestReportID {
		result.Outcome = "NOOP"
		return result, nil
	}
	if r.Revision >= math.MaxInt64 {
		return nil, ErrRevisionConflict
	}
	copy.LatestReportID = report.ID
	copy.LatestReportSequence = report.Sequence
	copy.Revision++
	copy.UpdatedAt = now.UTC()
	if !r.EverAdmitted {
		copy.State = "PENDING_TEST"
		if report.Valid && report.ConnectivityPassed && report.PressurePassed {
			copy.State = "PENDING_ACCESS"
		}
	}
	result.Outcome = "SELECTED"
	result.Changed = true
	return result, nil
}
