package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
)

type f12HTTPWriterFake struct {
	contracts int
	credits   int
	actorID   string
	expected  uint64
	command   supply.F12ContractWriteCommand
	archive   supply.F12ContractArchiveCommand
	credit    supply.F12CreditWriteCommand
	wrongCreditDirection bool
	wrongCreditCurrency  bool
	err       error
}

func (w *f12HTTPWriterFake) CreateContract(_ context.Context, meta supply.WriteMeta, input supply.F12ContractWriteCommand) (*supply.F12ContractVersion, error) {
	w.contracts++
	w.actorID, w.command = meta.ActorID, input
	if w.err != nil {
		return nil, w.err
	}
	return &supply.F12ContractVersion{ID: "701", ContractKey: "701", SubjectType: strings.TrimSpace(input.SubjectType), SubjectID: strings.TrimSpace(input.SubjectID), Status: "PROCESSING", DocumentName: strings.TrimSpace(input.DocumentName), DocumentNo: strings.TrimSpace(input.DocumentNo), FileID: strings.TrimSpace(input.FileID), CreatedBy: meta.ActorID, Version: 1, CreatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}, nil
}

func (w *f12HTTPWriterFake) ReviseContract(_ context.Context, meta supply.WriteMeta, key string, expected uint64, input supply.F12ContractWriteCommand) (*supply.F12ContractVersion, error) {
	w.contracts++
	w.actorID, w.expected, w.command = meta.ActorID, expected, input
	if w.err != nil {
		return nil, w.err
	}
	return &supply.F12ContractVersion{ID: "702", ContractKey: key, SubjectType: strings.TrimSpace(input.SubjectType), SubjectID: strings.TrimSpace(input.SubjectID), Status: "PROCESSING", DocumentName: strings.TrimSpace(input.DocumentName), DocumentNo: strings.TrimSpace(input.DocumentNo), FileID: strings.TrimSpace(input.FileID), CreatedBy: meta.ActorID, Version: expected + 1, CreatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}, nil
}

func (w *f12HTTPWriterFake) ArchiveContract(_ context.Context, meta supply.WriteMeta, key string, expected uint64, input supply.F12ContractArchiveCommand) (*supply.F12ContractVersion, error) {
	w.contracts++
	w.actorID, w.expected, w.archive = meta.ActorID, expected, input
	if w.err != nil {
		return nil, w.err
	}
	return &supply.F12ContractVersion{ID: "702", ContractKey: key, SubjectType: "SUPPLIER", SubjectID: "10", Status: "ARCHIVED", SourceFlowID: input.SourceFlowID, SourceModuleKey: input.SourceModuleKey, SourceFlowRevision: input.ExpectedFlowRevision, CreatedBy: meta.ActorID, Version: expected + 1, CreatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}, nil
}

func (w *f12HTTPWriterFake) AddCreditVersion(_ context.Context, meta supply.WriteMeta, expected uint64, input supply.F12CreditWriteCommand) (*supply.F12CreditVersion, error) {
	w.credits++
	w.actorID, w.expected, w.credit = meta.ActorID, expected, input
	if w.err != nil {
		return nil, w.err
	}
	direction := input.Direction
	currency := input.Currency
	if w.wrongCreditDirection {
		direction = "SUPPLIER_TO_US"
	}
	if w.wrongCreditCurrency {
		currency = "USD"
	}
	return &supply.F12CreditVersion{ID: "801", SubjectType: strings.TrimSpace(input.SubjectType), SubjectID: strings.TrimSpace(input.SubjectID), Direction: strings.TrimSpace(direction), Amount: input.Amount, Currency: strings.TrimSpace(currency), DocumentName: strings.TrimSpace(input.DocumentName), DocumentNo: strings.TrimSpace(input.DocumentNo), FileID: strings.TrimSpace(input.FileID), CreatedBy: meta.ActorID, Version: expected + 1, CreatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}, nil
}

