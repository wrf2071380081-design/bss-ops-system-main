package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func TestResourceStateStoreRejectsUnsafeSchemaAndMissingDependencies(t *testing.T) {
	if s, err := NewResourceStateStore(`public;SELECT 1`, nil); s != nil || err == nil {
		t.Fatal("unsafe schema accepted")
	}
	s, err := NewResourceStateStore("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Current(ctx, nil, "1"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.Current(ctx, nil, "1 OR TRUE"); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("invalid identity accepted", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Current(canceled, nil, "1"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden", err)
	}
	loader := s.FactsLoader(nil, nil)
	if _, err := loader(ctx, (*sql.Tx)(nil), ResourceProjection{ID: "1"}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal(err)
	}
}

func TestResourceControlProtocolRejectsIncompleteAndSecretPayloads(t *testing.T) {
	now := time.Date(2026, 10, 21, 4, 0, 0, 123456789, time.FixedZone("CST", 8*3600))
	input := &supply.Resource{ID: "9007199254740993", SupplierID: "21", StandardModelID: "31", BuyerID: "7", Region: "CN", SupplierModelID: "vendor", Coefficient: "0.68", State: "PENDING_TEST", Revision: 9007199254740993, OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, CreatedAt: now, UpdatedAt: now}
	canonical, err := canonicalStoredResource(input)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Coefficient != "0.6800" || canonical.UpdatedAt.Nanosecond() != 123456000 || canonical.UpdatedAt.Location() != time.UTC || input.Coefficient != "0.68" {
		t.Fatal("canonicalization changed original or lost database precision")
	}
	raw, err := json.Marshal(resourceControlFrom(canonical, "BIND"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"revision":"9007199254740993"`) {
		t.Fatal("bigint version lost string encoding")
	}
	if _, decoded, err := decodeResourceControl(raw); err != nil || *decoded != *canonical {
		t.Fatal("control round trip", err)
	}
	for _, change := range []func(*supply.Resource){
		func(r *supply.Resource) { r.ID = "01" }, func(r *supply.Resource) { r.Revision = 0 }, func(r *supply.Resource) { r.TargetVersion = math.MaxInt64 + 1 },
		func(r *supply.Resource) { r.State = "PLAN_OFFLINE" }, func(r *supply.Resource) { r.EverAdmitted = true }, func(r *supply.Resource) { r.Region = "US" },
		func(r *supply.Resource) { r.FormalConfigID = "91" }, func(r *supply.Resource) { r.LatestReportID = "51" }, func(r *supply.Resource) { r.SupplierModelID = "x\x00y" },
		func(r *supply.Resource) { r.SupplierModelID = strings.Repeat("中", 129) }, func(r *supply.Resource) { r.Coefficient = "0.68001" }, func(r *supply.Resource) { r.Coefficient = "0" },
		func(r *supply.Resource) { r.UpdatedAt = r.CreatedAt.Add(-time.Second) },
	} {
		copy := *canonical
		change(&copy)
		if _, err := canonicalStoredResource(&copy); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("bad stored state accepted %+v %v", copy, err)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(string(raw), `"contract":"supply.resource"`, `"contract":"other"`, 1),
		strings.Replace(string(raw), `"resource":{`, `"resource":{"api_key":"secret",`, 1),
		strings.Replace(string(raw), `"revision":"9007199254740993"`, `"revision":9007199254740993`, 1),
		string(raw) + `{}`, `{}`,
	} {
		if _, r, err := decodeResourceControl([]byte(bad)); !errors.Is(err, supply.ErrReadContract) || r != nil {
			t.Fatal("bad snapshot accepted", err)
		}
	}
}

func TestResourceCreationWorkFailsClosedBeforeResolvingTransaction(t *testing.T) {
	calls := 0
	store, _ := NewResourceStateStore("public", func(context.Context, *sql.Tx, ResourceStateLog) error { return nil })
	resolve := func(context.Context) (*sql.Tx, error) { calls++; return nil, nil }
	work := NewResourceCreationWork(store, resolve, ResourceCreationHooks{})
	if err := work.WithinResourceCreation(context.Background(), func(supply.ResourceCreationTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) || calls != 0 {
		t.Fatal("incomplete creation dependencies reached SQL", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := work.WithinResourceCreation(ctx, nil); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancellation hidden", err)
	}
}

func TestSelectedReportValidityRequiresMatchingStoredSelection(t *testing.T) {
	r := &supply.Resource{ID: "11", LatestReportID: "51", LatestReportSequence: 4, OriginRowVersion: 2, TargetVersion: 3}
	selected := &resourceTestEvidenceData{ReportID: "51", Sequence: 4, OriginRowVersion: 2, TargetVersion: 3, Outcome: "SELECTED", Valid: true}
	if !selectedReportEvidenceMatches(r, selected) {
		t.Fatal("matching B-validated selection was rejected")
	}
	for _, change := range []func(*resourceTestEvidenceData){
		func(e *resourceTestEvidenceData) { e.Valid = false },
		func(e *resourceTestEvidenceData) { e.ReportID = "52" },
		func(e *resourceTestEvidenceData) { e.Sequence++ },
		func(e *resourceTestEvidenceData) { e.TargetVersion++ },
		func(e *resourceTestEvidenceData) { e.Outcome = "IGNORED_OLDER" },
	} {
		copy := *selected
		change(&copy)
		if selectedReportEvidenceMatches(r, &copy) {
			t.Fatalf("unmatched or invalid evidence accepted: %+v", copy)
		}
	}
	if selectedReportEvidenceMatches(r, nil) {
		t.Fatal("missing B evidence was treated as valid")
	}
}
