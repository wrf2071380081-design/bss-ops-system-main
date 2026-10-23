package api

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 申报单状态（Q-B 最小集）。前端按这些值推导 permissions 与列表筛选。
const (
	submissionDraft       = "DRAFT"
	submissionPending     = "PENDING_APPROVAL"
	submissionApproved    = "APPROVED"
	submissionRejected    = "REJECTED"
	submissionWithdrawn   = "WITHDRAWN"
)

// SupplierSubmissionService 组合申报的读写与凭据加密。
type SupplierSubmissionService struct {
	store   *supplypg.SubmissionStore
	sealer  supply.SecretSealer
	starter TestJobStarter
	clock   func() time.Time
}

// NewSupplierSubmissionService 构造服务；sealer 可为空（此时不接受携带 Key 的写入）。
func NewSupplierSubmissionService(store *supplypg.SubmissionStore, sealer supply.SecretSealer, starter TestJobStarter, clock func() time.Time) *SupplierSubmissionService {
	if clock == nil {
		clock = time.Now
	}
	return &SupplierSubmissionService{store: store, sealer: sealer, starter: starter, clock: clock}
}

// ---- DTO（字段名与前端 supplierSubmissions.types.ts 严格一致）----

type submissionTestDTO struct {
	Category     string  `json:"category"`
	Status       string  `json:"status"`
	Tested       bool    `json:"tested"`
	Passed       bool    `json:"passed"`
	JobID        string  `json:"job_id,omitempty"`
	ReportID     string  `json:"report_id,omitempty"`
	ConnectionMS *string `json:"connection_ms"`
	RPS          *string `json:"rps"`
	TPS          *string `json:"tps"`
}

type submissionRowDTO struct {
	ID                 string              `json:"id"`
	InterfaceModelID   string              `json:"interface_model_id"`
	StandardModelID    string              `json:"standard_model_id"`
	StandardModelName  string              `json:"standard_model_name"`
	OfficialID         string              `json:"official_id"`
	Coefficient        string              `json:"coefficient"`
	TestBaseURL        string              `json:"test_base_url"`
	RowStatus          string              `json:"row_status"`
	ConnectivityTested bool                `json:"connectivity_tested"`
	ConnectivityPassed bool                `json:"connectivity_passed"`
	StressTested       bool                `json:"stress_tested"`
	StressPassed       bool                `json:"stress_passed"`
	ConnectivityJob    *submissionTestDTO  `json:"connectivity_job,omitempty"`
	StressJob          *submissionTestDTO  `json:"stress_job,omitempty"`
}

type submissionPermissionsDTO struct {
	CanEdit     bool `json:"can_edit"`
	CanSubmit   bool `json:"can_submit"`
	CanWithdraw bool `json:"can_withdraw"`
}