func f12WriteRouter(writer SupplyF12Writer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(infra.RequestID())
	r.Use(func(c *gin.Context) { c.Set("auth.operator", supplyHTTPStaff()); c.Next() })
	RegisterSupplyRoutes(r.Group("/api/internal"), SupplyDeps{IdemStore: newSupplyHTTPIdemStore(), F12Writer: writer})
	return r
}

func TestF12WriteHTTPUsesAuthenticatedActorAndIdempotentCreate(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10","file_id":"d07-file-10","reason":"首次登记"}`
	for i := 0; i < 2; i++ {
		rec := supplyHTTPCall(r, "POST", "/api/internal/contracts", body, "f12-contract-create", "trace")
		if rec.Code != 200 {
			t.Fatalf("create %d %s", rec.Code, rec.Body)
		}
	}
	if writer.contracts != 1 || writer.actorID != "7" || writer.command.SubjectID != "10" || writer.command.FileID != "d07-file-10" {
		t.Fatalf("write was not bound to authenticated actor/subject: %+v", writer)
	}
}

func TestF12WriteHTTPUsesCanonicalValuesForCommitAndReceiptValidation(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	contractBody := `{"subject_type":" SUPPLIER ","subject_id":" 10 ","document_name":" 采购合同 ","document_no":" S-10 ","file_id":" d07-file-10 ","reason":" 首次登记 "}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/contracts", contractBody, "f12-normalized-contract", "trace")
	if rec.Code != 200 || writer.contracts != 1 || writer.command.SubjectType != "SUPPLIER" || writer.command.SubjectID != "10" || writer.command.DocumentName != "采购合同" || writer.command.DocumentNo != "S-10" || writer.command.FileID != "d07-file-10" {
		t.Fatalf("contract command and committed receipt must use the same canonical values: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}

	creditBody := `{"subject_type":" CUSTOMER ","subject_id":" 10 ","direction":" US_TO_CUSTOMER ","amount":" 500 ","currency":" CNY ","document_name":" 授信函 ","document_no":" C-10 ","file_id":" d07-file-11 ","expected_latest_version":"0","reason":" 首次授信 "}`
	rec = supplyHTTPCall(r, "POST", "/api/internal/credits", creditBody, "f12-normalized-credit", "trace")
	if rec.Code != 200 || writer.credits != 1 || writer.credit.SubjectType != "CUSTOMER" || writer.credit.SubjectID != "10" || writer.credit.Direction != "US_TO_CUSTOMER" || writer.credit.Amount != "500.00" || writer.credit.Currency != "CNY" || writer.credit.FileID != "d07-file-11" {
		t.Fatalf("credit command and committed receipt must use the same canonical values: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}
}

func TestF12WriteHTTPRejectsSelfReportedActorAndStaleVersion(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10","file_id":"d07-file-10","reason":"登记","created_by":"8"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/contracts", body, "f12-self-actor", "trace")
	if rec.Code != 400 || writer.contracts != 0 {
		t.Fatalf("self-reported actor reached writer: status=%d body=%s calls=%d", rec.Code, rec.Body, writer.contracts)
	}
	writer.err = supply.ErrRevisionConflict
	body = `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-11","file_id":"d07-file-11","expected_latest_version":"2","reason":"修订"}`
	rec = supplyHTTPCall(r, "POST", "/api/internal/contracts/701/versions", body, "f12-stale", "trace")
	if rec.Code != 409 || writer.expected != 2 {
		t.Fatalf("stale version was not mapped/forwarded: status=%d body=%s expected=%d", rec.Code, rec.Body, writer.expected)
	}
}

