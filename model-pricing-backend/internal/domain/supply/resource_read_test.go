package supply

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResourceCacheStatisticsUseOriginalCountsAndPreserveMissingStates(t *testing.T) {
	for _, tc := range []struct{ hit, input, ratio, percent string }{{"1200", "2000", "0.600000", "60.00"}, {"0", "2000", "0.000000", "0.00"}, {"246899", "2000000", "0.123450", "12.34"}, {"9007199254740993", "9007199254740993", "1.000000", "100.00"}} {
		view, err := resourceCacheStats("51", &ReportCacheCounters{Status: "PROVIDED", HitTokens: tc.hit, InputTokens: tc.input, Scene: "固定前缀"})
		if err != nil || view.HitRate != tc.ratio || view.HitPercent != tc.percent || view.ReportID != "51" || view.HitTokens != tc.hit {
			t.Fatalf("wrong cache projection: %+v %v", view, err)
		}
	}
	missing, err := resourceCacheStats("51", nil)
	if err != nil || missing.Status != "NOT_PROVIDED" || missing.HitPercent != "" {
		t.Fatalf("missing cache became zero: %+v %v", missing, err)
	}
	notApplicable, err := resourceCacheStats("51", &ReportCacheCounters{Status: "NOT_APPLICABLE"})
	if err != nil || notApplicable.Status != "NOT_APPLICABLE" || notApplicable.HitPercent != "" {
		t.Fatal("not applicable became zero")
	}
	for _, raw := range []ReportCacheCounters{{Status: "PROVIDED", HitTokens: "1", InputTokens: "0"}, {Status: "PROVIDED", HitTokens: "3", InputTokens: "2"}, {Status: "PROVIDED", HitTokens: "-1", InputTokens: "2"}, {Status: "PROVIDED", HitTokens: "1.0", InputTokens: "2"}, {Status: "PROVIDED", HitTokens: "1e3", InputTokens: "2000"}, {Status: "NOT_PROVIDED", HitTokens: "1", InputTokens: "2"}} {
		if _, err := resourceCacheStats("51", &raw); !errors.Is(err, ErrReadContract) {
			t.Fatalf("bad counts accepted: %+v %v", raw, err)
		}
	}
}

