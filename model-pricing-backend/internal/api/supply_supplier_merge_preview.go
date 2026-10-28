package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplySupplierMergePreviewHandler struct {
	reader SupplySupplierMergePreviewReader
}

type supplySupplierMergePreviewRequest struct {
	TargetSupplierID string `json:"target_supplier_id"`
}

func (h *supplySupplierMergePreviewHandler) preview(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	sourceID := c.Param("id")
	if _, err := planPositiveID(sourceID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req supplySupplierMergePreviewRequest
	if err := decodeSupplyRequest(c, &req, []string{"target_supplier_id"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(strings.TrimSpace(req.TargetSupplierID)); err != nil {
		supplyHTTPError(c, supplyRequestError("target_supplier_id", "invalid supplier id"))
		return
	}
	if strings.TrimSpace(req.TargetSupplierID) == sourceID {
		supplyHTTPError(c, supplyRequestError("target_supplier_id", "source and target must differ"))
		return
	}
	result, err := h.reader.Preview(c.Request.Context(), sourceID, strings.TrimSpace(req.TargetSupplierID))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if result == nil || result.Source.ID != sourceID || result.Target.ID != strings.TrimSpace(req.TargetSupplierID) || result.Source.ID == result.Target.ID {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	response.Success(c, result)
}
