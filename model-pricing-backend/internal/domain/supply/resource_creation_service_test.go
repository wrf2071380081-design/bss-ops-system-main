package supply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type creationMemory struct {
	source      ResourceCreationContext
	stored      *Resource
	rowResource string
	effects     []ResourceCreationEffect
	allocations int
	fail        string
}

func creationStore() *creationMemory {
	p, s := creationFixture()
	return &creationMemory{source: ResourceCreationContext{Proof: p, Supplier: s, ModelID: "31"}}
}
func (f *creationMemory) WithinResourceCreation(ctx context.Context, run func(ResourceCreationTransaction) error) error {
	copy := *f
	copy.source = f.source
	proof := *f.source.Proof
	copy.source.Proof = &proof
	copy.effects = append([]ResourceCreationEffect{}, f.effects...)
	if f.stored != nil {
		stored := *f.stored
		copy.stored = &stored
	}
	if err := run(&copy); err != nil {
		return err
	}
	if f.fail == "commit" {
		return errors.New("commit failed")
	}
	*f = copy
	return nil
}
func (f *creationMemory) LockSubmissionCreation(context.Context, string, uint64) (*ResourceCreationContext, error) {
	if f.fail == "lock" {
		return nil, ErrDependencyUnavailable
	}
	copy := f.source
	copy.Existing = f.stored
	return &copy, nil
}
func (f *creationMemory) NextResourceID(context.Context) (string, error) {
	f.allocations++
	return "11", nil
}
func (f *creationMemory) InsertSubmissionResource(_ context.Context, r *Resource) error {
	if f.fail == "insert" {
		return errors.New("insert failed")
	}
	if f.stored != nil {
		return ErrIdentityConflict
	}
	copy := *r
	f.stored = &copy
	return nil
}
func (f *creationMemory) AppendResourceCreationEffect(_ context.Context, e ResourceCreationEffect) error {
	if f.rowResource != "" && f.rowResource != e.Resource.ID {
		return ErrIdentityConflict
	}
	f.rowResource = e.Resource.ID
	if f.fail == "row" {
		return errors.New("row failed")
	}
	f.effects = append(f.effects, e)
	if f.fail == "log" {
		return errors.New("log failed")
	}
	return nil
}
func createResourceCommand() ResourceCreationCommand {
	return ResourceCreationCommand{RowID: "71", RowVersion: 2}
}
func createResourceMeta() WriteMeta {
	return WriteMeta{ActorID: "8", Reason: "进入办理", RequestID: "create-resource"}
}
func TestResourceCreationServiceCreatesAndBindsOnlyAfterTrustedHandling(t *testing.T) {
	f := creationStore()
	svc := NewResourceCreationService(f, nil)
	r, err := svc.CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created || r.Resource.State != "PENDING_TEST" || f.rowResource != "11" || f.allocations != 1 || len(f.effects) != 1 || f.effects[0].FlowID != "81" || f.effects[0].Action != "CREATE" {
		t.Fatalf("bad creation %+v %+v", r, f)
	}
	r.Resource.State = "ADMITTED"
	if f.stored.State != "PENDING_TEST" {
		t.Fatal("return aliases persisted resource")
	}
}
func TestResourceCreationServiceRetryDoesNotResetCurrentResource(t *testing.T) {
	f := creationStore()
	svc := NewResourceCreationService(f, nil)
	if _, err := svc.CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand()); err != nil {
		t.Fatal(err)
	}
	f.stored.State = "ADMITTED"
	f.stored.EverAdmitted = true
	f.stored.Revision = 7
	f.stored.BuyerID = "9"
	f.stored.LatestReportID = "99"
	r, err := svc.CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
	if err != nil {
		t.Fatal(err)
	}
	if r.Created || r.Resource.State != "ADMITTED" || r.Resource.Revision != 7 || r.Resource.BuyerID != "9" || r.Resource.LatestReportID != "99" || f.allocations != 1 || len(f.effects) != 2 || f.effects[1].Action != "CREATE_NOOP" {
		t.Fatalf("retry reset current facts %+v %+v", r, f)
	}
}
func TestResourceCreationServiceRejectsIdentityCollisionAndStaleSource(t *testing.T) {
	for _, kind := range []string{"row", "supplier", "model", "region", "version", "source lookup", "revoked"} {
		f := creationStore()
		p := f.source.Proof
		stored, err := NewResourceFromSubmission("11", "8", p, f.source.Supplier, "31", modelNow)
		if err != nil {
			t.Fatal(err)
		}
		f.stored = stored
		switch kind {
		case "row":
			f.stored.OriginRowID = "72"
		case "supplier":
			f.stored.SupplierID = "22"
		case "model":
			f.stored.StandardModelID = "32"
		case "region":
			f.stored.Region = "OVERSEAS"
		case "version":
			f.stored.OriginRowVersion = 1
		case "source lookup":
			p.RowID = "72"
		case "revoked":
			p.ApprovedAndAssigned = false
		}
		result, err := NewResourceCreationService(f, nil).CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
		if err == nil || result != nil || f.rowResource != "" || len(f.effects) != 0 {
			t.Fatalf("%s silently adopted another resource %+v %v", kind, result, err)
		}
	}
}
func TestResourceCreationServiceRollsBackResourceRowAndLog(t *testing.T) {
	for _, kind := range []string{"insert", "row", "log", "commit", "lock"} {
		f := creationStore()
		f.fail = kind
		r, err := NewResourceCreationService(f, nil).CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
		if err == nil || r != nil || f.stored != nil || f.rowResource != "" || len(f.effects) != 0 {
			t.Fatalf("%s leaked partial creation %+v %v %+v", kind, r, err, f)
		}
		if kind == "lock" {
			if !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatal(err)
			}
		} else if !strings.Contains(err.Error(), kind+" failed") {
			t.Fatalf("wrong failure path %s: %v", kind, err)
		}
	}
}