func TestF12WriteHTTPMapsArchivedContractToConflict(t *testing.T) {
	writer := &f12HTTPWriterFake{err: supply.ErrF12ContractArchived}
	router := f12WriteRouter(writer)
	body := `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10-v2","file_id":"d07-file-12","expected_latest_version":"1","reason":"修订"}`
	rec := supplyHTTPCall(router, "POST", "/api/internal/contracts/701/versions", body, "f12-archived-contract-revision", "trace")
	if rec.Code != 409 || writer.contracts != 1 {
		t.Fatalf("archived contract revision should map to a conflict: status=%d body=%s calls=%d", rec.Code, rec.Body, writer.contracts)
	}
}

func TestF12WriteHTTPRequiresExpectedFlowRevisionForFlowWrites(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10","file_id":"d07-file-10","source_flow_id":"41","source_module_key":"CONTRACT","reason":"首次登记"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/contracts", body, "f12-flow-stale-missing-rev", "trace")
	if rec.Code != 400 || writer.contracts != 0 {
		t.Fatalf("flow write without expected revision reached service: status=%d body=%s calls=%d", rec.Code, rec.Body, writer.contracts)
	}
	body = `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10","file_id":"d07-file-10","source_flow_id":"41","source_module_key":"CONTRACT","expected_flow_revision":"12","reason":"首次登记"}`
	rec = supplyHTTPCall(r, "POST", "/api/internal/contracts", body, "f12-flow-valid-rev", "trace")
	if rec.Code != 200 || writer.contracts != 1 || writer.command.ExpectedFlowRevision != 12 {
		t.Fatalf("flow CAS revision was not parsed and forwarded: status=%d body=%s command=%+v", rec.Code, rec.Body, writer.command)
	}
}

