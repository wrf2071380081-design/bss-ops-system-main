package supply

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// F12CreditWriteCommand is the normalized input for appending a credit version.
// File ownership, subject authorization, version allocation, and persistence are
// verified by their transaction-bound providers; this validator only checks the
// stable business fields that do not depend on those providers.
type F12CreditWriteCommand struct {
	SubjectType     string
	SubjectID       string
	Direction       string
	Amount          string
	Currency        string
	DocumentName    string
	DocumentNo      string
	FileID          string
	SourceFlowID    string
	SourceModuleKey string
	// ExpectedFlowRevision is a client CAS token. The trusted B/C provider
	// compares it with the current flow under the write transaction; it is
	// never persisted as the source revision.
	ExpectedFlowRevision uint64
	SourceFlowRevision uint64
	Reason          string
}

// F12ContractWriteCommand is the normalized input for a contract version.
// Referenced file ownership and write authorization are checked by the
// transaction-bound providers, not by this request-shape validator.
type F12ContractWriteCommand struct {
	SubjectType     string
	SubjectID       string
	DocumentName    string
	DocumentNo      string
	FileID          string
	SourceFlowID    string
	SourceModuleKey string
	ExpectedFlowRevision uint64
	SourceFlowRevision uint64
	Reason          string
}

func ValidateF12ContractArchiveCommand(input F12ContractArchiveCommand) (F12ContractArchiveCommand, error) {
	input.SourceFlowID = strings.TrimSpace(input.SourceFlowID)
	input.SourceModuleKey = strings.TrimSpace(input.SourceModuleKey)
	if !validF12ExpectedFlowContext(input.SourceFlowID, input.SourceModuleKey, input.ExpectedFlowRevision) {
		return F12ContractArchiveCommand{}, invalid("expected_flow_revision", "flow, module, and expected revision must form a valid source context")
	}
	if input.SourceFlowRevision != 0 {
		return F12ContractArchiveCommand{}, invalid("source_flow_revision", "must be assigned by the trusted flow provider")
	}
	return input, nil
}

func validF12SourceContext(flowID, moduleKey string, revision uint64, requireRevision bool) bool {
	flowID, moduleKey = strings.TrimSpace(flowID), strings.TrimSpace(moduleKey)
	if flowID == "" && moduleKey == "" {
		return revision == 0
	}
	if !validID(flowID) || !validF12Text(moduleKey) {
		return false
	}
	if !requireRevision {
		return revision <= math.MaxInt32
	}
	return revision > 0 && revision <= math.MaxInt32
}

func validF12ExpectedFlowContext(flowID, moduleKey string, expectedRevision uint64) bool {
	flowID, moduleKey = strings.TrimSpace(flowID), strings.TrimSpace(moduleKey)
	if flowID == "" && moduleKey == "" {
		return expectedRevision == 0
	}
	return validID(flowID) && validF12Text(moduleKey) && expectedRevision > 0 && expectedRevision <= math.MaxInt32
}

func ValidateF12ContractWriteCommand(input F12ContractWriteCommand) (F12ContractWriteCommand, error) {
	input.SubjectType = strings.TrimSpace(input.SubjectType)
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.DocumentName = strings.TrimSpace(input.DocumentName)
	input.DocumentNo = strings.TrimSpace(input.DocumentNo)
	input.FileID = strings.TrimSpace(input.FileID)
	input.SourceFlowID = strings.TrimSpace(input.SourceFlowID)
	input.SourceModuleKey = strings.TrimSpace(input.SourceModuleKey)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.SubjectType != "SUPPLIER" && input.SubjectType != "CUSTOMER" {
		return F12ContractWriteCommand{}, invalid("subject_type", "expected SUPPLIER or CUSTOMER")
	}
	if !validID(input.SubjectID) {
		return F12ContractWriteCommand{}, invalid("subject_id", "expected a positive bigint string")
	}
	for _, field := range []struct{ name, value string }{
		{"document_name", input.DocumentName}, {"document_no", input.DocumentNo}, {"file_id", input.FileID}, {"reason", input.Reason},
	} {
		if !validF12Text(field.value) {
			return F12ContractWriteCommand{}, invalid(field.name, "field is required and must be valid text")
		}
	}
	if !validF12ExpectedFlowContext(input.SourceFlowID, input.SourceModuleKey, input.ExpectedFlowRevision) {
		return F12ContractWriteCommand{}, invalid("expected_flow_revision", "flow, module, and expected revision must form a valid source context")
	}
	return input, nil
}

func ValidateF12CreditWriteCommand(input F12CreditWriteCommand) (F12CreditWriteCommand, error) {
	input.SubjectType = strings.TrimSpace(input.SubjectType)
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.Direction = strings.TrimSpace(input.Direction)
	input.Amount = strings.TrimSpace(input.Amount)
	input.Currency = strings.TrimSpace(input.Currency)
	input.DocumentName = strings.TrimSpace(input.DocumentName)
	input.DocumentNo = strings.TrimSpace(input.DocumentNo)
	input.FileID = strings.TrimSpace(input.FileID)
	input.SourceFlowID = strings.TrimSpace(input.SourceFlowID)
	input.SourceModuleKey = strings.TrimSpace(input.SourceModuleKey)
	input.Reason = strings.TrimSpace(input.Reason)

	if input.SubjectType != "SUPPLIER" && input.SubjectType != "CUSTOMER" {
		return F12CreditWriteCommand{}, invalid("subject_type", "expected SUPPLIER or CUSTOMER")
	}
	if !validID(input.SubjectID) {
		return F12CreditWriteCommand{}, invalid("subject_id", "expected a positive bigint string")
	}
	if expected := f12CreditDirection(input.SubjectType); input.Direction != expected {
		return F12CreditWriteCommand{}, invalid("direction", "credit direction does not match subject type")
	}
	if !currencyCode.MatchString(input.Currency) {
		return F12CreditWriteCommand{}, invalid("currency", "expected a three-letter uppercase currency")
	}
	if !f12AmountText.MatchString(input.Amount) {
		return F12CreditWriteCommand{}, invalid("amount", "expected a positive decimal with at most two fractional digits")
	}
	integerPart := strings.SplitN(input.Amount, ".", 2)[0]
	if len(integerPart) > 18 {
		return F12CreditWriteCommand{}, invalid("amount", "amount exceeds the supported precision")
	}
	amount, err := decimal.NewFromString(input.Amount)
	if err != nil || !amount.IsPositive() {
		return F12CreditWriteCommand{}, invalid("amount", "expected a positive decimal with at most two fractional digits")
	}
	input.Amount = amount.StringFixed(2)
	for _, field := range []struct{ name, value string }{
		{"document_name", input.DocumentName}, {"document_no", input.DocumentNo}, {"file_id", input.FileID}, {"reason", input.Reason},
	} {
		if !validF12Text(field.value) {
			return F12CreditWriteCommand{}, invalid(field.name, "field is required and must be valid text")
		}
	}
	if !validF12ExpectedFlowContext(input.SourceFlowID, input.SourceModuleKey, input.ExpectedFlowRevision) {
		return F12CreditWriteCommand{}, invalid("expected_flow_revision", "flow, module, and expected revision must form a valid source context")
	}
	return input, nil
}

func validF12Text(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