func TestResourceCreationServiceRejectsInvalidInputAndConflictingBReference(t *testing.T) {
	f := creationStore()
	svc := NewResourceCreationService(f, nil)
	meta := createResourceMeta()
	meta.RequestID = strings.Repeat("a", 65)
	if r, err := svc.CreateFromSubmission(context.Background(), meta, createResourceCommand()); r != nil || err == nil || f.allocations != 0 {
		t.Fatal("invalid trace reached transaction")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := svc.CreateFromSubmission(ctx, createResourceMeta(), createResourceCommand()); r != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request created resource")
	}
	f.rowResource = "12"
	if r, err := svc.CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand()); r != nil || !errors.Is(err, ErrIdentityConflict) || f.stored != nil || f.rowResource != "12" {
		t.Fatalf("overwrote B reference %+v %v %+v", r, err, f)
	}
}

func TestResourceCreationThenAdmissionKeepsDraftReportAndSourceVersions(t *testing.T) {
	f := creationStore()
	f.source.Proof.Report = creationReport(f.source.Proof)
	created, err := NewResourceCreationService(f, nil).CreateFromSubmission(context.Background(), createResourceMeta(), createResourceCommand())
	if err != nil {
		t.Fatal(err)
	}
	if created.Resource.State != "PENDING_ACCESS" || created.Resource.EverAdmitted {
		t.Fatal("creation skipped admission")
	}
	record := resourceRecord(t, "11", "21", "31", "CN", "0.68")
	record.Resource = created.Resource
	record.Report = f.source.Proof.Report
	// Formal config is bound through the separate confirmed-config capability;
	// fixture supplies that fact here, never copies a submission credential.
	record.Resource.FormalConfigID = record.Config.ID
	record.Resource.FormalConfigRevision = record.Config.Revision
	proof := &AdmissionProof{FlowID: "81", NodeID: "82", FlowRevision: 4, SupplierID: "21", ResourceID: "11", Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, AssigneeID: "8", FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}
	admission := &admissionMemory{record: record, proof: proof}
	result, err := NewResourceAdmissionService(admission, nil).Admit(context.Background(), WriteMeta{ActorID: "8", Reason: "配置完成", RequestID: "admit-created-resource"}, ResourceAdmissionCommand{ResourceID: "11", ExpectedRevision: 1, ExternalConfirmed: true, CompletionNote: "实际配置已完成"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "ADMITTED" || !result.EverAdmitted || result.OriginRowVersion != 2 || result.TargetVersion != 3 || result.LatestReportID != "51" || f.source.Proof.Report.ResourceID != "" {
		t.Fatalf("source/report rewritten %+v", result)
	}
}
