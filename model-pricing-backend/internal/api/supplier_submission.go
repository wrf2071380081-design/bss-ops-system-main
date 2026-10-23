package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
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
	// decodeCredential 为 nil 表示「禁止解密」（配置可一键关闭）。
	decodeCredential func(ctx context.Context, stored, supplierID string) (string, error)
	starter          TestJobStarter
	clock            func() time.Time
}

// NewSupplierSubmissionService 构造服务；sealer 可为空（此时不接受携带 Key 的写入）。
func NewSupplierSubmissionService(store *supplypg.SubmissionStore, sealer supply.SecretSealer,
	decodeCredential func(ctx context.Context, stored, supplierID string) (string, error),
	starter TestJobStarter, clock func() time.Time) *SupplierSubmissionService {
	if clock == nil {
		clock = time.Now
	}
	return &SupplierSubmissionService{store: store, sealer: sealer, decodeCredential: decodeCredential, starter: starter, clock: clock}
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
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	submissionID, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	// 后端门禁（T-06）：不信任前端禁用态，直接调接口也必须被拦。
	switch err := h.checkSubmitGate(c.Request.Context(), submissionID); {
	case errors.Is(err, errGateNoRows):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "至少需要一行模型", 400))
		return
	case errors.Is(err, errGateConnectivityMissing):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "还有模型未完成连通测试，不能提交", 409))
		return
	case errors.Is(err, errGateStressMissing):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "至少需要一行完成压力测试，不能提交", 409))
		return
	case err != nil:
		writeSubmissionError(c, err)
		return
	}
	_ = supplierID
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
// 发起测试时的两类凭据错误，供 handler 翻译成明确文案。
var (
	errCredentialMissing      = errors.New("test credential missing")
	errCredentialUndecryptable = errors.New("test credential undecryptable")
)

// startRowTest 对单行发起一次测试并回写任务号（T-03）。
// 返回 skipped=true 表示该行这一类已经测过，未重复建任务。
func (h *supplierSubmissionHandler) startRowTest(ctx context.Context, supplierID, submissionID, rowID int64,
	platformCategory, idempotencyKey string) (string, bool, error) {
	row, err := h.svc.store.TestRow(ctx, supplierID, submissionID, rowID)
	if err != nil {
		return "", false, err
	}
	if platformCategory == "baseline" && row.TestJobID != nil {
		return strconv.FormatInt(*row.TestJobID, 10), true, nil
	}
	if platformCategory == "stress" && row.StressJobID != nil {
		return strconv.FormatInt(*row.StressJobID, 10), true, nil
	}
	apiKey := "demo-key"
	if row.TestBaseURL != "demo" {
		if h.svc.decodeCredential == nil {
			return "", false, errCredentialUndecryptable
		}
		if strings.TrimSpace(row.SealedAPIKey) == "" {
			return "", false, errCredentialMissing
		}
		plain, err := h.svc.decodeCredential(ctx, row.SealedAPIKey, strconv.FormatInt(supplierID, 10))
		if err != nil {
			return "", false, errCredentialUndecryptable
		}
		apiKey = plain
		if err := h.svc.store.LogCredentialOpen(ctx, supplierID, rowID); err != nil {
			return "", false, err
		}
	}
	resourceID, err := h.svc.store.EnsureTestResource(ctx, row)
	if err != nil {
		return "", false, err
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
			Protocol: "openai", APIKey: apiKey,
		},
		Params:         params,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return "", false, err
	}
	localJobID, convErr := strconv.ParseInt(result.LocalJobID, 10, 64)
	if convErr != nil {
		return "", false, convErr
	}
	if err := h.svc.store.AttachTestJob(ctx, rowID, localJobID, platformCategory); err != nil {
		return "", false, err
	}
	return result.LocalJobID, false, nil
}

// writeRowTestError 统一翻译单行/批量测试的错误。
func writeRowTestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errCredentialMissing):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "该申报行未填写测试 API Key", 400))
	case errors.Is(err, errCredentialUndecryptable):
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试凭据无法解密（密钥版本或主体不匹配）", 503))
	case errors.Is(err, tpdomain.ErrNotConfigured):
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
	case errors.Is(err, tpdomain.ErrInvalidCommand):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "发起测试的参数不完整", 400))
	default:
		writeSubmissionError(c, err)
	}
}

// rowTest 是「参与测试 / 查测试进度」入口（单行）。
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
	rawCategory := c.Query("category")
	if strings.TrimSpace(rawCategory) == "" {
		// 前端发起测试用 POST + body；查进度用 GET + query，两者都支持。
		var body struct {
			Category string `json:"category"`
		}
		_ = c.ShouldBindJSON(&body)
		rawCategory = body.Category
	}
	category, platformCategory, ok := resolveTestCategory(rawCategory)
	if !ok {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 CONNECTIVITY / STRESS", 400))
		return
	}
	if h.svc.starter == nil {
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
		return
	}
	jobID, skipped, err := h.startRowTest(c.Request.Context(), supplierID, submissionID, rowID,
		platformCategory, strings.TrimSpace(c.GetHeader("Idempotency-Key")))
	if err != nil {
		writeRowTestError(c, err)
		return
	}
	response.Success(c, gin.H{
		"job_id": jobID, "category": category, "status": "QUEUED",
		"tested": false, "passed": false, "skipped": skipped,
	})
}

