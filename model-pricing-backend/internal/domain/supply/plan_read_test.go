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
