package supply

import (
	"math"
	"strings"
	"time"
)

// StatePlan is the write aggregate. Read APIs expose ResourcePlan, never raw
// persistence snapshots. Snapshots do not contain endpoint credentials.
type StatePlan struct {
	ResourcePlan
	BeforeSnapshot, AfterSnapshot Resource
}
type StatePlanExecution struct {
	Plan           *StatePlan
	Resource       *Resource
	CompletionNote string
	FailureReason  string
}

func (p *StatePlan) copy() *StatePlan {
	if p == nil {
		return nil
	}
	copy := *p
	copy.ResourcePlan = *p.ResourcePlan.Clone()
	return &copy
}
func NewStatePlan(id, actor string, r *Resource, kind string, due time.Time, reason string, now time.Time) (*StatePlan, error) {
	if !validID(id) || !validID(actor) || r == nil || !validID(r.ID) || !validID(r.BuyerID) || r.Revision == 0 || r.Revision > math.MaxInt64 || (r.Region != "CN" && r.Region != "OVERSEAS") {
		return nil, invalid("resource", "valid plan identity and resource snapshot required")
	}
	if r.BuyerID != actor {
		return nil, ErrForbidden
	}
	if (kind != "OFFLINE" && kind != "ONLINE") || due.IsZero() || now.IsZero() || strings.TrimSpace(reason) == "" {
		return nil, invalid("plan", "kind, due date and reason required")
	}
	if !r.EverAdmitted || (kind == "OFFLINE" && r.State != "ADMITTED") || (kind == "ONLINE" && r.State != "OFFLINE") {
		return nil, ErrRevisionConflict
	}
	after := *r
	after.State = "OFFLINE"
	if kind == "ONLINE" {
		after.State = "ADMITTED"
	}
	return &StatePlan{ResourcePlan: ResourcePlan{ID: id, ResourceID: r.ID, Region: r.Region, Kind: kind, Status: "PENDING", CreatedBy: actor, Revision: 1, ExpectedResourceRevision: r.Revision, DueAt: due.UTC(), CreatedAt: now.UTC(), Reason: strings.TrimSpace(reason)}, BeforeSnapshot: *r, AfterSnapshot: after}, nil
}
func (p *StatePlan) authorize(actor string, expected uint64) error {
	if p == nil {
		return ErrNotFound
	}
	if !validID(p.ID) || !validID(p.ResourceID) || !validID(p.CreatedBy) || p.Revision == 0 || p.Revision >= math.MaxInt64 || p.ExpectedResourceRevision == 0 || p.ExpectedResourceRevision > math.MaxInt64 {
		return ErrReadContract
	}
	if actor != p.CreatedBy {
		return ErrForbidden
	}
	if expected != p.Revision || p.Status != "PENDING" {
		return ErrRevisionConflict
	}
	return nil
}
func (p *StatePlan) RevisedStatePlan(actor string, expected uint64, due time.Time, reason string) (*StatePlan, error) {
	if err := p.authorize(actor, expected); err != nil {
		return nil, err
	}
	if due.IsZero() || strings.TrimSpace(reason) == "" {
		return nil, invalid("plan", "due date and reason required")
	}
	copy := p.copy()
	copy.DueAt = due.UTC()
	copy.Reason = strings.TrimSpace(reason)
	copy.Revision++
	// Keep the original resource basis; changing a date is not silent rebasing.
	return copy, nil
}
func (p *StatePlan) CanceledStatePlan(actor string, expected uint64) (*StatePlan, error) {
	// A blocked plan may be canceled by its original initiator so a new plan can
	// capture fresh facts; it cannot be executed or silently rebased.
	if p != nil && p.Status == "BLOCKED" {
		copy := p.copy()
		copy.Status = "PENDING"
		if err := copy.authorize(actor, expected); err != nil {
			return nil, err
		}
		copy.Status = "CANCELED"
		copy.Revision++
		return copy, nil
	}
	if err := p.authorize(actor, expected); err != nil {
		return nil, err
	}
	copy := p.copy()
	copy.Status = "CANCELED"
	copy.Revision++
	return copy, nil
}

// ExecuteStatePlan returns a proposal only. No input is mutated. The application
// service must persist the plan/resource/log/notice/outbox in one transaction.
// A conflict returns a BLOCKED plan proposal AND ErrRevisionConflict. Persist
// that failure record and commit before mapping to HTTP409; do not roll it back
// by returning the business error from the transaction callback.
func (p *StatePlan) ExecuteStatePlan(actor string, expected uint64, record *ResourceRecord, confirmed bool, note string, now time.Time) (*StatePlanExecution, error) {
	if err := p.authorize(actor, expected); err != nil {
		return nil, err
	}
	if !confirmed || strings.TrimSpace(note) == "" || now.IsZero() {
		return nil, invalid("completion", "actual operation and evidence required")
	}
	if record == nil || record.Resource == nil {
		return nil, ErrDependencyUnavailable
	}
	r := record.Resource
	if r.ID != p.ResourceID || r.Region != p.Region {
		return nil, ErrReadContract
	}
	blocked := func(reason string) (*StatePlanExecution, error) {
		copy := p.copy()
		copy.Status = "BLOCKED"
		copy.Revision++
		return &StatePlanExecution{Plan: copy, CompletionNote: strings.TrimSpace(note), FailureReason: reason}, ErrRevisionConflict
	}
	if r.Revision != p.ExpectedResourceRevision || r.Revision >= math.MaxInt64 || !r.EverAdmitted || r.State != p.BeforeSnapshot.State {
		return blocked("RESOURCE_CHANGED")
	}
	if p.Kind != "ONLINE" && p.Kind != "OFFLINE" {
		return nil, ErrReadContract
	}
	after := *r
	after.State = "OFFLINE"
	if p.Kind == "ONLINE" {
		after.State = "ADMITTED"
		candidate := *record
		copy := after
		candidate.Resource = &copy
		snapshot, err := BuildResourceSnapshot(&candidate)
		if err != nil {
			return nil, err
		}
		if !snapshot.QuoteReadiness.Eligible {
			return blocked("RESOURCE_NOT_READY")
		}
	}
	at := now.UTC()
	after.Revision++
	after.UpdatedAt = at
	plan := p.copy()
	plan.Status = "EXECUTED"
	plan.Revision++
	plan.ExecutedAt = &at
	return &StatePlanExecution{Plan: plan, Resource: &after, CompletionNote: strings.TrimSpace(note)}, nil
}
