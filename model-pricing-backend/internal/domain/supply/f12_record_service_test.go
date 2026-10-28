package supply

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type f12RecordServiceFixture struct {
	subject    *F12RecordSubject
	authorized bool
	fileOwned  bool
	nextID     string
	contracts  []*F12ContractVersion
	credits    []*F12CreditVersion
	operations []F12RecordOperation
	logErr     error
	events     []string
	verifiedUploaders []string
	verifiedChains []F12AttachmentChain
	authorizedFlowRevision uint64
	flowExpectedRevision uint64
	flowID string
	flowModule string
}

type f12RecordServiceUOW struct{ fixture *f12RecordServiceFixture }

func (u f12RecordServiceUOW) Within(ctx context.Context, fn func(F12RecordTransaction) error) error {
	tx := &f12RecordServiceTx{fixture: u.fixture}
	if err := fn(tx); err != nil {
		return err
	}
	u.fixture.contracts = append(u.fixture.contracts, tx.pendingContracts...)
	u.fixture.credits = append(u.fixture.credits, tx.pendingCredits...)
	u.fixture.operations = append(u.fixture.operations, tx.pendingOps...)
	return nil
}

type f12RecordServiceTx struct {
	fixture          *f12RecordServiceFixture
	pendingContracts []*F12ContractVersion
	pendingCredits   []*F12CreditVersion
	pendingOps       []F12RecordOperation
}

func (tx *f12RecordServiceTx) ContractSubject(_ context.Context, contractKey string) (*F12RecordSubject, error) {
	tx.fixture.events = append(tx.fixture.events, "contract-subject")
	for _, row := range tx.fixture.contracts {
		if row.ContractKey == contractKey {
			return &F12RecordSubject{Type: row.SubjectType, ID: row.SubjectID}, nil
		}
	}
	return nil, ErrNotFound
}

func (tx *f12RecordServiceTx) SubjectForUpdate(_ context.Context, subjectType, subjectID string) (*F12RecordSubject, error) {
	tx.fixture.events = append(tx.fixture.events, "subject")
	if tx.fixture.subject == nil || tx.fixture.subject.Type != subjectType || tx.fixture.subject.ID != subjectID {
		return nil, ErrNotFound
	}
	copy := *tx.fixture.subject
	return &copy, nil
}

func (tx *f12RecordServiceTx) AuthorizeF12Write(_ context.Context, _, _, _, flowID, moduleKey string, expected uint64) (uint64, error) {
	tx.fixture.events = append(tx.fixture.events, "authorize")
	tx.fixture.flowID, tx.fixture.flowModule, tx.fixture.flowExpectedRevision = flowID, moduleKey, expected
	if !tx.fixture.authorized {
		return 0, ErrForbidden
	}
	if flowID != "" && expected != tx.fixture.authorizedFlowRevision {
		return 0, ErrRevisionConflict
	}
	return tx.fixture.authorizedFlowRevision, nil
}

func (tx *f12RecordServiceTx) AuthorizeF12Read(_ context.Context, _, _, _, _, _ string) error {
	tx.fixture.events = append(tx.fixture.events, "authorize-read")
	if !tx.fixture.authorized {
		return ErrForbidden
	}
	return nil
}

func (tx *f12RecordServiceTx) VerifyF12Attachment(_ context.Context, uploadedBy, _, _, _, _ string, chain F12AttachmentChain) error {
	tx.fixture.events = append(tx.fixture.events, "file")
	tx.fixture.verifiedUploaders = append(tx.fixture.verifiedUploaders, uploadedBy)
	tx.fixture.verifiedChains = append(tx.fixture.verifiedChains, chain)
	if !tx.fixture.fileOwned {
		return ErrForbidden
	}
	return nil
}

func (tx *f12RecordServiceTx) NextID(context.Context, F12RecordIDKind) (string, error) {
	tx.fixture.events = append(tx.fixture.events, "id")
	return tx.fixture.nextID, nil
}

func (tx *f12RecordServiceTx) ContractVersionsForUpdate(_ context.Context, contractKey string) ([]*F12ContractVersion, error) {
	tx.fixture.events = append(tx.fixture.events, "contract-history")
	rows := make([]*F12ContractVersion, 0, len(tx.fixture.contracts))
	for _, row := range tx.fixture.contracts {
		if row.ContractKey == contractKey {
			copy := *row
			rows = append(rows, &copy)
		}
	}
	return rows, nil
}

