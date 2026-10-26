package api

import (
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type supplyPricePlanWriteHandler struct{ writer SupplyPricePlanWriter }

func validPricePlanSupplierModelID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 128 && !strings.ContainsRune(value, 0)
}

type createPricePlanRequest struct {
	ResourceID               string `json:"resource_id"`
	SupplierModelID          string `json:"supplier_model_id"`
	ExpectedResourceRevision string `json:"expected_resource_revision"`
	AfterCoefficient         string `json:"after_coefficient"`
	AssigneeID               string `json:"assignee_id"`
	DueAt                    string `json:"due_at"`
	Reason                   string `json:"reason"`
}

type revisePricePlanRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	SupplierModelID  string `json:"supplier_model_id"`
	AfterCoefficient string `json:"after_coefficient"`
	AssigneeID       string `json:"assignee_id"`
	DueAt            string `json:"due_at"`
	Reason           string `json:"reason"`
}

func (h *supplyPricePlanWriteHandler) ready(c *gin.Context) bool {
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

func (h *supplyPricePlanWriteHandler) create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req createPricePlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"resource_id", "supplier_model_id", "expected_resource_revision", "after_coefficient", "assignee_id", "due_at", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(req.ResourceID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if strings.TrimSpace(req.SupplierModelID) == "" || len(req.SupplierModelID) > 128 || strings.ContainsRune(req.SupplierModelID, 0) {
		supplyHTTPError(c, supplyRequestError("supplier_model_id", "expected resource supplier model identifier"))
		return
	}
	expected, err := planPositiveID(req.ExpectedResourceRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(req.AssigneeID); err != nil {
		supplyHTTPError(c, supplyRequestError("assignee_id", "expected active staff bigint string"))
		return
	}
	due, err := planDue(req.DueAt)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	plan, err := h.writer.CreatePriceChange(c.Request.Context(), meta, req.ResourceID, expected, req.SupplierModelID, req.AfterCoefficient, req.AssigneeID, due)
	if err == nil && (plan == nil || plan.ResourceID != req.ResourceID || plan.SupplierModelID != req.SupplierModelID || plan.AfterSupplierModelID != req.SupplierModelID || !validPricePlanSupplierModelID(plan.BeforeSupplierModelID) || plan.Kind != "PRICE_CHANGE" || plan.CreatedBy != meta.ActorID || plan.AssigneeID != req.AssigneeID || plan.Status != "PENDING" || plan.Revision != 1 || plan.ExpectedResourceRevision != expected || plan.AfterCoefficient == "" || plan.BeforeCoefficient == "") {
		err = supply.ErrReadContract
	}
	h.result(c, plan, err)
}

func (h *supplyPricePlanWriteHandler) revise(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	if _, err := planPositiveID(c.Param("id")); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req revisePricePlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "supplier_model_id", "after_coefficient", "assignee_id", "due_at", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected == math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
		return
	}
	if strings.TrimSpace(req.SupplierModelID) == "" || len(req.SupplierModelID) > 128 || strings.ContainsRune(req.SupplierModelID, 0) {
		supplyHTTPError(c, supplyRequestError("supplier_model_id", "expected plan supplier model identifier"))
		return
	}
	if _, err := planPositiveID(req.AssigneeID); err != nil {
		supplyHTTPError(c, supplyRequestError("assignee_id", "expected active staff bigint string"))
		return
	}
	due, err := planDue(req.DueAt)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	plan, err := h.writer.RevisePriceChange(c.Request.Context(), meta, c.Param("id"), expected, req.SupplierModelID, req.AfterCoefficient, req.AssigneeID, due)
	if err == nil && (plan == nil || plan.ID != c.Param("id") || plan.SupplierModelID != req.SupplierModelID || plan.AfterSupplierModelID != req.SupplierModelID || !validPricePlanSupplierModelID(plan.BeforeSupplierModelID) || plan.Kind != "PRICE_CHANGE" || plan.CreatedBy != meta.ActorID || plan.AssigneeID != req.AssigneeID || plan.Status != "PENDING" || plan.Revision != expected+1) {
		err = supply.ErrReadContract
	}
	h.result(c, plan, err)
}

func (h *supplyPricePlanWriteHandler) result(c *gin.Context, plan *supply.ResourcePlan, err error) {
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if plan == nil || plan.Kind != "PRICE_CHANGE" || plan.ID == "" {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	if plan.ExecutedAt != nil && plan.ExecutedAt.Location() != time.UTC {
		plan = plan.Clone()
		at := plan.ExecutedAt.UTC()
		plan.ExecutedAt = &at
	}
	copy := plan.Clone()
	middleware.SetIdempotencyResult(c, copy)
	response.Success(c, copy)
}
