package supply

import (
	"errors"
	"testing"
	"time"
)

func TestAppendF12ContractVersionCreatesImmutableDocumentRevision(t *testing.T) {
	first := &F12ContractVersion{
		ID: "301", ContractKey: "300", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "采购合同", DocumentNo: "S-10", FileID: "file-s-10-v1", Reason: "首次登记",
		CreatedBy: "7", Version: 1, CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	command := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10-REV2",
		FileID: "file-s-10-v2", Reason: "更新签署版",
	}
	createdAt := time.Date(2026, 10, 24, 8, 30, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))

	got, err := AppendF12ContractVersion([]*F12ContractVersion{first}, 1, "302", "300", "7", command, createdAt)
	if err != nil {
		t.Fatalf("AppendF12ContractVersion() error = %v", err)
	}
	if got.ID != "302" || got.ContractKey != "300" || got.Version != 2 || got.Status != "PROCESSING" || got.DocumentNo != "S-10-REV2" || got.FileID != "file-s-10-v2" {
		t.Fatalf("appended contract = %#v", got)
	}
	if first.Version != 1 || first.DocumentNo != "S-10" || first.FileID != "file-s-10-v1" {
		t.Fatalf("appending mutated contract history: %#v", first)
	}
	if !got.CreatedAt.Equal(time.Date(2026, 10, 24, 0, 30, 0, 123456000, time.UTC)) {
		t.Fatalf("created_at = %s, want UTC microsecond timestamp", got.CreatedAt)
	}
}

func TestAppendF12ContractVersionRejectsStaleDuplicateAndArchivedHistory(t *testing.T) {
	command := F12ContractWriteCommand{
		SubjectType: "CUSTOMER", SubjectID: "10", DocumentName: "客户服务合同", DocumentNo: "C-10-v2",
		FileID: "file-c-10-v2", Reason: "更新合同",
	}
	first := &F12ContractVersion{
		ID: "401", ContractKey: "400", SubjectType: "CUSTOMER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "客户服务合同", DocumentNo: "C-10", FileID: "file-c-10", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	createdAt := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

	if _, err := AppendF12ContractVersion([]*F12ContractVersion{first}, 0, "402", "400", "7", command, createdAt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale expected version error = %v, want ErrRevisionConflict", err)
	}
	duplicate := *first
	duplicate.ID = "403"
	if _, err := AppendF12ContractVersion([]*F12ContractVersion{first, &duplicate}, 1, "404", "400", "7", command, createdAt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("duplicate version error = %v, want ErrRevisionConflict", err)
	}
	archived := *first
	archived.Status = "ARCHIVED"
	if _, err := AppendF12ContractVersion([]*F12ContractVersion{&archived}, 1, "405", "400", "7", command, createdAt); !errors.Is(err, ErrF12ContractArchived) {
		t.Fatalf("archived contract error = %v, want ErrF12ContractArchived", err)
	}
}

func TestArchiveF12ContractAppendsStatusOnlyVersion(t *testing.T) {
	first := &F12ContractVersion{
		ID: "501", ContractKey: "500", SubjectType: "SUPPLIER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "采购合同", DocumentNo: "S-10", FileID: "file-s-10-v1", Reason: "首次登记",
		SourceFlowID: "900", SourceModuleKey: "CONTRACT", SourceFlowRevision: 3,
		CreatedBy: "7", Version: 1, CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	createdAt := time.Date(2026, 10, 24, 8, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	got, err := ArchiveF12ContractVersion([]*F12ContractVersion{first}, 1, "502", "7", "合同已终止", F12ContractArchiveCommand{}, createdAt)
	if err != nil {
		t.Fatalf("ArchiveF12ContractVersion() error = %v", err)
	}
	if got.ID != "502" || got.ContractKey != "500" || got.Version != 2 || got.Status != "ARCHIVED" || got.Reason != "合同已终止" || got.CreatedBy != "7" {
		t.Fatalf("archived contract version = %#v", got)
	}
	if got.DocumentName != first.DocumentName || got.DocumentNo != first.DocumentNo || got.FileID != first.FileID {
		t.Fatalf("archiving changed contract document: %#v", got)
	}
	if got.SourceFlowID != "" || got.SourceModuleKey != "" || got.SourceFlowRevision != 0 {
		t.Fatalf("ordinary archive must not be attributed to the prior onboarding flow: %#v", got)
	}
	if first.Status != "PROCESSING" || first.Version != 1 || first.FileID != "file-s-10-v1" {
		t.Fatalf("archiving mutated previous history: %#v", first)
	}
	if first.SourceFlowID != "900" || first.SourceModuleKey != "CONTRACT" || first.SourceFlowRevision != 3 {
		t.Fatalf("archiving mutated historical flow provenance: %#v", first)
	}
}

func TestArchiveF12ContractRecordsTrustedFlowRevision(t *testing.T) {
	first := &F12ContractVersion{
		ID: "511", ContractKey: "510", SubjectType: "CUSTOMER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "客户合同", DocumentNo: "C-10", FileID: "file-c-10", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	command := F12ContractArchiveCommand{SourceFlowID: "901", SourceModuleKey: "CONTRACT", SourceFlowRevision: 8}
	archived, err := ArchiveF12ContractVersion([]*F12ContractVersion{first}, 1, "512", "9", "流程内归档", command, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ArchiveF12ContractVersion(flow) error = %v", err)
	}
	if archived.SourceFlowID != "901" || archived.SourceModuleKey != "CONTRACT" || archived.SourceFlowRevision != 8 || archived.CreatedBy != "9" {
		t.Fatalf("flow-authorized archive provenance = %#v", archived)
	}
}

func TestArchiveF12ContractRejectsStaleAndAlreadyArchived(t *testing.T) {
	processing := &F12ContractVersion{
		ID: "601", ContractKey: "600", SubjectType: "CUSTOMER", SubjectID: "10", Status: "PROCESSING",
		DocumentName: "客户合同", DocumentNo: "C-10", FileID: "file-c-10", CreatedBy: "7", Version: 1,
		CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	createdAt := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	if _, err := ArchiveF12ContractVersion([]*F12ContractVersion{processing}, 0, "602", "7", "归档", F12ContractArchiveCommand{}, createdAt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale archive error = %v, want ErrRevisionConflict", err)
	}
	archived := *processing
	archived.Status = "ARCHIVED"
	if _, err := ArchiveF12ContractVersion([]*F12ContractVersion{&archived}, 1, "603", "7", "再次归档", F12ContractArchiveCommand{}, createdAt); !errors.Is(err, ErrF12ContractArchived) {
		t.Fatalf("already archived error = %v, want ErrF12ContractArchived", err)
	}
}
