package api

import (
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type supplyF12WriteHandler struct{ writer SupplyF12Writer }

type f12ContractWriteRequest struct {
	SubjectType     string `json:"subject_type"`
	SubjectID       string `json:"subject_id"`
	DocumentName    string `json:"document_name"`
	DocumentNo      string `json:"document_no"`
	FileID          string `json:"file_id"`
	SourceFlowID    string `json:"source_flow_id"`
	SourceModuleKey string `json:"source_module_key"`
	ExpectedFlowRevision string `json:"expected_flow_revision"`
	Reason          string `json:"reason"`
}

type f12ContractRevisionRequest struct {
	SubjectType           string `json:"subject_type"`
	SubjectID             string `json:"subject_id"`
	DocumentName          string `json:"document_name"`
	DocumentNo            string `json:"document_no"`
	FileID                string `json:"file_id"`
	SourceFlowID          string `json:"source_flow_id"`
	SourceModuleKey       string `json:"source_module_key"`
	ExpectedFlowRevision  string `json:"expected_flow_revision"`
	ExpectedLatestVersion string `json:"expected_latest_version"`
	Reason                string `json:"reason"`
}

type f12ContractArchiveRequest struct {
	ExpectedLatestVersion string `json:"expected_latest_version"`
	SourceFlowID          string `json:"source_flow_id"`
	SourceModuleKey       string `json:"source_module_key"`
	ExpectedFlowRevision  string `json:"expected_flow_revision"`
	Reason                string `json:"reason"`
}

type f12CreditWriteRequest struct {
	SubjectType          string `json:"subject_type"`
	SubjectID            string `json:"subject_id"`
	Direction            string `json:"direction"`
	Amount               string `json:"amount"`
	Currency             string `json:"currency"`
	DocumentName         string `json:"document_name"`
	DocumentNo           string `json:"document_no"`
	FileID               string `json:"file_id"`
	SourceFlowID         string `json:"source_flow_id"`
	SourceModuleKey      string `json:"source_module_key"`
	ExpectedFlowRevision string `json:"expected_flow_revision"`
	ExpectedLatestVersion string `json:"expected_latest_version"`
	Reason               string `json:"reason"`
}

func (h *supplyF12WriteHandler) ready(c *gin.Context) bool {
	if h == nil || h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return false
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return false
	}
	return true
}

func f12ExpectedVersion(raw string, allowZero bool) (uint64, error) {
	if allowZero && raw == "0" {
		return 0, nil
	}
	version, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || version == 0 || version > math.MaxInt32 || strconv.FormatUint(version, 10) != raw {
		return 0, supplyRequestError("expected_latest_version", "expected a canonical version string")
	}
	return version, nil
}

func f12ExpectedFlowRevision(flowID, moduleKey, raw string) (uint64, error) {
	flowID, moduleKey, raw = strings.TrimSpace(flowID), strings.TrimSpace(moduleKey), strings.TrimSpace(raw)
	if flowID == "" && moduleKey == "" {
		if raw == "" || raw == "0" {
			return 0, nil
		}
		return 0, supplyRequestError("expected_flow_revision", "must be omitted for ordinary maintenance")
	}
	if flowID == "" || moduleKey == "" {
		return 0, supplyRequestError("source_flow_id", "flow and module must be provided together")
	}
	revision, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || revision == 0 || revision > math.MaxInt32 || strconv.FormatUint(revision, 10) != raw {
		return 0, supplyRequestError("expected_flow_revision", "flow writes require a canonical positive revision")
	}
	return revision, nil
}

func f12ContractCommand(req f12ContractWriteRequest, expectedFlowRevision uint64) (supply.F12ContractWriteCommand, error) {
	return supply.ValidateF12ContractWriteCommand(supply.F12ContractWriteCommand{
		SubjectType: req.SubjectType, SubjectID: req.SubjectID, DocumentName: req.DocumentName,
		DocumentNo: req.DocumentNo, FileID: req.FileID, SourceFlowID: req.SourceFlowID,
		SourceModuleKey: req.SourceModuleKey, ExpectedFlowRevision: expectedFlowRevision, Reason: req.Reason,
	})
}

