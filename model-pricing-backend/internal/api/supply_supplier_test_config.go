package api

import (
	"math"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplySupplierTestConfigHandler struct {
	service *supply.SupplierTestConfigService
}

type supplierTestConfigSaveRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	Reason           string `json:"reason"`
}

func supplierTestConfigExpectedRevision(raw string) (uint64, error) {
	if raw == "0" {
		return 0, nil
	}
	revision, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || revision == 0 || revision > math.MaxInt32 || strconv.FormatUint(revision, 10) != raw {
		return 0, supplyRequestError("expected_revision", "expected a canonical revision string")
	}
	return revision, nil
}

func (h *supplySupplierTestConfigHandler) ready(c *gin.Context) bool {
	if h == nil || h.service == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return false
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return false
	}
	return true
}

func (h *supplySupplierTestConfigHandler) get(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	supplierID := c.Param("id")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	view, err := h.service.Get(c.Request.Context(), supplyActor(c), supplierID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if view == nil || view.SupplierID != supplierID {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	response.Success(c, view)
}

func (h *supplySupplierTestConfigHandler) save(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	supplierID := c.Param("id")
	if _, err := planPositiveID(supplierID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req supplierTestConfigSaveRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "base_url", "api_key", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	revision, err := supplierTestConfigExpectedRevision(req.ExpectedRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	view, err := h.service.Save(c.Request.Context(), meta, supplierID, supply.SupplierTestConfigCommand{
		BaseURL: req.BaseURL, APIKey: req.APIKey, ExpectedRevision: revision,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if view == nil || view.SupplierID != supplierID || !view.Configured || !view.HasAPIKey || view.Revision <= revision {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	middleware.SetIdempotencyResult(c, view)
	response.Success(c, view)
}
