package supply

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type configBindingMemory struct {
	source  ResourceConfigBindingContext
	effects []ResourceConfigBindingEffect
	fail    string
	writes  int
}

// This composes the real domain services over memory transactions. It does not
// prove database atomicity, encryption strength or public/B route integration.
func TestResourceLifecycleWithCreatedFormalConfigAndAssignedBinding(t *testing.T) {
	ctx := context.Background()
	creation := creationStore()
	created, err := NewResourceCreationService(creation, nil).CreateFromSubmission(ctx, createResourceMeta(), createResourceCommand())
	if err != nil {
		t.Fatal(err)
	}
	report := creationReport(creation.source.Proof)
	report.Sequence = 2
	evidence := &evidenceMemory{source: TestEvidenceContext{Resource: created.Resource, Report: report, TrustedB: true}}
	selected, err := NewTestEvidenceService(evidence, nil).AttachTestEvidence(ctx, evidenceMeta(), TestEvidenceCommand{ResourceID: "11", ReportID: "51"})
	if err != nil {
		t.Fatal(err)
	}
	configs := newFormalMemory()
	configs.suppliers["21"] = FormalConfigSupplier{ID: "21", BuyerID: "7", Region: "CN", Status: "ACTIVE"}
	input := formalCommand()
	config, err := formalService(configs).Create(ctx, writeMeta("7"), "21", input)
	if err != nil {
		t.Fatal(err)
	}
	proof := &AdmissionProof{FlowID: "81", NodeID: "82", FlowRevision: 4, SupplierID: "21", ResourceID: "11", Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, AssigneeID: "8", FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}
	binding := &configBindingMemory{source: ResourceConfigBindingContext{Resource: selected.Resource, Proof: proof, Config: &ResourceFormalConfig{ID: config.ID, SupplierID: config.SupplierID, Region: config.Region, Status: config.Status, Revision: config.Revision, ConfirmedRevision: config.ConfirmedRevision, HasAPIKey: config.HasAPIKey}}}
	bound, err := NewResourceConfigBindingService(binding, nil).BindFormalConfig(ctx, bindingMeta(), ResourceConfigBindingCommand{ResourceID: "11", ExpectedRevision: 2, ConfigID: config.ID, ExpectedConfigRevision: config.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Resource.State != "PENDING_ACCESS" || bound.Resource.EverAdmitted || bound.Resource.Revision != 3 {
		t.Fatalf("binding changed admission state %+v", bound)
	}
	record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	record.Resource, record.Report, record.Config = bound.Resource, report, config
	admission := &admissionMemory{record: record, proof: proof}
	admitted, err := NewResourceAdmissionService(admission, nil).Admit(ctx, WriteMeta{ActorID: "8", Reason: "实际配置已完成", RequestID: "admit-bound-resource"}, ResourceAdmissionCommand{ResourceID: "11", ExpectedRevision: 3, ExternalConfirmed: true, CompletionNote: "登记接入"})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.State != "ADMITTED" || admitted.Revision != 4 || admitted.LatestReportSequence != 2 || admitted.FormalConfigID != config.ID || admitted.FormalConfigRevision != config.Revision {
		t.Fatalf("lost lifecycle facts %+v", admitted)
	}
	record.Resource = admitted
	snapshot, err := BuildResourceSnapshot(record)
	if err != nil || !snapshot.QuoteReadiness.Eligible {
		t.Fatalf("quote snapshot %+v %v", snapshot, err)
	}
	for _, value := range []any{binding.effects, snapshot} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), input.BaseURL) || strings.Contains(string(encoded), *input.APIKey) {
			t.Fatal("formal connection data leaked into resource effect/view")
		}
	}
}

func configBindingStore(t *testing.T) *configBindingMemory {
	r, c, p, _ := configBindingFixture(t)
	return &configBindingMemory{source: ResourceConfigBindingContext{Resource: r, Config: c, Proof: p}}
}
func (f *configBindingMemory) WithinResourceConfigBinding(ctx context.Context, run func(ResourceConfigBindingTransaction) error) error {
	copy := *f
	r := *f.source.Resource
	copy.source = f.source
	copy.source.Resource = &r
	copy.effects = append([]ResourceConfigBindingEffect{}, f.effects...)
	if err := run(&copy); err != nil {
		return err
	}
	if f.fail == "commit" {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *configBindingMemory) LockResourceConfigBinding(context.Context, string, string) (*ResourceConfigBindingContext, error) {
	if f.fail == "dependency" {
		return nil, ErrDependencyUnavailable
	}
	return &f.source, nil
}
func (f *configBindingMemory) SaveResourceConfigBinding(_ context.Context, r *Resource, expected uint64) error {
	if f.fail == "save" {
		return errors.New("save failed")
	}
	if f.source.Resource.Revision != expected {
		return ErrRevisionConflict
	}
	copy := *r
	f.source.Resource = &copy
	f.writes++
	return nil
}
func (f *configBindingMemory) AppendResourceConfigBindingEffect(_ context.Context, e ResourceConfigBindingEffect) error {
	f.effects = append(f.effects, e)
	if f.fail == "log" {
		return errors.New("log failed")
	}
	return nil
}
func bindingMeta() WriteMeta {
	return WriteMeta{ActorID: "8", Reason: "绑定已确认正式配置", RequestID: "bind-formal-config"}
}
func TestResourceConfigBindingServiceWritesReferenceAndSafeEffect(t *testing.T) {
	f := configBindingStore(t)
	_, _, _, cmd := configBindingFixture(t)
	svc := NewResourceConfigBindingService(f, nil)
	result, err := svc.BindFormalConfig(context.Background(), bindingMeta(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || f.writes != 1 || len(f.effects) != 1 || f.effects[0].Action != "BIND" || f.effects[0].Proof.NodeID != "82" {
		t.Fatalf("bad effect %+v", f)
	}
	cmd.ExpectedRevision = 2
	again, err := svc.BindFormalConfig(context.Background(), bindingMeta(), cmd)
	if err != nil || again.Changed || f.writes != 1 || len(f.effects) != 2 || f.effects[1].Action != "BIND_NOOP" {
		t.Fatalf("bad NOOP %+v %v", again, err)
	}
	again.Resource.FormalConfigID = "999"
	if f.source.Resource.FormalConfigID != "2199" {
		t.Fatal("result aliases stored resource")
	}
}
func TestResourceConfigBindingServiceRollsBackOnSaveLogCommitAndDependencyError(t *testing.T) {
	for _, kind := range []string{"save", "log", "commit", "dependency"} {
		f := configBindingStore(t)
		f.fail = kind
		_, _, _, cmd := configBindingFixture(t)
		r, err := NewResourceConfigBindingService(f, nil).BindFormalConfig(context.Background(), bindingMeta(), cmd)
		if err == nil || r != nil || f.source.Resource.FormalConfigID != "" || f.writes != 0 || len(f.effects) != 0 {
			t.Fatalf("%s partially saved %+v %v", kind, r, err)
		}
		if kind == "dependency" {
			if !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatal(err)
			}
		} else if !strings.Contains(err.Error(), kind+" failed") {
			t.Fatalf("wrong failure %s: %v", kind, err)
		}
	}
}
