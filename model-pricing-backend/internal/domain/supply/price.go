// Package supply implements the new supply business independently of legacy SKU tables.
package supply

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

// FieldError identifies invalid supplied data; missing draft information is reported separately.
type FieldError struct {
	Path   string
	Reason string
}

func (e *FieldError) Error() string     { return e.Path + ": " + e.Reason }
func invalid(path, reason string) error { return &FieldError{Path: path, Reason: reason} }

type PriceItem struct {
	ItemKey      string            `json:"item_key"`
	Label        string            `json:"label"`
	TaskType     string            `json:"task_type"`
	Amount       string            `json:"amount"`
	Currency     string            `json:"currency"`
	UnitCode     string            `json:"unit_code"`
	UnitQuantity string            `json:"unit_quantity"`
	Condition    string            `json:"condition"`
	Dimensions   map[string]string `json:"dimensions"`
	SourceURL    string            `json:"source_url"`
}

var unsignedDecimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
var stableKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// Plain decimal input is checked before parsing: no exponent, float conversion or rounding.
func monetaryDecimal(value, path string, positive bool) (decimal.Decimal, error) {
	if !unsignedDecimal.MatchString(value) {
		return decimal.Zero, invalid(path, "expected an unsigned plain decimal string")
	}
	parts := strings.SplitN(value, ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if len(integer) > 12 || (len(parts) == 2 && len(parts[1]) > 8) {
		return decimal.Zero, invalid(path, "exceeds numeric(20,8) precision")
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || (positive && !amount.IsPositive()) {
		return decimal.Zero, invalid(path, "must be a positive decimal")
	}
	return amount, nil
}

func publicURL(value string) bool {
	u, err := url.ParseRequestURI(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "key", "api_key", "apikey", "token", "access_token", "password", "secret", "authorization", "signature", "sig", "x-amz-signature", "credential":
			return false
		}
	}
	return true
}

func knownTask(task string) bool {
	switch task {
	case "TEXT_INPUT", "TEXT_OUTPUT", "CACHE_INPUT", "CACHE_STORAGE", "IMAGE_INPUT", "IMAGE_OUTPUT", "IMAGE_GENERATION", "VIDEO_INPUT", "VIDEO_OUTPUT", "VIDEO_GENERATION", "AUDIO_INPUT", "AUDIO_OUTPUT", "AUDIO_TRANSCRIPTION", "AUDIO_RECOGNITION":
		return true
	}
	return false
}
func knownUnit(unit string) bool {
	switch unit {
	case "TOKEN", "TOKEN_HOUR", "IMAGE", "SECOND", "MINUTE", "REQUEST", "CHARACTER":
		return true
	}
	return false
}
func knownDimension(key string) bool {
	switch key {
	case "context_tier", "resolution", "duration_seconds", "quality", "cache_type", "modality", "region", "deployment", "valid_from", "valid_to", "period", "account_tier":
		return true
	}
	return false
}

// ValidatePriceItems allows an empty draft, but never accepts an invalid partial price row.
// The vocabulary is explicit; additions require a contract and test change.
func ValidatePriceItems(items []PriceItem) error {
	seen := make(map[string]bool, len(items))
	scopes := map[string]string{}
	for i, p := range items {
		path := fmt.Sprintf("price_items[%d].", i)
		if !stableKey.MatchString(p.ItemKey) || seen[p.ItemKey] {
			return invalid(path+"item_key", "must be a unique stable key")
		}
		seen[p.ItemKey] = true
		if strings.TrimSpace(p.Label) == "" {
			return invalid(path+"label", "required")
		}
		if !knownTask(p.TaskType) {
			return invalid(path+"task_type", "unsupported task type")
		}
		if _, err := monetaryDecimal(p.Amount, path+"amount", false); err != nil {
			return err
		}
		if !currencyCode.MatchString(p.Currency) {
			return invalid(path+"currency", "expected three uppercase letters")
		}
		if !knownUnit(p.UnitCode) {
			return invalid(path+"unit_code", "unsupported billing unit")
		}
		if _, err := monetaryDecimal(p.UnitQuantity, path+"unit_quantity", true); err != nil {
			return err
		}
		if strings.TrimSpace(p.Condition) == "" {
			return invalid(path+"condition", "required")
		}
		for key, value := range p.Dimensions {
			if !knownDimension(key) || strings.TrimSpace(value) == "" {
				return invalid(path+"dimensions."+key, "unsupported dimension or empty value")
			}
		}
		if p.SourceURL != "" && !publicURL(p.SourceURL) {
			return invalid(path+"source_url", "expected a public HTTP(S) source without credentials")
		}
		dimensions := p.Dimensions
		if dimensions == nil {
			dimensions = map[string]string{}
		}
		// 同一 task_type 允许存在多档计费项：Claude 的缓存写入分「5 分钟」「1 小时」，缓存读取另计，
		// 三者的 task_type 都是 CACHE_INPUT，只有计费项名称（label）不同。所以 scope 必须带上 label，
		// 否则这些合法档位被当成「同一条件两个价」，写入被拒、已存的行连读都读不出来
		// （2026-11-25 标准模型库整页 500 的根因）。下游按 item_key 精确取价，不依赖 task_type 唯一。
		scope, _ := json.Marshal([]any{p.TaskType, strings.TrimSpace(p.Label), p.Currency, p.UnitCode, p.UnitQuantity, p.Condition, dimensions})
		amount, _ := decimal.NewFromString(p.Amount)
		if prior, ok := scopes[string(scope)]; ok && prior != amount.String() {
			return invalid(path+"amount", "conflicting amounts for the same scope; select one official price")
		}
		scopes[string(scope)] = amount.String()
	}
	return nil
}
