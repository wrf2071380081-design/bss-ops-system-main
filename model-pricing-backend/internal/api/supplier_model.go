// Package api 的 supplier_model.go：供应商门户「模型管理」列表（2026-10-28）。
//
// 背景：`/supplier/models` 一直只在前端存在，后端从未注册过，
// 页面拿到 gin 的纯文本 404，被前端统一翻成「数据不存在或当前账号不可见」。
//
// 没有复用 A 线的 ResourceReadStore：那套读模型要求 `resource.buyer_id` 非空、
// 且要和 facts 逐字段对得上，采购人没分配就会整页 500；供应商侧只看
// 「我有哪些模型 / 什么状态 / 测没测过」，所以走 supplypg 的窄查询。
//
// 隔离纪律与申报/凭据一致：supplier_id 只从登录态取，绝不接受请求参数指定。
package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 供应商侧资源状态白名单（与 resources.contract.ts 的 resourceStateLabels 一致）。
var supplierModelStates = map[string]bool{
	"PENDING_TEST":   true,
	"PENDING_ACCESS": true,
	"ADMITTED":       true,
	"OFFLINE":        true,
}

const (
	supplierModelDefaultSize = 20
	supplierModelMaxSize     = 100
	// supplierModelRevision 是给前端做字段校验用的占位版本号。
	// 000033 这张 resource 表没有 revision 列（A 线的版本在 facts 里），
	// 供应商侧的价格变更已改为「新建 PRICE_CHANGE 申报」，不再用它做乐观锁。
	supplierModelRevision = "1"
)

// supplierModelTestDTO 对上前端 `test` 字段。
type supplierModelTestDTO struct {
	ReportID           string  `json:"report_id,omitempty"`
	Status             string  `json:"status"`
	ConnectivityTested bool    `json:"connectivity_tested"`
	ConnectivityPassed bool    `json:"connectivity_passed"`
	StressTested       bool    `json:"stress_tested"`
	StressPassed       bool    `json:"stress_passed"`
	Valid              bool    `json:"valid"`
	ConnectionMS       *string `json:"connection_ms,omitempty"`
	RPS                *string `json:"rps,omitempty"`
	TPS                *string `json:"tps,omitempty"`
}

type supplierModelQuoteReadinessDTO struct {
	Eligible       bool     `json:"eligible"`
	BlockedReasons []string `json:"blocked_reasons"`
}

// supplierModelDTO 的字段名与 `supplierModels.types.ts` 严格一致。
type supplierModelDTO struct {
	ID              string `json:"id"`
	StandardModelID string `json:"standard_model_id"`
	ModelName       string `json:"model_name"`
	OfficialID      string `json:"official_id"`
	// Company / ModelType 供原型表格的「模型公司」「模型类型」两列使用。
	Company         string                         `json:"company"`
	ModelType       string                         `json:"model_type"`
	SupplierModelID string                         `json:"supplier_model_id"`
	Region          string                         `json:"region"`
	Coefficient     string                         `json:"coefficient"`
	State           string                         `json:"state"`
	EverAdmitted    bool                           `json:"ever_admitted"`
	Revision        string                         `json:"revision"`
	Test            supplierModelTestDTO           `json:"test"`
	QuoteReadiness  supplierModelQuoteReadinessDTO `json:"quote_readiness"`
}

