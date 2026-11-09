package api

import (
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

// 原型「操作记录」的三个筛选：操作人、操作类型、时间区间（左闭右开）。
func sourceOperationRange(values url.Values) (*time.Time, *time.Time, error) {
	from, err := supplyOptionalTime(values, "from")
	if err != nil {
		return nil, nil, err
	}
	to, err := supplyOptionalTime(values, "to")
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, supplyRequestError("from", "time range start must be earlier than end")
	}
	return from, to, nil
}

func supplyOptionalTime(values url.Values, key string) (*time.Time, error) {
	raw := values.Get(key)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, supplyRequestError(key, "invalid time, expected RFC3339")
	}
	return &parsed, nil
}

func (h *supplyModelHandler) sourceOperations(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size", "resource_id", "operator", "action", "from", "to")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	from, to, err := sourceOperationRange(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.operations.List(c.Request.Context(), supply.SourceOperationQuery{
		ModelID:    c.Param("id"),
		ResourceID: values.Get("resource_id"),
		Operator:   values.Get("operator"),
		Action:     values.Get("action"),
		From:       from,
		To:         to,
		Page:       page,
		Size:       size,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

// sourceOperationOptions 提供操作人 / 操作类型下拉的候选值。
func (h *supplyModelHandler) sourceOperationOptions(c *gin.Context) {
	values, err := supplyQuery(c, "resource_id")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	options, err := h.operations.Options(c.Request.Context(), supply.SourceOperationQuery{
		ModelID:    c.Param("id"),
		ResourceID: values.Get("resource_id"),
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, options)
}

func (h *supplyModelHandler) supplierOperations(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size", "entity_type", "operator", "action", "from", "to")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	from, to, err := sourceOperationRange(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.operations.List(c.Request.Context(), supply.SourceOperationQuery{
		SupplierID: c.Param("id"),
		EntityType: values.Get("entity_type"),
		Operator:   values.Get("operator"),
		Action:     values.Get("action"),
		From:       from,
		To:         to,
		Page:       page,
		Size:       size,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
