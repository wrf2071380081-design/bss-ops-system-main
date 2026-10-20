package supply

import (
	"context"
	"errors"
	"math"
	"strings"
)

var ErrReadContract = errors.New("standard model read adapter returned inconsistent data")

type ModelListQuery struct {
	Keyword, Company, ModelType, Status, VerifyStatus string
	Companies, ModelTypes, SupplierIDs, Regions       []string
	SortBy, Order                                     string
	Page, Size                                        int
	// Adapter obligations: NULLS LAST in both directions, then a stable ID tie break.
	NullsLast  bool
	TieBreakBy string
}
type ModelPage struct {
	List       []*StandardModel
	Total      int64
	Page, Size int
}
type StandardModelSnapshot struct {
	Model            *StandardModel
	CurrentVersionNo uint64
	IsCurrent        bool
	QuoteReadiness   ModelReadiness
}

// ModelReadStore has no actor/role scope: every authenticated internal staff member can read.
// SQL adapters must apply the validated sorting/filtering before pagination, never legacy SKU scopes.
type ModelReadStore interface {
	ListModels(context.Context, ModelListQuery) ([]*StandardModel, int64, error)
	GetModel(context.Context, string) (*StandardModel, error)
	ListModelVersions(context.Context, string, int, int) ([]*StandardModel, int64, error)
	GetModelVersion(context.Context, string, uint64) (*StandardModel, error)
}
type ModelReader struct{ store ModelReadStore }

func NewModelReader(store ModelReadStore) *ModelReader { return &ModelReader{store: store} }

func readPagination(page, size int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if size < 1 || size > 100 {
		return 0, 0, invalid("size", "expected 1..100")
	}
	if page < 1 || page > math.MaxInt/size {
		return 0, 0, invalid("page", "page is outside supported range")
	}
	return page, size, nil
}
func normalizeModelQuery(q ModelListQuery) (ModelListQuery, error) {
	var err error
	q.Page, q.Size, err = readPagination(q.Page, q.Size)
	if err != nil {
		return q, err
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	q.Company = strings.TrimSpace(q.Company)
	q, err = normalizeModelFilters(q)
	if err != nil {
		return q, err
	}
	if q.SortBy == "" {
		q.SortBy = "updated_at"
	}
	if q.Order == "" {
		q.Order = "desc"
	}
	q.Order = strings.ToLower(q.Order)
	switch q.SortBy {
	case "id", "name", "company", "model_type", "verify_status", "checked_at", "updated_at":
	default:
		return q, invalid("sort_by", "unsupported sort field")
	}
	if q.Order != "asc" && q.Order != "desc" {
		return q, invalid("order", "expected asc or desc")
	}
	if q.ModelType != "" && !modelTypeKnown(q.ModelType) {
		return q, invalid("model_type", "unsupported model type")
	}
	if q.Status != "" && q.Status != "ACTIVE" && q.Status != "INACTIVE" {
		return q, invalid("status", "unsupported model status")
	}
	if q.VerifyStatus != "" && q.VerifyStatus != "INCOMPLETE" && q.VerifyStatus != "UNVERIFIED" && q.VerifyStatus != "VERIFIED" {
		return q, invalid("verify_status", "unsupported verification status")
	}
	q.NullsLast = true
	q.TieBreakBy = "id"
	return q, nil
}
func readPage(rows []*StandardModel, total int64, page, size int) (*ModelPage, error) {
	if total < int64(len(rows)) || len(rows) > size {
		return nil, ErrReadContract
	}
	result := &ModelPage{List: make([]*StandardModel, 0, len(rows)), Total: total, Page: page, Size: size}
	for _, row := range rows {
		if row == nil || !validID(row.ID) || row.VersionNo == 0 || row.Revision == 0 {
			return nil, ErrReadContract
		}
		result.List = append(result.List, row.Clone())
	}
	return result, nil
}
func (r *ModelReader) available() error {
	if r == nil || r.store == nil {
		return ErrDependencyUnavailable
	}
	return nil
}

func (r *ModelReader) List(ctx context.Context, q ModelListQuery) (*ModelPage, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	q, err := normalizeModelQuery(q)
	if err != nil {
		return nil, err
	}
	rows, total, err := r.store.ListModels(ctx, q)
	if err != nil {
		return nil, err
	}
	return readPage(rows, total, q.Page, q.Size)
}
func (r *ModelReader) Get(ctx context.Context, id string) (*StandardModel, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	model, err := r.store.GetModel(ctx, id)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, ErrNotFound
	}
	if model.ID != id || model.VersionNo == 0 || model.Revision == 0 {
		return nil, ErrReadContract
	}
	return model.Clone(), nil
}
func (r *ModelReader) Versions(ctx context.Context, id string, page, size int) (*ModelPage, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	page, size, err := readPagination(page, size)
	if err != nil {
		return nil, err
	}
	rows, total, err := r.store.ListModelVersions(ctx, id, page, size)
	if err != nil {
		return nil, err
	}
	result, err := readPage(rows, total, page, size)
	if err != nil {
		return nil, err
	}
	for _, model := range result.List {
		if model.ID != id {
			return nil, ErrReadContract
		}
	}
	return result, nil
}
func (r *ModelReader) Snapshot(ctx context.Context, id string, version uint64) (*StandardModelSnapshot, error) {
	if version > math.MaxInt64 {
		return nil, invalid("version_no", "version exceeds bigint range")
	}
	current, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if version > current.VersionNo {
		return nil, ErrNotFound
	}
	if version == 0 || version == current.VersionNo {
		return &StandardModelSnapshot{Model: current, CurrentVersionNo: current.VersionNo, IsCurrent: true, QuoteReadiness: current.Readiness()}, nil
	}
	model, err := r.store.GetModelVersion(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, ErrNotFound
	}
	if model.ID != id || model.VersionNo != version {
		return nil, ErrReadContract
	}
	model = model.Clone()
	readiness := model.Readiness()
	readiness.Ready = false
	readiness.Reasons = append(readiness.Reasons, "HISTORICAL_MODEL_VERSION")
	return &StandardModelSnapshot{Model: model, CurrentVersionNo: current.VersionNo, QuoteReadiness: readiness}, nil
}
