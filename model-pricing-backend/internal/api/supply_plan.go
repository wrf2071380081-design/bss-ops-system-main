package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplyPlanHandler struct{ reader *supply.PlanReader }

// @Summary 资源计划列表
// @Tags supply-plans
// @Security BearerAuth
// @Param resource_id query string false "资源ID"
// @Param region query string false "CN/OVERSEAS"
// @Param kind query string false "ONLINE/OFFLINE/PRICE_CHANGE"
// @Param status query string false "PENDING/EXECUTING/EXECUTED/CANCELED/BLOCKED"
// @Param page query int false "页码"
// @Param size query int false "每页1至100"
// @Success 200 {object} response.Body{data=supply.PlanListPage}
// @Router /api/internal/plans [get]
func (h *supplyPlanHandler) list(c *gin.Context) {
	values, err := supplyQuery(c, "resource_id", "region", "kind", "status", "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.List(c.Request.Context(), supplyActor(c), supply.PlanListQuery{ResourceID: values.Get("resource_id"), Region: values.Get("region"), Kind: values.Get("kind"), Status: values.Get("status"), Page: page, Size: size})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

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
