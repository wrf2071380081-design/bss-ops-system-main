package supply

import (
	"context"
	"time"
)

type F12RecordSubject struct {
	Type string
	ID   string
}

type F12RecordOperation struct {
	EntityType, EntityID, Action, ActorID, Reason, RequestID string
	BeforeContract, AfterContract                            *F12ContractVersion
	BeforeCredit, AfterCredit                                *F12CreditVersion
	OccurredAt                                               time.Time
}

// F12RecordIDKind selects the identity sequence used for a newly appended row.
// contract and credit_line have independent PostgreSQL identity sequences.
type F12RecordIDKind string

const (
	F12ContractRecordID F12RecordIDKind = "contract"
	F12CreditRecordID   F12RecordIDKind = "credit_line"
)

// F12RecordUnitOfWork joins subject, authorization, attachment, version, and
// operation-log writes to the caller's existing transaction.
type F12RecordUnitOfWork interface {
	Within(context.Context, func(F12RecordTransaction) error) error
}

// F12RecordTransaction is the adapter contract for real subject, B/C flow,
// D07 attachment, persistence, and audit facts. It must not infer authorization
// from HTTP input or perform network file transfer while holding the SQL lock.
type F12RecordTransaction interface {
	// ContractSubject reads the immutable subject identity without locking; the
	// service locks the subject before locking its contract history.
	ContractSubject(context.Context, string) (*F12RecordSubject, error)
	SubjectForUpdate(context.Context, string, string) (*F12RecordSubject, error)
	AuthorizeF12Write(context.Context, string, string, string, string, string, uint64) (uint64, error)
	AuthorizeF12Read(context.Context, string, string, string, string, string) error
	// VerifyF12Attachment checks local, transaction-safe ownership metadata. It
	// must not make a network call to transfer or resolve file content.
	VerifyF12Attachment(context.Context, string, string, string, string, string, F12AttachmentChain) error
	NextID(context.Context, F12RecordIDKind) (string, error)
	ContractVersionsForUpdate(context.Context, string) ([]*F12ContractVersion, error)
	CreditVersionsForUpdate(context.Context, string, string, string, string) ([]*F12CreditVersion, error)
	AppendF12ContractVersion(context.Context, *F12ContractVersion) error
	AppendF12CreditVersion(context.Context, *F12CreditVersion) error
	AppendF12Operation(context.Context, F12RecordOperation) error
}

type F12RecordService struct {
	work F12RecordUnitOfWork
	now  func() time.Time
}

func NewF12RecordService(work F12RecordUnitOfWork, clock func() time.Time) *F12RecordService {
	if clock == nil {
		clock = time.Now
	}
	return &F12RecordService{work: work, now: clock}
}

func (s *F12RecordService) checkWrite(meta WriteMeta) error {
	if s == nil || s.work == nil {
		return ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) {
		return invalid("actor_id", "expected an authenticated staff bigint string")
	}
	return validateWriteMetaStorage(meta)
}

func f12WriteSubject(ctx context.Context, tx F12RecordTransaction, actorID, subjectType, subjectID, flowID, moduleKey string, expectedFlowRevision uint64) (uint64, error) {
	subject, err := tx.SubjectForUpdate(ctx, subjectType, subjectID)
	if err != nil {
		return 0, err
	}
	if subject == nil || subject.Type != subjectType || subject.ID != subjectID {
		return 0, ErrReadContract
	}
	return tx.AuthorizeF12Write(ctx, actorID, subjectType, subjectID, flowID, moduleKey, expectedFlowRevision)
}

func f12NextRecordID(ctx context.Context, tx F12RecordTransaction, kind F12RecordIDKind) (string, error) {
	if kind != F12ContractRecordID && kind != F12CreditRecordID {
		return "", ErrReadContract
	}
	id, err := tx.NextID(ctx, kind)
	if err != nil {
		return "", err
	}
	if !validID(id) {
		return "", ErrReadContract
	}
	return id, nil
}

func appendF12RecordOperation(ctx context.Context, tx F12RecordTransaction, meta WriteMeta, entityType, entityID, action string, beforeContract, afterContract *F12ContractVersion, beforeCredit, afterCredit *F12CreditVersion, now time.Time) error {
	return tx.AppendF12Operation(ctx, F12RecordOperation{
		EntityType: entityType, EntityID: entityID, Action: action, ActorID: meta.ActorID,
		Reason: meta.Reason, RequestID: meta.RequestID, BeforeContract: beforeContract, AfterContract: afterContract,
		BeforeCredit: beforeCredit, AfterCredit: afterCredit, OccurredAt: now,
	})
}

