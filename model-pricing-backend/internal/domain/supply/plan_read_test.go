package supply

import (
	"context"
	"testing"
	"time"
)

type planSummaryMemory struct{ record PlanReadRecord }

func (s *planSummaryMemory) GetPlanSummary(context.Context, string) (PlanReadRecord, error) {
	return s.record, nil
}
func TestPlanSummaryUsesInitiatorAndDoesNotExecuteOrBlockFutureDueTime(t *testing.T) {
	plan := &ResourcePlan{ID: "61", ResourceID: "31", Region: "CN", Kind: "OFFLINE", Status: "PENDING", CreatedBy: "7", Revision: 1, ExpectedResourceRevision: 3, DueAt: modelNow.Add(48 * time.Hour), CreatedAt: modelNow, Reason: "供应调整"}
	store := &planSummaryMemory{record: PlanReadRecord{Plan: plan, ResourceID: "31", ResourceRegion: "CN", ResourceRevision: 3, ResourceBuyerID: "8"}}
	viewer, err := NewPlanReader(store).Get(context.Background(), "8", "61")
	if err != nil || viewer.IsInitiator || !viewer.ReadOnly || !viewer.PreconditionMatches || !viewer.DueAt.Equal(plan.DueAt) {
		t.Fatalf("current buyer got initiator rights or future plan was blocked: %+v %v", viewer, err)
	}
	initiator, err := NewPlanReader(store).Get(context.Background(), "7", "61")
	if err != nil || !initiator.IsInitiator || !initiator.ReadOnly || !initiator.Permissions.CanExecute {
		t.Fatalf("initiator attribution lost: %+v %v", initiator, err)
	}
	viewerCanExecute, err := NewPlanReader(store).Get(context.Background(), "8", "61")
	if err != nil || viewerCanExecute.Permissions.CanExecute {
		t.Fatalf("another staff member received plan execution permission: %+v %v", viewerCanExecute, err)
	}
	store.record.Plan.Status = "BLOCKED"
	store.record.ExecutionFailureReason = "RESOURCE_CHANGED"
	blocked, err := NewPlanReader(store).Get(context.Background(), "7", "61")
	if err != nil || blocked.Permissions.CanExecute {
		t.Fatalf("blocked plan remained executable: %+v %v", blocked, err)
	}
	store.record.Plan.Status = "PENDING"
	store.record.ExecutionFailureReason = ""
	store.record.ResourceRevision = 4
	stale, err := NewPlanReader(store).Get(context.Background(), "7", "61")
	if err != nil || stale.PreconditionMatches || len(stale.BlockedReasons) != 1 || plan.Status != "PENDING" {
		t.Fatalf("GET mutated stale plan or hid mismatch: %+v %v", stale, err)
	}
}

func TestPriceChangePlanPermissionsSeparateInitiatorAndAssignedExecutor(t *testing.T) {
	plan := &ResourcePlan{ID: "81", ResourceID: "11", Region: "CN", Kind: "PRICE_CHANGE", Status: "PENDING", CreatedBy: "7", AssigneeID: "8", SupplierModelID: "vendor-model-v1", BeforeSupplierModelID: "vendor-model-v1", AfterSupplierModelID: "vendor-model-v1", Revision: 1, ExpectedResourceRevision: 2, BeforeCoefficient: "0.6800", AfterCoefficient: "0.5000", DueAt: modelNow.Add(time.Hour), CreatedAt: modelNow, Reason: "内部采购调整"}
	store := &planSummaryMemory{record: PlanReadRecord{Plan: plan, ResourceID: "11", ResourceRegion: "CN", ResourceBuyerID: "7", ResourceRevision: 2}}
	initiator, err := NewPlanReader(store).Get(context.Background(), "7", "81")
	if err != nil || !initiator.IsInitiator || !initiator.Permissions.CanRevise || !initiator.Permissions.CanCancel || initiator.Permissions.CanExecute {
		t.Fatalf("wrong initiator permissions: %+v %v", initiator, err)
	}
	executor, err := NewPlanReader(store).Get(context.Background(), "8", "81")
	if err != nil || executor.IsInitiator || executor.Permissions.CanRevise || executor.Permissions.CanCancel || !executor.Permissions.CanExecute {
		t.Fatalf("wrong assigned executor permissions: %+v %v", executor, err)
	}
	other, err := NewPlanReader(store).Get(context.Background(), "9", "81")
	if err != nil || other.Permissions.CanRevise || other.Permissions.CanCancel || other.Permissions.CanExecute {
		t.Fatalf("unrelated staff received plan permissions: %+v %v", other, err)
	}
}
