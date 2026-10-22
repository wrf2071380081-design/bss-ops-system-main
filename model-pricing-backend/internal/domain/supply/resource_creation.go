package supply

import (
	"math"
	"strings"
	"time"
)

// SubmissionResourceProof must be read from B inside the shared transaction.
// None of these flags or ownership fields are client authorization claims.
type SubmissionResourceProof struct {
	RowID, SupplierID, StandardModelID, Region, SupplierModelID, Coefficient, BuyerID, FlowID, AuthorizedActorID string
	RowVersion, TargetVersion, FlowRevision                                                                      uint64
	ApprovedAndAssigned, EnteredHandling                                                                         bool
	Report                                                                                                       *ResourceReportSummary
}
type ResourceCreationCommand struct {
	RowID      string
	RowVersion uint64
}

func NewResourceFromSubmission(id, actor string, proof *SubmissionResourceProof, supplier ResourceSupplier, modelID string, now time.Time) (*Resource, error) {
	if proof == nil {
		return nil, ErrDependencyUnavailable
	}
	p := proof
	for _, value := range []string{id, actor, p.RowID, p.SupplierID, p.StandardModelID, p.BuyerID, p.FlowID, p.AuthorizedActorID} {
		if !validID(value) {
			return nil, ErrReadContract
		}
	}
	if actor != p.AuthorizedActorID {
		return nil, ErrForbidden
	}
	if !p.ApprovedAndAssigned || !p.EnteredHandling {
		return nil, ErrRevisionConflict
	}
	for _, version := range []uint64{p.RowVersion, p.TargetVersion, p.FlowRevision} {
		if version == 0 || version > math.MaxInt64 {
			return nil, ErrReadContract
		}
	}
	if now.IsZero() || strings.TrimSpace(p.SupplierModelID) == "" || p.StandardModelID != modelID || p.SupplierID != supplier.ID || (p.Region != "CN" && p.Region != "OVERSEAS") || p.Region != supplier.Region {
		return nil, ErrReadContract
	}
	if supplier.Status != "ACTIVE" {
		return nil, ErrRevisionConflict
	}
	c, err := resourceCoefficient(p.Coefficient)
	if err != nil {
		return nil, err
	}
	r := &Resource{ID: id, SupplierID: p.SupplierID, StandardModelID: p.StandardModelID, Region: p.Region, SupplierModelID: p.SupplierModelID, Coefficient: c.StringFixed(4), BuyerID: p.BuyerID, State: "PENDING_TEST", Revision: 1, OriginRowID: p.RowID, OriginRowVersion: p.RowVersion, TargetVersion: p.TargetVersion, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if report := p.Report; report != nil {
		if report.Sequence == 0 || report.Sequence > math.MaxInt64 {
			return nil, ErrReadContract
		}
		if !validID(report.ID) || (report.ResourceID != "" && report.ResourceID != id) || report.SupplierID != r.SupplierID || report.StandardModelID != r.StandardModelID || report.Region != r.Region || report.OriginRowID != r.OriginRowID || report.OriginRowVersion != r.OriginRowVersion || report.TargetVersion != r.TargetVersion {
			return nil, ErrReadContract
		}
		r.LatestReportID = report.ID
		r.LatestReportSequence = report.Sequence
		if report.Valid && report.ConnectivityPassed && report.PressurePassed {
			r.State = "PENDING_ACCESS"
		}
	}
	return r, nil
}