func (tx *f12RecordServiceTx) CreditVersionsForUpdate(_ context.Context, subjectType, subjectID, direction, currency string) ([]*F12CreditVersion, error) {
	tx.fixture.events = append(tx.fixture.events, "credit-history")
	rows := make([]*F12CreditVersion, 0, len(tx.fixture.credits))
	for _, row := range tx.fixture.credits {
		if row.SubjectType == subjectType && row.SubjectID == subjectID && row.Direction == direction && row.Currency == currency {
			copy := *row
			rows = append(rows, &copy)
		}
	}
	return rows, nil
}

func (tx *f12RecordServiceTx) AppendF12ContractVersion(_ context.Context, record *F12ContractVersion) error {
	tx.fixture.events = append(tx.fixture.events, "append")
	copy := *record
	tx.pendingContracts = append(tx.pendingContracts, &copy)
	return nil
}

func (tx *f12RecordServiceTx) AppendF12CreditVersion(_ context.Context, record *F12CreditVersion) error {
	tx.fixture.events = append(tx.fixture.events, "append-credit")
	copy := *record
	tx.pendingCredits = append(tx.pendingCredits, &copy)
	return nil
}

func (tx *f12RecordServiceTx) AppendF12Operation(_ context.Context, operation F12RecordOperation) error {
	tx.fixture.events = append(tx.fixture.events, "operation")
	if tx.fixture.logErr != nil {
		return tx.fixture.logErr
	}
	tx.pendingOps = append(tx.pendingOps, operation)
	return nil
}

func TestF12RecordServiceCreatesContractWithAuthorizedSubjectAndFile(t *testing.T) {
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, fileOwned: true, nextID: "700",
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, func() time.Time {
		return time.Date(2026, 10, 24, 8, 30, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	})
	meta := WriteMeta{ActorID: "7", Reason: "供应商合同登记", RequestID: "f12-contract-create"}
	command := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10",
		FileID: "file-700", Reason: "首次登记",
	}

	got, err := service.CreateContract(context.Background(), meta, command)
	if err != nil {
		t.Fatalf("CreateContract() error = %v", err)
	}
	if got.ContractKey != "700" || got.ID != "700" || got.Version != 1 || got.Status != "PROCESSING" || got.CreatedBy != "7" {
		t.Fatalf("created contract = %#v", got)
	}
	if len(fixture.contracts) != 1 || len(fixture.operations) != 1 {
		t.Fatalf("persisted contract/log counts = %d/%d, want 1/1", len(fixture.contracts), len(fixture.operations))
	}
	if !reflect.DeepEqual(fixture.verifiedUploaders, []string{"7"}) {
		t.Fatalf("new contract attachment uploader checks = %v, want current actor", fixture.verifiedUploaders)
	}
	if !reflect.DeepEqual(fixture.verifiedChains, []F12AttachmentChain{{Kind: "CONTRACT"}}) {
		t.Fatalf("new contract attachment chain = %+v, want a new contract chain", fixture.verifiedChains)
	}
	if want := []string{"subject", "authorize", "file", "id", "append", "operation"}; !reflect.DeepEqual(fixture.events, want) {
		t.Fatalf("transaction call order = %v, want %v", fixture.events, want)
	}
}

func TestF12RecordServiceRejectsUnauthorizedAndRollsBackWhenAuditFails(t *testing.T) {
	command := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10",
		FileID: "file-700", Reason: "首次登记",
	}
	meta := WriteMeta{ActorID: "7", Reason: "供应商合同登记", RequestID: "f12-contract-create"}
	newFixture := func() *f12RecordServiceFixture {
		return &f12RecordServiceFixture{subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, fileOwned: true, nextID: "700"}
	}

	unauthorized := newFixture()
	unauthorized.authorized = false
	service := NewF12RecordService(f12RecordServiceUOW{fixture: unauthorized}, nil)
	if _, err := service.CreateContract(context.Background(), meta, command); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized CreateContract() error = %v, want ErrForbidden", err)
	}
	if len(unauthorized.contracts) != 0 || !reflect.DeepEqual(unauthorized.events, []string{"subject", "authorize"}) {
		t.Fatalf("unauthorized request reached writes: contracts=%d events=%v", len(unauthorized.contracts), unauthorized.events)
	}

	auditFailure := newFixture()
	auditFailure.logErr = errors.New("audit unavailable")
	service = NewF12RecordService(f12RecordServiceUOW{fixture: auditFailure}, nil)
	if _, err := service.CreateContract(context.Background(), meta, command); err == nil {
		t.Fatal("CreateContract() error = nil, want audit failure")
	}
	if len(auditFailure.contracts) != 0 || len(auditFailure.operations) != 0 {
		t.Fatalf("audit failure left partial writes: contracts=%d operations=%d", len(auditFailure.contracts), len(auditFailure.operations))
	}
}