type submissionDTO struct {
	ID          string                   `json:"id"`
	Kind        string                   `json:"kind"`
	Status      string                   `json:"status"`
	Revision    string                   `json:"revision"`
	Reason      string                   `json:"reason"`
	Rows        []submissionRowDTO       `json:"rows"`
	Permissions submissionPermissionsDTO `json:"permissions"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
	SubmittedAt *time.Time               `json:"submitted_at"`
}

type submissionWriteRow struct {
	ID              string `json:"id"`
	InterfaceModelID string `json:"interface_model_id"`
	StandardModelID  string `json:"standard_model_id"`
	Coefficient      string `json:"coefficient"`
	TestBaseURL      string `json:"test_base_url"`
	TestAPIKey       string `json:"test_api_key"`
}

type submissionWrite struct {
	Kind             string                `json:"kind"`
	ExpectedRevision string                `json:"expected_revision"`
	Reason           string                `json:"reason"`
	Rows             []submissionWriteRow  `json:"rows"`
}

// ---- handler ----

type supplierSubmissionHandler struct{ svc *SupplierSubmissionService }

// 前端测试类别 ↔ 平台门类。映射必须留在 API 边界：平台只认 baseline/stress。
var submissionCategoryToPlatform = map[string]string{"CONNECTIVITY": "baseline", "STRESS": "stress"}
var platformCategoryToSubmission = map[string]string{"baseline": "CONNECTIVITY", "stress": "STRESS"}

func (h *supplierSubmissionHandler) supplierID(c *gin.Context) (int64, bool) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.OperatorType != "SUPPLIER" || op.OperatorID <= 0 {
		response.Error(c, apperr.ErrUnauthorized)
		return 0, false
	}
	return op.OperatorID, true
}

func (h *supplierSubmissionHandler) list(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 || size < 1 || size > 100 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	headers, total, err := h.svc.store.Page(c.Request.Context(), supplierID, c.Query("status"), c.Query("kind"), page, size)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	ids := make([]int64, 0, len(headers))
	for _, item := range headers {
		ids = append(ids, item.ID)
	}
	rows, err := h.svc.store.Rows(c.Request.Context(), ids)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	list := make([]submissionDTO, 0, len(headers))
	for _, header := range headers {
		list = append(list, buildSubmissionDTO(header, rows[header.ID]))
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

func (h *supplierSubmissionHandler) detail(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	id, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	header, err := h.svc.store.Get(c.Request.Context(), supplierID, id)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	rows, err := h.svc.store.Rows(c.Request.Context(), []int64{id})
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, buildSubmissionDTO(*header, rows[id]))
}

func (h *supplierSubmissionHandler) modelOptions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 || size < 1 || size > 100 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	options, total, err := h.svc.store.ModelOptions(c.Request.Context(), c.Query("keyword"), page, size)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	list := make([]gin.H, 0, len(options))
	for _, option := range options {
		list = append(list, gin.H{
			"id": strconv.FormatInt(option.ID, 10), "name": option.Name,
			"official_id": option.OfficialID, "company": option.Company,
		})
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

func (h *supplierSubmissionHandler) create(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	var body submissionWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	kind := strings.TrimSpace(body.Kind)
	if kind != "MODEL_NEW" && kind != "PRICE_CHANGE" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "kind 只支持 MODEL_NEW / PRICE_CHANGE", 400))
		return
	}
	rows, ok := h.buildRows(c, supplierID, body.Rows)
	if !ok {
		return
	}
	region, err := h.svc.store.SupplierRegion(c.Request.Context(), supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	id, err := h.svc.store.CreateDraft(c.Request.Context(), supplierID, kind, region, strings.TrimSpace(body.Reason), rows)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, gin.H{"id": strconv.FormatInt(id, 10), "revision": "1", "status": submissionDraft})
}

func (h *supplierSubmissionHandler) update(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	id, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var body submissionWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	expected, err := positiveID(strings.TrimSpace(body.ExpectedRevision))
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "expected_revision 必填", 400))
		return
	}
	rows, ok := h.buildRows(c, supplierID, body.Rows)
	if !ok {
		return
	}
	revision, err := h.svc.store.UpdateDraft(c.Request.Context(), supplierID, id, expected, strings.TrimSpace(body.Reason), rows)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, gin.H{"id": strconv.FormatInt(id, 10), "revision": strconv.FormatInt(revision, 10), "status": submissionDraft})
}

func (h *supplierSubmissionHandler) submit(c *gin.Context) {
	h.transition(c, []string{submissionDraft, submissionRejected}, submissionPending, true)
}

func (h *supplierSubmissionHandler) withdraw(c *gin.Context) {
	h.transition(c, []string{submissionPending}, submissionWithdrawn, false)
}

func (h *supplierSubmissionHandler) transition(c *gin.Context, from []string, to string, markSubmitted bool) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	id, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var body struct {
		ExpectedRevision string `json:"expected_revision"`
	}
	_ = c.ShouldBindJSON(&body)
	expected, err := positiveID(strings.TrimSpace(body.ExpectedRevision))
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "expected_revision 必填", 400))
		return
	}
	revision, err := h.svc.store.Transition(c.Request.Context(), supplierID, id, expected, from, to, markSubmitted)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, gin.H{"id": strconv.FormatInt(id, 10), "revision": strconv.FormatInt(revision, 10), "status": to})
}

// rowTest 是「参与测试 / 查测试进度」入口。
// 一期依赖尚未合入的测试平台分支，这里明确回 503——
// 不能回 404：前端会把 404 翻译成"数据不存在或当前账号不可见"，更难排查。
// rowTest 发起一次测试并把任务回写到申报行（T-03，打通的关键一步）。
//
// 凭据现状：SecretSealer 只有 Seal 没有 Open，全系统无法解密供应商填的 Key。
// 因此一期只支持 `test_base_url=demo` 的目标（平台内置模拟人格，不需要真 Key）；
// 真实目标明确返回 503 并说明原因，避免"看着能点、点了没用"。
func (h *supplierSubmissionHandler) rowTest(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	submissionID, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	rowID, err := positiveID(c.Param("rowId"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	category := strings.ToUpper(strings.TrimSpace(c.Query("category")))
	platformCategory, exists := submissionCategoryToPlatform[category]
	if !exists {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 CONNECTIVITY / STRESS", 400))
		return
	}
	if h.svc.starter == nil {
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
		return
	}
	ctx := c.Request.Context()
	row, err := h.svc.store.TestRow(ctx, supplierID, submissionID, rowID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	if row.TestBaseURL != "demo" {
		response.Error(c, apperr.New(apperr.ErrSystem.Code,
			"该申报行使用真实测试地址；测试凭据目前无法解密，请先用 demo 目标联调", 503))
		return
	}
	if row.TestJobID != nil {
		// 已发过测试：不重复建任务（Q6 测试机会由 M3 统一管，这里先幂等返回）。
		response.Success(c, gin.H{
			"job_id": strconv.FormatInt(*row.TestJobID, 10), "category": category,
			"status": "RUNNING", "tested": false, "passed": false,
		})
		return
	}
	resourceID, err := h.svc.store.EnsureTestResource(ctx, row)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	params := map[string]any(nil)
	if platformCategory == "stress" {
		// 真机实测：stress 必须显式声明参考引擎，否则 400。
		params = map[string]any{
			"engine": "reference-performance-v1", "concurrency": []int{1},
			"duration": "30s", "warmup": true, "warmup_duration": "5s",
		}
	}
	result, err := h.svc.starter.Start(ctx, tpdomain.CreateCommand{
		ResourceID: strconv.FormatInt(resourceID, 10),
		ActorID:    strconv.FormatInt(supplierID, 10),
		Category:   tpdomain.Category(platformCategory),
		Owner:      row.SupplierName,
		Target: tpdomain.Target{
			Model: row.SupplierModelName, BaseURL: row.TestBaseURL,
			Protocol: "openai", APIKey: "demo-key",
		},
		Params:         params,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeTestStartError(c, err)
		return
	}
	localJobID, convErr := strconv.ParseInt(result.LocalJobID, 10, 64)
	if convErr != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	if err := h.svc.store.AttachTestJob(ctx, rowID, localJobID); err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	response.Success(c, gin.H{
		"job_id": result.LocalJobID, "category": category,
		"status": result.Status, "tested": false, "passed": false,
	})
}

// buildRows 校验行并把 API Key 加密；失败时已写好响应。
func (h *supplierSubmissionHandler) buildRows(c *gin.Context, supplierID int64, input []submissionWriteRow) ([]supplypg.SubmissionRowInput, bool) {
	if len(input) == 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "至少需要一行模型", 400))
		return nil, false
	}
	rows := make([]supplypg.SubmissionRowInput, 0, len(input))
	for _, row := range input {
		modelID, err := positiveID(strings.TrimSpace(row.StandardModelID))
		if err != nil {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "standard_model_id 非法", 400))
			return nil, false
		}
		if strings.TrimSpace(row.InterfaceModelID) == "" {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "interface_model_id 必填", 400))
			return nil, false
		}
		sealed := ""
		if key := strings.TrimSpace(row.TestAPIKey); key != "" {
			if h.svc.sealer == nil {
				response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试凭据加密未配置，无法保存 API Key", 503))
				return nil, false
			}
			envelope, err := h.svc.sealer.Seal(c.Request.Context(), key, supply.SecretBinding{
				Purpose:    "test-api-key",
				SupplierID: strconv.FormatInt(supplierID, 10),
			})
			if err != nil {
				writeSubmissionError(c, err)
				return nil, false
			}
			// 密文是二进制：base64 后连同密钥版本一起存，便于将来轮换。
			sealed = envelope.KeyVersion + ":" + base64.StdEncoding.EncodeToString(envelope.Ciphertext)
		}
		rows = append(rows, supplypg.SubmissionRowInput{
			StandardModelID: modelID, SupplierModelName: strings.TrimSpace(row.InterfaceModelID),
			Coefficient: strings.TrimSpace(row.Coefficient), TestBaseURL: strings.TrimSpace(row.TestBaseURL),
			SealedAPIKey: sealed,
		})
	}
	return rows, true
}

// buildSubmissionDTO 组装前端契约；顺带推导测试状态与权限。
func buildSubmissionDTO(header supplypg.SubmissionHeader, rows []supplypg.SubmissionRowRecord) submissionDTO {
	dto := submissionDTO{
		ID: strconv.FormatInt(header.ID, 10), Kind: header.Kind, Status: header.Status,
		Revision: strconv.FormatInt(header.Revision, 10), Reason: header.Reason,
		Rows: make([]submissionRowDTO, 0, len(rows)), Permissions: permissionsFor(header.Status),
		CreatedAt: header.CreatedAt, UpdatedAt: header.UpdatedAt, SubmittedAt: header.SubmittedAt,
	}
	for _, row := range rows {
		item := submissionRowDTO{
			ID: strconv.FormatInt(row.ID, 10), InterfaceModelID: row.SupplierModelName,
			StandardModelID: strconv.FormatInt(row.StandardModelID, 10), StandardModelName: row.StandardModelName,
			OfficialID: row.OfficialID, Coefficient: row.Coefficient, TestBaseURL: row.TestBaseURL,
			RowStatus: row.RowStatus,
		}
		if row.TestCategory != nil {
			category := platformCategoryToSubmission[*row.TestCategory]
			tested := row.TestStatus != nil && (*row.TestStatus == "SUCCEEDED" || *row.TestStatus == "FAILED" || *row.TestStatus == "ABORTED" || *row.TestStatus == "CANCELED")
			passed := row.TestVerdict != nil && (*row.TestVerdict == "pass" || *row.TestVerdict == "pass_with_warnings")
			test := &submissionTestDTO{
				Category: category, Status: deref(row.TestStatus), Tested: tested, Passed: passed,
				ConnectionMS: row.ConnectionMS, RPS: row.RPS, TPS: row.TPS,
			}
			switch category {
			case "CONNECTIVITY":
				item.ConnectivityJob = test
				item.ConnectivityTested, item.ConnectivityPassed = tested, passed
			case "STRESS":
				item.StressJob = test
				item.StressTested, item.StressPassed = tested, passed
			}
		}
		dto.Rows = append(dto.Rows, item)
	}
	return dto
}

// permissionsFor 是申报单是否可编辑/提交/撤回的唯一判定点。
func permissionsFor(status string) submissionPermissionsDTO {
	switch status {
	case submissionDraft:
		return submissionPermissionsDTO{CanEdit: true, CanSubmit: true, CanWithdraw: true}
	case submissionPending:
		return submissionPermissionsDTO{CanWithdraw: true}
	case submissionRejected:
		return submissionPermissionsDTO{CanEdit: true, CanSubmit: true}
	default:
		return submissionPermissionsDTO{}
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// positiveID 解析正整数标识。
func positiveID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// writeSubmissionError 把领域错误翻译成 HTTP 状态。
func writeSubmissionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, supply.ErrNotFound):
		response.Error(c, apperr.ErrNotFound)
	case errors.Is(err, supply.ErrRevisionConflict), errors.Is(err, supply.ErrInactive):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "申报单已变化，请刷新后重试", 409))
	case errors.Is(err, supply.ErrForbidden):
		response.Error(c, apperr.ErrForbidden)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// registerSupplierSubmissionRoutes 挂载供应商申报的正式接口。
func registerSupplierSubmissionRoutes(protected *gin.RouterGroup, deps Deps, idempotent gin.HandlerFunc) {
	handler := &supplierSubmissionHandler{svc: deps.SupplierSubmissions}
	group := protected.Group("/submissions")
	group.GET("", handler.list)
	// 静态段先于参数段注册，避免 /model-options 被 /:id 吞掉。
	group.GET("/model-options", handler.modelOptions)
	group.GET("/:id", handler.detail)
	group.GET("/:id/rows/:rowId/test", handler.rowTest)
	// 写接口一律挂幂等（红线）。
	group.POST("", idempotent, handler.create)
	group.PUT("/:id", idempotent, handler.update)
	group.POST("/:id/submit", idempotent, handler.submit)
	group.POST("/:id/withdraw", idempotent, handler.withdraw)
}
