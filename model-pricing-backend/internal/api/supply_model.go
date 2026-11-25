package api

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"math"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	infra "model_bss/internal/infra/middleware"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
	"slices"
	"strconv"
	"strings"
	"time"
)

type supplyModelHandler struct {
	reader       *supply.ModelReader
	// manualResources 用于「零货源模型手工登记一条资源」（运营兜底，落 PENDING_TEST）。
	manualResources *supplypg.ManualResourceStore
	writer       SupplyModelWriter
	manualWriter SupplyManualModelWriter
	operations   *supply.SourceOperationReader
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
	// LoadError 非空表示这一行读不出来、已降级成占位行。前端据此把该行渲染成
	// 「资料异常」，而不是让整页 500。内部原因不下发（细节对用户无意义，也泄露结构）。
	LoadError string `json:"load_error,omitempty"`
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

// supplyCanMaintain 判定"除维护人之外是否另有维护授权"（原型：五个成员都能维护标准模型）。
// 策略只在这里定：持 M1:E 即可维护；维护人本人不受此限（见 supplyModelResponse）。
// 注意：这只放开"编辑"，不放开"核验"——核验仍只给绑定维护人。
func supplyCanMaintain(c *gin.Context) bool {
	op := middleware.OperatorFrom(c)
	if op == nil {
		return false
	}
	return slices.Contains(op.Perms, perm.M1Edit)
}

func supplyModelResponse(model *supply.StandardModel, actor string, canMaintain bool, history bool) supplyModelDTO {
	copied := model.Clone()
	permissions := supplyModelPermissions{}
	version := model.VersionNo
	readiness := model.Readiness()
	if !history {
		maintainer := actor == model.MaintainerID
		permissions.CanEdit = maintainer || canMaintain
		// 核验不随编辑权放开：只有绑定维护人能把资料标成 VERIFIED。
		permissions.CanVerify = maintainer && model.Status == "ACTIVE" && len(model.MissingFields()) == 0 && model.VerifyStatus != "VERIFIED"
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
		// base_url 的校验来自两处（publicURL 的语法检查 + FormalURLPolicy 的环境策略：
		// 必须 https、不能带 query/fragment/内嵌凭据、域名要能解析到公网 IP）。
		// 对用户来说结论一致，没必要把英文 reason 甩给前端——直接给可执行的提示。
		if field.Path == "base_url" {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "接口地址必须是可公网解析的 https 地址", 400))
			return
		}
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "字段校验失败："+field.Path, 400))
	case errors.Is(err, supply.ErrIncomplete):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "官方资料尚未补齐，无法核验", 400))
	case errors.Is(err, supply.ErrQualificationMetadataRequired):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "当前资质结构要求提交资质类型，请刷新表单后重试", 400))
	case errors.Is(err, supply.ErrForbidden):
		response.Error(c, apperr.New(apperr.ErrForbidden.Code, "当前人员无权执行此操作", 403))
	case errors.Is(err, supply.ErrNotFound):
		response.Error(c, apperr.ErrNotFound)
	case errors.Is(err, supply.ErrQualificationFileUnavailable):
		response.Error(c, apperr.ErrNotFound)
	case errors.Is(err, supply.ErrIdentityConflict):
		// 唯一索引兜底（并发/预检漏网）：厂商 + 官方 Model ID 已被占用。
		// 不能和下面的 ErrRevisionConflict 混用同一句话——一个要用户"去核对是谁占了"，
		// 一个要用户"刷新重来"，文案给错就白拦了。
		response.Error(c, apperr.New(apperr.ErrConflict.Code, "已存在相同厂商与官方 Model ID 的标准模型，请到列表里查找后维护资料，不要重复新增", 409))
	case errors.Is(err, supply.ErrRevisionConflict), errors.Is(err, supply.ErrF12ContractArchived), errors.Is(err, supply.ErrInactive), errors.Is(err, supply.ErrSourceRejected), errors.Is(err, supply.ErrConfigInUse), errors.Is(err, supply.ErrConfigInactive), errors.Is(err, supply.ErrSupplierRegionLocked):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "记录状态或操作依据已发生变化，请刷新后核对", 409))
	case errors.Is(err, supply.ErrTestInProgress):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "测试进行中，请等待测试完成后再操作", 409))
	case errors.Is(err, supply.ErrTestEvidenceRequired):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "测试报告未通过或已失效，不能登记接入", 409))
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
	actor, canMaintain := supplyActor(c), supplyCanMaintain(c)
	for _, entry := range result.List {
		if entry.Model != nil {
			rows = append(rows, supplyModelResponse(entry.Model, actor, canMaintain, false))
			continue
		}
		// 单行读不出来只降级这一行：其余行照常返回，页面不再整块打不开。
		rows = append(rows, supplyModelUnreadable(entry.Failure))
	}
	response.Success(c, supplyModelPageDTO{rows, result.Total, result.Page, result.Size})
}

