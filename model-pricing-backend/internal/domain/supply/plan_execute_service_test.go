package supply

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type planExecuteFixture struct {
	plan                                            *StatePlan
	record                                          *ResourceRecord
	effects                                         []PlanExecutionEffect
	failPlan, failResource, failEffects, failCommit bool
}

func (f *planExecuteFixture) WithinPlanExecution(ctx context.Context, fn func(PlanExecutionTransaction) error) error {
	copy := *f
	copy.plan = f.plan.copy()
	record := *f.record
	r := *record.Resource
	record.Resource = &r
	copy.record = &record
	copy.effects = append([]PlanExecutionEffect{}, f.effects...)
	if err := fn(&copy); err != nil {
		return err
	}
	if f.failCommit {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *planExecuteFixture) LockPlanExecution(_ context.Context, id string) (*StatePlan, *ResourceRecord, error) {
	if f.plan.ID != id {
		return nil, nil, ErrNotFound
	}
	return f.plan, f.record, nil
}
func (f *planExecuteFixture) SaveExecutedPlan(_ context.Context, p *StatePlan, expected uint64) error {
	if f.failPlan {
		return errors.New("plan failed")
	}
	if f.plan.Revision != expected {
		return ErrRevisionConflict
	}
	f.plan = p.copy()
	return nil
}
func (f *planExecuteFixture) SavePlanResource(_ context.Context, r *Resource, expected uint64) error {
	if f.failResource {
		return errors.New("resource failed")
	}
	if f.record.Resource.Revision != expected {
		return ErrRevisionConflict
	}
	copy := *r
	f.record.Resource = &copy
	return nil
}
func (f *planExecuteFixture) AppendPlanExecutionEffect(_ context.Context, e PlanExecutionEffect) error {
	if f.failEffects {
		return errors.New("effect failed")
	}
	f.effects = append(f.effects, e)
	return nil
}
func planExecutionStore(t *testing.T) *planExecuteFixture {
	record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	p, err := NewStatePlan("81", "7", record.Resource, "OFFLINE", modelNow, "维护", modelNow)
	if err != nil {
		t.Fatal(err)
	}
	return &planExecuteFixture{plan: p, record: record}
}
func TestPlanExecutionTransactionCommitsSuccessAndAllEffects(t *testing.T) {
	f := planExecutionStore(t)
	f.record.Resource.BuyerID = "8"
	result, err := NewPlanExecutionService(f, func() time.Time { return modelNow }).Execute(context.Background(), WriteMeta{ActorID: "7", RequestID: "trace", Reason: "实际完成"}, "81", 1, true, "依据")
	if err != nil || result == nil || result.Plan.Status != "EXECUTED" || f.plan.Status != "EXECUTED" || f.record.Resource.State != "OFFLINE" || len(f.effects) != 1 || f.effects[0].EventType != "RESOURCE_OFFLINED" || f.effects[0].AffectedStatus != "PENDING" {
		t.Fatalf("incomplete success %+v %v", f, err)
	}
}
func TestPlanExecutionCommitsBlockedBeforeReturningConflict(t *testing.T) {
	f := planExecutionStore(t)
	f.record.Resource.Revision++
	result, err := NewPlanExecutionService(f, nil).Execute(context.Background(), WriteMeta{ActorID: "7", RequestID: "trace", Reason: "实际完成"}, "81", 1, true, "保留失败依据")
	if err != nil || result == nil || !result.Conflict || result.Plan.Status != "BLOCKED" || f.plan.Status != "BLOCKED" || f.record.Resource.State != "ADMITTED" || len(f.effects) != 1 || f.effects[0].EventType != "" || f.effects[0].FailureReason != "RESOURCE_CHANGED" || f.effects[0].CompletionNote != "保留失败依据" {
		t.Fatalf("blocked lost %+v %v", f, err)
	}
}
func TestPlanExecutionFailureRollsBackSuccessOrBlocked(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		for _, fail := range []string{"plan", "resource", "effects", "commit"} {
			if blocked && fail == "resource" {
				continue
			}
			t.Run(fmt.Sprintf("blocked=%v/%s", blocked, fail), func(t *testing.T) {
				f := planExecutionStore(t)
				if blocked {
					f.record.Resource.Revision++
				}
				before := *f.record.Resource
				switch fail {
				case "plan":
					f.failPlan = true
				case "resource":
					f.failResource = true
				case "effects":
					f.failEffects = true
				case "commit":
					f.failCommit = true
				}
				result, err := NewPlanExecutionService(f, nil).Execute(context.Background(), WriteMeta{ActorID: "7", RequestID: "trace", Reason: "实际完成"}, "81", 1, true, "依据")
				if err == nil || result != nil || f.plan.Status != "PENDING" || *f.record.Resource != before || len(f.effects) != 0 {
					t.Fatalf("partial commit %+v %v", f, err)
				}
			})
		}
	}
}
func TestPlanExecutionRejectsNonInitiatorAndInvalidConfirmationWithoutWriting(t *testing.T) {
	for _, kind := range []string{"actor", "revision", "confirm", "note"} {
		f := planExecutionStore(t)
		actor := "7"
		revision := uint64(1)
		confirm := true
		note := "依据"
		switch kind {
		case "actor":
			actor = "8"
		case "revision":
			revision = 2
		case "confirm":
			confirm = false
		case "note":
			note = " "
		}
		result, err := NewPlanExecutionService(f, nil).Execute(context.Background(), WriteMeta{ActorID: actor, RequestID: "trace", Reason: "实际完成"}, "81", revision, confirm, note)
		if err == nil || result != nil || f.plan.Status != "PENDING" || len(f.effects) != 0 {
			t.Fatalf("bad rejection %s %+v %v", kind, f, err)
		}
	}
}
