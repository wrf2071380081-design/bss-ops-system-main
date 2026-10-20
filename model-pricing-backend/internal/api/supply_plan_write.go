package api

import (
	"github.com/gin-gonic/gin"
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
	"strconv"
	"strings"
	"time"
)

type supplyPlanWriteHandler struct{ writer SupplyPlanWriter }
type createPlanRequest struct {
	ResourceID               string `json:"resource_id"`
	ExpectedResourceRevision string `json:"expected_resource_revision"`
	Kind                     string `json:"kind"`
	DueAt                    string `json:"due_at"`
	Reason                   string `json:"reason"`
}
type revisePlanRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	DueAt            string `json:"due_at"`
	Reason           string `json:"reason"`
}
type cancelPlanRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	Reason           string `json:"reason"`
}

func planPositiveID(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || n > math.MaxInt64 || strconv.FormatUint(n, 10) != value {
		return 0, supplyRequestError("id_or_revision", "expected canonical positive bigint string")
	}
	return n, nil
}
func (h *supplyPlanWriteHandler) ready(c *gin.Context) bool {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return false
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return false
	}
	return true
}
func planDue(value string) (time.Time, error) {
	due, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || due.IsZero() {
		return time.Time{}, supplyRequestError("due_at", "expected RFC3339 timestamp")
	}
	return due.UTC(), nil
}
func (h *supplyPlanWriteHandler) create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req createPlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"resource_id", "expected_resource_revision", "kind", "due_at", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(req.ResourceID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedResourceRevision)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if req.Kind != "ONLINE" && req.Kind != "OFFLINE" {
		supplyHTTPError(c, supplyRequestError("kind", "expected state plan kind"))
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
	result, err := h.writer.Create(c.Request.Context(), meta, req.ResourceID, expected, req.Kind, due)
	if err == nil && (result == nil || result.ResourceID != req.ResourceID || result.Kind != req.Kind || result.CreatedBy != meta.ActorID || result.Status != "PENDING" || result.Revision != 1 || result.ExpectedResourceRevision != expected) {
		err = supply.ErrReadContract
	}
	h.result(c, result, err)
}
func (h *supplyPlanWriteHandler) revise(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req revisePlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "due_at", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(c.Param("id")); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected == math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
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
	result, err := h.writer.Revise(c.Request.Context(), meta, c.Param("id"), expected, due)
	if err == nil && (result == nil || result.ID != c.Param("id") || result.Revision != expected+1 || result.Status != "PENDING" || result.CreatedBy != meta.ActorID) {
		err = supply.ErrReadContract
	}
	h.result(c, result, err)
}
func (h *supplyPlanWriteHandler) cancel(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	var req cancelPlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(c.Param("id")); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected == math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.writer.Cancel(c.Request.Context(), meta, c.Param("id"), expected)
	if err == nil && (result == nil || result.ID != c.Param("id") || result.Revision != expected+1 || result.Status != "CANCELED" || result.CreatedBy != meta.ActorID) {
		err = supply.ErrReadContract
	}
	h.result(c, result, err)
}
func (h *supplyPlanWriteHandler) result(c *gin.Context, p *supply.ResourcePlan, err error) {
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if p == nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	if _, err := planPositiveID(p.ID); err != nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	copy := p.Clone()
	middleware.SetIdempotencyResult(c, copy)
	response.Success(c, copy)
}
