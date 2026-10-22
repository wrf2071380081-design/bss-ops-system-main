package supply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type admissionMemory struct {
	record                                            *ResourceRecord
	proof                                             *AdmissionProof
	effects                                           []ResourceAdmissionEffect
	rowAdmitted                                       bool
	failSave, failRow, failLog, failEvent, failCommit bool
	proofErr                                          error
}

func (f *admissionMemory) WithinAdmission(ctx context.Context, run func(ResourceAdmissionTransaction) error) error {
	copy := *f
	record := *f.record
	r := *f.record.Resource
	record.Resource = &r
	copy.record = &record
	p := *f.proof
	copy.proof = &p
	copy.effects = append([]ResourceAdmissionEffect{}, f.effects...)
	if err := run(&copy); err != nil {
		return err
	}
	if f.failCommit {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *admissionMemory) LockAdmission(context.Context, string) (*ResourceRecord, *AdmissionProof, error) {
	return f.record, f.proof, f.proofErr
}
func (f *admissionMemory) SaveAdmittedResource(_ context.Context, r *Resource, expected uint64) error {
	if f.failSave {
		return errors.New("save failed")
	}
	if f.record.Resource.Revision != expected {
		return ErrRevisionConflict
	}
	copy := *r
	f.record.Resource = &copy
	return nil
}
func (f *admissionMemory) AppendAdmissionEffect(_ context.Context, effect ResourceAdmissionEffect) error {
	f.rowAdmitted = true
	if f.failRow {
		return errors.New("B row failed")
	}
	f.effects = append(f.effects, effect)
	if f.failLog {
		return errors.New("log failed")
	}
	if f.failEvent {
		return errors.New("outbox failed")
	}
	return nil
}
func admissionStore(t *testing.T) *admissionMemory {
	r, p, _ := admissionFixture(t)
	return &admissionMemory{record: r, proof: p}
}
func TestAdmissionServiceStagesResourceRowAndEffectTogether(t *testing.T) {
	f := admissionStore(t)
	_, _, cmd := admissionFixture(t)
	result, err := NewResourceAdmissionService(f, func() time.Time { return modelNow }).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "完成接入", RequestID: "admit-11"}, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "ADMITTED" || len(f.effects) != 1 || !f.rowAdmitted || f.effects[0].Proof.NodeID != "82" || f.effects[0].Proof.FlowRevision != 3 || f.effects[0].Before.State != "PENDING_ACCESS" || f.effects[0].After.Revision != 2 || f.effects[0].CompletionNote != cmd.CompletionNote || f.effects[0].EventType != "RESOURCE_ADMITTED" {
		t.Fatalf("bad admission effect %+v %+v", result, f)
	}
	result.State = "OFFLINE"
	if f.record.Resource.State != "ADMITTED" {
		t.Fatal("returned pointer aliases stored resource")
	}
}
func TestAdmissionRejectsInvalidCompletionTextBeforeWork(t *testing.T) {
	for _, note := range []string{"配置\x00记录", string([]byte{0xff})} {
		store := admissionStore(t)
		_, _, cmd := admissionFixture(t)
		cmd.CompletionNote = note
		got, err := NewResourceAdmissionService(store, nil).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "完成接入", RequestID: "completion-text"}, cmd)
		var field *FieldError
		if !errors.As(err, &field) || field.Path != "completion_note" || got != nil || store.rowAdmitted || len(store.effects) != 0 {
			t.Fatalf("invalid completion text reached work: %v", err)
		}
	}
}
func TestAdmissionServiceRollsBackResourceAndBRowOnAnyFailure(t *testing.T) {
	for _, kind := range []string{"save", "row", "log", "event", "commit", "dependency"} {
		f := admissionStore(t)
		switch kind {
		case "save":
			f.failSave = true
		case "row":
			f.failRow = true
		case "log":
			f.failLog = true
		case "event":
			f.failEvent = true
		case "commit":
			f.failCommit = true
		case "dependency":
			f.proofErr = ErrDependencyUnavailable
		}
		_, _, cmd := admissionFixture(t)
		result, err := NewResourceAdmissionService(f, nil).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "完成接入", RequestID: "trace"}, cmd)
		if err == nil || result != nil || f.record.Resource.State != "PENDING_ACCESS" || f.record.Resource.Revision != 1 || f.rowAdmitted || len(f.effects) != 0 {
			t.Fatalf("%s partial commit %+v %v %+v", kind, result, err, f)
		}
		if kind == "dependency" {
			if !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatalf("dependency changed: %v", err)
			}
		} else if !strings.Contains(err.Error(), map[string]string{"save": "save failed", "row": "B row failed", "log": "log failed", "event": "outbox failed", "commit": "commit failed"}[kind]) {
			t.Fatalf("wrong failure path %s: %v", kind, err)
		}
	}
}

func TestAdmissionServiceRejectsInvalidTraceAndCanceledRequestBeforeTransaction(t *testing.T) {
	f := admissionStore(t)
	_, _, cmd := admissionFixture(t)
	svc := NewResourceAdmissionService(f, nil)
	for _, meta := range []WriteMeta{{ActorID: "8", Reason: "接入", RequestID: strings.Repeat("a", 65)}, {ActorID: "8", Reason: strings.Repeat("界", 501), RequestID: "trace"}, {ActorID: "0", Reason: "接入", RequestID: "trace"}} {
		if r, err := svc.Admit(context.Background(), meta, cmd); r != nil || err == nil {
			t.Fatal("invalid meta admitted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := svc.Admit(ctx, WriteMeta{ActorID: "8", Reason: "接入", RequestID: "trace"}, cmd); r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request %+v %v", r, err)
	}
	if f.rowAdmitted || f.record.Resource.Revision != 1 {
		t.Fatal("invalid input mutated resource")
	}
	if _, err := NewResourceAdmissionService(nil, nil).Admit(context.Background(), WriteMeta{}, cmd); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatal(err)
	}
}
func TestAdmissionServiceDoesNotAdmitOtherResourcesFromBatchPass(t *testing.T) {
	for i := 0; i < 3; i++ {
		f := admissionStore(t)
		f.record.Report.ConnectivityPassed = i == 0
		f.record.Report.PressurePassed = i == 0
		_, _, cmd := admissionFixture(t)
		result, err := NewResourceAdmissionService(f, nil).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "逐行接入", RequestID: "trace"}, cmd)
		if i == 0 {
			if err != nil || result == nil {
				t.Fatalf("passed resource blocked %v", err)
			}
		} else if err == nil || result != nil || f.rowAdmitted || f.record.Resource.State != "PENDING_ACCESS" {
			t.Fatal("batch pass admitted untested resource")
		}
	}
}
