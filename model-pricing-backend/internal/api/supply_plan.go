package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplyPlanHandler struct{ reader *supply.PlanReader }

// @Summary 工作台计划只读摘要
// @Tags supply-plans
// @Security BearerAuth
// @Param id path string true "计划ID"
// @Success 200 {object} response.Body{data=supply.PlanSummary}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/plans/{id} [get]
func (h *supplyPlanHandler) get(c *gin.Context) {
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.Get(c.Request.Context(), supplyActor(c), c.Param("id"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