func resourceRecord(t *testing.T, id, supplier, model, region, coefficient string) *ResourceRecord {
	t.Helper()
	m, err := NewStandardModel(model, "7", completeModelData(), modelNow)
	if err != nil {
		t.Fatal(err)
	}
	m.VerifyStatus = "VERIFIED"
	m.CheckedBy = "7"
	r := &Resource{ID: id, SupplierID: supplier, StandardModelID: model, Region: region, SupplierModelID: "vendor-model-v1", Coefficient: coefficient, BuyerID: "7", State: "ADMITTED", EverAdmitted: true, Revision: 1, FormalConfigID: supplier + "99", FormalConfigRevision: 1, OriginRowID: id + "0", OriginRowVersion: 1, TargetVersion: 1, LatestReportID: id + "00", CreatedAt: modelNow, UpdatedAt: modelNow}
	return &ResourceRecord{Resource: r, Model: m, Supplier: ResourceSupplier{ID: supplier, Name: "供应商" + supplier, Region: region, Status: "ACTIVE"}, Config: &FormalConfigView{ID: r.FormalConfigID, SupplierID: supplier, Region: region, Status: "ACTIVE", HasAPIKey: true, Revision: 1, ConfirmedRevision: 1}, Report: &ResourceReportSummary{ID: r.LatestReportID, ResourceID: id, SupplierID: supplier, StandardModelID: model, Region: region, OriginRowID: r.OriginRowID, OriginRowVersion: 1, TargetVersion: 1, ConnectivityPassed: true, PressurePassed: true, Valid: true, Cache: &ReportCacheCounters{Status: "PROVIDED", HitTokens: "1200", InputTokens: "2000", Scene: "重复前缀"}}}
}
func TestResourceSnapshotRequiresOwnCurrentEvidenceAndConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*ResourceRecord)
	}{
		{"pressure failed", "TESTS_NOT_PASSED", func(r *ResourceRecord) { r.Report.PressurePassed = false }},
		{"connection failed", "TESTS_NOT_PASSED", func(r *ResourceRecord) { r.Report.ConnectivityPassed = false }},
		{"stale target", "TEST_TARGET_CHANGED", func(r *ResourceRecord) { r.Report.TargetVersion = 2 }},
		{"config changed", "FORMAL_CONFIG_CHANGED", func(r *ResourceRecord) { r.Config.Revision = 2; r.Config.ConfirmedRevision = 2 }},
		{"no key", "FORMAL_CONFIG_UNAVAILABLE", func(r *ResourceRecord) { r.Config.HasAPIKey = false }},
		{"unverified model", "MODEL_NOT_READY", func(r *ResourceRecord) { r.Model.VerifyStatus = "UNVERIFIED" }},
		{"inactive supplier", "SUPPLIER_INACTIVE", func(r *ResourceRecord) { r.Supplier.Status = "INACTIVE" }},
		{"not admitted", "RESOURCE_NOT_ADMITTED", func(r *ResourceRecord) { r.Resource.State = "PENDING_ACCESS"; r.Resource.EverAdmitted = false }},
		{"invalid report", "TEST_EVIDENCE_INVALID", func(r *ResourceRecord) { r.Report.Valid = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := resourceRecord(t, "31", "21", "11", "CN", "0.6800")
			tc.change(record)
			snapshot, err := BuildResourceSnapshot(record)
			if err != nil || snapshot.QuoteReadiness.Eligible || !strings.Contains(strings.Join(snapshot.QuoteReadiness.BlockedReasons, ","), tc.reason) {
				t.Fatalf("ineligible resource accepted: %+v %v", snapshot, err)
			}
			if tc.name == "stale target" && (snapshot.Test.CacheStats.HitTokens != "" || snapshot.Test.Status != "STALE") {
				t.Fatal("stale target reused cache or report metrics")
			}
		})
	}
	record := resourceRecord(t, "31", "21", "11", "CN", "0.68")
	snapshot, err := BuildResourceSnapshot(record)
	if err != nil || !snapshot.QuoteReadiness.Eligible || snapshot.Coefficient != "0.6800" || snapshot.Test.CacheStats.HitPercent != "60.00" {
		t.Fatalf("valid resource rejected: %+v %v", snapshot, err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "base_url") || strings.Contains(string(encoded), "api_key") || strings.Contains(string(encoded), "password") {
		t.Fatalf("resource leaked private fields: %s", encoded)
	}
	snapshot.OfficialPriceItems[0].Amount = "999"
	if record.Model.Data.PriceItems[0].Amount != "2.00000000" {
		t.Fatal("snapshot aliases official price")
	}
}
func TestResourceProjectionRejectsWrongIdentityAndInvalidCoefficients(t *testing.T) {
	for _, change := range []func(*ResourceRecord){func(r *ResourceRecord) { r.Report.ResourceID = "32" }, func(r *ResourceRecord) { r.Report.StandardModelID = "12" }, func(r *ResourceRecord) { r.Report.OriginRowID = "999" }, func(r *ResourceRecord) { r.Supplier.ID = "22" }, func(r *ResourceRecord) { r.Resource.Coefficient = "0" }, func(r *ResourceRecord) { r.Resource.Coefficient = "1.0001" }, func(r *ResourceRecord) { r.Resource.Coefficient = "0.68001" }} {
		record := resourceRecord(t, "31", "21", "11", "CN", "0.68")
		change(record)
		if _, err := BuildResourceSnapshot(record); !errors.Is(err, ErrReadContract) {
			t.Fatalf("malformed projection accepted: %v", err)
		}
	}
	record := resourceRecord(t, "31", "21", "11", "CN", "1")
	record.Report = nil
	record.Resource.LatestReportID = ""
	snapshot, err := BuildResourceSnapshot(record)
	if err != nil || snapshot.QuoteReadiness.Eligible || snapshot.Test.Status != "NOT_TESTED" || snapshot.Test.CacheStats.Status != "NOT_TESTED" {
		t.Fatalf("untested became passed or zero: %+v %v", snapshot, err)
	}
}

