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
			return nil, dataIssue("RESOURCE_METRICS_INVALID", "测试指标格式不合法（应为非负数字）")
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
	Resource   *Resource
	Model      *StandardModel
	Supplier   ResourceSupplier
	Config     *FormalConfigView
	Report     *ResourceReportSummary
	AdmittedAt *time.Time
	// DisplayReport 是「最近一次实际测试」的展示用快照。
	//
	// Report 是**生效证据**（决定能不能准入/报价），会被审批与重开清空；
	// DisplayReport 只在 Report 为空时补位，让「测过但还没正式化」的资源
	// 也能在列表里显示指标。它**绝不参与** QuoteReadiness 判定。
	DisplayReport *ResourceReportSummary
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
	// Warnings 是"不阻断可报价、但要提示"的问题（2026-11-25：标准模型资料不全改为提示，
	// 让接入+商务办完的资源能立刻进入客户报价）。
	Warnings             []string `json:"warnings"`
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
	AdmittedAt           *time.Time             `json:"admitted_at"`
}

func resourceStateKnown(state string) bool {
	return state == "PENDING_TEST" || state == "PENDING_ACCESS" || state == "ADMITTED" || state == "OFFLINE"
}
// resourceCoefficient 校验并归一采购系数（0 < 系数 ≤ 1，最多四位小数）。
// 失败时返回带原因的 ResourceDataError，供模型广场按行展示「系数不合法」。
func resourceCoefficient(value string) (decimal.Decimal, error) {
	if !unsignedDecimal.MatchString(value) {
		return decimal.Zero, dataIssue("RESOURCE_COEFFICIENT_INVALID", "采购系数不合法（须为数字）")
	}
	parts := strings.SplitN(value, ".", 2)
	if len(parts) == 2 && len(parts[1]) > 4 {
		return decimal.Zero, dataIssue("RESOURCE_COEFFICIENT_INVALID", "采购系数最多四位小数")
	}
	coefficient, err := decimal.NewFromString(value)
	if err != nil || !coefficient.IsPositive() || coefficient.GreaterThan(decimal.NewFromInt(1)) {
		return decimal.Zero, dataIssue("RESOURCE_COEFFICIENT_INVALID", "采购系数须大于 0 且不超过 1")
	}
	return coefficient, nil
}
func BuildResourceSnapshot(record *ResourceRecord) (*ResourceSnapshot, error) {
	if record == nil || record.Resource == nil {
		return nil, dataIssue("RESOURCE_MISSING", "资源不存在或已被删除")
	}
	if record.Model == nil {
		return nil, dataIssue("MODEL_METADATA_MISSING", "标准模型资料读取不到，无法生成资源视图")
	}
	r, m, supplier := record.Resource, record.Model, record.Supplier
	if m.VersionNo == 0 || m.VersionNo > math.MaxInt64 || m.Revision == 0 || m.Revision > math.MaxInt64 {
		return nil, dataIssue("MODEL_VERSION_INVALID", "标准模型缺少有效版本，请到标准模型库核对")
	}
	if ValidatePriceItems(m.Data.PriceItems) != nil {
		return nil, dataIssue("MODEL_PRICE_INVALID", "标准模型价格项不合法，请到标准模型库修正")
	}
	if r.TargetVersion > math.MaxInt64 || r.OriginRowVersion > math.MaxInt64 || r.FormalConfigRevision > math.MaxInt64 || (r.FormalConfigID != "" && !validID(r.FormalConfigID)) {
		return nil, dataIssue("RESOURCE_VERSION_INVALID", "资源的版本号超出允许范围")
	}
	// 逐项拆开判，是为了让前端能显示「到底哪里不对」而不是一句笼统的"数据异常"。
	switch {
	case strings.TrimSpace(r.BuyerID) == "" || !validID(r.BuyerID):
		return nil, dataIssue("RESOURCE_BUYER_MISSING", "该资源尚未分配采购人")
	case !validID(r.ID) || !validID(r.SupplierID) || !validID(r.StandardModelID) || !validID(r.OriginRowID):
		return nil, dataIssue("RESOURCE_IDENTITY_INVALID", "资源标识或关联 ID 不合法")
	case r.Revision == 0 || r.Revision > math.MaxInt64:
		return nil, dataIssue("RESOURCE_REVISION_MISSING", "资源缺少修订版本，尚未完成接入登记")
	case r.TargetVersion == 0 || r.OriginRowVersion == 0:
		return nil, dataIssue("RESOURCE_VERSION_MISSING", "资源缺少目标版本，尚未完成接入登记")
	case m.ID != r.StandardModelID || supplier.ID != r.SupplierID:
		return nil, dataIssue("RESOURCE_RELATION_MISMATCH", "资源与标准模型或供应商的关联不一致")
	case !resourceStateKnown(r.State):
		return nil, dataIssue("RESOURCE_STATE_INVALID", "资源状态不在允许范围内")
	case r.Region != "CN" && r.Region != "OVERSEAS":
		return nil, dataIssue("RESOURCE_REGION_INVALID", "资源部署节点不合法")
	case strings.TrimSpace(r.SupplierModelID) == "":
		return nil, dataIssue("SUPPLIER_MODEL_NAME_MISSING", "供应商侧模型名称为空")
	}
	coefficient, err := resourceCoefficient(r.Coefficient)
	if err != nil {
		return nil, err
	}
	var admittedAt *time.Time
	if record.AdmittedAt != nil {
		if !r.EverAdmitted || record.AdmittedAt.IsZero() {
			return nil, dataIssue("RESOURCE_ADMITTED_AT_INVALID", "准入时间与准入状态矛盾")
		}
		value := record.AdmittedAt.UTC().Truncate(time.Microsecond)
		admittedAt = &value
	}
	view := &ResourceSnapshot{ID: r.ID, SupplierID: r.SupplierID, SupplierName: supplier.Name, StandardModelID: m.ID, ModelName: m.Data.Name, OfficialID: m.Data.OfficialID, Company: m.Data.Company, ModelType: m.Data.ModelType, Region: r.Region, SupplierModelID: r.SupplierModelID, Coefficient: coefficient.StringFixed(4), BuyerID: r.BuyerID, State: r.State, EverAdmitted: r.EverAdmitted, Revision: r.Revision, FormalConfigID: r.FormalConfigID, FormalConfigRevision: r.FormalConfigRevision, OfficialPriceItems: m.Clone().Data.PriceItems, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, AdmittedAt: admittedAt}
	if view.OfficialPriceItems == nil {
		view.OfficialPriceItems = []PriceItem{}
	}
	ready := ResourceQuoteReadiness{BlockedReasons: []string{}, Warnings: []string{}, ResourceRevision: r.Revision, ModelVersionNo: m.VersionNo, FormalConfigRevision: r.FormalConfigRevision}
	block := func(reason string) { ready.BlockedReasons = append(ready.BlockedReasons, reason) }
	// warn 只做提示，不参与 Eligible 判定。
	warn := func(reason string) { ready.Warnings = append(ready.Warnings, reason) }
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
		// 业务裁决（2026-11-25）：资料不全/未核验不再阻断可报价，改为提示。
		warn("MODEL_NOT_READY")
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
			return nil, dataIssue("RESOURCE_EVIDENCE_POINTER_MISSING", "资源标记了生效测试报告，但读不到该报告")
		}
		block("TESTS_REQUIRED")
		// 没有生效证据（审批未正式化 / 办理重开）时，用最近一次实际测试的数据填充展示位。
		// 只填 view.Test 供页面显示指标；ready 仍保留 TESTS_REQUIRED，绝不因此放行准入。
		if display := record.DisplayReport; display != nil {
			metrics, err := copyResourceMetrics(display.Metrics)
			if err != nil {
				return nil, err
			}
			cacheDisplay, err := resourceCacheStats(display.ID, display.Cache)
			if err != nil {
				return nil, err
			}
			view.Test = ResourceTestView{ReportID: display.ID, Status: "COMPLETED", ConnectivityPassed: display.ConnectivityPassed, PressurePassed: display.PressurePassed, Valid: display.Valid, CacheStats: cacheDisplay, Metrics: metrics}
			// 前端契约要求 test.report_id / cache_stats.report_id / quote_readiness.test_evidence_ref
			// 三者指向同一份报告（resources.contract.ts 的 validateResourceRow），
			// 少填一个就会整行判成「供应资源响应不符合约定」。这里补上第三个。
			ready.TestEvidenceRef = display.ID
		}
	} else {
		if report.ID != r.LatestReportID || !validID(report.ID) || (report.ResourceID != "" && report.ResourceID != r.ID) || report.SupplierID != r.SupplierID || report.StandardModelID != m.ID || report.Region != r.Region || report.OriginRowID != r.OriginRowID {
			return nil, dataIssue("RESOURCE_REPORT_MISMATCH", "测试报告与资源身份不一致，请重新选取测试证据")
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
			// 压力测试只留指标、不参与"能不能报价/能不能接入"的判定：
			// 压测平台对 stress 不返回 verdict，pressure_passed 恒为 false，
			// 拿它当门槛等于把所有资源都判成不合格。
			if !report.ConnectivityPassed {
				block("TESTS_NOT_PASSED")
			}
		}
	}
	ready.Eligible = len(ready.BlockedReasons) == 0
	view.QuoteReadiness = ready
	return view, nil
}
