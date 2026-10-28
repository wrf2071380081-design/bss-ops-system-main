package api

import (
	"encoding/base64"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

const f12AttachmentRequestMaxBytes = 3 << 20

var f12AttachmentFileIDPattern = regexp.MustCompile(`^f12:v1:[a-f0-9]{32}$`)

type supplyF12AttachmentHandler struct{ service SupplyF12AttachmentService }

type f12AttachmentUploadRequest struct {
	SubjectType          string `json:"subject_type"`
	SubjectID            string `json:"subject_id"`
	Purpose              string `json:"purpose"`
	SourceFlowID         string `json:"source_flow_id"`
	SourceModuleKey      string `json:"source_module_key"`
	ExpectedFlowRevision string `json:"expected_flow_revision"`
	FileName             string `json:"file_name"`
	ContentBase64        string `json:"content_base64"`
	Reason               string `json:"reason"`
}

func (h *supplyF12AttachmentHandler) upload(c *gin.Context) {
	if h == nil || h.service == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req f12AttachmentUploadRequest
	if err := decodeSupplyRequestLimit(c, &req, []string{"subject_type", "subject_id", "purpose", "file_name", "content_base64", "reason"}, f12AttachmentRequestMaxBytes); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expectedFlowRevision, err := f12ExpectedFlowRevision(req.SourceFlowID, req.SourceModuleKey, req.ExpectedFlowRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	content, err := base64.StdEncoding.Strict().DecodeString(req.ContentBase64)
	if err != nil || len(content) == 0 || len(content) > supply.F12AttachmentMaxFileBytes {
		supplyHTTPError(c, &supply.FieldError{Path: "file", Reason: "invalid file encoding or size exceeds 2 MiB"})
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.service.Upload(c.Request.Context(), meta, supply.F12AttachmentUploadCommand{
		SubjectType: req.SubjectType, SubjectID: req.SubjectID, Purpose: req.Purpose,
		SourceFlowID: req.SourceFlowID, SourceModuleKey: req.SourceModuleKey, ExpectedFlowRevision: expectedFlowRevision,
		FileName: req.FileName, Content: content,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if result == nil || result.ID == "" || result.SubjectType != req.SubjectType || result.SubjectID != req.SubjectID || result.Purpose != req.Purpose || result.UploadedBy != meta.ActorID || result.Size != len(content) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, result)
	response.Success(c, result)
}

func (h *supplyF12AttachmentHandler) download(c *gin.Context) {
	if h == nil || h.service == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, err := supplyQuery(c, "subject_type", "subject_id", "purpose")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	fileID := c.Param("fileID")
	if !f12AttachmentFileIDPattern.MatchString(fileID) {
		supplyHTTPError(c, supply.ErrNotFound)
		return
	}
	file, err := h.service.Download(c.Request.Context(), supplyActor(c), values.Get("subject_type"), values.Get("subject_id"), fileID, values.Get("purpose"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if file == nil || file.ID != fileID || file.Size != len(file.Content) || file.Size < 1 || file.Size > supply.F12AttachmentMaxFileBytes || file.Name == "" || strings.ContainsAny(file.Name, `/\\`) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	switch file.ContentType {
	case "application/pdf", "image/png", "image/jpeg":
	default:
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Name})
	if disposition == "" {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", disposition)
	c.Data(http.StatusOK, file.ContentType, file.Content)
}
