package api

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"model_bss/internal/domain/supply"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

var supplyReplacementFields = []string{"company", "name", "official_id", "model_type", "input_modalities", "output_modalities", "capacity", "abilities", "price_items", "api_access", "lifecycle", "sources", "checked_at"}

func supplyRequestError(path, reason string) error {
	return &supply.FieldError{Path: path, Reason: reason}
}

// JSON decoders normally silently accept duplicate keys. Check the whole token tree first,
// including nested structs/maps, and bound depth before decoding a typed request.
func supplyJSONValue(dec *json.Decoder, token json.Token, depth int) error {
	if depth > 64 {
		return supplyRequestError("body", "JSON nesting is too deep")
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] || key != strings.ToLower(key) {
				return supplyRequestError("body", "duplicate or noncanonical JSON field")
			}
			seen[key] = true
			value, err := dec.Token()
			if err != nil {
				return err
			}
			if err = supplyJSONValue(dec, value, depth+1); err != nil {
				return err
			}
		}
		closing, err := dec.Token()
		if err != nil || closing != json.Delim('}') {
			return supplyRequestError("body", "invalid JSON object")
		}
	case '[':
		for dec.More() {
			value, err := dec.Token()
			if err != nil {
				return err
			}
			if err = supplyJSONValue(dec, value, depth+1); err != nil {
				return err
			}
		}
		closing, err := dec.Token()
		if err != nil || closing != json.Delim(']') {
			return supplyRequestError("body", "invalid JSON array")
		}
	default:
		return supplyRequestError("body", "unexpected JSON delimiter")
	}
	return nil
}
func decodeSupplyRequest(c *gin.Context, target any, required []string) error {
	return decodeSupplyRequestLimit(c, target, required, 1<<20)
}

func decodeSupplyRequestLimit(c *gin.Context, target any, required []string, maxBytes int64) error {
	if maxBytes < 1 {
		return supplyRequestError("body", "invalid body size limit")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes))
	if err != nil || !utf8.Valid(raw) {
		return supplyRequestError("body", "JSON body is invalid or exceeds the request size limit")
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	first, err := tokens.Token()
	if err != nil || first != json.Delim('{') {
		return supplyRequestError("body", "expected a JSON object")
	}
	if err = supplyJSONValue(tokens, first, 0); err != nil {
		return supplyRequestError("body", "JSON has invalid, duplicate or noncanonical fields")
	}
	if _, err = tokens.Token(); err != io.EOF {
		return supplyRequestError("body", "only one JSON object is allowed")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(target); err != nil {
		return supplyRequestError("body", "JSON field or value type does not match the contract")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return supplyRequestError("body", "invalid JSON object")
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return supplyRequestError(key, "required for full replacement")
		}
	}
	return nil
}
func supplyQuery(c *gin.Context, allowed ...string) (url.Values, error) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, supplyRequestError("query", "invalid query encoding")
	}
	keys := map[string]bool{}
	for _, key := range allowed {
		keys[key] = true
	}
	for key, v := range values {
		if !keys[key] || len(v) != 1 {
			return nil, supplyRequestError("query", "unknown or repeated query field")
		}
	}
	return values, nil
}
func supplyPageParams(values url.Values) (int, int, error) {
	var page, size int
	for _, item := range []struct {
		key    string
		target *int
	}{{"page", &page}, {"size", &size}} {
		if raw, ok := values[item.key]; ok {
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 {
				return 0, 0, supplyRequestError(item.key, "expected a positive integer")
			}
			*item.target = n
		}
	}
	return page, size, nil
}
