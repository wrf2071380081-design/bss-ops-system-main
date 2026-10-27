package api

import (
	"model_bss/internal/domain/supply"
	"model_bss/pkg/response"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

type supplyF12ReadHandler struct{ reader SupplyF12Reader }

func f12ReadQuery(c *gin.Context, allowed ...string) (url.Values, int, int, error) {
	values, err := supplyQuery(c, allowed...)
	if err != nil {
		return nil, 0, 0, err
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		return nil, 0, 0, err
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	return values, page, size, nil
}

func validateF12Subject(values url.Values) error {
	if values.Get("subject_type") != "SUPPLIER" && values.Get("subject_type") != "CUSTOMER" {
		return supplyRequestError("subject_type", "expected SUPPLIER or CUSTOMER")
	}
	if _, err := planPositiveID(values.Get("subject_id")); err != nil {
		return supplyRequestError("subject_id", "expected a positive bigint string")
	}
	return nil
}

func (h *supplyF12ReadHandler) contracts(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, page, size, err := f12ReadQuery(c, "subject_type", "subject_id", "status", "contract_key", "latest_only", "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if err := validateF12Subject(values); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if status := values.Get("status"); status != "" && status != "PROCESSING" && status != "ARCHIVED" {
		supplyHTTPError(c, supplyRequestError("status", "expected PROCESSING or ARCHIVED"))
		return
	}
	if values.Has("contract_key") {
		if _, err := planPositiveID(values.Get("contract_key")); err != nil {
			supplyHTTPError(c, supplyRequestError("contract_key", "expected a positive bigint string"))
			return
		}
	}
	if values.Has("latest_only") && values.Get("latest_only") != "true" && values.Get("latest_only") != "false" {
		supplyHTTPError(c, supplyRequestError("latest_only", "expected true or false"))
		return
	}
	if values.Get("latest_only") == "true" && values.Get("contract_key") == "" {
		supplyHTTPError(c, supplyRequestError("contract_key", "latest lookup requires an archive key"))
		return
	}
	result, err := h.reader.ListContracts(c.Request.Context(), supply.F12ContractQuery{
		SubjectType: values.Get("subject_type"), SubjectID: values.Get("subject_id"), Status: values.Get("status"),
		ContractKey: values.Get("contract_key"), LatestOnly: values.Get("latest_only") == "true", Page: page, Size: size,
	})
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *supplyF12ReadHandler) credits(c *gin.Context) {
	if h == nil || h.reader == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	values, page, size, err := f12ReadQuery(c, "subject_type", "subject_id", "direction", "currency", "as_of", "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if err := validateF12Subject(values); err != nil {
		supplyHTTPError(c, err)
		return
	}
	expectedDirection := "SUPPLIER_TO_US"
	if values.Get("subject_type") == "CUSTOMER" {
		expectedDirection = "US_TO_CUSTOMER"
	}
	if direction := values.Get("direction"); direction != "" && direction != expectedDirection {
		supplyHTTPError(c, supplyRequestError("direction", "credit direction does not match subject type"))
		return
	}
	if currency := values.Get("currency"); currency != "" {
		valid := len(currency) == 3
		for _, ch := range currency {
			valid = valid && ch >= 'A' && ch <= 'Z'
		}
		if !valid {
			supplyHTTPError(c, supplyRequestError("currency", "expected a three-letter uppercase currency"))
			return
		}
	}
	query := supply.F12CreditQuery{
		SubjectType: values.Get("subject_type"), SubjectID: values.Get("subject_id"), Direction: values.Get("direction"), Currency: values.Get("currency"), Page: page, Size: size,
	}
	if raw, ok := values["as_of"]; ok {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw[0])
		if parseErr != nil {
			supplyHTTPError(c, supplyRequestError("as_of", "expected RFC3339 timestamp"))
			return
		}
		parsed = parsed.UTC()
		query.AsOf = &parsed
	}
	result, err := h.reader.ListCredits(c.Request.Context(), query)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