func (h *supplyF12WriteHandler) createContract(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req f12ContractWriteRequest
	if err := decodeSupplyRequest(c, &req, []string{"subject_type", "subject_id", "document_name", "document_no", "file_id", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expectedFlowRevision, err := f12ExpectedFlowRevision(req.SourceFlowID, req.SourceModuleKey, req.ExpectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	command, err := f12ContractCommand(req, expectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	row, err := h.writer.CreateContract(c.Request.Context(), meta, command)
	if err == nil && (row == nil || row.SubjectType != command.SubjectType || row.SubjectID != command.SubjectID || row.CreatedBy != meta.ActorID || row.Version != 1 || row.Status != "PROCESSING" || row.ID != row.ContractKey || row.DocumentName != command.DocumentName || row.DocumentNo != command.DocumentNo || row.FileID != command.FileID) {
		err = supply.ErrReadContract
	}
	h.result(c, row, err)
}

func (h *supplyF12WriteHandler) reviseContract(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	contractKey := c.Param("contractKey")
	if _, err := planPositiveID(contractKey); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req f12ContractRevisionRequest
	if err := decodeSupplyRequest(c, &req, []string{"subject_type", "subject_id", "document_name", "document_no", "file_id", "expected_latest_version", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := f12ExpectedVersion(req.ExpectedLatestVersion, false)
	if err != nil || expected == math.MaxInt32 {
		supplyHTTPError(c, supplyRequestError("expected_latest_version", "invalid version"))
		return
	}
	expectedFlowRevision, err := f12ExpectedFlowRevision(req.SourceFlowID, req.SourceModuleKey, req.ExpectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	command, err := supply.ValidateF12ContractWriteCommand(supply.F12ContractWriteCommand{
		SubjectType: req.SubjectType, SubjectID: req.SubjectID, DocumentName: req.DocumentName, DocumentNo: req.DocumentNo,
		FileID: req.FileID, SourceFlowID: req.SourceFlowID, SourceModuleKey: req.SourceModuleKey,
		ExpectedFlowRevision: expectedFlowRevision, Reason: req.Reason,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	row, err := h.writer.ReviseContract(c.Request.Context(), meta, contractKey, expected, command)
	if err == nil && (row == nil || row.ContractKey != contractKey || row.SubjectType != command.SubjectType || row.SubjectID != command.SubjectID || row.CreatedBy != meta.ActorID || row.Version != expected+1 || row.Status == "ARCHIVED" || row.DocumentName != command.DocumentName || row.DocumentNo != command.DocumentNo || row.FileID != command.FileID) {
		err = supply.ErrReadContract
	}
	h.result(c, row, err)
}

func (h *supplyF12WriteHandler) archiveContract(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	contractKey := c.Param("contractKey")
	if _, err := planPositiveID(contractKey); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req f12ContractArchiveRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_latest_version", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := f12ExpectedVersion(req.ExpectedLatestVersion, false)
	if err != nil || expected == math.MaxInt32 {
		supplyHTTPError(c, supplyRequestError("expected_latest_version", "invalid version"))
		return
	}
	expectedFlowRevision, err := f12ExpectedFlowRevision(req.SourceFlowID, req.SourceModuleKey, req.ExpectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	command := supply.F12ContractArchiveCommand{SourceFlowID: strings.TrimSpace(req.SourceFlowID), SourceModuleKey: strings.TrimSpace(req.SourceModuleKey), ExpectedFlowRevision: expectedFlowRevision}
	row, err := h.writer.ArchiveContract(c.Request.Context(), meta, contractKey, expected, command)
	if err == nil && (row == nil || row.ContractKey != contractKey || row.CreatedBy != meta.ActorID || row.Version != expected+1 || row.Status != "ARCHIVED" || row.SourceFlowID != command.SourceFlowID || row.SourceModuleKey != command.SourceModuleKey || row.SourceFlowRevision != expectedFlowRevision) {
		err = supply.ErrReadContract
	}
	h.result(c, row, err)
}

func (h *supplyF12WriteHandler) addCreditVersion(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req f12CreditWriteRequest
	if err := decodeSupplyRequest(c, &req, []string{"subject_type", "subject_id", "direction", "amount", "currency", "document_name", "document_no", "file_id", "expected_latest_version", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := f12ExpectedVersion(req.ExpectedLatestVersion, true)
	if err != nil || expected == math.MaxInt32 {
		supplyHTTPError(c, supplyRequestError("expected_latest_version", "invalid version"))
		return
	}
	expectedFlowRevision, err := f12ExpectedFlowRevision(req.SourceFlowID, req.SourceModuleKey, req.ExpectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	command := supply.F12CreditWriteCommand{
		SubjectType: req.SubjectType, SubjectID: req.SubjectID, Direction: req.Direction, Amount: req.Amount,
		Currency: req.Currency, DocumentName: req.DocumentName, DocumentNo: req.DocumentNo, FileID: req.FileID,
		SourceFlowID: req.SourceFlowID, SourceModuleKey: req.SourceModuleKey, ExpectedFlowRevision: expectedFlowRevision, Reason: req.Reason,
	}
	command, err = supply.ValidateF12CreditWriteCommand(command)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	row, err := h.writer.AddCreditVersion(c.Request.Context(), meta, expected, command)
	if err == nil && (row == nil || row.SubjectType != command.SubjectType || row.SubjectID != command.SubjectID || row.CreatedBy != meta.ActorID || row.Version != expected+1 || row.Amount != command.Amount || row.Direction != command.Direction || row.Currency != command.Currency || row.DocumentName != command.DocumentName || row.DocumentNo != command.DocumentNo || row.FileID != command.FileID) {
		err = supply.ErrReadContract
	}
	h.result(c, row, err)
}

func (h *supplyF12WriteHandler) result(c *gin.Context, row any, err error) {
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if row == nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, row)
	response.Success(c, row)
}
