package supply

import (
	"context"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	F12RequiredContract = "CONTRACT"
	F12RequiredCredit   = "CREDIT"
)

// F12ReadinessRequest identifies an A-side subject and a trusted B/C flow
// context. For CUSTOMER, SubjectID is customer_profile.id; the provider must
// resolve its authoritative relation to the onboarding customer/flow ID and
// must never assume the two tables use the same numeric ID. Flow metadata and
// required items are always resolved server-side, never taken from this request.
type F12ReadinessRequest struct {
	SubjectType string
	SubjectID   string
	FlowID      string
	ModuleKey   string
}

type F12ReadinessRequirement struct {
	Item     string `json:"item"`
	Currency string `json:"currency,omitempty"`
}

type F12BusinessContext struct {
	SubjectType   string
	SubjectID     string
	FlowID        string
	ModuleKey     string
	FlowRevision  uint64
	RequiredItems []F12ReadinessRequirement
}

type F12ContractVersionRef struct {
	RecordID    string `json:"contract_id"`
	ContractKey string `json:"contract_key"`
	Version     uint64 `json:"version,string"`
}

type F12CreditVersionRef struct {
	RecordID string `json:"credit_line_id"`
	Version  uint64 `json:"version,string"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

type F12ReadinessSnapshot struct {
	SubjectType    string                    `json:"subject_type"`
	SubjectID      string                    `json:"subject_id"`
	FlowID         string                    `json:"source_flow_id"`
	ModuleKey      string                    `json:"source_module_key"`
	FlowRevision   uint64                    `json:"source_flow_revision,string"`
	RequiredItems  []F12ReadinessRequirement `json:"required_items"`
	Ready          bool                      `json:"ready"`
	Contracts      []F12ContractVersionRef   `json:"contracts"`
	Credits        []F12CreditVersionRef     `json:"credits"`
	MissingReasons []string                  `json:"missing_reasons"`
}

type F12BusinessContextProvider interface {
	// Resolve must return the same A-side subject identity as the request while
	// resolving the authoritative flow identity, revision, and required items.
	// In particular it must verify the CUSTOMER customer_profile-to-customer
	// mapping from trusted data instead of comparing IDs by coincidence.
	ResolveF12BusinessContext(context.Context, F12ReadinessRequest) (*F12BusinessContext, error)
}

// F12ReadinessEvidenceStore returns only the latest effective records that are
// explicitly sourced from the requested flow and module.
type F12ReadinessEvidenceStore interface {
	ListF12ReadinessEvidence(context.Context, F12ReadinessRequest) ([]*F12ContractRecord, []*F12CreditRecord, error)
}

type F12ReadinessService struct {
	contexts F12BusinessContextProvider
	evidence F12ReadinessEvidenceStore
}

func NewF12ReadinessService(contexts F12BusinessContextProvider, evidence F12ReadinessEvidenceStore) *F12ReadinessService {
	return &F12ReadinessService{contexts: contexts, evidence: evidence}
}

func (s *F12ReadinessService) Get(ctx context.Context, request F12ReadinessRequest) (*F12ReadinessSnapshot, error) {
	if s == nil || s.contexts == nil || s.evidence == nil {
		return nil, ErrDependencyUnavailable
	}
	request.SubjectType = strings.TrimSpace(request.SubjectType)
	request.SubjectID = strings.TrimSpace(request.SubjectID)
	request.FlowID = strings.TrimSpace(request.FlowID)
	request.ModuleKey = strings.TrimSpace(request.ModuleKey)
	if _, _, err := normalizeF12Subject(request.SubjectType, request.SubjectID, 1, 1); err != nil {
		return nil, err
	}
	if !validID(request.FlowID) || !validF12Text(request.ModuleKey) || len(request.ModuleKey) > 64 {
		return nil, invalid("flow_ref", "a valid flow ID and module key are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	first, err := s.contexts.ResolveF12BusinessContext(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := validateF12BusinessContext(request, first); err != nil {
		return nil, err
	}
	contracts, credits, err := s.evidence.ListF12ReadinessEvidence(ctx, request)
	if err != nil {
		return nil, err
	}
	last, err := s.contexts.ResolveF12BusinessContext(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := validateF12BusinessContext(request, last); err != nil {
		return nil, err
	}
	if first.FlowRevision != last.FlowRevision || !sameF12Requirements(first.RequiredItems, last.RequiredItems) {
		return nil, ErrRevisionConflict
	}
	return evaluateF12Readiness(last, contracts, credits)
}

func validateF12BusinessContext(request F12ReadinessRequest, facts *F12BusinessContext) error {
	if facts == nil || facts.SubjectType != request.SubjectType || facts.SubjectID != request.SubjectID || facts.FlowID != request.FlowID || facts.ModuleKey != request.ModuleKey || facts.FlowRevision == 0 || facts.FlowRevision > math.MaxInt32 || len(facts.RequiredItems) == 0 {
		return ErrReadContract
	}
	seen := make(map[string]bool, len(facts.RequiredItems))
	for _, item := range facts.RequiredItems {
		key := strings.TrimSpace(item.Item) + "\x00" + strings.TrimSpace(item.Currency)
		switch item.Item {
		case F12RequiredContract:
			if item.Currency != "" {
				return ErrReadContract
			}
		case F12RequiredCredit:
			if item.Currency != "" && !currencyCode.MatchString(item.Currency) {
				return ErrReadContract
			}
		default:
			return ErrReadContract
		}
		if seen[key] {
			return ErrReadContract
		}
		seen[key] = true
	}
	if seen[F12RequiredCredit+"\x00"] {
		for _, item := range facts.RequiredItems {
			if item.Item == F12RequiredCredit && item.Currency != "" {
				return ErrReadContract
			}
		}
	}
	return nil
}

func sameF12Requirements(left, right []F12ReadinessRequirement) bool {
	if len(left) != len(right) {
		return false
	}
	keys := make(map[string]int, len(left))
	for _, item := range left {
		keys[item.Item+"\x00"+item.Currency]++
	}
	for _, item := range right {
		key := item.Item + "\x00" + item.Currency
		if keys[key] == 0 {
			return false
		}
		keys[key]--
	}
	return true
}

func evaluateF12Readiness(facts *F12BusinessContext, contracts []*F12ContractRecord, credits []*F12CreditRecord) (*F12ReadinessSnapshot, error) {
	snapshot := &F12ReadinessSnapshot{
		SubjectType: facts.SubjectType, SubjectID: facts.SubjectID, FlowID: facts.FlowID,
		ModuleKey: facts.ModuleKey, FlowRevision: facts.FlowRevision, Ready: true,
		RequiredItems: append([]F12ReadinessRequirement(nil), facts.RequiredItems...),
		Contracts: []F12ContractVersionRef{}, Credits: []F12CreditVersionRef{}, MissingReasons: []string{},
	}
	for _, item := range facts.RequiredItems {
		switch item.Item {
		case F12RequiredContract:
			matches := make([]*F12ContractRecord, 0)
			seenKeys := make(map[string]bool)
			for _, row := range contracts {
				if row == nil || !validID(row.ID) || !validID(row.ContractKey) || row.Version == 0 || row.Version > math.MaxInt32 || row.SubjectType != facts.SubjectType || row.SubjectID != facts.SubjectID || row.SourceFlowID != facts.FlowID || row.SourceModuleKey != facts.ModuleKey || row.SourceFlowRevision == 0 || row.SourceFlowRevision > facts.FlowRevision {
					return nil, ErrReadContract
				}
				if row.IsLatest && row.Status == "PROCESSING" && row.HasAttachment && strings.TrimSpace(row.FileID) != "" && row.DocumentName != "" && row.DocumentNo != "" {
					if seenKeys[row.ContractKey] {
						return nil, ErrRevisionConflict
					}
					seenKeys[row.ContractKey] = true
					matches = append(matches, row)
				}
			}
			if len(matches) == 0 {
				snapshot.Ready = false
				snapshot.MissingReasons = append(snapshot.MissingReasons, "CONTRACT_REQUIRED")
				continue
			}
			for _, row := range matches {
				snapshot.Contracts = append(snapshot.Contracts, F12ContractVersionRef{RecordID: row.ID, ContractKey: row.ContractKey, Version: row.Version})
			}
		case F12RequiredCredit:
			matches := make([]*F12CreditRecord, 0)
			seenCurrencies := make(map[string]bool)
			for _, row := range credits {
				if row == nil || !validID(row.ID) || row.Version == 0 || row.Version > math.MaxInt32 || row.SubjectType != facts.SubjectType || row.SubjectID != facts.SubjectID || row.SourceFlowID != facts.FlowID || row.SourceModuleKey != facts.ModuleKey || row.SourceFlowRevision == 0 || row.SourceFlowRevision > facts.FlowRevision || row.Direction != f12CreditDirection(facts.SubjectType) || !currencyCode.MatchString(row.Currency) {
					return nil, ErrReadContract
				}
				if (item.Currency == "" || row.Currency == item.Currency) && row.HasAttachment && strings.TrimSpace(row.FileID) != "" && row.DocumentName != "" && row.DocumentNo != "" {
					amount, err := decimal.NewFromString(row.Amount)
					if err != nil || !amount.IsPositive() {
						continue
					}
					if seenCurrencies[row.Currency] {
						return nil, ErrRevisionConflict
					}
					seenCurrencies[row.Currency] = true
					matches = append(matches, row)
				}
			}
			if len(matches) == 0 {
				snapshot.Ready = false
				missing := "CREDIT_REQUIRED"
				if item.Currency != "" {
					missing += ":" + item.Currency
				}
				snapshot.MissingReasons = append(snapshot.MissingReasons, missing)
				continue
			}
			for _, row := range matches {
				snapshot.Credits = append(snapshot.Credits, F12CreditVersionRef{RecordID: row.ID, Version: row.Version, Currency: row.Currency, Amount: row.Amount})
			}
		}
	}
	return snapshot, nil
}
