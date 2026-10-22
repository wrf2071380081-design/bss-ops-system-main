package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"model_bss/internal/domain/supply"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ResourceProjection contains only native, non-secret resource/supplier columns.
type ResourceProjection struct {
	ID, SupplierID, StandardModelID, Region, SupplierModelID, Coefficient, BuyerID, State, LatestReportID string
	EverAdmitted                                                                                          bool
	CreatedAt, UpdatedAt                                                                                  time.Time
	Supplier                                                                                              supply.ResourceSupplier
}

// ResourceReadFacts comes from persisted A/B facts in the same transaction.
// ProjectionUpdatedAt binds those facts to a native row; it is not a revision.
type ResourceReadFacts struct {
	ResourceID                                            string
	Projection                                            ResourceProjection
	ProjectionUpdatedAt                                   time.Time
	Revision                                              uint64
	OriginRowID                                           string
	OriginRowVersion, TargetVersion, LatestReportSequence uint64
	FormalConfigID                                        string
	FormalConfigRevision                                  uint64
	Config                                                *supply.FormalConfigView
	Report                                                *ReportEvidenceRef
}
type ResourceFactsLoader func(context.Context, *sql.Tx, ResourceProjection) (*ResourceReadFacts, error)
type ResourceReadStore struct {
	schema string
	read   ModelReadRunner
	facts  ResourceFactsLoader
}

var _ supply.ResourceReadStore = (*ResourceReadStore)(nil)

func NewResourceReadStore(schema string, read ModelReadRunner, facts ResourceFactsLoader) (*ResourceReadStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	return &ResourceReadStore{schema, read, facts}, nil
}
func (s *ResourceReadStore) within(ctx context.Context, run func(*sql.Tx) error) error {
	if s == nil || s.facts == nil {
		return supply.ErrDependencyUnavailable
	}
	return (&ModelReadStore{schema: s.schema, read: s.read}).within(ctx, run)
}
func (s *ResourceReadStore) table(name string) string { return `"` + s.schema + `"."` + name + `"` }

func resourceReadQuery(q supply.ResourceListQuery, view string) (string, []any, error) {
	if q.View != view || !modelPageValid(q.Page, q.Size) || !q.NullsLast || q.TieBreakBy != "id" || (q.Order != "asc" && q.Order != "desc") {
		return "", nil, supply.ErrReadContract
	}
	allowed := []string{"id", "name", "company", "model_type", "supplier_count", "resource_count", "available_count", "coefficient_min", "coefficient_max"}
	if view == "flat" {
		allowed = []string{"id", "supplier_name", "region", "coefficient", "state", "updated_at", "connection_ms", "rps", "tps", "cache_hit_rate"}
	}
	if !slices.Contains(allowed, q.SortBy) {
		return "", nil, supply.ErrReadContract
	}
	where := []string{"TRUE"}
	args := []any{}
	add := func(v string) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	for _, f := range []struct {
		value, column string
		limit         int
	}{
		{q.Keyword, "", 200}, {q.Company, "m.company", 128}, {q.ModelType, "m.model_type", 32},
		{q.SupplierID, "r.supplier_id", 19}, {q.StandardModelID, "r.standard_model_id", 19}, {q.Region, "r.region", 16}, {q.State, "r.state", 24},
	} {
		if !utf8.ValidString(f.value) || strings.ContainsRune(f.value, 0) || utf8.RuneCountInString(f.value) > f.limit {
			return "", nil, supply.ErrReadContract
		}
		if f.value == "" {
			continue
		}
		value := f.value
		switch f.column {
		case "r.supplier_id", "r.standard_model_id":
			if !modelIdentity(value) {
				return "", nil, supply.ErrReadContract
			}
		case "r.region":
			if value != "CN" && value != "OVERSEAS" {
				return "", nil, supply.ErrReadContract
			}
		case "r.state":
			if !slices.Contains([]string{"PENDING_TEST", "PENDING_ACCESS", "ADMITTED", "OFFLINE"}, value) {
				return "", nil, supply.ErrReadContract
			}
		case "m.model_type":
			if !slices.Contains([]string{"TEXT", "IMAGE", "AUDIO", "VIDEO"}, value) {
				return "", nil, supply.ErrReadContract
			}
		}
		if f.column == "r.state" && value == "PENDING_ACCESS" {
			where = append(where, "(r.state="+add("PENDING_ADMIT")+" OR r.state="+add("PENDING_ACCESS")+")")
		} else if f.column == "" {
			where = append(where, `strpos(lower(m.name||' '||m.official_id||' '||s.name||' '||r.supplier_model_name),lower(`+add(value)+`::text))>0`)
		} else {
			where = append(where, f.column+"="+add(value))
		}
	}
	return strings.Join(where, " AND "), args, nil
}