// batchTests 一键测全部行（某一门类）。
// 已完成的行跳过，返回发起/跳过/失败三项计数，便于前端提示"还有几行没测"。
func (h *supplierSubmissionHandler) batchTests(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	submissionID, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var body struct {
		Category string `json:"category"`
	}
	_ = c.ShouldBindJSON(&body)
	category, platformCategory, ok := resolveTestCategory(body.Category)
	if !ok {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 CONNECTIVITY / STRESS", 400))
		return
	}
	if h.svc.starter == nil {
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
		return
	}
	ctx := c.Request.Context()
	header, err := h.svc.store.Get(ctx, supplierID, submissionID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	if header.Status != submissionDraft {
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "只有草稿可以发起测试", 409))
		return
	}
	rows, err := h.svc.store.Rows(ctx, []int64{submissionID})
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	started, skipped, failed := 0, 0, 0
	firstErr := error(nil)
	baseKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	for _, row := range rows[submissionID] {
		jobID, wasSkipped, err := h.startRowTest(ctx, supplierID, submissionID, row.ID, platformCategory,
			baseKey+"-"+strconv.FormatInt(row.ID, 10))
		_ = jobID
		switch {
		case err != nil:
			failed++
			if firstErr == nil {
				firstErr = err
			}
		case wasSkipped:
			skipped++
		default:
			started++
		}
	}
	result := gin.H{"category": category, "started": started, "skipped": skipped, "failed": failed}
	if failed > 0 && started == 0 && skipped == 0 {
		// 一发都没成功时把错误原因带回，避免前端只看到 0/0/0。
		writeRowTestError(c, firstErr)
		return
	}
	response.Success(c, result)
}

// checkSubmitGate 是提交门禁的后端校验（T-06）：
// 每一行都要测过连通，且至少一行测过压力。前端已拦，这里防直接调接口绕过。
func (h *supplierSubmissionHandler) checkSubmitGate(ctx context.Context, submissionID int64) error {
	rows, err := h.svc.store.Rows(ctx, []int64{submissionID})
	if err != nil {
		return err
	}
	list := rows[submissionID]
	if len(list) == 0 {
		return errGateNoRows
	}
	anyStress := false
	for _, row := range list {
		if row.ConnectivityStatus == nil || !isTerminalStatus(*row.ConnectivityStatus) {
			return errGateConnectivityMissing
		}
		if row.StressStatus != nil && isTerminalStatus(*row.StressStatus) {
			anyStress = true
		}
	}
	if !anyStress {
		return errGateStressMissing
	}
	return nil
}

var (
	errGateNoRows               = errors.New("submission has no rows")
	errGateConnectivityMissing  = errors.New("some rows have no connectivity test")
	errGateStressMissing        = errors.New("no row has stress test")
)

// resolveTestCategory 把前端类别翻译成平台门类，并把映射收在一处。
func resolveTestCategory(raw string) (string, string, bool) {
	category := strings.ToUpper(strings.TrimSpace(raw))
	platformCategory, ok := submissionCategoryToPlatform[category]
	return category, platformCategory, ok && category != ""
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
			envelope, err := h.svc.sealer.Seal(c.Request.Context(), key,
				supplysecurity.TestCredentialBinding(strconv.FormatInt(supplierID, 10)))
			if err != nil {
				writeSubmissionError(c, err)
				return nil, false
			}
			sealed = supplysecurity.EncodeTestCredential(envelope)
		}
		rowID, _ := strconv.ParseInt(strings.TrimSpace(row.ID), 10, 64)
		rows = append(rows, supplypg.SubmissionRowInput{
			ID: rowID, StandardModelID: modelID, SupplierModelName: strings.TrimSpace(row.InterfaceModelID),
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
		if row.ConnectivityStatus != nil {
			tested := isTerminalStatus(*row.ConnectivityStatus)
			passed := verdictOK(row.ConnectivityVerdict)
			item.ConnectivityJob = &submissionTestDTO{
				Category: "CONNECTIVITY", Status: *row.ConnectivityStatus, Tested: tested, Passed: passed,
				ConnectionMS: row.ConnectionMS,
			}
			item.ConnectivityTested, item.ConnectivityPassed = tested, passed
		}
		if row.StressStatus != nil {
			tested := isTerminalStatus(*row.StressStatus)
			passed := verdictOK(row.StressVerdict)
			item.StressJob = &submissionTestDTO{
				Category: "STRESS", Status: *row.StressStatus, Tested: tested, Passed: passed,
				RPS: row.RPS, TPS: row.TPS,
			}
			item.StressTested, item.StressPassed = tested, passed
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

func isTerminalStatus(status string) bool {
	switch status {
	case "SUCCEEDED", "FAILED", "ABORTED", "CANCELED":
		return true
	}
	return false
}

func verdictOK(verdict *string) bool {
	return verdict != nil && (*verdict == "pass" || *verdict == "pass_with_warnings")
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
	group.POST("/:id/rows/:rowId/test", idempotent, handler.rowTest)
	// 写接口一律挂幂等（红线）。
	group.POST("", idempotent, handler.create)
	group.PUT("/:id", idempotent, handler.update)
	group.POST("/:id/tests", idempotent, handler.batchTests)
	group.POST("/:id/submit", idempotent, handler.submit)
	group.POST("/:id/withdraw", idempotent, handler.withdraw)
}
