package api

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
)

type supplyResourceOperationHandler struct {
	admitter SupplyResourceAdmitter
	binder   SupplyResourceConfigBinder
}

type admitResourceRequest struct {
	ExpectedRevision  string `json:"expected_revision"`
	ExternalConfirmed bool   `json:"external_confirmed"`
	CompletionNote    string `json:"completion_note"`
	Reason            string `json:"reason"`
}

type bindFormalConfigRequest struct {
	ExpectedRevision       string `json:"expected_revision"`
	FormalConfigID         string `json:"formal_config_id"`
	ExpectedConfigRevision string `json:"expected_config_revision"`
	Reason                 string `json:"reason"`
}

type resourceAdmissionReceipt struct {
	ResourceID   string    `json:"resource_id"`
	State        string    `json:"state"`
	EverAdmitted bool      `json:"ever_admitted"`
	Revision     string    `json:"revision"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type resourceConfigBindingReceipt struct {
	ResourceID           string    `json:"resource_id"`
	FormalConfigID       string    `json:"formal_config_id"`
	FormalConfigRevision string    `json:"formal_config_revision"`
	ResourceRevision     string    `json:"resource_revision"`
	Changed              bool      `json:"changed"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (h *supplyResourceOperationHandler) admit(c *gin.Context) {
	if h == nil || h.admitter == nil {
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
	var req admitResourceRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "external_confirmed", "completion_note", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected >= math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid incrementable revision"))
		return
	}
	if !req.ExternalConfirmed || strings.TrimSpace(req.CompletionNote) == "" {
		supplyHTTPError(c, supplyRequestError("completion", "external completion confirmation and evidence are required"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	resourceID := c.Param("id")
	resource, err := h.admitter.Admit(c.Request.Context(), meta, supply.ResourceAdmissionCommand{
		ResourceID: resourceID, ExpectedRevision: expected, ExternalConfirmed: req.ExternalConfirmed, CompletionNote: req.CompletionNote,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if resource == nil || resource.ID != resourceID || resource.State != "ADMITTED" || !resource.EverAdmitted || resource.Revision != expected+1 || resource.UpdatedAt.IsZero() {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	receipt := resourceAdmissionReceipt{ResourceID: resource.ID, State: resource.State, EverAdmitted: resource.EverAdmitted, Revision: uintString(resource.Revision), UpdatedAt: resource.UpdatedAt.UTC()}
	middleware.SetIdempotencyResult(c, receipt)
	response.Success(c, receipt)
}

func (h *supplyResourceOperationHandler) bindFormalConfig(c *gin.Context) {
	if h == nil || h.binder == nil {
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
	var req bindFormalConfigRequest
	if err := decodeSupplyRequest(c, &req, []string{"expected_revision", "formal_config_id", "expected_config_revision", "reason"}); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expected, err := planPositiveID(req.ExpectedRevision)
	if err != nil || expected >= math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "invalid incrementable revision"))
		return
	}
	if _, err := planPositiveID(req.FormalConfigID); err != nil {
		supplyHTTPError(c, supplyRequestError("formal_config_id", "invalid formal configuration ID"))
		return
	}
	expectedConfigRevision, err := planPositiveID(req.ExpectedConfigRevision)
	if err != nil {
		supplyHTTPError(c, supplyRequestError("expected_config_revision", "invalid configuration revision"))
		return
	}
	meta, err := supplyWriteMeta(c, strings.TrimSpace(req.Reason))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	resourceID := c.Param("id")
	change, err := h.binder.BindFormalConfig(c.Request.Context(), meta, supply.ResourceConfigBindingCommand{
		ResourceID: resourceID, ExpectedRevision: expected, ConfigID: req.FormalConfigID, ExpectedConfigRevision: expectedConfigRevision,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if change == nil || change.Resource == nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	resource := change.Resource
	expectedResourceRevision := expected
	if change.Changed {
		expectedResourceRevision++
	}
	if resource.ID != resourceID || resource.FormalConfigID != req.FormalConfigID || resource.FormalConfigRevision != expectedConfigRevision || resource.Revision != expectedResourceRevision || resource.EverAdmitted || resource.UpdatedAt.IsZero() {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	receipt := resourceConfigBindingReceipt{
		ResourceID: resource.ID, FormalConfigID: resource.FormalConfigID,
		FormalConfigRevision: uintString(resource.FormalConfigRevision), ResourceRevision: uintString(resource.Revision),
		Changed: change.Changed, UpdatedAt: resource.UpdatedAt.UTC(),
	}
	middleware.SetIdempotencyResult(c, receipt)
	response.Success(c, receipt)
}

func uintString(value uint64) string {
	return strconv.FormatUint(value, 10)
}
