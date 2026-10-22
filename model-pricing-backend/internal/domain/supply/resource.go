package supply

import (
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Resource struct {
	ID, SupplierID, StandardModelID, Region, SupplierModelID, Coefficient, BuyerID, State string
	EverAdmitted                                                                          bool
	Revision                                                                              uint64
	FormalConfigID                                                                        string
	FormalConfigRevision                                                                  uint64
	OriginRowID                                                                           string
	OriginRowVersion, TargetVersion                                                       uint64
	LatestReportID                                                                        string
	LatestReportSequence                                                                  uint64
	CreatedAt, UpdatedAt                                                                  time.Time
}
type ResourceSupplier struct{ ID, Name, Region, Status string }

// Nullable decimal strings distinguish an actual zero from an absent metric.
type ResourceReportMetrics struct {
	ConnectionMS *string `json:"connection_ms"`
	RPS          *string `json:"rps"`
	TPS          *string `json:"tps"`
}

func copyResourceMetrics(metrics ResourceReportMetrics) (ResourceReportMetrics, error) {
	copyMetric := func(value *string) (*string, error) {
		if value == nil {
			return nil, nil
		}
		if !unsignedDecimal.MatchString(*value) {
			return nil, ErrReadContract
		}
		copy := *value
		return &copy, nil
	}
	var result ResourceReportMetrics
	var err error
	result.ConnectionMS, err = copyMetric(metrics.ConnectionMS)
	if err != nil {
		return result, err
	}
	result.RPS, err = copyMetric(metrics.RPS)
	if err != nil {
		return result, err
	}
	result.TPS, err = copyMetric(metrics.TPS)
	return result, err
}

// B supplies facts for the selected immutable report. ResourceID may be empty
// for draft tests, but origin row/version and all business identity must match.
type ResourceReportSummary struct {
	Sequence                                                         uint64
	ID, ResourceID, SupplierID, StandardModelID, Region, OriginRowID string
	OriginRowVersion, TargetVersion                                  uint64
	ConnectivityPassed, PressurePassed, Valid                        bool
	Cache                                                            *ReportCacheCounters
	Metrics                                                          ResourceReportMetrics
}
type ResourceRecord struct {
	Resource *Resource
	Model    *StandardModel
	Supplier ResourceSupplier
	Config   *FormalConfigView
	Report   *ResourceReportSummary
}
type ResourceTestView struct {
	ReportID           string                `json:"report_id,omitempty"`
	Status             string                `json:"status"`
	ConnectivityPassed bool                  `json:"connectivity_passed"`
	PressurePassed     bool                  `json:"pressure_passed"`
	Valid              bool                  `json:"valid"`
	CacheStats         ResourceCacheStats    `json:"cache_stats"`
	Metrics            ResourceReportMetrics `json:"metrics"`
}
type ResourceQuoteReadiness struct {
	Eligible             bool     `json:"eligible"`
	BlockedReasons       []string `json:"blocked_reasons"`
	ResourceRevision     uint64   `json:"resource_revision,string"`
	ModelVersionNo       uint64   `json:"model_version_no,string"`
	FormalConfigRevision uint64   `json:"formal_config_revision,string"`
	TestEvidenceRef      string   `json:"test_evidence_ref,omitempty"`
}
type ResourceSnapshot struct {
	ID                   string                 `json:"id"`
	SupplierID           string                 `json:"supplier_id"`
	SupplierName         string                 `json:"supplier_name"`
	StandardModelID      string                 `json:"standard_model_id"`
	ModelName            string                 `json:"model_name"`
	OfficialID           string                 `json:"official_id"`
	Company              string                 `json:"company"`
	ModelType            string                 `json:"model_type"`
	Region               string                 `json:"region"`
	SupplierModelID      string                 `json:"supplier_model_id"`
	Coefficient          string                 `json:"coefficient"`
	BuyerID              string                 `json:"buyer_id"`
	State                string                 `json:"state"`
	EverAdmitted         bool                   `json:"ever_admitted"`
	Revision             uint64                 `json:"revision,string"`
	FormalConfigID       string                 `json:"formal_config_id,omitempty"`
	FormalConfigRevision uint64                 `json:"formal_config_revision,string"`
	Test                 ResourceTestView       `json:"test"`
	OfficialPriceItems   []PriceItem            `json:"official_price_items"`
	QuoteReadiness       ResourceQuoteReadiness `json:"quote_readiness"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

func resourceStateKnown(state string) bool {
	return state == "PENDING_TEST" || state == "PENDING_ACCESS" || state == "ADMITTED" || state == "OFFLINE"
}
func resourceCoefficient(value string) (decimal.Decimal, error) {
	if !unsignedDecimal.MatchString(value) {
		return decimal.Zero, ErrReadContract
	}
	parts := strings.SplitN(value, ".", 2)
	if len(parts) == 2 && len(parts[1]) > 4 {
		return decimal.Zero, ErrReadContract
	}
	coefficient, err := decimal.NewFromString(value)
	if err != nil || !coefficient.IsPositive() || coefficient.GreaterThan(decimal.NewFromInt(1)) {
		return decimal.Zero, ErrReadContract
	}
	return coefficient, nil
}
func BuildResourceSnapshot(record *ResourceRecord) (*ResourceSnapshot, error) {
	if record == nil || record.Resource == nil || record.Model == nil {
		return nil, ErrReadContract
	}
	r, m, supplier := record.Resource, record.Model, record.Supplier
	if m.VersionNo == 0 || m.VersionNo > math.MaxInt64 || m.Revision == 0 || m.Revision > math.MaxInt64 || ValidatePriceItems(m.Data.PriceItems) != nil {
		return nil, ErrReadContract
	}
	if r.TargetVersion > math.MaxInt64 || r.OriginRowVersion > math.MaxInt64 || r.FormalConfigRevision > math.MaxInt64 || (r.FormalConfigID != "" && !validID(r.FormalConfigID)) {
		return nil, ErrReadContract
	}
	if !validID(r.ID) || !validID(r.SupplierID) || !validID(r.StandardModelID) || !validID(r.BuyerID) || !validID(r.OriginRowID) || r.Revision == 0 || r.Revision > math.MaxInt64 || r.TargetVersion == 0 || r.OriginRowVersion == 0 || m.ID != r.StandardModelID || supplier.ID != r.SupplierID || !resourceStateKnown(r.State) || (r.Region != "CN" && r.Region != "OVERSEAS") || strings.TrimSpace(r.SupplierModelID) == "" {
		return nil, ErrReadContract
	}
	coefficient, err := resourceCoefficient(r.Coefficient)
	if err != nil {
		return nil, err
	}
	view := &ResourceSnapshot{ID: r.ID, SupplierID: r.SupplierID, SupplierName: supplier.Name, StandardModelID: m.ID, ModelName: m.Data.Name, OfficialID: m.Data.OfficialID, Company: m.Data.Company, ModelType: m.Data.ModelType, Region: r.Region, SupplierModelID: r.SupplierModelID, Coefficient: coefficient.StringFixed(4), BuyerID: r.BuyerID, State: r.State, EverAdmitted: r.EverAdmitted, Revision: r.Revision, FormalConfigID: r.FormalConfigID, FormalConfigRevision: r.FormalConfigRevision, OfficialPriceItems: m.Clone().Data.PriceItems, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if view.OfficialPriceItems == nil {
		view.OfficialPriceItems = []PriceItem{}
	}
	ready := ResourceQuoteReadiness{BlockedReasons: []string{}, ResourceRevision: r.Revision, ModelVersionNo: m.VersionNo, FormalConfigRevision: r.FormalConfigRevision}
	block := func(reason string) { ready.BlockedReasons = append(ready.BlockedReasons, reason) }
	if r.State != "ADMITTED" || !r.EverAdmitted {
		block("RESOURCE_NOT_ADMITTED")
	}
	if supplier.Status != "ACTIVE" {
		block("SUPPLIER_INACTIVE")
	}
	if supplier.Region != r.Region {
		block("SUPPLIER_REGION_MISMATCH")
	}
	if !m.Readiness().Ready {
		block("MODEL_NOT_READY")
	}
	cfg := record.Config
	if cfg == nil || r.FormalConfigID == "" || cfg.ID != r.FormalConfigID || cfg.SupplierID != r.SupplierID || cfg.Region != r.Region || cfg.Status != "ACTIVE" || !cfg.HasAPIKey || cfg.Revision == 0 || cfg.ConfirmedRevision != cfg.Revision {
		block("FORMAL_CONFIG_UNAVAILABLE")
	} else if r.FormalConfigRevision != cfg.Revision {
		block("FORMAL_CONFIG_CHANGED")
	}
	cache, _ := resourceCacheStats("", nil)
	view.Test = ResourceTestView{Status: "NOT_TESTED", CacheStats: cache}
	report := record.Report
	if report == nil {
		if r.LatestReportID != "" {
			return nil, ErrReadContract
		}
		block("TESTS_REQUIRED")
	} else {
		if report.ID != r.LatestReportID || !validID(report.ID) || (report.ResourceID != "" && report.ResourceID != r.ID) || report.SupplierID != r.SupplierID || report.StandardModelID != m.ID || report.Region != r.Region || report.OriginRowID != r.OriginRowID {
			return nil, ErrReadContract
		}
		view.Test.ReportID = report.ID
		ready.TestEvidenceRef = report.ID
		if report.TargetVersion != r.TargetVersion || report.OriginRowVersion != r.OriginRowVersion {
			view.Test.Status = "STALE"
			view.Test.CacheStats = ResourceCacheStats{ReportID: report.ID, Status: "NOT_PROVIDED"}
			block("TEST_TARGET_CHANGED")
		} else {
			cache, err := resourceCacheStats(report.ID, report.Cache)
			if err != nil {
				return nil, err
			}
			metrics, err := copyResourceMetrics(report.Metrics)
			if err != nil {
				return nil, err
			}
			view.Test = ResourceTestView{ReportID: report.ID, Status: "COMPLETED", ConnectivityPassed: report.ConnectivityPassed, PressurePassed: report.PressurePassed, Valid: report.Valid, CacheStats: cache, Metrics: metrics}
			if !report.Valid {
				block("TEST_EVIDENCE_INVALID")
			}
			if !report.ConnectivityPassed || !report.PressurePassed {
				block("TESTS_NOT_PASSED")
			}
		}
	}
	ready.Eligible = len(ready.BlockedReasons) == 0
	view.QuoteReadiness = ready
	return view, nil
}