// supplyModelUnreadableMessage 是下发给用户的降级文案：说清「能做什么」，
// 不暴露内部契约细节（具体原因写在后端日志里）。
const supplyModelUnreadableMessage = "该模型资料读取异常，暂时无法查看详情，请维护人重新保存一次资料后再试"

// supplyModelUnreadable 把读不出来的行降级成占位 DTO：保留平面列上可信的展示字段
// （ID/名称/公司/类型/状态/更新时间），其余留空并带上 load_error。
func supplyModelUnreadable(failure *supply.ModelFailure) supplyModelDTO {
	readiness := supply.ModelReadiness{Ready: false, Reasons: []string{"UNREADABLE_ROW"}}
	if failure == nil {
		return supplyModelDTO{VerifyStatus: "INCOMPLETE", MissingFields: []string{}, QuoteReadiness: readiness, Permissions: supplyModelPermissions{}, LoadError: supplyModelUnreadableMessage}
	}
	// 只记 ID 和原因：有了这两项就能直接定位到行，不必再靠"逐页扫"猜。
	log.Printf("standard model list degraded: model_id=%s status=%s reason=%s", failure.ID, failure.Status, failure.Reason)
	status := failure.Status
	if status != "ACTIVE" && status != "INACTIVE" {
		status = "ACTIVE"
	}
	return supplyModelDTO{
		ModelData:      supply.ModelData{Company: failure.Company, Name: failure.Name, ModelType: failure.ModelType},
		ID:             failure.ID,
		Status:         status,
		VerifyStatus:   "INCOMPLETE",
		UpdatedAt:      failure.UpdatedAt,
		MissingFields:  []string{},
		QuoteReadiness: readiness,
		Permissions:    supplyModelPermissions{},
		LoadError:      supplyModelUnreadableMessage,
	}
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
	response.Success(c, supplyModelResponse(model, supplyActor(c), supplyCanMaintain(c), false))
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
	for _, entry := range result.List {
		if entry.Model == nil {
			continue
		}
		rows = append(rows, supplyModelResponse(entry.Model, supplyActor(c), false, true))
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
	return supply.WriteMeta{ActorID: supplyActor(c), Reason: reason, RequestID: requestID, CanMaintain: supplyCanMaintain(c)}, nil
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
	dto := supplyModelResponse(model, supplyActor(c), supplyCanMaintain(c), false)
	if expectedRevision > 0 {
		changed := model.Revision != expectedRevision
		dto.Changed = &changed
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

// identityTaken 在**新增**前拿当前标准模型库对照一次（厂商 + 官方 Model ID）。
//
// 为什么要有这道预检（2026-11-24）：
//  1. 唯一索引 uk_standard_model_company_official 只能事后拦住，错误文案是笼统的
//     「记录状态或操作依据已发生变化」，用户根本看不出撞的是哪一条；
//  2. 前端需要一句能直接照做的话：库里已有的那条叫什么、ID 是多少。
//
// 预检只负责"把话说清楚"，并发时仍由唯一索引兜底（supplypg.modelUniqueError →
// supply.ErrIdentityConflict → 本文件的 supplyHTTPError）。
// 预检查不动就一律放行，绝不因为预检失败挡住正常新增。
func (h *supplyModelHandler) identityTaken(c *gin.Context, data supply.ModelData) *apperr.Error {
	if h.reader == nil {
		return nil
	}
	officialID := strings.TrimSpace(data.OfficialID)
	company := strings.TrimSpace(data.Company)
	if officialID == "" || company == "" {
		return nil
	}
	page, err := h.reader.List(c.Request.Context(), supply.ModelListQuery{
		Keyword: officialID, Company: company, Page: 1, Size: 100,
	})
	if err != nil || page == nil {
		return nil
	}
	for _, entry := range page.List {
		model := entry.Model
		// 读不出来的行不参与"身份重复"预检：它的官方 ID 不可信，硬判重会误挡正常新增。
		if model == nil || !strings.EqualFold(strings.TrimSpace(model.Data.OfficialID), officialID) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(model.Data.Name), strings.TrimSpace(data.Name)) {
			conflict := apperr.New(apperr.ErrConflict.Code, fmt.Sprintf(
				"库里已有同名模型：%s（官方 ID %s，ID %s），请到列表里找到它维护资料，不要重复新增",
				model.Data.Name, model.Data.OfficialID, model.ID), 409)
			return &conflict
		}
		conflict := apperr.New(apperr.ErrConflict.Code, fmt.Sprintf(
			"官方 Model ID「%s」已被模型「%s」（ID %s，厂商 %s）占用，请核对后再提交",
			model.Data.OfficialID, model.Data.Name, model.ID, model.Data.Company), 409)
		return &conflict
	}
	return nil
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
	if bad := h.identityTaken(c, req.ModelData); bad != nil {
		response.Error(c, *bad)
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

// @Summary 手动补全并留痕标准模型（可待补齐）
// @Tags supply-standard-models
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "写操作幂等键"
// @Param body body supplyCreateModelRequest true "资料及操作说明；完整资料由当前维护人核验"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,409,503 {object} response.Body
// @Router /api/internal/standard-models/manual-completion [post]
func (h *supplyModelHandler) manualCreate(c *gin.Context) {
	if h.manualWriter == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	var req supplyCreateModelRequest
	if err := decodeSupplyRequest(c, &req, nil); err != nil {
		supplyHTTPError(c, err)
		return
	}
	// 新增必须有最小必填集：名称/官方 ID/公司/类型。
	// 以前什么都不填也能建，直接落出「名称与官方 ID 为空」的行——这类行会让列表与模型广场
	// 的一致性校验失败（2026-11-18 两处 500 的根因），因此在入口就拦掉。
	if msg := validateNewModel(req.ModelData); msg != "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, msg, 400))
		return
	}
	// 新增前先跟**当前**标准模型库对照（厂商 + 官方 ID）；命中就给出"是哪一条"的 409。
	// 旧口径只靠唯一索引事后拦，文案是笼统的"记录状态已变化"，用户看不出撞了谁。
	if bad := h.identityTaken(c, req.ModelData); bad != nil {
		response.Error(c, *bad)
		return
	}
	meta, err := supplyWriteMeta(c, req.Reason)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	model, err := h.manualWriter.SaveManual(c.Request.Context(), meta, "", 0, req.ModelData)
	h.writeResult(c, model, err, 0)
}

// validateNewModel 返回第一条不满足的必填提示；空串表示通过。
func validateNewModel(data supply.ModelData) string {
	if strings.TrimSpace(data.Name) == "" {
		return "请填写标准模型名称"
	}
	if strings.TrimSpace(data.OfficialID) == "" {
		return "请填写官方模型 ID（模型厂商的原始标识，如 deepseek-ai/DeepSeek-V3）"
	}
	if strings.TrimSpace(data.Company) == "" {
		return "请填写模型公司"
	}
	switch strings.TrimSpace(data.ModelType) {
	case "TEXT", "IMAGE", "VIDEO", "AUDIO":
	default:
		return "请选择模型类型（TEXT / IMAGE / VIDEO / AUDIO）"
	}
	return ""
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

// @Summary 手动补全并留痕标准模型
// @Tags supply-standard-models
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "标准模型ID"
// @Param Idempotency-Key header string true "写操作幂等键"
// @Param body body supplyUpdateModelRequest true "全部资料、字符串expected_revision及操作说明"
// @Success 200 {object} response.Body{data=supplyModelDTO}
// @Failure 400,401,403,404,409,503 {object} response.Body
// @Router /api/internal/standard-models/{id}/manual-completion [put]
func (h *supplyModelHandler) manualUpdate(c *gin.Context) {
	if h.manualWriter == nil {
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
	model, err := h.manualWriter.SaveManual(c.Request.Context(), meta, c.Param("id"), req.ExpectedRevision, req.ModelData)
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