type resourceReadEntry struct {
	record *supply.ResourceRecord
	view   *supply.ResourceSnapshot
}

func (s *ResourceReadStore) load(ctx context.Context, tx *sql.Tx, where string, args []any) ([]resourceReadEntry, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.id::text,r.supplier_id::text,r.standard_model_id::text,r.region,r.supplier_model_name,r.coefficient::text,r.buyer_id::text,r.state,r.ever_admitted,r.latest_test_report_id::text,r.created_at,r.updated_at,s.id::text,s.name,s.region,s.status FROM `+s.table("resource")+` r LEFT JOIN `+s.table("supplier")+` s ON s.id=r.supplier_id LEFT JOIN `+s.table("standard_model")+` m ON m.id=r.standard_model_id WHERE `+where+` ORDER BY r.id ASC`, args...)
	if err != nil {
		return nil, err
	}
	projections := []ResourceProjection{}
	for rows.Next() {
		var p ResourceProjection
		var buyer, report, supplier, name, region, status sql.NullString
		err = rows.Scan(&p.ID, &p.SupplierID, &p.StandardModelID, &p.Region, &p.SupplierModelID, &p.Coefficient, &buyer, &p.State, &p.EverAdmitted, &report, &p.CreatedAt, &p.UpdatedAt, &supplier, &name, &region, &status)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if !buyer.Valid || !supplier.Valid || !name.Valid || !region.Valid || !status.Valid {
			rows.Close()
			return nil, supply.ErrReadContract
		}
		p.BuyerID = buyer.String
		p.LatestReportID = report.String
		p.Supplier = supply.ResourceSupplier{ID: supplier.String, Name: name.String, Region: region.String, Status: status.String}
		if p.State == "PENDING_ADMIT" {
			p.State = "PENDING_ACCESS"
		}
		projections = append(projections, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Close rows before model/fact/report queries on this same connection.
	entries := make([]resourceReadEntry, 0, len(projections))
	models := map[string]*supply.StandardModel{}
	modelStore := &ModelReadStore{schema: s.schema}
	for _, p := range projections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := s.facts(ctx, tx, p)
		if err != nil {
			return nil, err
		}
		if f == nil || f.ResourceID != p.ID || !f.ProjectionUpdatedAt.Equal(p.UpdatedAt) || !resourceProjectionEqual(f.Projection, p) || f.Revision == 0 || f.LatestReportSequence > math.MaxInt64 || (f.FormalConfigID == "") != (f.FormalConfigRevision == 0) {
			return nil, supply.ErrReadContract
		}
		r := &supply.Resource{ID: p.ID, SupplierID: p.SupplierID, StandardModelID: p.StandardModelID, Region: p.Region, SupplierModelID: p.SupplierModelID, Coefficient: p.Coefficient, BuyerID: p.BuyerID, State: p.State, EverAdmitted: p.EverAdmitted, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
			Revision: f.Revision, OriginRowID: f.OriginRowID, OriginRowVersion: f.OriginRowVersion, TargetVersion: f.TargetVersion, FormalConfigID: f.FormalConfigID, FormalConfigRevision: f.FormalConfigRevision, LatestReportID: p.LatestReportID, LatestReportSequence: f.LatestReportSequence}
		model, exists := models[p.StandardModelID]
		if !exists {
			model, err = modelStore.current(ctx, tx, p.StandardModelID)
			if err != nil {
				return nil, err
			}
			models[p.StandardModelID] = model
		}
		record := &supply.ResourceRecord{Resource: r, Model: model, Supplier: p.Supplier, Config: f.Config}
		if p.LatestReportID == "" {
			if f.Report != nil || f.LatestReportSequence != 0 {
				return nil, supply.ErrReadContract
			}
		} else {
			ref := f.Report
			if ref == nil || ref.ReportID != p.LatestReportID || ref.ResourceID != p.ID || ref.SupplierID != p.SupplierID || ref.StandardModelID != p.StandardModelID || ref.Region != p.Region || ref.OriginRowID != f.OriginRowID || ref.Sequence != f.LatestReportSequence {
				return nil, supply.ErrReadContract
			}
			record.Report, err = ReadSelectedReport(ctx, tx, s.schema, *ref)
			if err != nil {
				return nil, err
			}
		}
		view, err := supply.BuildResourceSnapshot(record)
		if err != nil {
			return nil, err
		}
		entries = append(entries, resourceReadEntry{record, view})
	}
	return entries, nil
}

// Supplier state may change independently. Resource columns must agree with
// their persisted control snapshot, including changes that kept updated_at.
func resourceProjectionEqual(a, b ResourceProjection) bool {
	a.Supplier = supply.ResourceSupplier{}
	b.Supplier = supply.ResourceSupplier{}
	a.CreatedAt = a.CreatedAt.UTC()
	b.CreatedAt = b.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	b.UpdatedAt = b.UpdatedAt.UTC()
	x, y := resourceNumber(a.Coefficient), resourceNumber(b.Coefficient)
	if x == nil || y == nil {
		return false
	}
	a.Coefficient = x.RatString()
	b.Coefficient = y.RatString()
	if a.State == "PENDING_ADMIT" {
		a.State = "PENDING_ACCESS"
	}
	if b.State == "PENDING_ADMIT" {
		b.State = "PENDING_ACCESS"
	}
	return a == b
}

func (s *ResourceReadStore) GetResource(ctx context.Context, id string) (*supply.ResourceRecord, error) {
	if !modelIdentity(id) {
		return nil, supply.ErrReadContract
	}
	var record *supply.ResourceRecord
	err := s.within(ctx, func(tx *sql.Tx) error {
		rows, err := s.load(ctx, tx, "r.id=$1", []any{id})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return supply.ErrNotFound
		}
		if len(rows) != 1 {
			return supply.ErrReadContract
		}
		record = rows[0].record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

func resourceIDCompare(a, b string) int {
	x, _ := strconv.ParseUint(a, 10, 63)
	y, _ := strconv.ParseUint(b, 10, 63)
	if x < y {
		return -1
	}
	if x > y {
		return 1
	}
	return 0
}
func resourceNumber(value string) *big.Rat { n, _ := new(big.Rat).SetString(value); return n }
func resourceMetric(e resourceReadEntry, sortBy string) *big.Rat {
	switch sortBy {
	case "coefficient":
		return resourceNumber(e.view.Coefficient)
	case "connection_ms":
		if e.view.Test.Metrics.ConnectionMS != nil {
			return resourceNumber(*e.view.Test.Metrics.ConnectionMS)
		}
	case "rps":
		if e.view.Test.Metrics.RPS != nil {
			return resourceNumber(*e.view.Test.Metrics.RPS)
		}
	case "tps":
		if e.view.Test.Metrics.TPS != nil {
			return resourceNumber(*e.view.Test.Metrics.TPS)
		}
	case "cache_hit_rate":
		cache := e.view.Test.CacheStats
		if cache.Status == "PROVIDED" {
			hit, _ := new(big.Int).SetString(cache.HitTokens, 10)
			input, _ := new(big.Int).SetString(cache.InputTokens, 10)
			return new(big.Rat).SetFrac(hit, input)
		}
	}
	return nil
}
func resourceFlatCompare(a, b resourceReadEntry, q supply.ResourceListQuery) int {
	n := 0
	switch q.SortBy {
	case "id":
		n = resourceIDCompare(a.view.ID, b.view.ID)
	case "supplier_name":
		n = strings.Compare(a.view.SupplierName, b.view.SupplierName)
	case "region":
		n = strings.Compare(a.view.Region, b.view.Region)
	case "state":
		n = strings.Compare(a.view.State, b.view.State)
	case "updated_at":
		n = a.view.UpdatedAt.Compare(b.view.UpdatedAt)
	default:
		x, y := resourceMetric(a, q.SortBy), resourceMetric(b, q.SortBy)
		if x == nil && y != nil {
			return 1
		}
		if x != nil && y == nil {
			return -1
		}
		if x != nil {
			n = x.Cmp(y)
		}
	}
	if q.Order == "desc" {
		n = -n
	}
	if n == 0 {
		return resourceIDCompare(a.view.ID, b.view.ID)
	}
	return n
}
func resourcePageRange(length int, q supply.ResourceListQuery) (int, int) {
	start := (q.Page - 1) * q.Size
	if start >= length {
		return length, length
	}
	return start, min(start+q.Size, length)
}
func (s *ResourceReadStore) ListResourceRows(ctx context.Context, q supply.ResourceListQuery) ([]*supply.ResourceRecord, int64, error) {
	where, args, err := resourceReadQuery(q, "flat")
	if err != nil {
		return nil, 0, err
	}
	result := []*supply.ResourceRecord{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		entries, err := s.load(ctx, tx, where, args)
		if err != nil {
			return err
		}
		total = int64(len(entries))
		slices.SortFunc(entries, func(a, b resourceReadEntry) int { return resourceFlatCompare(a, b, q) })
		start, end := resourcePageRange(len(entries), q)
		for _, e := range entries[start:end] {
			result = append(result, e.record)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

type resourceReadGroup struct {
	id, name, company, modelType string
	suppliers                    map[string]bool
	entries                      []resourceReadEntry
	available                    int
	min, max                     *big.Rat
}

func resourceGroupCompare(a, b *resourceReadGroup, q supply.ResourceListQuery) int {
	n := 0
	cmpCount := func(x, y int) int {
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	}
	switch q.SortBy {
	case "id":
		n = resourceIDCompare(a.id, b.id)
	case "name":
		n = strings.Compare(a.name, b.name)
	case "company":
		n = strings.Compare(a.company, b.company)
	case "model_type":
		n = strings.Compare(a.modelType, b.modelType)
	case "supplier_count":
		n = cmpCount(len(a.suppliers), len(b.suppliers))
	case "resource_count":
		n = cmpCount(len(a.entries), len(b.entries))
	case "available_count":
		n = cmpCount(a.available, b.available)
	case "coefficient_min":
		n = a.min.Cmp(b.min)
	case "coefficient_max":
		n = a.max.Cmp(b.max)
	}
	if q.Order == "desc" {
		n = -n
	}
	if n == 0 {
		return resourceIDCompare(a.id, b.id)
	}
	return n
}
func (s *ResourceReadStore) ListResourceGroups(ctx context.Context, q supply.ResourceListQuery) ([][]*supply.ResourceRecord, int64, error) {
	where, args, err := resourceReadQuery(q, "grouped")
	if err != nil {
		return nil, 0, err
	}
	result := [][]*supply.ResourceRecord{}
	var total int64
	err = s.within(ctx, func(tx *sql.Tx) error {
		entries, err := s.load(ctx, tx, where, args)
		if err != nil {
			return err
		}
		byID := map[string]*resourceReadGroup{}
		groups := []*resourceReadGroup{}
		for _, e := range entries {
			v := e.view
			g := byID[v.StandardModelID]
			coefficient := resourceNumber(v.Coefficient)
			if g == nil {
				g = &resourceReadGroup{id: v.StandardModelID, name: v.ModelName, company: v.Company, modelType: v.ModelType, suppliers: map[string]bool{}, min: coefficient, max: coefficient}
				byID[g.id] = g
				groups = append(groups, g)
			}
			g.entries = append(g.entries, e)
			g.suppliers[v.SupplierID] = true
			if v.QuoteReadiness.Eligible {
				g.available++
			}
			if coefficient.Cmp(g.min) < 0 {
				g.min = coefficient
			}
			if coefficient.Cmp(g.max) > 0 {
				g.max = coefficient
			}
		}
		total = int64(len(groups))
		slices.SortFunc(groups, func(a, b *resourceReadGroup) int { return resourceGroupCompare(a, b, q) })
		start, end := resourcePageRange(len(groups), q)
		for _, g := range groups[start:end] {
			members := make([]*supply.ResourceRecord, 0, len(g.entries))
			for _, e := range g.entries {
				members = append(members, e.record)
			}
			result = append(result, members)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
