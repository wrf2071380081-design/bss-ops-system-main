package supply

import (
	"context"
	"errors"
	"testing"
)

type f12ReadinessContextFixture struct {
	facts []*F12BusinessContext
	calls int
}

func (f *f12ReadinessContextFixture) ResolveF12BusinessContext(_ context.Context, _ F12ReadinessRequest) (*F12BusinessContext, error) {
	if len(f.facts) == 0 {
		return nil, ErrDependencyUnavailable
	}
	index := f.calls
	if index >= len(f.facts) {
		index = len(f.facts) - 1
	}
	f.calls++
	copy := *f.facts[index]
	copy.RequiredItems = append([]F12ReadinessRequirement(nil), copy.RequiredItems...)
	return &copy, nil
}

type f12ReadinessEvidenceFixture struct {
	contracts []*F12ContractRecord
	credits   []*F12CreditRecord
}

func (f *f12ReadinessEvidenceFixture) ListF12ReadinessEvidence(context.Context, F12ReadinessRequest) ([]*F12ContractRecord, []*F12CreditRecord, error) {
	return f.contracts, f.credits, nil
}

func f12ReadinessFacts(revision uint64) *F12BusinessContext {
	return &F12BusinessContext{
		SubjectType: "SUPPLIER", SubjectID: "8", FlowID: "12", ModuleKey: "CONTRACT",
		FlowRevision: revision,
		RequiredItems: []F12ReadinessRequirement{{Item: F12RequiredContract}, {Item: F12RequiredCredit, Currency: "CNY"}},
	}
}

func TestF12ReadinessReturnsExactCurrentFlowReferences(t *testing.T) {
	contexts := &f12ReadinessContextFixture{facts: []*F12BusinessContext{f12ReadinessFacts(3)}}
	evidence := &f12ReadinessEvidenceFixture{
		contracts: []*F12ContractRecord{{
			ID: "21", ContractKey: "31", SubjectType: "SUPPLIER", SubjectID: "8", Status: "PROCESSING",
			DocumentName: "合同", DocumentNo: "C-1", FileID: "f12:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", HasAttachment: true,
			SourceFlowID: "12", SourceModuleKey: "CONTRACT", SourceFlowRevision: 2, IsLatest: true, Version: 1,
		}},
		credits: []*F12CreditRecord{{
			ID: "41", SubjectType: "SUPPLIER", SubjectID: "8", Direction: "SUPPLIER_TO_US", Amount: "100.00", Currency: "CNY",
			DocumentName: "授信", DocumentNo: "L-1", FileID: "f12:v1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", HasAttachment: true,
			SourceFlowID: "12", SourceModuleKey: "CONTRACT", SourceFlowRevision: 2, Version: 1,
		}},
	}
	service := NewF12ReadinessService(contexts, evidence)
	result, err := service.Get(context.Background(), F12ReadinessRequest{SubjectType: "SUPPLIER", SubjectID: "8", FlowID: "12", ModuleKey: "CONTRACT"})
	if err != nil || result == nil || !result.Ready || len(result.Contracts) != 1 || result.Contracts[0].RecordID != "21" || result.Contracts[0].Version != 1 || len(result.Credits) != 1 || result.Credits[0].RecordID != "41" || result.Credits[0].Amount != "100.00" {
		t.Fatalf("expected readiness to include exact current version references, got %+v, %v", result, err)
	}
}

func TestF12ReadinessListsMissingFirstOnboardingDocuments(t *testing.T) {
	service := NewF12ReadinessService(&f12ReadinessContextFixture{facts: []*F12BusinessContext{f12ReadinessFacts(3)}}, &f12ReadinessEvidenceFixture{})
	result, err := service.Get(context.Background(), F12ReadinessRequest{SubjectType: "SUPPLIER", SubjectID: "8", FlowID: "12", ModuleKey: "CONTRACT"})
	if err != nil || result == nil || result.Ready || len(result.MissingReasons) != 2 || result.MissingReasons[0] != "CONTRACT_REQUIRED" || result.MissingReasons[1] != "CREDIT_REQUIRED:CNY" {
		t.Fatalf("missing first-flow records must be explicit and block readiness, got %+v, %v", result, err)
	}
}

func TestF12ReadinessRejectsFlowRevisionChangeDuringRead(t *testing.T) {
	contexts := &f12ReadinessContextFixture{facts: []*F12BusinessContext{f12ReadinessFacts(3), f12ReadinessFacts(4)}}
	service := NewF12ReadinessService(contexts, &f12ReadinessEvidenceFixture{})
	_, err := service.Get(context.Background(), F12ReadinessRequest{SubjectType: "SUPPLIER", SubjectID: "8", FlowID: "12", ModuleKey: "CONTRACT"})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("concurrent workflow change must return conflict, got %v", err)
	}
}
