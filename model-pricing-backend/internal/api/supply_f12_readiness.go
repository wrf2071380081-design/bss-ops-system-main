package api

import (
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"

	"github.com/gin-gonic/gin"
)

type supplyF12ReadinessHandler struct {
	reader SupplyF12ReadinessReader
}

func (h *supplyF12ReadinessHandler) get(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, err := supplyQuery(c, "subject_type", "subject_id", "flow_id", "module_key")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if err := validateF12Subject(values); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if _, err := planPositiveID(values.Get("flow_id")); err != nil {
		supplyHTTPError(c, supplyRequestError("flow_id", "expected a positive bigint string"))
		return
	}
	moduleKey := values.Get("module_key")
	if moduleKey == "" || len(moduleKey) > 64 {
		supplyHTTPError(c, supplyRequestError("module_key", "expected a module key of at most 64 characters"))
		return
	}
	result, err := h.reader.Get(c.Request.Context(), supply.F12ReadinessRequest{
		SubjectType: values.Get("subject_type"), SubjectID: values.Get("subject_id"),
		FlowID: values.Get("flow_id"), ModuleKey: moduleKey,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if result == nil || result.SubjectType != values.Get("subject_type") || result.SubjectID != values.Get("subject_id") || result.FlowID != values.Get("flow_id") || result.ModuleKey != moduleKey || result.FlowRevision == 0 {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	response.Success(c, result)
}
