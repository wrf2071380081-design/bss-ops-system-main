package supply

import (
	"errors"
	"math"
	"strings"
	"time"
)

var ErrF12ContractArchived = errors.New("F12 contract is archived")

type F12ContractVersion struct {
	ID              string    `json:"id"`
	ContractKey     string    `json:"contract_key"`
	SubjectType     string    `json:"subject_type"`
	SubjectID       string    `json:"subject_id"`
	Status          string    `json:"status"`
	DocumentName    string    `json:"document_name"`
	DocumentNo      string    `json:"document_no"`
	FileID          string    `json:"file_id"`
	SourceFlowID    string    `json:"source_flow_id,omitempty"`
	SourceModuleKey string    `json:"source_module_key,omitempty"`
	SourceFlowRevision uint64 `json:"source_flow_revision,string,omitempty"`
	Reason          string    `json:"reason"`
	CreatedBy       string    `json:"created_by"`
	Version         uint64    `json:"version,string"`
	CreatedAt       time.Time `json:"created_at"`
}

type F12ContractArchiveCommand struct {
	SourceFlowID         string
	SourceModuleKey      string
	ExpectedFlowRevision uint64
	// SourceFlowRevision is populated only from the trusted transaction-bound
	// authorization provider. It is never accepted from the HTTP request.
	SourceFlowRevision uint64
}

func ArchiveF12ContractVersion(existing []*F12ContractVersion, expectedLatest uint64, id, actorID, reason string, command F12ContractArchiveCommand, createdAt time.Time) (*F12ContractVersion, error) {
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	if !validID(actorID) {
		return nil, invalid("actor_id", "expected an authenticated staff bigint string")
	}
	reason = strings.TrimSpace(reason)
	if !validF12Text(reason) {
		return nil, invalid("reason", "field is required and must be valid text")
	}
	if createdAt.IsZero() {
		return nil, invalid("created_at", "expected a valid timestamp")
	}
	if !validF12SourceContext(command.SourceFlowID, command.SourceModuleKey, command.SourceFlowRevision, true) {
		return nil, invalid("source_flow_revision", "flow archives require a trusted positive flow revision")
	}
	if len(existing) == 0 || existing[0] == nil {
		return nil, ErrNotFound
	}
	first := existing[0]
	maxVersion, versions, err := f12ContractHistory(existing, first.ContractKey, first.SubjectType, first.SubjectID, id)
	if err != nil {
		return nil, err
	}
	if expectedLatest != maxVersion || maxVersion == 0 || maxVersion == math.MaxInt32 {
		return nil, ErrRevisionConflict
	}
	latest := versions[maxVersion]
	if latest.Status == "ARCHIVED" {
		return nil, ErrF12ContractArchived
	}
	archived := *latest
	archived.ID = id
	archived.Status = "ARCHIVED"
	archived.Reason = reason
	archived.CreatedBy = actorID
	// The archived version records the actual archive context. Ordinary archive
	// clears old onboarding provenance, while a flow-authorized archive records
	// the new revision returned by the trusted provider. The prior version stays
	// immutable in either case.
	archived.SourceFlowID = command.SourceFlowID
	archived.SourceModuleKey = command.SourceModuleKey
	archived.SourceFlowRevision = command.SourceFlowRevision
	archived.Version = maxVersion + 1
	archived.CreatedAt = createdAt.UTC().Truncate(time.Microsecond)
	return &archived, nil
}

func AppendF12ContractVersion(existing []*F12ContractVersion, expectedLatest uint64, id, contractKey, actorID string, input F12ContractWriteCommand, createdAt time.Time) (*F12ContractVersion, error) {
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	if !validID(contractKey) {
		return nil, invalid("contract_key", "expected a positive bigint string")
	}
	if !validID(actorID) {
		return nil, invalid("actor_id", "expected an authenticated staff bigint string")
	}
	if createdAt.IsZero() {
		return nil, invalid("created_at", "expected a valid timestamp")
	}
	canonical, err := ValidateF12ContractWriteCommand(input)
	if err != nil {
		return nil, err
	}
	if !validF12SourceContext(canonical.SourceFlowID, canonical.SourceModuleKey, canonical.SourceFlowRevision, true) {
		return nil, invalid("source_flow_revision", "flow records require a trusted positive flow revision")
	}

	maxVersion, seenVersions, err := f12ContractHistory(existing, contractKey, canonical.SubjectType, canonical.SubjectID, id)
	if err != nil {
		return nil, err
	}
	if expectedLatest != maxVersion || maxVersion == math.MaxInt32 {
		return nil, ErrRevisionConflict
	}
	if maxVersion > 0 && seenVersions[maxVersion].Status == "ARCHIVED" {
		return nil, ErrF12ContractArchived
	}

	status := "PROCESSING"
	if maxVersion > 0 {
		status = seenVersions[maxVersion].Status
	}
	return &F12ContractVersion{
		ID: id, ContractKey: contractKey, SubjectType: canonical.SubjectType, SubjectID: canonical.SubjectID,
		Status: status, DocumentName: canonical.DocumentName, DocumentNo: canonical.DocumentNo, FileID: canonical.FileID,
		SourceFlowID: canonical.SourceFlowID, SourceModuleKey: canonical.SourceModuleKey, SourceFlowRevision: canonical.SourceFlowRevision, Reason: canonical.Reason,
		CreatedBy: actorID, Version: maxVersion + 1,
		CreatedAt: createdAt.UTC().Truncate(time.Microsecond),
	}, nil
}

