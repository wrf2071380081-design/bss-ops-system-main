package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"model_bss/internal/domain/supply"
)

// ReportEvidenceRef must be supplied by B inside the same read snapshot, never
// assembled from an HTTP body or inferred from report arrival time.
type ReportEvidenceRef struct {
	BaselineReportID, StressReportID, TargetFingerprint                           string
	ReportID, JobID, ResourceID, SupplierID, StandardModelID, Region, OriginRowID string
	OriginRowVersion, TargetVersion, Sequence                                     uint64
	Valid                                                                         bool
}

func ReadSelectedReport(ctx context.Context, tx *sql.Tx, schema string, ref ReportEvidenceRef) (*supply.ResourceReportSummary, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	for _, id := range []string{ref.ReportID, ref.JobID, ref.ResourceID, ref.SupplierID, ref.StandardModelID, ref.OriginRowID} {
		if !modelIdentity(id) {
			return nil, supply.ErrReadContract
		}
	}
	for _, version := range []uint64{ref.OriginRowVersion, ref.TargetVersion, ref.Sequence} {
		if version == 0 || version > math.MaxInt64 {
			return nil, supply.ErrReadContract
		}
	}
	if ref.Region != "CN" && ref.Region != "OVERSEAS" {
		return nil, supply.ErrReadContract
	}
	if tx == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ref.BaselineReportID != "" || ref.StressReportID != "" {
		return readNativeReportPair(ctx, tx, schema, ref)
	}
	table := func(name string) string { return `"` + schema + `"."` + name + `"` }
	var job, resource string
	var jobResource, supplier, model, region sql.NullString
	var connection, stress sql.NullBool
	var ms, rps, tps sql.NullString
	var cache ReportCacheRow
	err := tx.QueryRowContext(ctx, `SELECT p.test_job_id::text,p.resource_id::text,j.resource_id::text,r.supplier_id::text,r.standard_model_id::text,r.region,
 p.connection_passed,p.stress_passed,p.connection_ms::text,p.rps::text,p.tps::text,p.cache_hit_tokens,p.cache_input_tokens,p.scene
 FROM `+table("test_report")+` p LEFT JOIN `+table("test_job")+` j ON j.id=p.test_job_id LEFT JOIN `+table("resource")+` r ON r.id=p.resource_id WHERE p.id=$1`, ref.ReportID).Scan(&job, &resource, &jobResource, &supplier, &model, &region, &connection, &stress, &ms, &rps, &tps, &cache.CacheHitTokens, &cache.CacheInputTokens, &cache.Scene)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if job != ref.JobID || resource != ref.ResourceID || !jobResource.Valid || jobResource.String != ref.ResourceID || !supplier.Valid || supplier.String != ref.SupplierID || !model.Valid || model.String != ref.StandardModelID || !region.Valid || region.String != ref.Region {
		return nil, supply.ErrReadContract
	}
	decimalValue := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		value := v.String
		return &value
	}
	return &supply.ResourceReportSummary{ID: ref.ReportID, ResourceID: resource, SupplierID: ref.SupplierID, StandardModelID: ref.StandardModelID, Region: ref.Region, OriginRowID: ref.OriginRowID, OriginRowVersion: ref.OriginRowVersion, TargetVersion: ref.TargetVersion, Sequence: ref.Sequence,
		ConnectivityPassed: connection.Valid && connection.Bool, PressurePassed: stress.Valid && stress.Bool, Valid: ref.Valid && connection.Valid && stress.Valid, Metrics: supply.ResourceReportMetrics{ConnectionMS: decimalValue(ms), RPS: decimalValue(rps), TPS: decimalValue(tps)}, Cache: cache.DomainCache()}, nil
}
