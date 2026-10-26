package supply

import (
	"errors"
	"testing"
	"time"
)

func TestInternalPriceChangePlanAppliesCoefficientOnlyForAssignedExecutor(t *testing.T) {
	record := resourceRecord(t, "11", "21", "31", "CN", "0.6800")
	record.Resource.Revision = 2
	record.Resource.EverAdmitted = true
	record.Resource.State = "ADMITTED"
	plan, err := NewPriceChangePlan("81", "9", "8", "vendor-model-v1", record.Resource, "0.5000", modelNow.Add(time.Hour), "内部采购调整", modelNow)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := plan.ExecuteStatePlan("9", 1, record, true, "供应商后台已调整", modelNow.Add(2*time.Hour)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("creator must not execute another person's assigned plan: %v", err)
	}
	result, err := plan.ExecuteStatePlan("8", 1, record, true, "供应商后台已调整", modelNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Status != "EXECUTED" || result.Plan.ExecutedBy != "8" || result.Plan.SupplierModelID != "vendor-model-v1" || result.Resource.Coefficient != "0.5000" || result.Resource.Revision != 3 {
		t.Fatalf("price change was not recorded: plan=%+v resource=%+v", result.Plan.ResourcePlan, result.Resource)
	}
	if result.Resource.State != "ADMITTED" || result.Resource.TargetVersion != record.Resource.TargetVersion || result.Resource.LatestReportID != record.Resource.LatestReportID || result.Resource.FormalConfigID != record.Resource.FormalConfigID {
		t.Fatalf("price change rewrote unrelated resource facts: %+v", result.Resource)
	}
	if plan.Status != "PENDING" || record.Resource.Coefficient != "0.6800" {
		t.Fatal("proposal mutated persisted inputs")
	}
}

func TestInternalPriceChangePlanBlocksStaleResourceWithoutChangingCoefficient(t *testing.T) {
	record := resourceRecord(t, "11", "21", "31", "CN", "0.6800")
	record.Resource.Revision = 2
	plan, err := NewPriceChangePlan("81", "9", "8", "vendor-model-v1", record.Resource, "0.5000", modelNow, "内部采购调整", modelNow)
	if err != nil {
		t.Fatal(err)
	}
	record.Resource.Revision++
	change, err := plan.ExecuteStatePlan("8", 1, record, true, "供应商后台已调整", modelNow.Add(time.Hour))
	if !errors.Is(err, ErrRevisionConflict) || change == nil || change.Plan.Status != "BLOCKED" || change.Resource != nil || change.FailureReason != "RESOURCE_CHANGED" {
		t.Fatalf("stale price plan should commit a blocked result: %+v %v", change, err)
	}
}

func TestInternalPriceChangePlanRejectsSupplierModelIDThatDoesNotMatchResource(t *testing.T) {
	record := resourceRecord(t, "11", "21", "31", "CN", "0.6800")
	record.Resource.Revision = 2
	plan, err := NewPriceChangePlan("81", "9", "8", "vendor-model-v2", record.Resource, "0.6800", modelNow.Add(time.Hour), "内部采购调整", modelNow)
	if err != nil || plan.BeforeSupplierModelID != "vendor-model-v1" || plan.AfterSupplierModelID != "vendor-model-v2" || plan.BeforeCoefficient != plan.AfterCoefficient {
		t.Fatalf("model ID-only price plan was not snapshotted: %+v %v", plan, err)
	}
	result, err := plan.ExecuteStatePlan("8", 1, record, true, "外部模型标识已调整", modelNow.Add(2*time.Hour))
	if err != nil || result.Resource.SupplierModelID != "vendor-model-v2" || result.Resource.TargetVersion != 2 || result.Resource.Revision != 3 {
		t.Fatalf("execution did not apply target model ID and advance report target: %+v %v", result, err)
	}
	updated := *record
	updated.Resource = result.Resource
	snapshot, err := BuildResourceSnapshot(&updated)
	if err != nil || snapshot.Test.Status != "STALE" || snapshot.QuoteReadiness.Eligible {
		t.Fatalf("report for the old supplier model ID remained quote-ready: %+v %v", snapshot, err)
	}
}