func TestF12ContractRevisionAndCreditHTTPForwardFlowCASContext(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	router := f12WriteRouter(writer)
	revisionBody := `{"subject_type":"SUPPLIER","subject_id":"10","document_name":"采购合同","document_no":"S-10-v2","file_id":"d07-file-12","source_flow_id":"41","source_module_key":"CONTRACT","expected_flow_revision":"13","expected_latest_version":"1","reason":"修订合同"}`
	rec := supplyHTTPCall(router, "POST", "/api/internal/contracts/701/versions", revisionBody, "f12-flow-revise", "trace")
	if rec.Code != 200 || writer.contracts != 1 || writer.expected != 1 || writer.command.SourceFlowID != "41" || writer.command.SourceModuleKey != "CONTRACT" || writer.command.ExpectedFlowRevision != 13 {
		t.Fatalf("contract revision flow context was not forwarded: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}
	creditBody := `{"subject_type":"CUSTOMER","subject_id":"10","direction":"US_TO_CUSTOMER","amount":"500.00","currency":"CNY","document_name":"授信函","document_no":"C-10","file_id":"d07-file-13","source_flow_id":"42","source_module_key":"CREDIT","expected_flow_revision":"8","expected_latest_version":"0","reason":"首次授信"}`
	rec = supplyHTTPCall(router, "POST", "/api/internal/credits", creditBody, "f12-flow-credit", "trace")
	if rec.Code != 200 || writer.credits != 1 || writer.expected != 0 || writer.credit.SourceFlowID != "42" || writer.credit.SourceModuleKey != "CREDIT" || writer.credit.ExpectedFlowRevision != 8 {
		t.Fatalf("credit flow context was not forwarded: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}
}

func TestF12ArchiveHTTPForwardsFlowCASContext(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"expected_latest_version":"2","source_flow_id":"41","source_module_key":"CONTRACT","expected_flow_revision":"12","reason":"完成流程归档"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/contracts/701/archive", body, "f12-flow-archive", "trace")
	if rec.Code != 200 || writer.contracts != 1 || writer.expected != 2 || writer.archive.SourceFlowID != "41" || writer.archive.SourceModuleKey != "CONTRACT" || writer.archive.ExpectedFlowRevision != 12 {
		t.Fatalf("flow archive context not forwarded: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}
}

func TestF12ArchiveHTTPRejectsFlowWithoutCASRevision(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"expected_latest_version":"2","source_flow_id":"41","source_module_key":"CONTRACT","reason":"完成流程归档"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/contracts/701/archive", body, "f12-flow-archive-missing-revision", "trace")
	if rec.Code != 400 || writer.contracts != 0 {
		t.Fatalf("flow archive without expected revision reached writer: status=%d body=%s calls=%d", rec.Code, rec.Body, writer.contracts)
	}
}

func TestF12CreditHTTPAllowsInitialVersionAndMapsVersionConflict(t *testing.T) {
	writer := &f12HTTPWriterFake{}
	r := f12WriteRouter(writer)
	body := `{"subject_type":"CUSTOMER","subject_id":"10","direction":"US_TO_CUSTOMER","amount":"500.00","currency":"CNY","document_name":"授信函","document_no":"C-10","file_id":"d07-file-11","expected_latest_version":"0","reason":"首次授信"}`
	rec := supplyHTTPCall(r, "POST", "/api/internal/credits", body, "f12-credit-initial", "trace")
	if rec.Code != 200 || writer.credits != 1 || writer.expected != 0 || writer.credit.Direction != "US_TO_CUSTOMER" || writer.actorID != "7" {
		t.Fatalf("initial credit request failed: status=%d body=%s writer=%+v", rec.Code, rec.Body, writer)
	}
	writer.err = supply.ErrRevisionConflict
	body = `{"subject_type":"CUSTOMER","subject_id":"10","direction":"US_TO_CUSTOMER","amount":"450.00","currency":"CNY","document_name":"授信函","document_no":"C-10-v2","file_id":"d07-file-12","expected_latest_version":"1","reason":"额度调整"}`
	rec = supplyHTTPCall(r, "POST", "/api/internal/credits", body, "f12-credit-v2", "trace")
	if rec.Code != 409 || writer.expected != 1 {
		t.Fatalf("credit version conflict mismatch: status=%d body=%s expected=%d", rec.Code, rec.Body, writer.expected)
	}
}

func TestF12CreditHTTPRejectsWriterReceiptWithWrongDirectionOrCurrency(t *testing.T) {
	for _, tc := range []struct {
		name, key       string
		wrongDirection bool
		wrongCurrency  bool
	}{
		{name: "direction", key: "f12-credit-invalid-direction", wrongDirection: true},
		{name: "currency", key: "f12-credit-invalid-currency", wrongCurrency: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &f12HTTPWriterFake{wrongCreditDirection: tc.wrongDirection, wrongCreditCurrency: tc.wrongCurrency}
			r := f12WriteRouter(writer)
			body := `{"subject_type":"CUSTOMER","subject_id":"10","direction":"US_TO_CUSTOMER","amount":"500.00","currency":"CNY","document_name":"授信函","document_no":"C-10","file_id":"d07-file-11","expected_latest_version":"0","reason":"首次授信"}`
			rec := supplyHTTPCall(r, "POST", "/api/internal/credits", body, tc.key, "trace")
			if rec.Code != 500 || writer.credits != 1 || strings.Contains(rec.Body.String(), `"code":0`) {
				t.Fatalf("credit writer receipt with mismatched %s was returned as success: status=%d body=%s calls=%d", tc.name, rec.Code, rec.Body, writer.credits)
			}
		})
	}
}

func TestF12WriteHTTPIsUnavailableWithoutWriterAndWithoutIdempotency(t *testing.T) {
	rec := supplyHTTPCall(f12WriteRouter(nil), "POST", "/api/internal/contracts", `{}`, "missing-writer", "trace")
	if rec.Code != 503 {
		t.Fatalf("missing writer status=%d body=%s", rec.Code, rec.Body)
	}
	writer := &f12HTTPWriterFake{}
	rec = supplyHTTPCall(f12WriteRouter(writer), "POST", "/api/internal/contracts", `{}`, "", "trace")
	if rec.Code != 400 || writer.contracts != 0 {
		t.Fatalf("missing idempotency key status=%d calls=%d", rec.Code, writer.contracts)
	}
}