func f12ContractHistory(existing []*F12ContractVersion, contractKey, subjectType, subjectID, newID string) (uint64, map[uint64]*F12ContractVersion, error) {
	maxVersion := uint64(0)
	versions := make(map[uint64]*F12ContractVersion, len(existing))
	seenIDs := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		if row == nil || !validID(row.ID) || !validID(row.ContractKey) || !validID(row.SubjectID) || !validF12SourceContext(row.SourceFlowID, row.SourceModuleKey, row.SourceFlowRevision, true) ||
			(row.SubjectType != "SUPPLIER" && row.SubjectType != "CUSTOMER") ||
			(row.Status != "PROCESSING" && row.Status != "ARCHIVED") || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() {
			return 0, nil, ErrReadContract
		}
		if row.ContractKey != contractKey || row.SubjectType != subjectType || row.SubjectID != subjectID {
			return 0, nil, ErrReadContract
		}
		if _, exists := seenIDs[row.ID]; exists {
			return 0, nil, ErrReadContract
		}
		seenIDs[row.ID] = struct{}{}
		if row.ID == newID {
			return 0, nil, ErrRevisionConflict
		}
		if _, exists := versions[row.Version]; exists {
			return 0, nil, ErrRevisionConflict
		}
		versions[row.Version] = row
		if row.Version > maxVersion {
			maxVersion = row.Version
		}
	}
	if maxVersion != uint64(len(versions)) {
		return 0, nil, ErrReadContract
	}
	for version, row := range versions {
		if row.Status == "ARCHIVED" && version != maxVersion {
			return 0, nil, ErrReadContract
		}
	}
	return maxVersion, versions, nil
}

type F12CreditVersion struct {
	ID              string    `json:"id"`
	SubjectType     string    `json:"subject_type"`
	SubjectID       string    `json:"subject_id"`
	Direction       string    `json:"direction"`
	Amount          string    `json:"amount"`
	Currency        string    `json:"currency"`
	DocumentName    string    `json:"document_name"`
	DocumentNo      string    `json:"document_no"`
	FileID          string    `json:"file_id"`
	SourceFlowID    string    `json:"source_flow_id,omitempty"`
	SourceModuleKey string    `json:"source_module_key,omitempty"`
	SourceFlowRevision uint64 `json:"source_flow_revision,string,omitempty"`
	Reason          string    `json:"reason"`
	CreatedBy       string    `json:"created_by"`
	Version         uint64    `json:"version,string"`
	CreatedAt       time.Time `json:"created_at"`
}

func AppendF12CreditVersion(existing []*F12CreditVersion, expectedLatest uint64, id, actorID string, input F12CreditWriteCommand, createdAt time.Time) (*F12CreditVersion, error) {
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	if !validID(actorID) {
		return nil, invalid("actor_id", "expected an authenticated staff bigint string")
	}
	if createdAt.IsZero() {
		return nil, invalid("created_at", "expected a valid timestamp")
	}
	canonical, err := ValidateF12CreditWriteCommand(input)
	if err != nil {
		return nil, err
	}
	if !validF12SourceContext(canonical.SourceFlowID, canonical.SourceModuleKey, canonical.SourceFlowRevision, true) {
		return nil, invalid("source_flow_revision", "flow records require a trusted positive flow revision")
	}

	maxVersion := uint64(0)
	seenIDs := make(map[string]struct{}, len(existing))
	seenVersions := make(map[uint64]struct{}, len(existing))
	for _, row := range existing {
		if row == nil || !validID(row.ID) || !validID(row.SubjectID) || !validF12SourceContext(row.SourceFlowID, row.SourceModuleKey, row.SourceFlowRevision, true) ||
			(row.SubjectType != "SUPPLIER" && row.SubjectType != "CUSTOMER") ||
			row.Direction != f12CreditDirection(row.SubjectType) ||
			!currencyCode.MatchString(row.Currency) || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() {
			return nil, ErrReadContract
		}
		if row.SubjectType != canonical.SubjectType || row.SubjectID != canonical.SubjectID ||
			row.Direction != canonical.Direction || row.Currency != canonical.Currency {
			return nil, ErrReadContract
		}
		if _, exists := seenIDs[row.ID]; exists {
			return nil, ErrReadContract
		}
		seenIDs[row.ID] = struct{}{}
		if row.ID == id {
			return nil, ErrRevisionConflict
		}
		if _, exists := seenVersions[row.Version]; exists {
			return nil, ErrRevisionConflict
		}
		seenVersions[row.Version] = struct{}{}
		if row.Version > maxVersion {
			maxVersion = row.Version
		}
	}

	if maxVersion != uint64(len(seenVersions)) {
		return nil, ErrReadContract
	}
	if expectedLatest != maxVersion || maxVersion == math.MaxInt32 {
		return nil, ErrRevisionConflict
	}
	return &F12CreditVersion{
		ID: id, SubjectType: canonical.SubjectType, SubjectID: canonical.SubjectID,
		Direction: canonical.Direction, Amount: canonical.Amount, Currency: canonical.Currency,
		DocumentName: canonical.DocumentName, DocumentNo: canonical.DocumentNo, FileID: canonical.FileID,
		SourceFlowID: canonical.SourceFlowID, SourceModuleKey: canonical.SourceModuleKey, SourceFlowRevision: canonical.SourceFlowRevision, Reason: canonical.Reason,
		CreatedBy: actorID, Version: maxVersion + 1,
		CreatedAt: createdAt.UTC().Truncate(time.Microsecond),
	}, nil
}
