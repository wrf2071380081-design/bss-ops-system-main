package supply

import "testing"

func TestValidateF12CreditWriteCommandNormalizesBothSubjectDirections(t *testing.T) {
	tests := []struct {
		name       string
		command    F12CreditWriteCommand
		wantAmount string
		wantName   string
	}{
		{
			name:       "supplier",
			command:    F12CreditWriteCommand{SubjectType: "SUPPLIER", SubjectID: "10", Direction: "SUPPLIER_TO_US", Amount: "100", Currency: "USD", DocumentName: "  供应商授信  ", DocumentNo: "  S-10  ", FileID: "  file-s-10  ", Reason: "  新增  "},
			wantAmount: "100.00",
			wantName:   "供应商授信",
		},
		{
			name:       "customer",
			command:    F12CreditWriteCommand{SubjectType: "CUSTOMER", SubjectID: "10", Direction: "US_TO_CUSTOMER", Amount: "80.5", Currency: "CNY", DocumentName: "客户授信", DocumentNo: "C-10", FileID: "file-c-10", Reason: "调整"},
			wantAmount: "80.50",
			wantName:   "客户授信",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateF12CreditWriteCommand(tt.command)
			if err != nil {
				t.Fatalf("ValidateF12CreditWriteCommand() error = %v", err)
			}
			if got.Amount != tt.wantAmount || got.DocumentName != tt.wantName || got.DocumentNo == "" || got.FileID == "" || got.Reason == "" {
				t.Fatalf("normalized command = %#v, want amount %s, name %q, and required trimmed fields", got, tt.wantAmount, tt.wantName)
			}
		})
	}
}

func TestValidateF12CreditWriteCommandRejectsWrongDirectionAndInvalidAmount(t *testing.T) {
	base := F12CreditWriteCommand{SubjectType: "SUPPLIER", SubjectID: "10", Direction: "SUPPLIER_TO_US", Amount: "1.00", Currency: "USD", DocumentName: "授信", DocumentNo: "S-10", FileID: "file-10", Reason: "新增"}
	tests := []struct {
		name   string
		change func(*F12CreditWriteCommand)
	}{
		{name: "customer direction on supplier", change: func(c *F12CreditWriteCommand) { c.Direction = "US_TO_CUSTOMER" }},
		{name: "zero amount", change: func(c *F12CreditWriteCommand) { c.Amount = "0" }},
		{name: "negative amount", change: func(c *F12CreditWriteCommand) { c.Amount = "-1.00" }},
		{name: "too many decimals", change: func(c *F12CreditWriteCommand) { c.Amount = "1.001" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := base
			tt.change(&command)
			if _, err := ValidateF12CreditWriteCommand(command); err == nil {
				t.Fatal("ValidateF12CreditWriteCommand() accepted an invalid direction or amount")
			}
		})
	}
}

func TestValidateF12ContractWriteCommandRequiresSubjectDocumentAndFile(t *testing.T) {
	base := F12ContractWriteCommand{SubjectType: "CUSTOMER", SubjectID: "10", DocumentName: "  客户合同  ", DocumentNo: "  C-10  ", FileID: "  file-c-10  ", Reason: "  新增  "}
	got, err := ValidateF12ContractWriteCommand(base)
	if err != nil {
		t.Fatalf("ValidateF12ContractWriteCommand() error = %v", err)
	}
	if got.SubjectID != "10" || got.DocumentName != "客户合同" || got.DocumentNo != "C-10" || got.FileID != "file-c-10" || got.Reason != "新增" {
		t.Fatalf("normalized command = %#v", got)
	}
	for _, tc := range []struct {
		name   string
		change func(*F12ContractWriteCommand)
	}{
		{name: "invalid subject type", change: func(c *F12ContractWriteCommand) { c.SubjectType = "ACCOUNT" }},
		{name: "missing document number", change: func(c *F12ContractWriteCommand) { c.DocumentNo = " " }},
		{name: "missing file", change: func(c *F12ContractWriteCommand) { c.FileID = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := base
			tc.change(&command)
			if _, err := ValidateF12ContractWriteCommand(command); err == nil {
				t.Fatal("ValidateF12ContractWriteCommand() accepted an invalid contract write")
			}
		})
	}
}

func TestF12FlowWriteRequiresSeparateExpectedRevisionCAS(t *testing.T) {
	base := F12ContractWriteCommand{
		SubjectType: "SUPPLIER", SubjectID: "10", DocumentName: "采购合同", DocumentNo: "S-10", FileID: "f12-file",
		SourceFlowID: "41", SourceModuleKey: "CONTRACT", Reason: "首次办理",
	}
	if _, err := ValidateF12ContractWriteCommand(base); err == nil {
		t.Fatal("flow write without an expected flow revision was accepted")
	}
	base.ExpectedFlowRevision = 12
	if _, err := ValidateF12ContractWriteCommand(base); err != nil {
		t.Fatalf("valid flow CAS token was rejected: %v", err)
	}
	base.SourceFlowID = ""
	base.SourceModuleKey = ""
	if _, err := ValidateF12ContractWriteCommand(base); err == nil {
		t.Fatal("ordinary write accepted a detached flow revision token")
	}
}
