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

type supplyResourceStateHandler struct{ writer SupplyResourceStateWriter }
type resourceStateRequest struct {
	ExpectedRevision  string `json:"expected_revision"`
	Action            string `json:"action"`
	ExternalConfirmed bool   `json:"external_confirmed"`
	CompletionNote    string `json:"completion_note"`
	Reason            string `json:"reason"`
}
type resourceStateReceipt struct {
	ID           string    `json:"id"`
	State        string    `json:"state"`
	Revision     uint64    `json:"revision,string"`
	EverAdmitted bool      `json:"ever_admitted"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// @Summary 登记资源直接上下架
// @Tags supply-resources
// @Security BearerAuth
// @Param id path string true "资源bigint字符串ID"
// @Param Idempotency-Key header string true "幂等键"
// @Param body body resourceStateRequest true "已完成实际操作的登记依据"
// @Success 200 {object} response.Body{data=resourceStateReceipt}
// @Failure 400,401,403,409,503 {object} response.Body
// @Router /api/internal/resources/{id}/state [post]
func (h *supplyResourceStateHandler) change(c *gin.Context) {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var req resourceStateRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "action", "external_confirmed", "completion_note", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	revision, err := strconv.ParseUint(req.ExpectedRevision, 10, 64)
	if err != nil || revision == 0 || revision > math.MaxInt64 || strconv.FormatUint(revision, 10) != req.ExpectedRevision || (req.Action != "OFFLINE" && req.Action != "ONLINE") || !req.ExternalConfirmed || strings.TrimSpace(req.CompletionNote) == "" {
		supplyHTTPError(c, supplyRequestError("command", "invalid state registration"))
		return
	}
	id := c.Param("id")
	resourceID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || resourceID == 0 || resourceID > math.MaxInt64 || strconv.FormatUint(resourceID, 10) != id {
		supplyHTTPError(c, supplyRequestError("id", "invalid resource ID"))
		return
	}
	meta, err := supplyWriteMeta(c, req.Reason)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.writer.Change(c.Request.Context(), meta, supply.ResourceStateCommand{ResourceID: id, ExpectedRevision: revision, Action: req.Action, ExternalConfirmed: req.ExternalConfirmed, CompletionNote: strings.TrimSpace(req.CompletionNote)})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	expectedState := "OFFLINE"
	if req.Action == "ONLINE" {
		expectedState = "ADMITTED"
	}
	if result == nil || result.ID != id || result.State != expectedState || result.Revision != revision+1 || !result.EverAdmitted {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	receipt := resourceStateReceipt{ID: result.ID, State: result.State, Revision: result.Revision, EverAdmitted: result.EverAdmitted, UpdatedAt: result.UpdatedAt}
	middleware.SetIdempotencyResult(c, receipt)
	response.Success(c, receipt)
}
