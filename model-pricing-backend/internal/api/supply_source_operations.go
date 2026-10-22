package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

func (h *supplyModelHandler) sourceOperations(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size", "resource_id")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.operations.List(c.Request.Context(), supply.SourceOperationQuery{ModelID: c.Param("id"), ResourceID: values.Get("resource_id"), Page: page, Size: size})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
