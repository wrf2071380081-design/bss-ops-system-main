package supply

import (
	"context"
	"errors"
	"testing"
	"time"
)

type coefficientFixture struct {
	resource                       Resource
	proof                          CoefficientSourceProof
	operations                     []ResourceCoefficientOperation
	events                         []ResourceCoefficientEvent
	failLog, failEvent, failCommit bool
}

func (f *coefficientFixture) WithinCoefficient(ctx context.Context, run func(ResourceCoefficientTransaction) error) error {
	copy := *f
	copy.operations = append([]ResourceCoefficientOperation{}, f.operations...)
	copy.events = append([]ResourceCoefficientEvent{}, f.events...)
	if err := run(&copy); err != nil {
		return err
	}
	if f.failCommit {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *coefficientFixture) ResourceForUpdate(context.Context, string) (*Resource, error) {
	r := f.resource
	return &r, nil
}
func (f *coefficientFixture) VerifyCoefficientSource(context.Context, CoefficientSource, string, *Resource) (CoefficientSourceProof, error) {
	return f.proof, nil
}
func (f *coefficientFixture) UpdateCoefficient(_ context.Context, r *Resource, expected uint64) error {
	if f.resource.Revision != expected {
		return ErrRevisionConflict
	}
	f.resource = *r
	return nil
}
func (f *coefficientFixture) AppendCoefficientOperation(_ context.Context, op ResourceCoefficientOperation) error {
	if f.failLog {
		return errors.New("log failed")
	}
	f.operations = append(f.operations, op)
	return nil
}
func (f *coefficientFixture) AppendCoefficientEvent(_ context.Context, event ResourceCoefficientEvent) error {
	if f.failEvent {
		return errors.New("outbox failed")
	}
	f.events = append(f.events, event)
	return nil
}
func coefficientStore() *coefficientFixture {
	return &coefficientFixture{resource: Resource{ID: "11", SupplierID: "21", StandardModelID: "31", Region: "CN", BuyerID: "7", Coefficient: "0.6800", Revision: 2, State: "ADMITTED", EverAdmitted: true, TargetVersion: 4, LatestReportID: "51", UpdatedAt: modelNow}, proof: CoefficientSourceProof{Authorized: true, ResourceID: "11", ExpectedResourceRevision: 2, Coefficient: "0.5000"}}
}
func coefficientSource() CoefficientSource {
	return CoefficientSource{Kind: "SUBMISSION", ID: "81", Revision: 3}
}
func TestResourceCoefficientUsesVerifiedSourceAndPreservesTestTarget(t *testing.T) {
	f := coefficientStore()
	now := modelNow.Add(time.Hour)
	result, err := NewResourceCoefficientService(f, func() time.Time { return now }).Change(context.Background(), WriteMeta{ActorID: "7", Reason: "已确认调价", RequestID: "trace"}, "11", 2, "0.5", coefficientSource())
	if err != nil {
		t.Fatal(err)
	}
	if result.Coefficient != "0.5000" || result.Revision != 3 || result.TargetVersion != 4 || result.LatestReportID != "51" || result.State != "ADMITTED" || !result.EverAdmitted || len(f.events) != 1 || len(f.operations) != 1 {
		t.Fatalf("bad result %+v %+v", result, f)
	}
	if f.events[0].BeforeCoefficient != "0.6800" || f.events[0].AfterCoefficient != "0.5000" || f.operations[0].Source.ID != "81" {
		t.Fatal("missing history")
	}
	result.Coefficient = "1"
	if f.resource.Coefficient != "0.5000" {
		t.Fatal("aliased result")
	}
}
func TestResourceCoefficientRejectsUnverifiedOrMismatchedSource(t *testing.T) {
	for _, mutate := range []func(*coefficientFixture){func(f *coefficientFixture) { f.proof.Authorized = false }, func(f *coefficientFixture) { f.proof.ResourceID = "12" }, func(f *coefficientFixture) { f.proof.ExpectedResourceRevision = 1 }, func(f *coefficientFixture) { f.proof.Coefficient = "0.6" }} {
		f := coefficientStore()
		mutate(f)
		_, err := NewResourceCoefficientService(f, nil).Change(context.Background(), WriteMeta{ActorID: "8", Reason: "调价", RequestID: "trace"}, "11", 2, "0.5", coefficientSource())
		if err == nil || f.resource.Revision != 2 || len(f.events) != 0 {
			t.Fatalf("unauthorized mutation: %+v %v", f, err)
		}
	}
}
func TestResourceCoefficientNoopLogsWithoutRevisionOrEvent(t *testing.T) {
	f := coefficientStore()
	f.proof.Coefficient = "0.68"
	r, err := NewResourceCoefficientService(f, nil).Change(context.Background(), WriteMeta{ActorID: "7", Reason: "重复确认", RequestID: "trace"}, "11", 2, "0.6800", coefficientSource())
	if err != nil || r.Revision != 2 || !r.UpdatedAt.Equal(modelNow) || len(f.events) != 0 || len(f.operations) != 1 || f.operations[0].Action != "CHANGE_COEFFICIENT_NOOP" {
		t.Fatalf("bad noop %+v %v", f, err)
	}
}

func TestResourceCoefficientPlanInitiatorCanDifferFromCurrentBuyer(t *testing.T) {
	f := coefficientStore()
	source := coefficientSource()
	source.Kind = "PLAN"
	result, err := NewResourceCoefficientService(f, nil).Change(context.Background(), WriteMeta{ActorID: "8", Reason: "按已确认计划登记", RequestID: "trace"}, "11", 2, "0.5", source)
	if err != nil || result.BuyerID != "7" || f.operations[0].ActorID != "8" || f.events[0].Source.Kind != "PLAN" {
		t.Fatalf("plan authority replaced by current buyer: %+v %v", f, err)
	}
}
func TestResourceCoefficientFailureRollsBackAllEffects(t *testing.T) {
	for _, kind := range []string{"log", "event", "commit", "revision", "input", "manual"} {
		t.Run(kind, func(t *testing.T) {
			f := coefficientStore()
			expected := uint64(2)
			amount := "0.5"
			source := coefficientSource()
			switch kind {
			case "log":
				f.failLog = true
			case "event":
				f.failEvent = true
			case "commit":
				f.failCommit = true
			case "revision":
				expected = 1
			case "input":
				amount = "0.00001"
			case "manual":
				source.Kind = "MANUAL"
			}
			result, err := NewResourceCoefficientService(f, nil).Change(context.Background(), WriteMeta{ActorID: "7", Reason: "调价", RequestID: "trace"}, "11", expected, amount, source)
			if err == nil || result != nil || f.resource.Revision != 2 || len(f.operations) != 0 || len(f.events) != 0 {
				t.Fatalf("partial success %+v %v", f, err)
			}
		})
	}
}
