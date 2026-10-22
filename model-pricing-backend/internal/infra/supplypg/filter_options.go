package supplypg

import (
	"context"
	"database/sql"
	"fmt"
	"model_bss/internal/domain/supply"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FilterOptionsStore implements only directory capabilities. Compose it with
// the complete model/resource repositories; it does not invent missing models
// or resource readiness to satisfy their read interfaces.
type FilterOptionsStore struct {
	db     *sql.DB
	schema string
	read   ModelReadRunner
}

var _ supply.ModelFilterOptionsStore = (*FilterOptionsStore)(nil)
var _ supply.ResourceFilterOptionsStore = (*FilterOptionsStore)(nil)

func NewFilterOptionsStore(db *sql.DB) *FilterOptionsStore {
	return &FilterOptionsStore{db: db, schema: "public"}
}
func (s *FilterOptionsStore) ModelFilterOptions(ctx context.Context, q supply.ModelOptionsQuery) (supply.ModelFilterOptions, error) {
	if utf8.RuneCountInString(q.Keyword) > 200 {
		return supply.ModelFilterOptions{}, supply.ErrReadContract
	}
	return s.options(ctx, q.Regions, q.Keyword, q.Limit, true)
}
func (s *FilterOptionsStore) ResourceFilterOptions(ctx context.Context, q supply.ResourceOptionsQuery) (supply.ResourceFilterOptions, error) {
	regions := []string{}
	if q.Region != "" {
		regions = append(regions, q.Region)
	}
	opts, err := s.options(ctx, regions, q.Keyword, q.Limit, false)
	if err != nil {
		return supply.ResourceFilterOptions{}, err
	}
	result := supply.ResourceFilterOptions{Companies: opts.Companies, Suppliers: []supply.ResourceSupplierOption{}, HasMore: opts.HasMore}
	for _, supplier := range opts.Suppliers {
		result.Suppliers = append(result.Suppliers, supply.ResourceSupplierOption{ID: supplier.ID, Name: supplier.Name, Region: supplier.Region})
	}
	return result, nil
}

type filterOptionQueries struct {
	company, supplier         string
	companyArgs, supplierArgs []any
}

func buildFilterOptionQueries(regions []string, keyword string, limit int, globalCompanies bool) (filterOptionQueries, error) {
	var result filterOptionQueries
	if limit < 1 || limit > 50 || len(regions) > 2 {
		return result, supply.ErrReadContract
	}
	args := []any{keyword}
	regionClause := ""
	seen := map[string]bool{}
	placeholders := []string{}
	for _, region := range regions {
		if (region != "CN" && region != "OVERSEAS") || seen[region] {
			return result, supply.ErrReadContract
		}
		seen[region] = true
		args = append(args, region)
		placeholders = append(placeholders, fmt.Sprintf("$%d::text", len(args)))
	}
	if len(placeholders) > 0 {
		regionClause = " AND r.region IN (" + strings.Join(placeholders, ",") + ")"
	}
	args = append(args, limit+1)
	// EXISTS avoids duplicates before limiting. Consistent supplier/resource
	// regions define the valid relation; frozen suppliers remain readable.
	result.supplier = `SELECT s.id::text,s.name,s.region FROM public.supplier s WHERE btrim(s.name)<>''
		AND strpos(lower(s.name),lower($1::text))>0 AND EXISTS
		(SELECT 1 FROM public.resource r WHERE r.supplier_id=s.id AND r.region=s.region` + regionClause + `)
		ORDER BY s.name ASC,s.id ASC LIMIT ` + fmt.Sprintf("$%d", len(args))
	result.supplierArgs = append([]any(nil), args...)
	result.company = `SELECT DISTINCT m.company FROM public.standard_model m WHERE btrim(m.company)<>'' AND strpos(lower(m.company),lower($1::text))>0`
	if globalCompanies {
		result.companyArgs = []any{keyword, limit + 1}
		result.company += ` ORDER BY m.company ASC LIMIT $2`
	} else {
		result.company += ` AND EXISTS (SELECT 1 FROM public.resource r JOIN public.supplier s ON s.id=r.supplier_id AND s.region=r.region WHERE r.standard_model_id=m.id` + regionClause + `) ORDER BY m.company ASC LIMIT ` + fmt.Sprintf("$%d", len(args))
		result.companyArgs = append([]any(nil), args...)
	}
	return result, nil
}
func (s *FilterOptionsStore) options(ctx context.Context, regions []string, keyword string, limit int, globalCompanies bool) (supply.ModelFilterOptions, error) {
	empty := supply.ModelFilterOptions{}
	if s == nil || (s.db == nil && s.read == nil) {
		return empty, supply.ErrDependencyUnavailable
	}
	if !modelSchemaName.MatchString(s.schema) {
		return empty, mappingError("schema")
	}
	queries, err := buildFilterOptionQueries(regions, keyword, limit, globalCompanies)
	if err != nil {
		return empty, err
	}
	// Only the validated identifier replaces the fixed schema token. Search
	// values remain bound arguments in the original query builder.
	queries.company = strings.ReplaceAll(queries.company, "public.", `"`+s.schema+`".`)
	queries.supplier = strings.ReplaceAll(queries.supplier, "public.", `"`+s.schema+`".`)
	run := s.read
	if run == nil {
		run = ModelReadTransactions(s.db)
	}
	var result supply.ModelFilterOptions
	err = run(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		var e error
		result, e = readFilterOptions(ctx, tx, queries, limit)
		return e
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}
func readFilterOptions(ctx context.Context, tx *sql.Tx, queries filterOptionQueries, limit int) (supply.ModelFilterOptions, error) {
	empty := supply.ModelFilterOptions{}
	result := supply.ModelFilterOptions{Companies: []string{}, Suppliers: []supply.ModelSupplierOption{}}
	rows, err := tx.QueryContext(ctx, queries.company, queries.companyArgs...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	for rows.Next() {
		var company string
		if err := rows.Scan(&company); err != nil {
			return empty, err
		}
		result.Companies = append(result.Companies, company)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if err := rows.Close(); err != nil {
		return empty, err
	}
	rows, err = tx.QueryContext(ctx, queries.supplier, queries.supplierArgs...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	for rows.Next() {
		var supplier supply.ModelSupplierOption
		if err := rows.Scan(&supplier.ID, &supplier.Name, &supplier.Region); err != nil {
			return empty, err
		}
		id, err := strconv.ParseInt(supplier.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != supplier.ID || (supplier.Region != "CN" && supplier.Region != "OVERSEAS") {
			return empty, supply.ErrReadContract
		}
		result.Suppliers = append(result.Suppliers, supplier)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if err := rows.Close(); err != nil {
		return empty, err
	}
	result.HasMore = len(result.Companies) > limit || len(result.Suppliers) > limit
	if len(result.Companies) > limit {
		result.Companies = result.Companies[:limit]
	}
	if len(result.Suppliers) > limit {
		result.Suppliers = result.Suppliers[:limit]
	}
	return result, nil
}
