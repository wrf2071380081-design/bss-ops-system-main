package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

func (h *supplyModelHandler) modelOperations(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size", "sort_by", "order")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.ListOperations(c.Request.Context(), supply.ModelOperationQuery{ModelID: c.Param("id"), Page: page, Size: size, SortBy: values.Get("sort_by"), Order: values.Get("order")})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *supplyModelHandler) modelOperation(c *gin.Context) {
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.GetOperation(c.Request.Context(), c.Param("id"), c.Param("operationId"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
