package api

import (
	"math"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplyFormalConfigHandler struct {
	writer SupplyFormalConfigWriter
	reader SupplyFormalConfigReader
}

type createFormalConfigRequest struct {
	Scope             string  `json:"scope"`
	BaseURL           string  `json:"base_url"`
	APIKey            *string `json:"api_key"`
	ExternalConfirmed bool    `json:"external_confirmed"`
	Reason            string  `json:"reason"`
}

type updateFormalConfigRequest struct {
	ExpectedRevision  string  `json:"expected_revision"`
	Scope             string  `json:"scope"`
	BaseURL           string  `json:"base_url"`
	APIKey            *string `json:"api_key"`
	ExternalConfirmed bool    `json:"external_confirmed"`
	Reason            string  `json:"reason"`
}

type disableFormalConfigRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	Reason           string `json:"reason"`
}

func (h *supplyFormalConfigHandler) list(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	supplierID := c.Param("id")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	views, err := h.reader.List(c.Request.Context(), supplierID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result := make([]supply.FormalConfigView, 0, len(views))
	for _, view := range views {
		if !validFormalConfigView(view, supplierID) {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		result = append(result, *view)
	}
	response.Success(c, result)
}

func (h *supplyFormalConfigHandler) ready(c *gin.Context) bool {
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

func (h *supplyFormalConfigHandler) create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	supplierID := c.Param("id")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req createFormalConfigRequest
	if err := decodeSupplyRequest(c, &req, []string{"scope", "base_url", "api_key", "external_confirmed", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if req.APIKey == nil || strings.TrimSpace(*req.APIKey) == "" || !req.ExternalConfirmed {
		supplyHTTPError(c, supplyRequestError("configuration", "a nonempty API key and explicit external confirmation are required"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	view, err := h.writer.Create(c.Request.Context(), meta, supplierID, supply.FormalConfigInput{
		Scope: req.Scope, BaseURL: req.BaseURL, APIKey: req.APIKey, ExternalConfirmed: req.ExternalConfirmed,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if !validFormalConfigView(view, supplierID) || view.Status != "ACTIVE" || view.Revision != 1 || view.ConfirmedRevision != 1 || view.CreatedBy != meta.ActorID || view.UpdatedBy != meta.ActorID || view.ConfirmedBy != meta.ActorID || !view.HasAPIKey {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	h.result(c, view)
}

func (h *supplyFormalConfigHandler) update(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	supplierID, configID := c.Param("id"), c.Param("configId")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(configID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req updateFormalConfigRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "scope", "base_url", "external_confirmed", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
		return
	}
	if !req.ExternalConfirmed || (req.APIKey != nil && strings.TrimSpace(*req.APIKey) == "") {
		supplyHTTPError(c, supplyRequestError("configuration", "external confirmation is required and a supplied API key cannot be empty"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	view, err := h.writer.Update(c.Request.Context(), meta, supplierID, configID, expected, supply.FormalConfigInput{
		Scope: req.Scope, BaseURL: req.BaseURL, APIKey: req.APIKey, ExternalConfirmed: req.ExternalConfirmed,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if !validFormalConfigView(view, supplierID) || view.ID != configID || view.Status != "ACTIVE" || view.Revision < expected || (view.Revision != expected && view.Revision != expected+1) || view.ConfirmedRevision != view.Revision || !view.HasAPIKey {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	h.result(c, view)
}

func (h *supplyFormalConfigHandler) disable(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	supplierID, configID := c.Param("id"), c.Param("configId")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(configID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req disableFormalConfigRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	view, err := h.writer.Disable(c.Request.Context(), meta, supplierID, configID, expected)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	validRevision := view != nil && (view.Revision == expected || (expected < math.MaxInt64 && view.Revision == expected+1))
	if !validFormalConfigView(view, supplierID) || view.ID != configID || view.Status != "INACTIVE" || !validRevision {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	h.result(c, view)
}

func validFormalConfigView(view *supply.FormalConfigView, supplierID string) bool {
	if view == nil || view.SupplierID != supplierID || !positiveBigintString(view.ID) || (view.Region != "CN" && view.Region != "OVERSEAS") || strings.TrimSpace(view.Scope) == "" || strings.TrimSpace(view.BaseURL) == "" || (view.Status != "ACTIVE" && view.Status != "INACTIVE") || view.Revision == 0 || view.Revision > math.MaxInt64 || view.ConfirmedRevision == 0 || view.ConfirmedRevision > view.Revision || !positiveBigintString(view.CreatedBy) || !positiveBigintString(view.UpdatedBy) || !positiveBigintString(view.ConfirmedBy) || view.CreatedAt.IsZero() || view.UpdatedAt.Before(view.CreatedAt) || view.ConfirmedAt.Before(view.CreatedAt) || view.ConfirmedAt.After(view.UpdatedAt) {
		return false
	}
	if view.Status == "ACTIVE" && view.ConfirmedRevision != view.Revision {
		return false
	}
	if view.HasAPIKey != (view.APIKeyMask == "********") {
		return false
	}
	return true
}

func positiveBigintString(value string) bool {
	_, err := planPositiveID(value)
	return err == nil
}

func (h *supplyFormalConfigHandler) result(c *gin.Context, view *supply.FormalConfigView) {
	copy := *view
	middleware.SetIdempotencyResult(c, copy)
	response.Success(c, copy)
}
