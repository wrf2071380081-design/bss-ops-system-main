package supply

import (
	"errors"
	"testing"
	"time"
)

func lifecycleResource() *Resource {
	return &Resource{ID: "11", BuyerID: "7", Region: "CN", State: "ADMITTED", EverAdmitted: true, Revision: 2}
}
func TestStatePlanCreationRecordsSnapshotWithoutChangingResource(t *testing.T) {
	r := lifecycleResource()
	before := *r
	due := modelNow.Add(time.Hour)
	p, err := NewStatePlan("81", "7", r, "OFFLINE", due, " 维护 ", modelNow)
	if err != nil {
		t.Fatal(err)
	}
	if *r != before || p.Status != "PENDING" || p.CreatedBy != "7" || p.ExpectedResourceRevision != 2 || p.Revision != 1 || p.Reason != "维护" || !p.DueAt.Equal(due) {
		t.Fatalf("invalid plan %+v", p)
	}
	if _, err := NewStatePlan("81", "8", r, "OFFLINE", due, "维护", modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unbound creation %v", err)
	}
	if _, err := NewStatePlan("81", "7", r, "ONLINE", due, "维护", modelNow); err == nil {
		t.Fatal("online plan for admitted resource")
	}
}
func TestStatePlanRevisionAndCancellationUseOriginalInitiator(t *testing.T) {
	r := lifecycleResource()
	p, _ := NewStatePlan("81", "7", r, "OFFLINE", modelNow.Add(time.Hour), "维护", modelNow)
	r.BuyerID = "8"
	if _, err := p.RevisedStatePlan("8", 1, modelNow.Add(2*time.Hour), "修改"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer impersonation %v", err)
	}
	changed, err := p.RevisedStatePlan("7", 1, modelNow.Add(2*time.Hour), "修改")
	if err != nil || changed.Revision != 2 || p.Revision != 1 || changed.ExpectedResourceRevision != 2 {
		t.Fatalf("bad revision %+v %v", changed, err)
	}
	if _, err := changed.CanceledStatePlan("7", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale cancel %v", err)
	}
	canceled, err := changed.CanceledStatePlan("7", 2)
	if err != nil || canceled.Status != "CANCELED" || canceled.Revision != 3 || changed.Status != "PENDING" {
		t.Fatalf("bad cancel %+v %v", canceled, err)
	}
	if _, err := canceled.RevisedStatePlan("7", 3, modelNow, "again"); err == nil {
		t.Fatal("changed canceled plan")
	}
}
func TestStatePlanExecutionBeforeDueRequiresInitiatorAndExternalEvidence(t *testing.T) {
	record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	record.Resource.Revision = 2
	p, _ := NewStatePlan("81", "7", record.Resource, "OFFLINE", modelNow.Add(24*time.Hour), "维护", modelNow)
	record.Resource.BuyerID = "8"
	change, err := p.ExecuteStatePlan("7", 1, record, true, " 平台操作依据 ", modelNow.Add(time.Hour))
	if err != nil || change.Plan.Status != "EXECUTED" || change.Plan.Revision != 2 || change.Plan.ExecutedAt == nil || change.Resource.State != "OFFLINE" || change.Resource.Revision != 3 || change.CompletionNote != "平台操作依据" {
		t.Fatalf("bad execute %+v %v", change, err)
	}
	if p.Status != "PENDING" || record.Resource.State != "ADMITTED" {
		t.Fatal("mutated input before persistence")
	}
	if _, err := p.ExecuteStatePlan("8", 1, record, true, "依据", modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other execution %v", err)
	}
	if _, err := p.ExecuteStatePlan("7", 1, record, false, "依据", modelNow); err == nil {
		t.Fatal("unconfirmed execute")
	}
	if _, err := p.ExecuteStatePlan("7", 1, record, true, " ", modelNow); err == nil {
		t.Fatal("empty evidence")
	}
}
func TestStatePlanResourceConflictReturnsBlockedProposal(t *testing.T) {
	r := lifecycleResource()
	p, _ := NewStatePlan("81", "7", r, "OFFLINE", modelNow, "维护", modelNow)
	r.Revision = 3
	change, err := p.ExecuteStatePlan("7", 1, &ResourceRecord{Resource: r}, true, "依据", modelNow)
	if !errors.Is(err, ErrRevisionConflict) || change == nil || change.Plan.Status != "BLOCKED" || change.Resource != nil || p.Status != "PENDING" {
		t.Fatalf("no blocked proposal %+v %v", change, err)
	}
}
func TestStatePlanOnlineRechecksCurrentFacts(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
		record.Resource.State = "OFFLINE"
		p, _ := NewStatePlan("81", "7", record.Resource, "ONLINE", modelNow, "恢复", modelNow)
		if blocked {
			record.Report.PressurePassed = false
		}
		change, err := p.ExecuteStatePlan("7", 1, record, true, "已完成上架", modelNow)
		if blocked {
			if !errors.Is(err, ErrRevisionConflict) || change == nil || change.Plan.Status != "BLOCKED" || change.Resource != nil {
				t.Fatalf("ignored report %+v %v", change, err)
			}
		} else if err != nil || change.Resource.State != "ADMITTED" || change.Plan.Status != "EXECUTED" {
			t.Fatalf("ready failed %+v %v", change, err)
		}
	}
}