type resourceReadMemory struct {
	groups [][]*ResourceRecord
	rows   []*ResourceRecord
	total  int64
	query  ResourceListQuery
}

func (s *resourceReadMemory) ListResourceGroups(_ context.Context, q ResourceListQuery) ([][]*ResourceRecord, int64, error) {
	s.query = q
	return s.groups, s.total, nil
}
func (s *resourceReadMemory) ListResourceRows(_ context.Context, q ResourceListQuery) ([]*ResourceRecord, int64, error) {
	s.query = q
	return s.rows, s.total, nil
}
func (s *resourceReadMemory) GetResource(_ context.Context, id string) (*ResourceRecord, error) {
	for _, row := range s.rows {
		if row.Resource.ID == id {
			return row, nil
		}
	}
	return nil, ErrNotFound
}
func TestResourceReaderGroupsAfterFilteringAndPagesModels(t *testing.T) {
	cn1 := resourceRecord(t, "31", "21", "11", "CN", "0.7")
	cn2 := resourceRecord(t, "32", "22", "11", "CN", "0.5")
	cn2.Report.PressurePassed = false
	overseas := resourceRecord(t, "33", "23", "11", "OVERSEAS", "0.1")
	store := &resourceReadMemory{groups: [][]*ResourceRecord{{cn1, cn2, overseas}}, total: 2}
	result, err := NewResourceReader(store).List(context.Background(), ResourceListQuery{Region: "CN", Page: 1, Size: 1})
	if err != nil || result.Total != 2 || len(result.Groups) != 1 {
		t.Fatalf("model pagination lost: %+v %v", result, err)
	}
	group := result.Groups[0]
	if group.ResourceCount != 2 || group.SupplierCount != 2 || group.AvailableCount != 1 || group.CoefficientMin != "0.5000" || group.CoefficientMax != "0.7000" || len(group.Regions) != 1 || group.Regions[0] != "CN" || group.StateCounts["ADMITTED"] != 2 {
		t.Fatalf("group leaked excluded region or counted resource page: %+v", group)
	}
	if !store.query.NullsLast || store.query.TieBreakBy != "id" || store.query.View != "grouped" {
		t.Fatal("query lost stable pagination requirements")
	}
	store.rows = []*ResourceRecord{cn1}
	store.total = 3
	flat, err := NewResourceReader(store).List(context.Background(), ResourceListQuery{View: "flat", Size: 1})
	if err != nil || flat.Total != 3 || len(flat.Resources) != 1 || len(flat.Groups) != 0 {
		t.Fatalf("flat applied group total: %+v %v", flat, err)
	}
}
func TestResourceReaderRejectsBadQueriesAndMissingDependencies(t *testing.T) {
	store := &resourceReadMemory{total: 0}
	for _, query := range []ResourceListQuery{{View: "unknown"}, {Region: "DOMESTIC"}, {Size: 101}, {Page: -1}, {SortBy: "name;delete"}, {View: "flat", SortBy: "supplier_count"}, {SupplierID: "0"}, {StandardModelID: "01"}, {State: "APPROVED"}, {Order: "random"}} {
		if _, err := NewResourceReader(store).List(context.Background(), query); err == nil {
			t.Fatalf("bad query accepted: %+v", query)
		}
	}
	if _, err := NewResourceReader(nil).List(context.Background(), ResourceListQuery{}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("missing store opened: %v", err)
	}
}