func TestF12RecordServiceRevisesContractWithoutChangingPriorVersion(t *testing.T) {
	first := &F12ContractVersion{
		ID: "701", ContractKey: "700", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "采购合同", DocumentNo: "S-10", FileID: "file-701", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, fileOwned: true, nextID: "702",
		contracts: []*F12ContractVersion{first},
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, nil)
	meta := WriteMeta{ActorID: "7", Reason: "合同换版", RequestID: "f12-contract-v2"}
	command := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10-v2",
		FileID: "file-702", Reason: "更新签署版",
	}

	got, err := service.ReviseContract(context.Background(), meta, "700", 1, command)
	if err != nil {
		t.Fatalf("ReviseContract() error = %v", err)
	}
	if got.Version != 2 || got.DocumentNo != "S-10-v2" || got.FileID != "file-702" || first.Version != 1 || first.DocumentNo != "S-10" {
		t.Fatalf("revised/prior versions = %#v / %#v", got, first)
	}
	if len(fixture.contracts) != 2 || len(fixture.operations) != 1 {
		t.Fatalf("persisted contract/log counts = %d/%d, want 2/1", len(fixture.contracts), len(fixture.operations))
	}
	if !reflect.DeepEqual(fixture.verifiedUploaders, []string{"7"}) {
		t.Fatalf("new revision attachment uploader checks = %v, want current actor", fixture.verifiedUploaders)
	}
	if !reflect.DeepEqual(fixture.verifiedChains, []F12AttachmentChain{{Kind: "CONTRACT", ContractKey: "700"}}) {
		t.Fatalf("new revision attachment chain = %+v, want contract 700", fixture.verifiedChains)
	}
}

func TestF12RecordServiceAllowsReusingAttachmentFromSameContractChain(t *testing.T) {
	first := &F12ContractVersion{
		ID: "701", ContractKey: "700", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "采购合同", DocumentNo: "S-10", FileID: "file-701", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, fileOwned: true, nextID: "702",
		contracts: []*F12ContractVersion{first},
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, nil)
	meta := WriteMeta{ActorID: "8", Reason: "合同换版", RequestID: "f12-contract-reuse"}
	command := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10-v2",
		FileID: "file-701", Reason: "沿用同一合同附件",
	}
	if _, err := service.ReviseContract(context.Background(), meta, "700", 1, command); err != nil {
		t.Fatalf("ReviseContract() with same-chain attachment error = %v", err)
	}
	if !reflect.DeepEqual(fixture.verifiedUploaders, []string{""}) {
		t.Fatalf("same-chain attachment uploader checks = %v, want no uploader restriction", fixture.verifiedUploaders)
	}
	if !reflect.DeepEqual(fixture.verifiedChains, []F12AttachmentChain{{Kind: "CONTRACT", ContractKey: "700"}}) {
		t.Fatalf("same-chain attachment scope = %+v, want contract 700", fixture.verifiedChains)
	}
}

