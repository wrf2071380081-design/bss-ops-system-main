package api

import (
	"github.com/gin-gonic/gin"
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
	"strings"
)

type supplyApprovedPriceHandler struct{ writer SupplyApprovedPriceWriter }
type approvedPriceRequest struct {
	ExpectedRevision  string `json:"expected_revision"`
	SourceRowID       string `json:"source_row_id"`
	SourceRevision    string `json:"source_revision"`
	Coefficient       string `json:"coefficient"`
	ExternalConfirmed bool   `json:"external_confirmed"`
	CompletionNote    string `json:"completion_note"`
	Reason            string `json:"reason"`
}

func (h *supplyApprovedPriceHandler) apply(c *gin.Context) {
	if h == nil || h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(c.Param("id")); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req approvedPriceRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "source_row_id", "source_revision", "coefficient", "external_confirmed", "completion_note", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected >= math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid incrementable revision"))
		return
	}
	if _, err := planPositiveID(req.SourceRowID); err != nil {
		supplyHTTPError(c, supplyRequestError("source_row_id", "invalid submission row"))
		return
	}
	revision, err := planPositiveID(req.SourceRevision)
	if err != nil {
		supplyHTTPError(c, supplyRequestError("source_revision", "invalid source revision"))
		return
	}
	note := strings.TrimSpace(req.CompletionNote)
	if !req.ExternalConfirmed || note == "" || len(note) > 2000 {
		supplyHTTPError(c, supplyRequestError("completion_note", "external completion confirmation and evidence required"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	resource, err := h.writer.Change(supplypg.WithApprovedPriceCompletion(c.Request.Context(), note), meta, c.Param("id"), expected, req.Coefficient, supply.CoefficientSource{Kind: "SUBMISSION", ID: req.SourceRowID, Revision: revision})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if resource == nil || resource.ID != c.Param("id") || (resource.Revision != expected && resource.Revision != expected+1) {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	receipt := gin.H{"resource_id": resource.ID, "revision": uintString(resource.Revision), "coefficient": resource.Coefficient, "changed": resource.Revision != expected}
	middleware.SetIdempotencyResult(c, receipt)
	response.Success(c, receipt)
}