func (s *F12RecordService) CreateContract(ctx context.Context, meta WriteMeta, input F12ContractWriteCommand) (*F12ContractVersion, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	canonical, err := ValidateF12ContractWriteCommand(input)
	if err != nil {
		return nil, err
	}

	var result *F12ContractVersion
	err = s.work.Within(ctx, func(tx F12RecordTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		flowRevision, err := f12WriteSubject(ctx, tx, meta.ActorID, canonical.SubjectType, canonical.SubjectID, canonical.SourceFlowID, canonical.SourceModuleKey, canonical.ExpectedFlowRevision)
		if err != nil {
			return err
		}
		canonical.SourceFlowRevision = flowRevision
		if err := tx.VerifyF12Attachment(ctx, meta.ActorID, canonical.SubjectType, canonical.SubjectID, canonical.FileID, "CONTRACT", F12AttachmentChain{Kind: "CONTRACT"}); err != nil {
			return err
		}
		id, err := f12NextRecordID(ctx, tx, F12ContractRecordID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		record, err := AppendF12ContractVersion(nil, 0, id, id, meta.ActorID, canonical, now)
		if err != nil {
			return err
		}
		if err := tx.AppendF12ContractVersion(ctx, record); err != nil {
			return err
		}
		if err := appendF12RecordOperation(ctx, tx, meta, "contract", record.ContractKey, "CREATE", nil, record, nil, nil, now); err != nil {
			return err
		}
		copy := *record
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *F12RecordService) ReviseContract(ctx context.Context, meta WriteMeta, contractKey string, expectedLatest uint64, input F12ContractWriteCommand) (*F12ContractVersion, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	if !validID(contractKey) {
		return nil, invalid("contract_key", "expected a positive bigint string")
	}
	canonical, err := ValidateF12ContractWriteCommand(input)
	if err != nil {
		return nil, err
	}
	var result *F12ContractVersion
	err = s.work.Within(ctx, func(tx F12RecordTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		flowRevision, err := f12WriteSubject(ctx, tx, meta.ActorID, canonical.SubjectType, canonical.SubjectID, canonical.SourceFlowID, canonical.SourceModuleKey, canonical.ExpectedFlowRevision)
		if err != nil {
			return err
		}
		canonical.SourceFlowRevision = flowRevision
		existing, err := tx.ContractVersionsForUpdate(ctx, contractKey)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			return ErrNotFound
		}
		maxVersion, versions, err := f12ContractHistory(existing, contractKey, canonical.SubjectType, canonical.SubjectID, "")
		if err != nil {
			return err
		}
		if expectedLatest != maxVersion || maxVersion == 0 {
			return ErrRevisionConflict
		}
		if versions[maxVersion].Status == "ARCHIVED" {
			return ErrF12ContractArchived
		}
		expectedUploader := meta.ActorID
		for _, version := range versions {
			if version.FileID == canonical.FileID {
				expectedUploader = ""
				break
			}
		}
		if err := tx.VerifyF12Attachment(ctx, expectedUploader, canonical.SubjectType, canonical.SubjectID, canonical.FileID, "CONTRACT", F12AttachmentChain{Kind: "CONTRACT", ContractKey: contractKey}); err != nil {
			return err
		}
		id, err := f12NextRecordID(ctx, tx, F12ContractRecordID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		record, err := AppendF12ContractVersion(existing, expectedLatest, id, contractKey, meta.ActorID, canonical, now)
		if err != nil {
			return err
		}
		if err := tx.AppendF12ContractVersion(ctx, record); err != nil {
			return err
		}
		before := *versions[maxVersion]
		if err := appendF12RecordOperation(ctx, tx, meta, "contract", contractKey, "UPDATE", &before, record, nil, nil, now); err != nil {
			return err
		}
		copy := *record
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *F12RecordService) ArchiveContract(ctx context.Context, meta WriteMeta, contractKey string, expectedLatest uint64, input F12ContractArchiveCommand) (*F12ContractVersion, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	if !validID(contractKey) {
		return nil, invalid("contract_key", "expected a positive bigint string")
	}
	canonical, err := ValidateF12ContractArchiveCommand(input)
	if err != nil {
		return nil, err
	}
	var result *F12ContractVersion
	err = s.work.Within(ctx, func(tx F12RecordTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		subject, err := tx.ContractSubject(ctx, contractKey)
		if err != nil {
			return err
		}
		if subject == nil || !validID(subject.ID) || (subject.Type != "SUPPLIER" && subject.Type != "CUSTOMER") {
			return ErrReadContract
		}
		flowRevision, err := f12WriteSubject(ctx, tx, meta.ActorID, subject.Type, subject.ID, canonical.SourceFlowID, canonical.SourceModuleKey, canonical.ExpectedFlowRevision)
		if err != nil {
			return err
		}
		canonical.SourceFlowRevision = flowRevision
		existing, err := tx.ContractVersionsForUpdate(ctx, contractKey)
		if err != nil {
			return err
		}
		if len(existing) == 0 || existing[0] == nil {
			return ErrNotFound
		}
		first := existing[0]
		maxVersion, versions, err := f12ContractHistory(existing, contractKey, first.SubjectType, first.SubjectID, "")
		if err != nil {
			return err
		}
		if expectedLatest != maxVersion || maxVersion == 0 {
			return ErrRevisionConflict
		}
		if versions[maxVersion].Status == "ARCHIVED" {
			return ErrF12ContractArchived
		}
		if first.SubjectType != subject.Type || first.SubjectID != subject.ID {
			return ErrReadContract
		}
		id, err := f12NextRecordID(ctx, tx, F12ContractRecordID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		record, err := ArchiveF12ContractVersion(existing, expectedLatest, id, meta.ActorID, meta.Reason, canonical, now)
		if err != nil {
			return err
		}
		if err := tx.AppendF12ContractVersion(ctx, record); err != nil {
			return err
		}
		before := *versions[maxVersion]
		if err := appendF12RecordOperation(ctx, tx, meta, "contract", contractKey, "ARCHIVE", &before, record, nil, nil, now); err != nil {
			return err
		}
		copy := *record
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *F12RecordService) AddCreditVersion(ctx context.Context, meta WriteMeta, expectedLatest uint64, input F12CreditWriteCommand) (*F12CreditVersion, error) {
	if err := s.checkWrite(meta); err != nil {
		return nil, err
	}
	canonical, err := ValidateF12CreditWriteCommand(input)
	if err != nil {
		return nil, err
	}
	var result *F12CreditVersion
	err = s.work.Within(ctx, func(tx F12RecordTransaction) error {
		if tx == nil {
			return ErrDependencyUnavailable
		}
		flowRevision, err := f12WriteSubject(ctx, tx, meta.ActorID, canonical.SubjectType, canonical.SubjectID, canonical.SourceFlowID, canonical.SourceModuleKey, canonical.ExpectedFlowRevision)
		if err != nil {
			return err
		}
		canonical.SourceFlowRevision = flowRevision
		existing, err := tx.CreditVersionsForUpdate(ctx, canonical.SubjectType, canonical.SubjectID, canonical.Direction, canonical.Currency)
		if err != nil {
			return err
		}
		id, err := f12NextRecordID(ctx, tx, F12CreditRecordID)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		record, err := AppendF12CreditVersion(existing, expectedLatest, id, meta.ActorID, canonical, now)
		if err != nil {
			return err
		}
		expectedUploader := meta.ActorID
		for _, version := range existing {
			if version.FileID == canonical.FileID {
				expectedUploader = ""
				break
			}
		}
		if err := tx.VerifyF12Attachment(ctx, expectedUploader, canonical.SubjectType, canonical.SubjectID, canonical.FileID, "CREDIT", F12AttachmentChain{Kind: "CREDIT", Direction: canonical.Direction, Currency: canonical.Currency}); err != nil {
			return err
		}
		if err := tx.AppendF12CreditVersion(ctx, record); err != nil {
			return err
		}
		var before *F12CreditVersion
		for _, row := range existing {
			if row.Version == expectedLatest {
				copy := *row
				before = &copy
				break
			}
		}
		if err := appendF12RecordOperation(ctx, tx, meta, "credit_line", record.ID, "UPDATE", nil, nil, before, record, now); err != nil {
			return err
		}
		copy := *record
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