func TestF12RecordServiceArchivesContractAndAddsCreditVersion(t *testing.T) {
	contract := &F12ContractVersion{
		ID: "801", ContractKey: "800", SubjectType: "CUSTOMER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "客户合同", DocumentNo: "C-10", FileID: "file-801", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	credit := &F12CreditVersion{
		ID: "901", SubjectType: "CUSTOMER", SubjectID: "10", Direction: "US_TO_CUSTOMER", Amount: "100.00", Currency: "CNY",
		DocumentName: "授信协议", DocumentNo: "C-10-L1", FileID: "file-901", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "CUSTOMER", ID: "10"}, authorized: true, fileOwned: true, nextID: "902",
		contracts: []*F12ContractVersion{contract}, credits: []*F12CreditVersion{credit},
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, nil)
	meta := WriteMeta{ActorID: "7", Reason: "资料更新", RequestID: "f12-record-write"}

	archived, err := service.ArchiveContract(context.Background(), meta, "800", 1, F12ContractArchiveCommand{})
	if err != nil {
		t.Fatalf("ArchiveContract() error = %v", err)
	}
	if archived.Version != 2 || archived.Status != "ARCHIVED" || archived.FileID != contract.FileID || contract.Status != "PROCESSING" {
		t.Fatalf("archive result/prior = %#v / %#v", archived, contract)
	}

	fixture.nextID = "902"
	creditCommand := F12CreditWriteCommand{
		SubjectType: "CUSTOMER", SubjectID: "10", Direction: "US_TO_CUSTOMER", Amount: "80", Currency: "CNY",
		DocumentName: "授信协议", DocumentNo: "C-10-L2", FileID: "file-902", Reason: "调整额度",
	}
	updated, err := service.AddCreditVersion(context.Background(), meta, 1, creditCommand)
	if err != nil {
		t.Fatalf("AddCreditVersion() error = %v", err)
	}
	if updated.Version != 2 || updated.Amount != "80.00" || credit.Amount != "100.00" {
		t.Fatalf("credit current/prior versions = %#v / %#v", updated, credit)
	}
	if got := fixture.verifiedChains[len(fixture.verifiedChains)-1]; got != (F12AttachmentChain{Kind: "CREDIT", Direction: "US_TO_CUSTOMER", Currency: "CNY"}) {
		t.Fatalf("credit attachment chain = %+v", got)
	}
	if len(fixture.contracts) != 2 || len(fixture.credits) != 2 || len(fixture.operations) != 2 {
		t.Fatalf("persisted contract/credit/log counts = %d/%d/%d, want 2/2/2", len(fixture.contracts), len(fixture.credits), len(fixture.operations))
	}
}

func TestF12RecordServiceArchivesContractWithTrustedFlowRevision(t *testing.T) {
	contract := &F12ContractVersion{
		ID: "811", ContractKey: "810", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "供应商合同", DocumentNo: "S-10", FileID: "file-811", SourceFlowID: "900", SourceModuleKey: "CONTRACT", SourceFlowRevision: 7,
		CreatedBy: "7", Version: 1, CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, authorizedFlowRevision: 9, nextID: "812",
		contracts: []*F12ContractVersion{contract},
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, nil)
	meta := WriteMeta{ActorID: "8", Reason: "流程完成归档", RequestID: "f12-flow-archive"}
	archived, err := service.ArchiveContract(context.Background(), meta, "810", 1, F12ContractArchiveCommand{
		SourceFlowID: "901", SourceModuleKey: "CONTRACT", ExpectedFlowRevision: 9,
	})
	if err != nil {
		t.Fatalf("flow ArchiveContract() error = %v", err)
	}
	if archived.SourceFlowID != "901" || archived.SourceModuleKey != "CONTRACT" || archived.SourceFlowRevision != 9 || archived.CreatedBy != "8" {
		t.Fatalf("archived flow provenance = %#v", archived)
	}
	if fixture.flowID != "901" || fixture.flowModule != "CONTRACT" || fixture.flowExpectedRevision != 9 {
		t.Fatalf("trusted authorization context = %q/%q/revision %d", fixture.flowID, fixture.flowModule, fixture.flowExpectedRevision)
	}
}

func TestF12RecordServiceRejectsStaleFlowArchiveBeforeAppendingVersionOrLog(t *testing.T) {
	contract := &F12ContractVersion{
		ID: "821", ContractKey: "820", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "供应商合同", DocumentNo: "S-10", FileID: "file-821", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	fixture := &f12RecordServiceFixture{
		subject: &F12RecordSubject{Type: "SUPPLIER", ID: "10"}, authorized: true, authorizedFlowRevision: 9, nextID: "822",
		contracts: []*F12ContractVersion{contract},
	}
	service := NewF12RecordService(f12RecordServiceUOW{fixture: fixture}, nil)
	meta := WriteMeta{ActorID: "8", Reason: "流程内归档", RequestID: "f12-stale-flow-archive"}
	_, err := service.ArchiveContract(context.Background(), meta, "820", 1, F12ContractArchiveCommand{
		SourceFlowID: "901", SourceModuleKey: "CONTRACT", ExpectedFlowRevision: 8,
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale flow archive error = %v, want ErrRevisionConflict", err)
	}
	if len(fixture.contracts) != 1 || len(fixture.operations) != 0 {
		t.Fatalf("stale flow archive changed contract/log state: contracts=%d operations=%d", len(fixture.contracts), len(fixture.operations))
	}
}
