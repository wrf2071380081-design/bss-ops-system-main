package supply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type evidenceMemory struct {
	source  TestEvidenceContext
	effects []TestEvidenceEffect
	fail    string
	writes  int
}

func evidenceStore(t *testing.T) *evidenceMemory {
	r, report := evidenceFixture(t)
	return &evidenceMemory{source: TestEvidenceContext{Resource: r, Report: report, TrustedB: true}}
}
func (f *evidenceMemory) WithinTestEvidence(ctx context.Context, run func(TestEvidenceTransaction) error) error {
	copy := *f
	r := *f.source.Resource
	copy.source = f.source
	copy.source.Resource = &r
	copy.effects = append([]TestEvidenceEffect{}, f.effects...)
	if err := run(&copy); err != nil {
		return err
	}
	if f.fail == "commit" {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *evidenceMemory) LockTestEvidence(context.Context, string, string) (*TestEvidenceContext, error) {
	if f.fail == "B" {
		return nil, ErrDependencyUnavailable
	}
	return &f.source, nil
}
func (f *evidenceMemory) SaveTestEvidence(_ context.Context, r *Resource, expected uint64) error {
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
func (f *evidenceMemory) AppendTestEvidenceEffect(_ context.Context, e TestEvidenceEffect) error {
	f.effects = append(f.effects, e)
	if f.fail == "log" {
		return errors.New("log failed")
	}
	return nil
}
func evidenceCommand() TestEvidenceCommand {
	return TestEvidenceCommand{ResourceID: "11", ReportID: "1100"}
}
func evidenceMeta() WriteMeta {
	return WriteMeta{ActorID: "8", Reason: "关联B测试报告", RequestID: "attach-report"}
}
func TestEvidenceServiceSelectsOnceAndLogsDuplicateWithoutResourceWrite(t *testing.T) {
	f := evidenceStore(t)
	svc := NewTestEvidenceService(f, nil)
	first, err := svc.AttachTestEvidence(context.Background(), evidenceMeta(), evidenceCommand())
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != "SELECTED" || f.writes != 1 || len(f.effects) != 1 || f.effects[0].Before.State != "PENDING_TEST" || f.effects[0].After.State != "PENDING_ACCESS" {
		t.Fatalf("wrong effect %+v", f)
	}
	again, err := svc.AttachTestEvidence(context.Background(), evidenceMeta(), evidenceCommand())
	if err != nil || again.Outcome != "NOOP" || f.writes != 1 || len(f.effects) != 2 || f.effects[1].Outcome != "NOOP" {
		t.Fatalf("duplicate changed resource %+v %v", again, err)
	}
	again.Resource.State = "OFFLINE"
	if f.source.Resource.State != "PENDING_ACCESS" {
		t.Fatal("returned resource aliases storage")
	}
}
func TestEvidenceServiceIgnoresOldReportAndRejectsUntrustedCall(t *testing.T) {
	f := evidenceStore(t)
	f.source.Resource.LatestReportID = "99"
	f.source.Resource.LatestReportSequence = 5
	r, err := NewTestEvidenceService(f, nil).AttachTestEvidence(context.Background(), evidenceMeta(), evidenceCommand())
	if err != nil || r.Outcome != "IGNORED_OLDER" || f.writes != 0 || len(f.effects) != 1 || f.source.Resource.LatestReportID != "99" {
		t.Fatalf("old callback %+v %v", r, err)
	}
	f = evidenceStore(t)
	f.source.TrustedB = false
	if _, err := NewTestEvidenceService(f, nil).AttachTestEvidence(context.Background(), evidenceMeta(), evidenceCommand()); !errors.Is(err, ErrForbidden) || f.writes != 0 {
		t.Fatalf("untrusted callback accepted %v", err)
	}
}
func TestEvidenceServiceRollsBackSaveLogCommitAndDependencyFailures(t *testing.T) {
	for _, kind := range []string{"save", "log", "commit", "B"} {
		f := evidenceStore(t)
		f.fail = kind
		r, err := NewTestEvidenceService(f, nil).AttachTestEvidence(context.Background(), evidenceMeta(), evidenceCommand())
		if err == nil || r != nil || f.writes != 0 || len(f.effects) != 0 || f.source.Resource.LatestReportID != "" {
			t.Fatalf("%s partially saved %+v %v", kind, r, err)
		}
		if kind == "B" {
			if !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatal(err)
			}
		} else if !strings.Contains(err.Error(), kind+" failed") {
			t.Fatalf("wrong failure path %s: %v", kind, err)
		}
	}
}

func TestEvidenceServiceChecksRequestedReportAndCancellation(t *testing.T) {
	f := evidenceStore(t)
	cmd := evidenceCommand()
	cmd.ReportID = "99"
	if r, err := NewTestEvidenceService(f, nil).AttachTestEvidence(context.Background(), evidenceMeta(), cmd); r != nil || !errors.Is(err, ErrReadContract) {
		t.Fatalf("wrong report accepted %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := NewTestEvidenceService(f, nil).AttachTestEvidence(ctx, evidenceMeta(), evidenceCommand()); r != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled callback ran")
	}
	if f.writes != 0 || len(f.effects) != 0 {
		t.Fatal("invalid callback mutated resource")
	}
}

func TestEvidenceServiceConnectsCreationRetestAndAdmission(t *testing.T) {
	creation := creationStore()
	created, err := NewResourceCreationService(creation, nil).CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
	if err != nil {
		t.Fatal(err)
	}
	if created.Resource.State != "PENDING_TEST" {
		t.Fatal("creation without report skipped testing")
	}
	report := creationReport(creation.source.Proof)
	report.Sequence = 2
	evidence := &evidenceMemory{source: TestEvidenceContext{Resource: created.Resource, Report: report, TrustedB: true}}
	selected, err := NewTestEvidenceService(evidence, nil).AttachTestEvidence(context.Background(), evidenceMeta(), TestEvidenceCommand{ResourceID: "11", ReportID: "51"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Resource.State != "PENDING_ACCESS" || selected.Resource.Revision != 2 {
		t.Fatal("passed test did not prepare resource")
	}
	record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	record.Resource = selected.Resource
	record.Report = report
	// Separate formal-config binding is represented by a trusted fixture here.
	record.Resource.FormalConfigID = record.Config.ID
	record.Resource.FormalConfigRevision = record.Config.Revision
	p := creation.source.Proof
	admission := &admissionMemory{record: record, proof: &AdmissionProof{FlowID: p.FlowID, FlowRevision: p.FlowRevision, NodeID: "82", SupplierID: p.SupplierID, ResourceID: "11", Region: p.Region, OriginRowID: p.RowID, OriginRowVersion: p.RowVersion, TargetVersion: p.TargetVersion, AssigneeID: "8", FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}}
	admitted, err := NewResourceAdmissionService(admission, nil).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "测试与配置已完成", RequestID: "admit-after-test"}, ResourceAdmissionCommand{ResourceID: "11", ExpectedRevision: 2, ExternalConfirmed: true, CompletionNote: "对应平台已实际配置"})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.State != "ADMITTED" || admitted.Revision != 3 || admitted.LatestReportSequence != 2 || admitted.LatestReportID != "51" || admitted.OriginRowVersion != 2 || admitted.TargetVersion != 3 {
		t.Fatalf("chain lost source/test facts %+v", admitted)
	}
}
