package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"testing"
	"time"
)

func TestPlanStoreRejectsInvalidDependencies(t *testing.T) {
	if _, err := NewPlanStore("public;drop", nil, nil, nil); err == nil {
		t.Fatal("unsafe schema accepted")
	}
	s, err := NewPlanStore("public", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPlanSummary(context.Background(), "1"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing snapshot runner accepted", err)
	}
	if err := s.WithinPlanMaintenance(context.Background(), func(supply.PlanMaintenanceTransaction) error {
		t.Fatal("callback invoked without dependencies")
		return nil
	}); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing write dependencies accepted", err)
	}
}

func TestCanonicalStatePlanUpgradesLegacyPricePlanForecast(t *testing.T) {
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	resource := supply.Resource{
		ID: "11", SupplierID: "21", StandardModelID: "31", Region: "CN", SupplierModelID: "vendor-model-v1",
		Coefficient: "0.6800", BuyerID: "7", State: "ADMITTED", EverAdmitted: true, Revision: 2,
		OriginRowID: "41", OriginRowVersion: 1, TargetVersion: 1,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	plan := &supply.StatePlan{
		ResourcePlan: supply.ResourcePlan{
			ID: "81", ResourceID: resource.ID, Region: resource.Region, Kind: "PRICE_CHANGE", Status: "PENDING",
			CreatedBy: "7", AssigneeID: "8", SupplierModelID: resource.SupplierModelID,
			Revision: 1, ExpectedResourceRevision: resource.Revision, BeforeCoefficient: "0.6800", AfterCoefficient: "0.5000",
			DueAt: now.Add(time.Hour), CreatedAt: now, Reason: "历史计划兼容",
		},
		BeforeSnapshot: resource, AfterSnapshot: resource,
	}
	canonical, err := canonicalStatePlan(plan)
	if err != nil || canonical.BeforeSupplierModelID != resource.SupplierModelID || canonical.AfterSupplierModelID != resource.SupplierModelID || canonical.AfterSnapshot.Coefficient != "0.5000" {
		t.Fatalf("legacy plan forecast was not reconstructed: %+v %v", canonical, err)
	}
}
