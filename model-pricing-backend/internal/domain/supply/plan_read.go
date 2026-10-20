package supply

import (
	"context"
	"math"
	"strings"
	"time"
)

type ResourcePlan struct {
	ID                       string     `json:"id"`
	ResourceID               string     `json:"resource_id"`
	Region                   string     `json:"region"`
	Kind                     string     `json:"kind"`
	Status                   string     `json:"status"`
	CreatedBy                string     `json:"created_by"`
	CreatorName              string     `json:"creator_name"`
	Revision                 uint64     `json:"revision,string"`
	ExpectedResourceRevision uint64     `json:"expected_resource_revision,string"`
	DueAt                    time.Time  `json:"due_at"`
	ExecutedAt               *time.Time `json:"executed_at"`
	CreatedAt                time.Time  `json:"created_at"`
	Reason                   string     `json:"reason"`
}

func (p *ResourcePlan) Clone() *ResourcePlan {
	if p == nil {
		return nil
	}
	copy := *p
	if p.ExecutedAt != nil {
		at := p.ExecutedAt.UTC()
		copy.ExecutedAt = &at
	}
	return &copy
}

type PlanReadRecord struct {
	Plan                                        *ResourcePlan
	ResourceID, ResourceRegion, ResourceBuyerID string
	ResourceRevision                            uint64
}
type PlanReadStore interface {
	GetPlanSummary(context.Context, string) (PlanReadRecord, error)
}
type PlanSummary struct {
	*ResourcePlan
	IsInitiator             bool     `json:"is_initiator"`
	ReadOnly                bool     `json:"read_only"`
	PreconditionMatches     bool     `json:"precondition_matches"`
	CurrentResourceRevision uint64   `json:"current_resource_revision,string"`
	BlockedReasons          []string `json:"blocked_reasons"`
}
type PlanReader struct{ store PlanReadStore }

func NewPlanReader(store PlanReadStore) *PlanReader { return &PlanReader{store: store} }
func (r *PlanReader) Get(ctx context.Context, actor, id string) (*PlanSummary, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actor) || !validID(id) {
		return nil, invalid("id", "expected plan and authenticated staff bigint strings")
	}
	record, err := r.store.GetPlanSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	p := record.Plan
	if p == nil || p.ID != id || !validID(p.ResourceID) || !validID(p.CreatedBy) || !validID(record.ResourceBuyerID) || record.ResourceID != p.ResourceID || record.ResourceRegion != p.Region || (p.Region != "CN" && p.Region != "OVERSEAS") || p.Revision == 0 || p.Revision > math.MaxInt64 || p.ExpectedResourceRevision == 0 || p.ExpectedResourceRevision > math.MaxInt64 || record.ResourceRevision == 0 || record.ResourceRevision > math.MaxInt64 || p.DueAt.IsZero() || p.CreatedAt.IsZero() || strings.TrimSpace(p.Reason) == "" {
		return nil, ErrReadContract
	}
	if p.Kind != "ONLINE" && p.Kind != "OFFLINE" && p.Kind != "PRICE_CHANGE" {
		return nil, ErrReadContract
	}
	if p.Status != "PENDING" && p.Status != "EXECUTING" && p.Status != "EXECUTED" && p.Status != "CANCELED" && p.Status != "BLOCKED" {
		return nil, ErrReadContract
	}
	if p.Status == "EXECUTED" && (p.ExecutedAt == nil || p.ExecutedAt.IsZero()) {
		return nil, ErrReadContract
	}
	summary := &PlanSummary{ResourcePlan: p.Clone(), IsInitiator: p.CreatedBy == actor, ReadOnly: true, PreconditionMatches: p.ExpectedResourceRevision == record.ResourceRevision, CurrentResourceRevision: record.ResourceRevision, BlockedReasons: []string{}}
	summary.DueAt = summary.DueAt.UTC()
	summary.CreatedAt = summary.CreatedAt.UTC()
	if !summary.PreconditionMatches {
		summary.BlockedReasons = append(summary.BlockedReasons, "RESOURCE_CHANGED")
	}
	return summary, nil
}