func TestResourceReaderRejectsInvalidSearchBeforeStore(t *testing.T) {
	for _, q := range []ResourceListQuery{{Keyword: strings.Repeat("中", 201)}, {Keyword: "a\x00b"}, {Keyword: string([]byte{0xff})}, {Company: strings.Repeat("中", 129)}, {Company: "a\x00b"}} {
		store := &resourceReadMemory{}
		got, err := NewResourceReader(store).List(context.Background(), q)
		var field *FieldError
		if !errors.As(err, &field) || got != nil || store.query.Page != 0 {
			t.Fatalf("invalid search reached store: %+v %v", q, err)
		}
	}
	for _, keyword := range []string{strings.Repeat("中", 201), "a\x00b", string([]byte{0xff})} {
		store := &resourceOptionsMemory{}
		got, err := NewResourceReader(store).Options(context.Background(), "", keyword)
		var field *FieldError
		if !errors.As(err, &field) || got != nil || store.query.Limit != 0 {
			t.Fatalf("invalid options search reached store: %v", err)
		}
	}
	if _, err := NewResourceReader(&resourceReadMemory{}).List(context.Background(), ResourceListQuery{Keyword: strings.Repeat("中", 200), Company: strings.Repeat("中", 128)}); err != nil {
		t.Fatal("limits count characters, not bytes", err)
	}
}

func TestResourceRejectsMalformedOfficialModel(t *testing.T) {
	for _, change := range []func(*ResourceRecord){func(r *ResourceRecord) { r.Model.VersionNo = 0 }, func(r *ResourceRecord) {
		r.Model.Data.PriceItems[0].SourceURL = "https://vendor.example?api_key=secret"
	}} {
		record := resourceRecord(t, "31", "21", "11", "CN", "0.68")
		change(record)
		if _, err := BuildResourceSnapshot(record); !errors.Is(err, ErrReadContract) {
			t.Fatalf("malformed official model leaked into resource: %v", err)
		}
	}
}

func TestResourceMetricsPreserveStringsAndExcludeStaleTargets(t *testing.T) {
	record := resourceRecord(t, "31", "21", "11", "CN", "0.68")
	connection, rps, tps := "0.00", "12345.12345678", "9007199254740993"
	record.Report.Metrics = ResourceReportMetrics{ConnectionMS: &connection, RPS: &rps, TPS: &tps}
	snapshot, err := BuildResourceSnapshot(record)
	if err != nil || snapshot.Test.Metrics.ConnectionMS == nil || *snapshot.Test.Metrics.ConnectionMS != "0.00" || *snapshot.Test.Metrics.RPS != "12345.12345678" || *snapshot.Test.Metrics.TPS != "9007199254740993" {
		t.Fatalf("metrics changed precision or zero: %+v %v", snapshot, err)
	}
	*snapshot.Test.Metrics.RPS = "9"
	if *record.Report.Metrics.RPS != "12345.12345678" {
		t.Fatal("response mutated report")
	}
	record.Report.TargetVersion = 2
	snapshot, err = BuildResourceSnapshot(record)
	if err != nil || snapshot.Test.Metrics.RPS != nil {
		t.Fatalf("stale target metrics reused: %+v %v", snapshot, err)
	}
	record.Report.TargetVersion = 1
	bad := "1e9"
	record.Report.Metrics.TPS = &bad
	if _, err := BuildResourceSnapshot(record); !errors.Is(err, ErrReadContract) {
		t.Fatalf("malformed metric accepted: %v", err)
	}
}

func TestResourceDraftReportUsesOwnRowAndVersion(t *testing.T) {
	record := resourceRecord(t, "31", "21", "11", "CN", "0.68")
	record.Report.ResourceID = ""
	snapshot, err := BuildResourceSnapshot(record)
	if err != nil || !snapshot.QuoteReadiness.Eligible {
		t.Fatalf("draft row report could not bind to created resource: %+v %v", snapshot, err)
	}
	record.Report.OriginRowVersion = 2
	snapshot, err = BuildResourceSnapshot(record)
	if err != nil || snapshot.QuoteReadiness.Eligible || snapshot.Test.Status != "STALE" {
		t.Fatalf("other row version admitted: %+v %v", snapshot, err)
	}
}
