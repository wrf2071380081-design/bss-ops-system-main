package supply

import "context"

type PlanListQuery struct {
	ResourceID, Region, Kind, Status string
	Page, Size                       int
	SortBy, Order, TieBreakBy        string
}
type PlanListPage struct {
	List  []*PlanSummary `json:"list"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

// Query all permitted internal read records; no buyer ownership read filter.
// Filter before paging; created_at DESC then numeric id DESC. Return each plan
// together with current resource facts from a consistent snapshot.
type PlanListStore interface {
	ListPlanSummaries(context.Context, PlanListQuery) ([]PlanReadRecord, int64, error)
}

func (r *PlanReader) List(ctx context.Context, actor string, q PlanListQuery) (*PlanListPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actor) {
		return nil, invalid("actor", "authenticated staff required")
	}
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return nil, err
	}
	if (q.ResourceID != "" && !validID(q.ResourceID)) || (q.Region != "" && q.Region != "CN" && q.Region != "OVERSEAS") || (q.Kind != "" && q.Kind != "ONLINE" && q.Kind != "OFFLINE" && q.Kind != "PRICE_CHANGE") || (q.Status != "" && q.Status != "PENDING" && q.Status != "EXECUTING" && q.Status != "EXECUTED" && q.Status != "CANCELED" && q.Status != "BLOCKED") {
		return nil, invalid("filters", "invalid plan filter")
	}
	q.SortBy = "created_at"
	q.Order = "desc"
	q.TieBreakBy = "id"
	store, ok := r.store.(PlanListStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	records, total, err := store.ListPlanSummaries(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < 0 || total < int64(len(records)) || len(records) > q.Size || (int64(q.Page-1)*int64(q.Size) >= total && len(records) > 0) {
		return nil, ErrReadContract
	}
	result := &PlanListPage{List: make([]*PlanSummary, 0, len(records)), Total: total, Page: q.Page, Size: q.Size}
	seen := map[string]bool{}
	for _, record := range records {
		if record.Plan == nil || !validID(record.Plan.ID) {
			return nil, ErrReadContract
		}
		p := record.Plan
		if seen[p.ID] || (q.ResourceID != "" && p.ResourceID != q.ResourceID) || (q.Region != "" && p.Region != q.Region) || (q.Kind != "" && p.Kind != q.Kind) || (q.Status != "" && p.Status != q.Status) {
			return nil, ErrReadContract
		}
		summary, err := buildPlanSummary(record, actor, p.ID)
		if err != nil {
			return nil, err
		}
		seen[p.ID] = true
		result.List = append(result.List, summary)
	}
	return result, nil
}