type supplierModelPageDTO struct {
	List  []supplierModelDTO `json:"list"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

type supplierModelQuery struct {
	Page    int
	Size    int
	Keyword string
	Region  string
	State   string
}

// supplierModelReader 是列表所需的最小仓储接口（便于单测替换）。
type supplierModelReader interface {
	List(ctx context.Context, supplierID int64, page, size int) ([]supplypg.SupplierModelRecord, int64, error)
}

// SupplierModelService 是供应商门户模型列表服务。
type SupplierModelService struct {
	store supplierModelReader
}

// NewSupplierModelService 构造。store 为 nil 时返回 nil，调用方据此不注册路由。
func NewSupplierModelService(store supplierModelReader) *SupplierModelService {
	if store == nil {
		return nil
	}
	return &SupplierModelService{store: store}
}

// List 返回本主体资源；supplier_id 由调用方从登录态解析后传入。
func (s *SupplierModelService) List(ctx context.Context, supplierID int64, q supplierModelQuery) (*supplierModelPageDTO, error) {
	if supplierID <= 0 {
		return nil, apperr.ErrForbidden
	}
	records, total, err := s.store.List(ctx, supplierID, q.Page, q.Size)
	if err != nil {
		return nil, err
	}
	rows := make([]supplierModelDTO, 0, len(records))
	for i := range records {
		rows = append(rows, buildSupplierModelDTO(records[i]))
	}
	return &supplierModelPageDTO{List: rows, Total: total, Page: q.Page, Size: q.Size}, nil
}

// buildSupplierModelDTO 把窄查询结果映射成前端契约。
func buildSupplierModelDTO(record supplypg.SupplierModelRecord) supplierModelDTO {
	test := supplierModelTestDTO{
		ReportID:           record.ReportID,
		Status:             "NOT_TESTED",
		ConnectivityTested: record.HasReport,
		ConnectivityPassed: record.HasReport && record.ConnectionPassed,
		StressTested:       record.HasReport,
		StressPassed:       record.HasReport && record.StressPassed,
		Valid:              record.HasReport,
	}
	if record.HasReport {
		test.Status = "COMPLETED"
		test.ConnectionMS = record.ConnectionMS
		test.RPS = record.RPS
		test.TPS = record.TPS
	}
	reasons := supplierQuoteBlocks(record)
	return supplierModelDTO{
		ID:              record.ID,
		StandardModelID: record.StandardModelID,
		ModelName:       record.ModelName,
		OfficialID:      record.OfficialID,
		Company:         record.Company,
		ModelType:       record.ModelType,
		SupplierModelID: record.SupplierModelID,
		Region:          record.Region,
		Coefficient:     record.Coefficient,
		State:           record.State,
		EverAdmitted:    record.EverAdmitted,
		Revision:        supplierModelRevision,
		Test:            test,
		QuoteReadiness:  supplierModelQuoteReadinessDTO{Eligible: len(reasons) == 0, BlockedReasons: reasons},
	}
}

// supplierQuoteBlocks 是给供应商看的「暂不可报价」原因。
//
// 只保留供应商能自己处理的三条：没接入、没测试、测试没过。
// 内部口径（正式配置是否绑定、模型资料是否核验）不在这里暴露，
// 否则供应商会拿着内部字段名来问。
func supplierQuoteBlocks(record supplypg.SupplierModelRecord) []string {
	reasons := make([]string, 0, 3)
	if record.State != "ADMITTED" || !record.EverAdmitted {
		reasons = append(reasons, "RESOURCE_NOT_ADMITTED")
	}
	if !record.HasReport {
		reasons = append(reasons, "TESTS_REQUIRED")
	} else if !record.ConnectionPassed || !record.StressPassed {
		reasons = append(reasons, "TESTS_NOT_PASSED")
	}
	return reasons
}

// ---- handler ----

type supplierModelHandler struct{ svc *SupplierModelService }

// supplierModelList 读取并校验分页/筛选参数。
func supplierModelList(c *gin.Context) supplierModelQuery {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	size, err := strconv.Atoi(c.DefaultQuery("size", strconv.Itoa(supplierModelDefaultSize)))
	if err != nil || size < 1 {
		size = supplierModelDefaultSize
	}
	if size > supplierModelMaxSize {
		size = supplierModelMaxSize
	}
	return supplierModelQuery{
		Page: page, Size: size,
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Region:  strings.TrimSpace(c.Query("region")),
		State:   strings.TrimSpace(c.Query("state")),
	}
}

// list 返回本主体模型列表。
//
//	@Summary	供应商门户 模型管理列表
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int		false	"页码，默认 1"
//	@Param		size	query	int		false	"每页条数，默认 20，上限 100"
//	@Success	200		{object}	response.Body{data=supplierModelPageDTO}
//	@Failure	400,401	{object}	response.Body
//	@Router		/api/supplier/models [get]
func (h *supplierModelHandler) list(c *gin.Context) {
	supplierID, _, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	query := supplierModelList(c)
	if query.Region != "" && query.Region != "CN" && query.Region != "OVERSEAS" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "部署节点只支持 CN / OVERSEAS", 400))
		return
	}
	if query.State != "" && !supplierModelStates[query.State] {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "资源状态取值不合法", 400))
		return
	}
	result, err := h.svc.List(c.Request.Context(), supplierID, query)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, result)
}

// registerSupplierModelRoutes 挂载供应商门户模型列表。
//
// 只读接口，不挂 RequirePerm（供应商角色零内部权限点），隔离靠登录态 supplier_id。
func registerSupplierModelRoutes(protected *gin.RouterGroup, svc *SupplierModelService) {
	if svc == nil {
		return
	}
	handler := &supplierModelHandler{svc: svc}
	protected.GET("/models", handler.list)
}
