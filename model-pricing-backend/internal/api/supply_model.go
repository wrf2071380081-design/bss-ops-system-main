package api

import (
	"errors"
	"github.com/gin-gonic/gin"
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	infra "model_bss/internal/infra/middleware"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
	"strconv"
	"strings"
	"time"
)

type supplyModelHandler struct {
	reader     *supply.ModelReader
	writer     SupplyModelWriter
	operations *supply.SourceOperationReader
}
type supplyModelPermissions struct {
	CanEdit   bool `json:"can_edit"`
	CanVerify bool `json:"can_verify"`
}
type supplyModelDTO struct {
	supply.ModelData
	ID               string                 `json:"id"`
	MaintainerID     string                 `json:"maintainer_id"`
	Status           string                 `json:"status"`
	VerifyStatus     string                 `json:"verify_status"`
	CheckedBy        string                 `json:"checked_by"`
	VersionNo        uint64                 `json:"version_no,string"`
	CurrentVersionNo uint64                 `json:"current_version_no,omitempty,string"`
	Revision         uint64                 `json:"revision,string"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	MissingFields    []string               `json:"missing_fields"`
	QuoteReadiness   supply.ModelReadiness  `json:"quote_readiness"`
	Permissions      supplyModelPermissions `json:"permissions"`
	Changed          *bool                  `json:"changed,omitempty"`
}
type supplyModelPageDTO struct {
	List  []supplyModelDTO `json:"list"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`
}

func supplyActor(c *gin.Context) string {
	return strconv.FormatInt(middleware.OperatorFrom(c).StaffID, 10)
}
func supplyModelResponse(model *supply.StandardModel, actor string, history bool) supplyModelDTO {
	copied := model.Clone()
	permissions := supplyModelPermissions{}
	version := model.VersionNo
	readiness := model.Readiness()
	if !history {
		permissions.CanEdit = actor == model.MaintainerID
		permissions.CanVerify = permissions.CanEdit && model.Status == "ACTIVE" && len(model.MissingFields()) == 0 && model.VerifyStatus != "VERIFIED"
	} else {
		version = 0
		readiness.Ready = false
		readiness.Reasons = append(readiness.Reasons, "HISTORICAL_MODEL_VERSION")
	}
	return supplyModelDTO{ModelData: copied.Data, ID: model.ID, MaintainerID: model.MaintainerID, Status: model.Status, VerifyStatus: model.VerifyStatus, CheckedBy: model.CheckedBy, VersionNo: model.VersionNo, CurrentVersionNo: version, Revision: model.Revision, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, MissingFields: model.MissingFields(), QuoteReadiness: readiness, Permissions: permissions}
}
func supplyHTTPError(c *gin.Context, err error) {
	var field *supply.FieldError
	switch {
	case errors.As(err, &field):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "字段校验失败："+field.Path, 400))
	case errors.Is(err, supply.ErrIncomplete):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "官方资料尚未补齐，无法核验", 400))
	case errors.Is(err, supply.ErrForbidden):
		response.Error(c, apperr.New(apperr.ErrForbidden.Code, "当前人员无权执行此操作", 403))
	case errors.Is(err, supply.ErrNotFound):
		response.Error(c, apperr.ErrNotFound)
	case errors.Is(err, supply.ErrRevisionConflict), errors.Is(err, supply.ErrIdentityConflict), errors.Is(err, supply.ErrInactive), errors.Is(err, supply.ErrSourceRejected), errors.Is(err, supply.ErrConfigInUse), errors.Is(err, supply.ErrConfigInactive), errors.Is(err, supply.ErrSupplierRegionLocked):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "记录状态或操作依据已发生变化，请刷新后核对", 409))
	case errors.Is(err, supply.ErrDependencyUnavailable):
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "供应侧依赖尚未就绪", 503))
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// list returns new standard models, not legacy SKUs.
// @Summary 标准模型列表
// @Tags supply-standard-models
// @Security BearerAuth
// @Param page query int false "页码，默认1"
// @Param size query int false "每页数量，1至100"
// @Param company query string false "公司"
// @Param model_type query string false "TEXT/IMAGE/VIDEO/AUDIO"
// @Param keyword query string false "关键词"
// @Param status query string false "ACTIVE/INACTIVE"
// @Param verify_status query string false "INCOMPLETE/UNVERIFIED/VERIFIED"
// @Param sort_by query string false "id/name/company/model_type/verify_status/checked_at/updated_at"
// @Param order query string false "asc/desc"
// @Success 200 {object} response.Body{data=supplyModelPageDTO}
// @Failure 400,401,403,503 {object} response.Body
// @Router /api/internal/standard-models [get]
func (h *supplyModelHandler) list(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size", "keyword", "company", "model_type", "companies", "model_types", "supplier_ids", "regions", "status", "verify_status", "sort_by", "order")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	query := supply.ModelListQuery{Page: page, Size: size, Keyword: values.Get("keyword"), Company: values.Get("company"), ModelType: values.Get("model_type"), Status: values.Get("status"), VerifyStatus: values.Get("verify_status"), SortBy: values.Get("sort_by"), Order: values.Get("order")}
	for _, field := range []struct {
		key    string
		target *[]string
	}{{"companies", &query.Companies}, {"model_types", &query.ModelTypes}, {"supplier_ids", &query.SupplierIDs}, {"regions", &query.Regions}} {
		*field.target, err = supplyModelArray(values, field.key)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
	}
	result, err := h.reader.List(c.Request.Context(), query)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	rows := make([]supplyModelDTO, 0, len(result.List))
	for _, m := range result.List {
		rows = append(rows, supplyModelResponse(m, supplyActor(c), false))
	}
	response.Success(c, supplyModelPageDTO{rows, result.Total, result.Page, result.Size})
}

// @Summary 标准模型详情
// @Tags supply-standard-models
// @Security BearerAuth
// @Param id path string true "标准模型bigint字符串ID"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/standard-models/{id} [get]
func (h *supplyModelHandler) get(c *gin.Context) {
	if _, err := supplyQuery(c); err != nil {
		supplyHTTPError(c, err)
		return
	}
	model, err := h.reader.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	response.Success(c, supplyModelResponse(model, supplyActor(c), false))
}

// @Summary 标准模型不可变历史版本
// @Tags supply-standard-models
// @Security BearerAuth
// @Param id path string true "标准模型ID"
// @Param page query int false "页码"
// @Param size query int false "每页数量，1至100"
// @Success 200 {object} response.Body{data=supplyModelPageDTO}
// @Failure 400,401,403,404,503 {object} response.Body
// @Router /api/internal/standard-models/{id}/versions [get]
func (h *supplyModelHandler) versions(c *gin.Context) {
	values, err := supplyQuery(c, "page", "size")
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	page, size, err := supplyPageParams(values)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	result, err := h.reader.Versions(c.Request.Context(), c.Param("id"), page, size)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	rows := make([]supplyModelDTO, 0, len(result.List))
	for _, m := range result.List {
		rows = append(rows, supplyModelResponse(m, supplyActor(c), true))
	}
	response.Success(c, supplyModelPageDTO{rows, result.Total, result.Page, result.Size})
}

type supplyCreateModelRequest struct {
	supply.ModelData
	Reason string `json:"reason"`
}
type supplyUpdateModelRequest struct {
	supply.ModelData
	ExpectedRevision uint64 `json:"expected_revision,string"`
	Reason           string `json:"reason"`
}
type supplyVerifyModelRequest struct {
	ExpectedRevision uint64 `json:"expected_revision,string"`
	CheckedAt        string `json:"checked_at"`
	Reason           string `json:"reason"`
}

func supplyWriteMeta(c *gin.Context, reason string) (supply.WriteMeta, error) {
	if strings.TrimSpace(reason) == "" {
		return supply.WriteMeta{}, supplyRequestError("reason", "a change reason is required")
	}
	requestID := infra.FromContext(c)
	if requestID == "" {
		return supply.WriteMeta{}, supply.ErrDependencyUnavailable
	}
	return supply.WriteMeta{ActorID: supplyActor(c), Reason: reason, RequestID: requestID}, nil
}
func (h *supplyModelHandler) writeResult(c *gin.Context, model *supply.StandardModel, err error, expectedRevision uint64) {
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if model == nil {
		supplyHTTPError(c, supply.ErrReadContract)
		return
	}
	dto := supplyModelResponse(model, supplyActor(c), false)
	if expectedRevision > 0 {
		changed := model.Revision != expectedRevision
		dto.Changed = &changed
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

// @Summary 保存标准模型资料（可待补齐）
// @Tags supply-standard-models
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "写操作幂等键"
// @Param body body supplyCreateModelRequest true "模型资料及原因；维护人取认证人员"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,409,503 {object} response.Body
// @Router /api/internal/standard-models [post]
func (h *supplyModelHandler) create(c *gin.Context) {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	var req supplyCreateModelRequest
	if err := decodeSupplyRequest(c, &req, nil); err != nil {
		supplyHTTPError(c, err)
		return
	}
	meta, err := supplyWriteMeta(c, req.Reason)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	model, err := h.writer.Create(c.Request.Context(), meta, req.ModelData)
	h.writeResult(c, model, err, 0)
}

// @Summary 整体替换标准模型可编辑资料
// @Tags supply-standard-models
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "标准模型ID"
// @Param Idempotency-Key header string true "写操作幂等键"
// @Param body body supplyUpdateModelRequest true "全部资料、字符串expected_revision及原因"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,404,409,503 {object} response.Body
// @Router /api/internal/standard-models/{id} [put]
func (h *supplyModelHandler) update(c *gin.Context) {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	var req supplyUpdateModelRequest
	if err := decodeSupplyRequest(c, &req, supplyReplacementFields); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if req.ExpectedRevision == 0 || req.ExpectedRevision > math.MaxInt64 {
		supplyHTTPError(c, supplyRequestError("expected_revision", "a positive bigint revision string is required"))
		return
	}
	meta, err := supplyWriteMeta(c, req.Reason)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	model, err := h.writer.Update(c.Request.Context(), meta, c.Param("id"), req.ExpectedRevision, req.ModelData)
	h.writeResult(c, model, err, req.ExpectedRevision)
}

// @Summary 人工核验标准模型
// @Tags supply-standard-models
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "标准模型ID"
// @Param Idempotency-Key header string true "写操作幂等键"
// @Param body body supplyVerifyModelRequest true "字符串expected_revision、核验日期及原因"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,404,409,503 {object} response.Body
// @Router /api/internal/standard-models/{id}/verify [post]
func (h *supplyModelHandler) verify(c *gin.Context) {
	if h.writer == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	var req supplyVerifyModelRequest
	if err := decodeSupplyRequest(c, &req, nil); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if req.ExpectedRevision == 0 || req.ExpectedRevision > math.MaxInt64 || req.CheckedAt == "" {
		supplyHTTPError(c, supplyRequestError("expected_revision/checked_at", "bigint revision and verification date are required"))
		return
	}
	meta, err := supplyWriteMeta(c, req.Reason)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	model, err := h.writer.Verify(c.Request.Context(), meta, c.Param("id"), req.ExpectedRevision, req.CheckedAt)
	h.writeResult(c, model, err, req.ExpectedRevision)
}
