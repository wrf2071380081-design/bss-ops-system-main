package supply

import (
	"math/big"
	"regexp"
)

// ReportCacheCounters is a fact on one immutable B report, never an editable
// resource property. The enclosing report supplies its identity.
type ReportCacheCounters struct {
	Status      string
	HitTokens   string
	InputTokens string
	Scene       string
}
type ResourceCacheStats struct {
	ReportID    string `json:"report_id,omitempty"`
	Status      string `json:"status"`
	HitTokens   string `json:"hit_tokens,omitempty"`
	InputTokens string `json:"input_tokens,omitempty"`
	HitRate     string `json:"hit_rate,omitempty"`
	HitPercent  string `json:"hit_percent,omitempty"`
	Scene       string `json:"scene,omitempty"`
}

var tokenCount = regexp.MustCompile(`^[0-9]+$`)

func resourceCacheStats(reportID string, raw *ReportCacheCounters) (ResourceCacheStats, error) {
	view := ResourceCacheStats{ReportID: reportID, Status: "NOT_PROVIDED"}
	if reportID == "" && raw == nil {
		view.Status = "NOT_TESTED"
		return view, nil
	}
	if !validID(reportID) {
		return view, ErrReadContract
	}
	if raw == nil {
		return view, nil
	}
	view.Status = raw.Status
	view.Scene = raw.Scene
	switch raw.Status {
	case "NOT_PROVIDED", "NOT_APPLICABLE":
		if raw.HitTokens != "" || raw.InputTokens != "" {
			return view, ErrReadContract
		}
		return view, nil
	case "PROVIDED":
	default:
		return view, ErrReadContract
	}
	if !tokenCount.MatchString(raw.HitTokens) || !tokenCount.MatchString(raw.InputTokens) {
		return view, ErrReadContract
	}
	hit, ok := new(big.Int).SetString(raw.HitTokens, 10)
	if !ok {
		return view, ErrReadContract
	}
	input, ok := new(big.Int).SetString(raw.InputTokens, 10)
	if !ok || input.Sign() <= 0 || hit.Cmp(input) > 0 {
		return view, ErrReadContract
	}
	ratio := new(big.Rat).SetFrac(hit, input)
	view.HitTokens = raw.HitTokens
	view.InputTokens = raw.InputTokens
	view.HitRate = ratio.FloatString(6)
	// Compute percentage independently of the rounded ratio summary.
	view.HitPercent = new(big.Rat).Mul(ratio, big.NewRat(100, 1)).FloatString(2)
	return view, nil
}

// ProjectResourceCacheStats applies the domain's nullable-counter policy to a
// single immutable report row. Infrastructure readers use this instead of
// reimplementing cache validation or treating missing counters as zero.
func ProjectResourceCacheStats(reportID string, raw *ReportCacheCounters) (ResourceCacheStats, error) {
	return resourceCacheStats(reportID, raw)
}
