package api

import (
	"math"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

type supplyPlanExecutionHandler struct{ executor SupplyPlanExecutor }

type executePlanRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	Confirmed        bool   `json:"confirmed"`
	CompletionNote   string `json:"completion_note"`
	Reason           string `json:"reason"`
}

// execute records external completion for the original plan initiator. The domain
// transaction stages BLOCKED as a successful persistence outcome; the handler marks
// that exact 409 response as a replayable terminal result, which the idempotency
// middleware commits together with the transaction and replays for the same request.
// @Summary 登记资源上下架计划实际完成
// @Tags supply-resource-plans
// @Security BearerAuth
// @Param id path string true "计划ID"
// @Param body body executePlanRequest true "实际操作确认、依据和预期版本"
// @Success 200 {object} response.Body{data=supply.ResourcePlan}
// @Failure 400,401,403,404,409,503 {object} response.Body
// @Router /api/internal/plans/{id}/execute [post]
func (h *supplyPlanExecutionHandler) execute(c *gin.Context) {
	if h == nil || h.executor == nil {
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
	var req executePlanRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "confirmed", "completion_note", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected == math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid revision"))
		return
	}
	if !req.Confirmed || strings.TrimSpace(req.CompletionNote) == "" {
		supplyHTTPError(c, supplyRequestError("completion", "actual operation confirmation and evidence are required"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.executor.Execute(c.Request.Context(), meta, c.Param("id"), expected, req.Confirmed, req.CompletionNote)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	executor := ""
	if result != nil && result.Plan != nil {
		executor = result.Plan.CreatedBy
		if result.Plan.Kind == "PRICE_CHANGE" {
			executor = result.Plan.AssigneeID
		}
	}
	if result == nil || result.Plan == nil || result.Plan.ID != c.Param("id") || executor != meta.ActorID || result.Plan.Revision != expected+1 {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	plan := result.Plan.Clone()
	middleware.SetIdempotencyResult(c, plan)
	if result.Conflict {
		if plan.Status != "BLOCKED" || plan.ExecutedAt != nil {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		conflict := apperr.New(apperr.ErrIdempotency.Code, "资源状态或接入条件已变化，实际完成登记已阻断；请查看计划详情后重新处理", 409)
		middleware.SetIdempotencyTerminalResponse(c, conflict.Status, conflict.Code, conflict.Message, plan)
		response.ErrorData(c, conflict, plan)
		return
	}
	if plan.Status != "EXECUTED" || plan.ExecutedAt == nil || plan.ExecutedAt.IsZero() {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	response.Success(c, plan)
}
