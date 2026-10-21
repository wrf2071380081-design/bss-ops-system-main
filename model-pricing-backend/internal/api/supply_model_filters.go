package api

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"model_bss/pkg/response"
	"net/url"
	"strings"
	"unicode/utf8"
)

func supplyModelArray(values url.Values, key string) ([]string, error) {
	raw, ok := values[key]
	if !ok {
		return nil, nil
	}
	if len(raw) != 1 || len(raw[0]) > 32768 || !utf8.ValidString(raw[0]) || !strings.HasPrefix(strings.TrimSpace(raw[0]), "[") {
		return nil, supplyRequestError(key, "expected JSON string array")
	}
	var result []string
	if err := json.Unmarshal([]byte(raw[0]), &result); err != nil || result == nil || len(result) > 50 {
		return nil, supplyRequestError(key, "expected at most 50 strings")
	}
	return result, nil
}
func (h *supplyModelHandler) options(c *gin.Context) {
	values, err := supplyQuery(c, "keyword", "regions")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	regions, err := supplyModelArray(values, "regions")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.Options(c.Request.Context(), regions, values.Get("keyword"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, result)
}
