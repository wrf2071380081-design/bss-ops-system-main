package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stateFixture struct {
	record                  ResourceRecord
	effects                 []ResourceStateEffect
	failEffects, failCommit bool
}

func (f *stateFixture) WithinResourceState(ctx context.Context, fn func(ResourceStateTransaction) error) error {
	copy := *f
	r := *f.record.Resource
	copy.record.Resource = &r
	copy.effects = append([]ResourceStateEffect{}, f.effects...)
	if err := fn(&copy); err != nil {
		return err
	}
	if f.failCommit {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *stateFixture) ResourceStateForUpdate(context.Context, string) (*ResourceRecord, error) {
	return &f.record, nil
}
func (f *stateFixture) UpdateResourceState(_ context.Context, r *Resource, expected uint64) error {
	if f.record.Resource.Revision != expected {
		return ErrRevisionConflict
	}
	copy := *r
	f.record.Resource = &copy
	return nil
}
func (f *stateFixture) AppendResourceStateEffect(_ context.Context, e ResourceStateEffect) error {
	if f.failEffects {
		return errors.New("notice/log/outbox failed")
	}
	f.effects = append(f.effects, e)
	return nil
}
func offlineStore() *stateFixture {
	return &stateFixture{record: ResourceRecord{Resource: &Resource{ID: "11", BuyerID: "7", Region: "CN", State: "ADMITTED", Revision: 2, EverAdmitted: true, TargetVersion: 4, LatestReportID: "51", UpdatedAt: modelNow}}}
}
func stateMeta() WriteMeta {
	return WriteMeta{ActorID: "7", Reason: "供应维护", RequestID: "trace"}
}
func stateCommand() ResourceStateCommand {
	return ResourceStateCommand{ResourceID: "11", ExpectedRevision: 2, Action: "OFFLINE", ExternalConfirmed: true, CompletionNote: "已在国内平台完成下架"}
}
func TestResourceOfflineRecordsActualStateAndPendingImpact(t *testing.T) {
	f := offlineStore()
	now := modelNow.Add(time.Hour)
	result, err := NewResourceStateService(f, func() time.Time { return now }).Change(context.Background(), stateMeta(), stateCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "OFFLINE" || result.Revision != 3 || !result.EverAdmitted || result.TargetVersion != 4 || result.LatestReportID != "51" || len(f.effects) != 1 {
		t.Fatalf("bad state %+v", result)
	}
	e := f.effects[0]
	if e.EventType != "RESOURCE_OFFLINED" || e.AffectedStatus != "PENDING" || e.Before.State != "ADMITTED" || e.After.State != "OFFLINE" || e.CompletionNote == "" {
		t.Fatalf("incomplete effects %+v", e)
	}
	result.State = "ADMITTED"
	if f.record.Resource.State != "OFFLINE" {
		t.Fatal("aliased state")
	}
}
func TestResourceStateRejectsUnauthorizedUnconfirmedAndStale(t *testing.T) {
	for _, kind := range []string{"actor", "confirm", "note", "revision", "never", "pending", "repeat", "admit"} {
		t.Run(kind, func(t *testing.T) {
			f := offlineStore()
			meta := stateMeta()
			cmd := stateCommand()
			switch kind {
			case "actor":
				meta.ActorID = "8"
			case "confirm":
				cmd.ExternalConfirmed = false
			case "note":
				cmd.CompletionNote = " "
			case "revision":
				cmd.ExpectedRevision = 1
			case "never":
				f.record.Resource.EverAdmitted = false
			case "pending":
				f.record.Resource.State = "PENDING_ACCESS"
			case "repeat":
				f.record.Resource.State = "OFFLINE"
			case "admit":
				cmd.Action = "ADMIT"
			}
			before := *f.record.Resource
			r, err := NewResourceStateService(f, nil).Change(context.Background(), meta, cmd)
			if err == nil || r != nil || *f.record.Resource != before || len(f.effects) != 0 {
				t.Fatalf("partial change %+v %v", f, err)
			}
		})
	}
}
func TestResourceStateEffectsAndCommitFailureRollback(t *testing.T) {
	for _, kind := range []string{"effects", "commit"} {
		t.Run(kind, func(t *testing.T) {
			f := offlineStore()
			f.failEffects = kind == "effects"
			f.failCommit = kind == "commit"
			r, err := NewResourceStateService(f, nil).Change(context.Background(), stateMeta(), stateCommand())
			if err == nil || r != nil || f.record.Resource.Revision != 2 || len(f.effects) != 0 {
				t.Fatalf("partial success %+v %v", f, err)
			}
		})
	}
}
func TestResourceOnlineFailsClosedWithoutCurrentReadiness(t *testing.T) {
	f := offlineStore()
	f.record.Resource.State = "OFFLINE"
	cmd := stateCommand()
	cmd.Action = "ONLINE"
	r, err := NewResourceStateService(f, nil).Change(context.Background(), stateMeta(), cmd)
	if err == nil || r != nil || f.record.Resource.State != "OFFLINE" || len(f.effects) != 0 {
		t.Fatalf("online without facts %+v %v", f, err)
	}
}

func TestResourceOnlineRechecksModelConfigAndOwnEvidence(t *testing.T) {
	for _, kind := range []string{"ready", "model", "config", "report", "target"} {
		t.Run(kind, func(t *testing.T) {
			record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
			record.Resource.State = "OFFLINE"
			record.Resource.BuyerID = "7"
			record.Resource.Revision = 2
			switch kind {
			case "model":
				record.Model.VerifyStatus = "UNVERIFIED"
			case "config":
				record.Config.ConfirmedRevision = 0
			case "report":
				record.Report.PressurePassed = false
			case "target":
				record.Resource.TargetVersion++
			}
			f := &stateFixture{record: *record}
			cmd := stateCommand()
			cmd.Action = "ONLINE"
			r, err := NewResourceStateService(f, nil).Change(context.Background(), stateMeta(), cmd)
			if kind == "ready" {
				if err != nil || r.State != "ADMITTED" || r.Revision != 3 || len(f.effects) != 1 || f.effects[0].EventType != "RESOURCE_ONLINED" {
					t.Fatalf("failed ready online %+v %v", f, err)
				}
			} else if err == nil || r != nil || f.record.Resource.State != "OFFLINE" || len(f.effects) != 0 {
				t.Fatalf("ignored blocker %s %+v %v", kind, f, err)
			}
		})
	}
}
