package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
	"model_bss/internal/infra/supplypg"
	tpinfra "model_bss/internal/infra/testplatform"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 申报单状态（Q-B 最小集）。前端按这些值推导 permissions 与列表筛选。
const (
	submissionDraft = "DRAFT"
	// submissionPendingTest 表示已提交但仍有模型未通过连通测试（原型「等待测试补齐」）。
	submissionPendingTest = "PENDING_TEST"
	submissionPending     = "PENDING_APPROVAL"
	submissionApproved    = "APPROVED"
	submissionRejected    = "REJECTED"
	submissionWithdrawn   = "WITHDRAWN"
)

// 申报类型（与前端 kind 一致）。
const (
	// submissionKindModelNew 是模型新增申报：要跑连通/压力测试，受提交门禁约束。
	submissionKindModelNew = "MODEL_NEW"
	// submissionKindPriceChange 是价格变更申报：只调采购系数，不换受测目标，不跑门禁。
	submissionKindPriceChange = "PRICE_CHANGE"
)

// SupplierSubmissionService 组合申报的读写与测试发起。
//
// 凭据策略（路线 A，2026-10-26 与 A 侧对齐）：测试 Key 按原样入库，
// 列名仍为 test_api_key_enc（历史命名）。加密层已随 A 侧重构移除，
// 因此这里不再做封装/解密；安全改进见交接文档的待办项。
type SupplierSubmissionService struct {
	store   *supplypg.SubmissionStore
	starter TestJobStarter
	clock   func() time.Time
}

// NewSupplierSubmissionService 构造服务。
func NewSupplierSubmissionService(store *supplypg.SubmissionStore, starter TestJobStarter, clock func() time.Time) *SupplierSubmissionService {
	if clock == nil {
		clock = time.Now
	}
	return &SupplierSubmissionService{store: store, starter: starter, clock: clock}
}

// now 取当前时间（测试可注入）。
func (s *SupplierSubmissionService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

// ---- DTO（字段名与前端 supplierSubmissions.types.ts 严格一致）----

type submissionTestDTO struct {
	Category string `json:"category"`
	Status   string `json:"status"`
	Tested   bool   `json:"tested"`
	Passed   bool   `json:"passed"`
	// Verdict 是平台给出的判定（pass / pass_with_warnings / fail）。
	// 压力测试没有判定，留空——前端据此区分"未通过"和"已完成但无判定"。
	Verdict string `json:"verdict,omitempty"`
	// Stale=true 表示测试目标已变化（改了地址/Key/模型），该结果不再有效。
	Stale        bool    `json:"stale,omitempty"`
	JobID        string  `json:"job_id,omitempty"`
	ReportID     string  `json:"report_id,omitempty"`
	ConnectionMS *string `json:"connection_ms"`
	RPS          *string `json:"rps"`
	TPS          *string `json:"tps"`
}

type submissionRowDTO struct {
	ID                string `json:"id"`
	InterfaceModelID  string `json:"interface_model_id"`
	StandardModelID   string `json:"standard_model_id"`
	StandardModelName string `json:"standard_model_name"`
	OfficialID        string `json:"official_id"`
	Coefficient       string `json:"coefficient"`
	TestBaseURL       string `json:"test_base_url"`
	// ParticipateTest 是压力测试勾选（原型「压力测试」列）。
	ParticipateTest bool `json:"participate_test"`
	// SupplementRequired 表示该行已提交但仍未通过（原型「待补」）。
	SupplementRequired bool               `json:"supplement_required"`
	RowStatus          string             `json:"row_status"`
	ConnectivityTested bool               `json:"connectivity_tested"`
	ConnectivityPassed bool               `json:"connectivity_passed"`
	StressTested       bool               `json:"stress_tested"`
	StressPassed       bool               `json:"stress_passed"`
	ConnectivityJob    *submissionTestDTO `json:"connectivity_job,omitempty"`
	StressJob          *submissionTestDTO `json:"stress_job,omitempty"`
}

type submissionPermissionsDTO struct {
	CanEdit     bool `json:"can_edit"`
	CanSubmit   bool `json:"can_submit"`
	CanWithdraw bool `json:"can_withdraw"`
}

type submissionDTO struct {
	ID            string   `json:"id"`
	SubmissionNo  string   `json:"submission_no"`
	SubmitterName string   `json:"submitter_name"`
	Region        string   `json:"region"`
	RowCount      int      `json:"row_count"`
	ModelNames    []string `json:"model_names"`
	// PendingRowCount 是「提交后仍未通过」的行数，前端用于显示"待补 N 个"。
	PendingRowCount int                      `json:"pending_row_count"`
	Kind            string                   `json:"kind"`
	Status          string                   `json:"status"`
	Revision        string                   `json:"revision"`
	Reason          string                   `json:"reason"`
	Rows            []submissionRowDTO       `json:"rows"`
	Permissions     submissionPermissionsDTO `json:"permissions"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
	SubmittedAt     *time.Time               `json:"submitted_at"`
}

type submissionWriteRow struct {
	ID               string `json:"id"`
	ParticipateTest  bool   `json:"participate_test"`
	InterfaceModelID string `json:"interface_model_id"`
	StandardModelID  string `json:"standard_model_id"`
	Coefficient      string `json:"coefficient"`
	TestBaseURL      string `json:"test_base_url"`
	TestAPIKey       string `json:"test_api_key"`
}

type submissionWrite struct {
	Kind             string               `json:"kind"`
	ExpectedRevision string               `json:"expected_revision"`
	Reason           string               `json:"reason"`
	Rows             []submissionWriteRow `json:"rows"`
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
	filter := supplypg.SubmissionFilter{
		Status: filterQueryValues(c, "status"), Kind: filterQueryValues(c, "kind"),
		Company: filterQueryValues(c, "company"), ModelType: filterQueryValues(c, "model_type"),
		Region: filterQueryValues(c, "region"), Submitter: filterQueryValues(c, "submitter"),
	}
	headers, total, err := h.svc.store.Page(c.Request.Context(), supplierID, filter, page, size)
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
	// 列表里所有申报都属于同一主体，凭据只取一次。
	credential, err := h.svc.store.TestCredential(c.Request.Context(), supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	list := make([]submissionDTO, 0, len(headers))
	for _, header := range headers {
		list = append(list, buildSubmissionDTO(header, applyCredential(rows[header.ID], credential)))
	}
	response.Success(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

// filterQueryValues 读多值筛选参数。
//
// 原型筛选项是勾选框（可多选），前端用逗号拼接传过来；
// 同时也接受重复参数（?status=A&status=B），方便直接调接口的人。
func filterQueryValues(c *gin.Context, key string) []string {
	raw := c.QueryArray(key)
	if len(raw) == 0 {
		if single := c.Query(key); single != "" {
			raw = []string{single}
		}
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, piece := range strings.Split(item, ",") {
			if trimmed := strings.TrimSpace(piece); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	}
	return values
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
	credential, err := h.svc.store.TestCredential(c.Request.Context(), supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, buildSubmissionDTO(*header, applyCredential(rows[id], credential)))
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

// filterOptions 返回列表筛选项（按该供应商已有数据收窄）。
func (h *supplierSubmissionHandler) filterOptions(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	option, err := h.svc.store.FilterOptions(c.Request.Context(), supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	response.Success(c, gin.H{
		"companies": option.Companies, "model_types": option.ModelTypes,
		"regions": option.Regions, "submitters": option.Submitters,
		"statuses": []string{submissionDraft, submissionPendingTest, submissionPending,
			submissionApproved, submissionRejected, submissionWithdrawn},
		"kinds": []string{submissionKindModelNew, submissionKindPriceChange},
	})
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
	if kind != submissionKindModelNew && kind != submissionKindPriceChange {
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
	// 返回完整 DTO：前端保存后的 normalizeSaved 直接读 dto.rows，只回 id 会让页面崩。
	if !h.respondWithDTO(c, supplierID, id) {
		return
	}
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
	if _, err := h.svc.store.UpdateDraft(c.Request.Context(), supplierID, id, expected, strings.TrimSpace(body.Reason), rows); err != nil {
		writeSubmissionError(c, err)
		return
	}
	if !h.respondWithDTO(c, supplierID, id) {
		return
	}
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
	ctx := c.Request.Context()
	// 价格变更没有"测试覆盖"语义（只调系数，不换受测目标），门禁只对模型新增生效。
	header, err := h.svc.store.Get(ctx, supplierID, submissionID)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	if header.Kind == submissionKindModelNew {
		// 原型口径（2026-10-27 确认）：覆盖 + 至少一个压力模型即可提交，不看是否通过。
		if err := h.evaluateSubmitGate(ctx, supplierID, submissionID); err != nil {
			writeSubmissionError(c, err)
			return
		}
	}
	submitter, _ := h.svc.store.SubmitterName(ctx, supplierID)
	// 原型：提交即「待审批」。PENDING_TEST 只留给改造前的历史数据。
	h.transition(c, []string{submissionDraft, submissionRejected, submissionWithdrawn, submissionPendingTest},
		submissionPending, true, submitter)
}

func (h *supplierSubmissionHandler) withdraw(c *gin.Context) {
	h.transition(c, []string{submissionPending, submissionPendingTest}, submissionWithdrawn, false, "")
}

func (h *supplierSubmissionHandler) transition(c *gin.Context, from []string, to string, markSubmitted bool, submitterName string) {
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
	accountID := int64(0)
	if op := middleware.OperatorFrom(c); op != nil {
		accountID = op.AccountID
	}
	revision, err := h.svc.store.Transition(c.Request.Context(), supplierID, id, expected, from, to, markSubmitted, submitterName, accountID)
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
var errCredentialMissing = errors.New("test credential missing")

// 单行测试的结果动作。
const (
	actionStarted     = "STARTED"
	actionSkipped     = "SKIPPED"
	actionRetried     = "RETRIED"
	actionNotSelected = "NOT_SELECTED"
	actionFailed      = "FAILED"
)

// startRowTest 对单行发起一次测试并回写任务号（T-03）。
// 返回 action 描述本次动作：发起 / 跳过（已有有效结果或正在跑）/ 重测 / 未勾选。
func (h *supplierSubmissionHandler) startRowTest(ctx context.Context, supplierID, submissionID, rowID int64,
	platformCategory, idempotencyKey string) (string, string, error) {
	row, err := h.svc.store.TestRow(ctx, supplierID, submissionID, rowID)
	if err != nil {
		return "", actionFailed, err
	}
	// 压力测试只测勾选行（原型「压力测试」列）。
	if platformCategory == "stress" && !row.ParticipateTest {
		return "", actionNotSelected, nil
	}
	// 000046：URL/Key 统一取**主体**凭据；行上的两列只是快照。
	// 未登记凭据时直接给出可操作的提示，别让平台侧报 400。
	baseURL, apiKey := supplypg.CredentialForTest(&supplypg.SupplierTestCredential{BaseURL: row.EffectiveBaseURL, APIKey: row.EffectiveAPIKey})
	if strings.TrimSpace(baseURL) == "" {
		return "", actionFailed, errCredentialMissing
	}
	// demo 目标由 CredentialForTest 补占位 Key；其余目标必须真有 Key。
	if strings.TrimSpace(apiKey) == "" {
		return "", actionFailed, errCredentialMissing
	}
	// 本次发单的指纹按当前主体凭据现算：改了凭据就该能重测，旧结果也随之失效。
	fingerprint := tpdomain.TargetFingerprint(row.StandardModelID, row.SupplierModelName, baseURL, apiKey)
	if jobID, skip := shouldSkipRetest(row, platformCategory, fingerprint); skip {
		return jobID, actionSkipped, nil
	}
	hadJob := (platformCategory == "stress" && row.StressJobID != nil) || (platformCategory != "stress" && row.TestJobID != nil)
	// 快照与指纹先归位，再发单：两个指纹出自同一份凭据，后续才判得准「失效」。
	if err := h.svc.store.RefreshRowSnapshot(ctx, rowID, baseURL, apiKey, fingerprint); err != nil {
		return "", actionFailed, err
	}
	resourceID, err := h.svc.store.EnsureTestResource(ctx, row)
	if err != nil {
		return "", actionFailed, err
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
		ResourceID:        strconv.FormatInt(resourceID, 10),
		TargetFingerprint: fingerprint,
		ActorID:           strconv.FormatInt(supplierID, 10),
		Category:          tpdomain.Category(platformCategory),
		Owner:             row.SupplierName,
		Target: tpdomain.Target{
			// 平台按官方 ID 识别模型；供应商自定义名只用于展示。
			Model:   modelForTest(baseURL, row.OfficialID, row.SupplierModelName),
			BaseURL: baseURL, Protocol: "openai", APIKey: apiKey,
		},
		Params:         params,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return "", actionFailed, err
	}
	localJobID, convErr := strconv.ParseInt(result.LocalJobID, 10, 64)
	if convErr != nil {
		return "", actionFailed, convErr
	}
	if err := h.svc.store.AttachTestJob(ctx, rowID, localJobID, platformCategory); err != nil {
		return "", actionFailed, err
	}
	if hadJob {
		return result.LocalJobID, actionRetried, nil
	}
	return result.LocalJobID, actionStarted, nil
}

// shouldSkipRetest 判断该行这一类测试能否跳过。
//
// 规则（对齐任务书「同一受测配置一次有效测试；配置变化后可重测」）：
//   - 无任务 → 不跳过
//   - 有任务且指纹与当前行一致：
//       排队/运行中 → 跳过（避免重复建任务）
//       已终态且已有报告 → 跳过（结果有效，含不达标）
//       已终态但没有报告（工具异常）→ 重测
//   - 指纹不一致（改了地址/Key/模型）→ 重测
//
// 判「有没有结果」看的是**报告是否存在**，不是 verdict 是否非空。
// 压力测试本来就没有 verdict（只有 RPS/TPS），按 verdict 判会把每一次
// 已完成的压测都当成工具异常，于是每点一次「发起测试」就重跑一轮
// ——2026-10-28 实测：一条申报单在 4 分钟里跑出 5 个压测任务。
//
// currentFingerprint 是按**主体当前凭据**算出来的指纹（000046）。
// 不能再用行上的 test_fingerprint 比：那是上一次写入的快照，
// 凭据改了它不会自己变，会导致"改了凭据还不让重测"。
//
// 旧实现是"有任务就跳过"，导致改了配置也永远不能重测。
func shouldSkipRetest(row *supplypg.TestRow, platformCategory, currentFingerprint string) (string, bool) {
	var jobID *int64
	var status, jobFingerprint string
	var hasReport bool
	if platformCategory == "stress" {
		jobID, status, jobFingerprint, hasReport = row.StressJobID, row.StressJobStatus, row.StressJobFingerprint, row.StressJobHasReport
	} else {
		jobID, status, jobFingerprint, hasReport = row.TestJobID, row.TestJobStatus, row.TestJobFingerprint, row.TestJobHasReport
	}
	if jobID == nil {
		return "", false
	}
	id := strconv.FormatInt(*jobID, 10)
	if currentFingerprint != "" && jobFingerprint != "" && currentFingerprint != jobFingerprint {
		return id, false // 配置变了：允许重测
	}
	if !isTerminalStatus(status) {
		return id, true // 排队/运行中
	}
	if hasReport {
		return id, true // 有报告就是有效结果（不达标也算）
	}
	return id, false // 终态但没有报告：工具异常，重试
}

// demoTestModel 是测试平台 demo 模式内置的模型名。
//
// 实测结论（2026-10-26）：demo 目标只接受平台内置的 demo 模型名——
// 传官方 ID（如 deepseek-chat）直接 400；传自定义名（demo/rowA）虽受理但 L1 门控失败。
// 真实目标仍按约定传官方 ID，因此这里按 base_url 分支，不做全局替换。
const demoTestModel = "demo/deepseek-v4"

// modelForTest 取发给平台的模型标识。
func modelForTest(baseURL, officialID, supplierModelName string) string {
	if strings.EqualFold(strings.TrimSpace(baseURL), "demo") {
		return demoTestModel
	}
	if strings.TrimSpace(officialID) != "" {
		return strings.TrimSpace(officialID)
	}
	return strings.TrimSpace(supplierModelName)
}

// writeRowTestError 统一翻译单行/批量测试的错误。
func writeRowTestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errCredentialMissing):
		// 000046：凭据是主体级，缺了就是「还没登记」，不是「这一行没填」。
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请先在申报页登记本主体的测试接口 URL 与 API Key", 400))
	case errors.Is(err, tpdomain.ErrNotConfigured):
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
	case errors.Is(err, tpdomain.ErrInvalidCommand):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "发起测试的参数不完整", 400))
	case err != nil:
		var apiErr *tpinfra.APIError
		if errors.As(err, &apiErr) {
			// 平台明确拒绝时把原因带出去，不再笼统报"系统错误"。
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "测试平台拒绝： "+apiErr.Error(), 400))
			return
		}
		writeSubmissionError(c, err)
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
	// 查进度走只读快照，绝不发单。
	// 这条分支原来是直接调 startRowTest：一个 GET 就会去测试平台建任务，
	// 页面上的「刷新结果」于是变成了「又发起一轮压测」（2026-10-28 实测）。
	if c.Request.Method == http.MethodGet {
		h.writeTestSnapshot(c, supplierID, submissionID, rowID, category, platformCategory)
		return
	}
	if h.svc.starter == nil {
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未接入", 503))
		return
	}
	jobID, action, err := h.startRowTest(c.Request.Context(), supplierID, submissionID, rowID,
		platformCategory, strings.TrimSpace(c.GetHeader("Idempotency-Key")))
	if err != nil {
		writeRowTestError(c, err)
		return
	}
	// 单行发起也算「发起测试」：未勾选时不能悄悄回 200 说没事发生。
	if action == actionNotSelected {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "该模型未勾选压力测试，请先勾选后再发起测试", 400))
		return
	}
	response.Success(c, gin.H{
		"job_id": jobID, "category": category, "status": "QUEUED",
		"tested": false, "passed": false, "skipped": action == actionSkipped, "action": action,
	})
}

// writeTestSnapshot 回读某一行某一类测试的当前结果，纯读不写。
// 返回结构与发起测试一致，前端可以用同一套 applyTest 落地。
func (h *supplierSubmissionHandler) writeTestSnapshot(c *gin.Context, supplierID, submissionID, rowID int64,
	category, platformCategory string) {
	snap, err := h.svc.store.TestSnapshot(c.Request.Context(), supplierID, submissionID, rowID, platformCategory)
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	// 判定口径与 buildSubmissionDTO 保持一致：终态且未失效才算「测过」。
	tested := snap.HasJob && isTerminalStatus(snap.Status) && !snap.Stale
	passed := tested && verdictTextOK(snap.Verdict)
	jobID, status := "", snap.Status
	if snap.JobID != nil {
		jobID = strconv.FormatInt(*snap.JobID, 10)
	}
	if status == "" {
		status = "NOT_STARTED"
	}
	response.Success(c, gin.H{
		"job_id": jobID, "category": category, "status": status,
		"tested": tested, "passed": passed, "skipped": false, "action": "QUERY",
		"verdict": snap.Verdict, "stale": snap.Stale,
		"connection_ms": snap.ConnectionMS, "rps": snap.RPS, "tps": snap.TPS,
	})
}

// verdictTextOK 是 verdictOK 的字符串版（快照读出来的是字符串）。
func verdictTextOK(value string) bool {
	return value == "pass" || value == "pass_with_warnings"
}

// batchTests 是「发起测试／异常重试」的唯一入口。
//
// 入参两种写法都支持：
//   {"categories":["CONNECTIVITY","STRESS"]}  自动模式（前端单按钮用这个）
//   {"category":"CONNECTIVITY"}               兼容旧调用
//
// 自动模式的语义：所有行做连通；压力只做勾选了 participate_test 的行。
// 已有有效结果或正在跑的行跳过；配置变了或上次工具异常的行重测。
// categories 里含 STRESS 但一行都没勾时直接 400，不发起任何测试。
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
		Category   string   `json:"category"`
		Categories []string `json:"categories"`
	}
	_ = c.ShouldBindJSON(&body)
	raw := body.Categories
	if len(raw) == 0 && strings.TrimSpace(body.Category) != "" {
		raw = []string{body.Category}
	}
	if len(raw) == 0 {
		raw = []string{"CONNECTIVITY", "STRESS"}
	}
	type target struct{ label, platform string }
	targets := make([]target, 0, len(raw))
	for _, item := range raw {
		label, platformCategory, valid := resolveTestCategory(item)
		if !valid {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 CONNECTIVITY / STRESS", 400))
			return
		}
		targets = append(targets, target{label: label, platform: platformCategory})
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
	// 待补状态也允许继续测试；只有已接入/已通过的单子不再发起。
	switch header.Status {
	case submissionDraft, submissionRejected, submissionWithdrawn, submissionPendingTest:
	default:
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "当前状态不能发起测试", 409))
		return
	}
	rows, err := h.svc.store.Rows(ctx, []int64{submissionID})
	if err != nil {
		writeSubmissionError(c, err)
		return
	}
	// 压测是「门禁的一部分」：一个都没勾时连连通性都不该发起。
	// 原来这里静默放过（每一行都记 NOT_SELECTED 然后回 200），前端还按成功提示，
	// 结果供应商以为测过了、提交时却被门禁挡住（2026-10-28 用户报障）。
	wantStress := false
	for _, t := range targets {
		if t.platform == "stress" {
			wantStress = true
			break
		}
	}
	if wantStress && !anyStressSelected(rows[submissionID]) {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请至少勾选一个压力测试", 400))
		return
	}
	type item struct {
		RowID    string `json:"row_id"`
		Category string `json:"category"`
		Action   string `json:"action"`
		JobID    string `json:"job_id,omitempty"`
		Reason   string `json:"reason,omitempty"`
	}
	items := make([]item, 0, len(rows[submissionID])*len(targets))
	counts := map[string]int{}
	firstErr := error(nil)
	baseKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	for _, t := range targets {
		for _, row := range rows[submissionID] {
			// 幂等键带纳秒后缀：重测必须新建平台任务，不能被上次响应回放。
			key := baseKey + "-" + strconv.FormatInt(row.ID, 10) + "-" + t.platform + "-" + strconv.FormatInt(h.svc.now().UnixNano(), 10)
			jobID, action, err := h.startRowTest(ctx, supplierID, submissionID, row.ID, t.platform, key)
			entry := item{RowID: strconv.FormatInt(row.ID, 10), Category: t.label, Action: action, JobID: jobID}
			if err != nil {
				entry.Action = actionFailed
				entry.Reason = testFailureReason(err)
				if firstErr == nil {
					firstErr = err
				}
			}
			counts[entry.Action]++
			items = append(items, entry)
		}
	}
	result := gin.H{
		"categories": raw, "items": items,
		"started": counts[actionStarted], "skipped": counts[actionSkipped],
		"retried": counts[actionRetried], "not_selected": counts[actionNotSelected],
		"failed": counts[actionFailed],
	}
	if counts[actionFailed] > 0 && len(items) == counts[actionFailed] {
		// 全部失败时把原因带回，避免前端只看到 0。
		writeRowTestError(c, firstErr)
		return
	}
	response.Success(c, result)
}

// testFailureReason 给逐项明细用的简短原因（前端直接展示）。
func testFailureReason(err error) string {
	switch {
	case errors.Is(err, errCredentialMissing):
		return "未填写测试 Key"
	case errors.Is(err, tpdomain.ErrNotConfigured):
		return "测试平台未接入"
	default:
		var apiErr *tpinfra.APIError
		if errors.As(err, &apiErr) {
			return apiErr.Error()
		}
		return "发起失败"
	}
}

// evaluateSubmitGate 按**原型口径**判断是否允许提交（2026-10-27 确认）。
//
// 原型的规则只有两条，且都写在申报页底部那句话里：
//   - 所有模型均执行连通性测试；
//   - 至少勾选一个压力测试模型（勾选的行要有压力结果）。
//
// **不看 verdict**——原型明确「结果不达标也属于有效结果」，
// 未通过的行提交后由 supplement_required 标成「待补」，而不是拦在门口。
//
// 失效（改了主体凭据 / 改了接口模型）的旧结果不算覆盖：那是旧配置的报告。
// 这类判断必须用主体当前凭据重算，否则供应商改了凭据还能拿旧结果过门禁。
func (h *supplierSubmissionHandler) evaluateSubmitGate(ctx context.Context, supplierID, submissionID int64) error {
	rows, err := h.svc.store.Rows(ctx, []int64{submissionID})
	if err != nil {
		return err
	}
	credential, err := h.svc.store.TestCredential(ctx, supplierID)
	if err != nil {
		return err
	}
	list := applyCredential(rows[submissionID], credential)
	if len(list) == 0 {
		return errGateNoRows
	}
	return checkSubmissionCoverage(list)
}

// checkSubmissionCoverage 是门禁的纯规则部分（不碰数据库，便于单测）。
//
// 返回的 apperr 直接对前端展示，所以文案要带计数——只报 409 不告诉供应商
// 「还差几个模型」，页面上的提示语就只能是干巴巴的「不能提交」。
func checkSubmissionCoverage(list []supplypg.SubmissionRowRecord) error {
	missingConnectivity, selected, pendingStress := 0, 0, 0
	for _, row := range list {
		if row.ConnectivityStatus == nil || row.ConnectivityStale || !isTerminalStatus(*row.ConnectivityStatus) {
			missingConnectivity++
		}
		if !row.ParticipateTest {
			continue
		}
		selected++
		if row.StressStatus == nil || row.StressStale || !isTerminalStatus(*row.StressStatus) {
			pendingStress++
		}
	}
	switch {
	case missingConnectivity > 0:
		return apperr.New(apperr.ErrIdempotency.Code,
			fmt.Sprintf("还有 %d 个模型没有连通性测试结果，请先发起测试", missingConnectivity), 409)
	case selected == 0:
		return apperr.New(apperr.ErrIdempotency.Code, "请至少勾选一个压力测试模型后再提交", 409)
	case pendingStress > 0:
		return apperr.New(apperr.ErrIdempotency.Code,
			fmt.Sprintf("勾选的模型里还有 %d 个没有压力测试结果，请先发起测试", pendingStress), 409)
	}
	return nil
}

var (
	errGateNoRows = errors.New("submission has no rows")
	// 下面几个保留作语义锚点：日志、单测与后续「等待测试补齐」环节会用到，
	// 当前门禁直接返回带计数的 apperr，不再靠它们分支。
	errGateNoPassedRow         = errors.New("no row has a passed connectivity test")
	errGateConnectivityMissing = errors.New("some rows have no connectivity test")
	errGateStressMissing       = errors.New("no row has stress test")
	errGateReportStale         = errors.New("test report is stale after target change")
)

// anyStressSelected 报告这张单子里是否有任意一行勾选了压力测试。
func anyStressSelected(rows []supplypg.SubmissionRowRecord) bool {
	for _, row := range rows {
		if row.ParticipateTest {
			return true
		}
	}
	return false
}

// resolveTestCategory 把前端类别翻译成平台门类，并把映射收在一处。
func resolveTestCategory(raw string) (string, string, bool) {
	category := strings.ToUpper(strings.TrimSpace(raw))
	platformCategory, ok := submissionCategoryToPlatform[category]
	return category, platformCategory, ok && category != ""
}

// buildRows 校验行并把主体凭据写进快照；失败时已写好响应。
//
// 000046 起 rows[].test_base_url / test_api_key **不再由前端提供**：
// 凭据是主体资源（/supplier/test-credential），这里统一取主体值做快照。
// 旧前端仍会带上这两个字段，收到也忽略，避免直接 400 把页面打挂。
func (h *supplierSubmissionHandler) buildRows(c *gin.Context, supplierID int64, input []submissionWriteRow) ([]supplypg.SubmissionRowInput, bool) {
	if len(input) == 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "至少需要一行模型", 400))
		return nil, false
	}
	// 未登记凭据不拦：原型允许先把申报行填好、后补凭据，真拦在发测试那一步。
	credential, err := h.svc.store.TestCredential(c.Request.Context(), supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return nil, false
	}
	snapshotURL, snapshotKey := "", ""
	if credential != nil {
		snapshotURL, snapshotKey = credential.BaseURL, credential.APIKey
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
		// 路线 A：按原样保存（与 A 侧正式配置口径一致）。
		storedKey := strings.TrimSpace(snapshotKey)
		if len(storedKey) > 512 {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "测试 Key 超出长度上限", 400))
			return nil, false
		}
		rowID, _ := strconv.ParseInt(strings.TrimSpace(row.ID), 10, 64)
		rows = append(rows, supplypg.SubmissionRowInput{
			ID: rowID, StandardModelID: modelID, SupplierModelName: strings.TrimSpace(row.InterfaceModelID),
			Coefficient: strings.TrimSpace(row.Coefficient), TestBaseURL: strings.TrimSpace(snapshotURL),
			SealedAPIKey: storedKey, ParticipateTest: row.ParticipateTest,
		})
	}
	return rows, true
}

// respondWithDTO 读出完整申报并回包（创建/保存草稿后前端需要 rows 才能继续渲染）。
func (h *supplierSubmissionHandler) respondWithDTO(c *gin.Context, supplierID, id int64) bool {
	ctx := c.Request.Context()
	header, err := h.svc.store.Get(ctx, supplierID, id)
	if err != nil {
		writeSubmissionError(c, err)
		return false
	}
	rows, err := h.svc.store.Rows(ctx, []int64{id})
	if err != nil {
		writeSubmissionError(c, err)
		return false
	}
	credential, err := h.svc.store.TestCredential(ctx, supplierID)
	if err != nil {
		writeSubmissionError(c, err)
		return false
	}
	response.Success(c, buildSubmissionDTO(*header, applyCredential(rows[id], credential)))
	return true
}

// applyCredential 用主体当前凭据重算每行的「结果是否失效」并同步展示用地址。
// cred 为 nil 时原样返回（退化成按行快照判定，历史数据不会被一片判红）。
func applyCredential(rows []supplypg.SubmissionRowRecord, cred *supplypg.SupplierTestCredential) []supplypg.SubmissionRowRecord {
	if cred == nil {
		return rows
	}
	for i := range rows {
		rows[i].ApplyCredential(cred)
	}
	return rows
}

// buildSubmissionDTO 组装前端契约；顺带推导测试状态与权限。
func buildSubmissionDTO(header supplypg.SubmissionHeader, rows []supplypg.SubmissionRowRecord) submissionDTO {
	submitted := header.Status == submissionPendingTest || header.Status == submissionPending || header.Status == submissionApproved
	dto := submissionDTO{
		ID: strconv.FormatInt(header.ID, 10), SubmissionNo: header.SubmissionNo,
		SubmitterName: header.SubmitterName, Region: header.Region,
		Kind: header.Kind, Status: header.Status,
		Revision: strconv.FormatInt(header.Revision, 10), Reason: header.Reason,
		Rows: make([]submissionRowDTO, 0, len(rows)), Permissions: permissionsFor(header.Status),
		CreatedAt: header.CreatedAt, UpdatedAt: header.UpdatedAt, SubmittedAt: header.SubmittedAt,
		ModelNames: make([]string, 0, len(rows)),
	}
	dto.RowCount = len(rows)
	for _, row := range rows {
		item := submissionRowDTO{
			ID: strconv.FormatInt(row.ID, 10), InterfaceModelID: row.SupplierModelName,
			StandardModelID: strconv.FormatInt(row.StandardModelID, 10), StandardModelName: row.StandardModelName,
			OfficialID: row.OfficialID, Coefficient: row.Coefficient, TestBaseURL: row.TestBaseURL,
			ParticipateTest: row.ParticipateTest,
		}
		dto.ModelNames = append(dto.ModelNames, row.StandardModelName)
		if row.ConnectivityStatus != nil {
			tested := isTerminalStatus(*row.ConnectivityStatus)
			passed := verdictOK(row.ConnectivityVerdict)
			item.ConnectivityJob = &submissionTestDTO{
				Category: "CONNECTIVITY", Status: *row.ConnectivityStatus, Tested: tested && !row.ConnectivityStale,
				Passed: passed, Verdict: deref(row.ConnectivityVerdict), Stale: row.ConnectivityStale, ConnectionMS: row.ConnectionMS,
			}
			item.ConnectivityTested, item.ConnectivityPassed = tested && !row.ConnectivityStale, passed && !row.ConnectivityStale
		}
		if row.StressStatus != nil {
			tested := isTerminalStatus(*row.StressStatus)
			passed := verdictOK(row.StressVerdict)
			item.StressJob = &submissionTestDTO{
				Category: "STRESS", Status: *row.StressStatus, Tested: tested && !row.StressStale,
				Passed: passed, Verdict: deref(row.StressVerdict), Stale: row.StressStale, RPS: row.RPS, TPS: row.TPS,
			}
			item.StressTested, item.StressPassed = tested && !row.StressStale, passed && !row.StressStale
		}
		item.RowStatus = deriveRowStatus(row, submitted)
		item.SupplementRequired = submitted && item.RowStatus != rowPassed
		if item.SupplementRequired {
			dto.PendingRowCount++
		}
		dto.Rows = append(dto.Rows, item)
	}
	return dto
}

// 行状态（派生，不落库）：异步回调会改变测试结果，落库会过期。
const (
	rowUntested   = "UNTESTED"
	rowRunning    = "RUNNING"
	rowStale      = "STALE"
	rowPassed     = "PASSED"
	rowFailed     = "FAILED"
	rowSupplement = "SUPPLEMENT"
)

func deriveRowStatus(row supplypg.SubmissionRowRecord, submitted bool) string {
	switch {
	case row.ConnectivityStale:
		return rowStale
	case row.ConnectivityStatus == nil:
		if submitted {
			return rowSupplement
		}
		return rowUntested
	case !isTerminalStatus(*row.ConnectivityStatus):
		return rowRunning
	case verdictOK(row.ConnectivityVerdict):
		return rowPassed
	default:
		if submitted {
			return rowSupplement
		}
		return rowFailed
	}
}

// permissionsFor 是申报单是否可编辑/提交/撤回的唯一判定点。
func permissionsFor(status string) submissionPermissionsDTO {
	switch status {
	case submissionDraft:
		return submissionPermissionsDTO{CanEdit: true, CanSubmit: true, CanWithdraw: true}
	case submissionPending:
		return submissionPermissionsDTO{CanWithdraw: true}
	case submissionRejected, submissionWithdrawn:
		return submissionPermissionsDTO{CanEdit: true, CanSubmit: true}
	case submissionPendingTest:
		// 等待测试补齐：可以继续测、可以再提交、也可以撤回。
		return submissionPermissionsDTO{CanEdit: true, CanSubmit: true, CanWithdraw: true}
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
	var appErr apperr.Error
	switch {
	case errors.Is(err, supply.ErrNotFound):
		response.Error(c, apperr.ErrNotFound)
	case errors.Is(err, supply.ErrRevisionConflict), errors.Is(err, supply.ErrInactive):
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "申报单已变化，请刷新后重试", 409))
	case errors.Is(err, supply.ErrTestInProgress):
		// 这里原来是 default → 500「系统错误」：供应商在测试跑着的时候勾一下压力测试，
		// 保存直接报系统错误，勾选永远存不进去。改成能看懂、能行动的 409。
		// 注意文案：前端靠「申报单已变化」区分版本冲突与测试占用，别改成同一句。
		response.Error(c, apperr.New(apperr.ErrIdempotency.Code, "测试进行中，请等测试结果回来后再修改受测目标", 409))
	case errors.Is(err, supply.ErrForbidden):
		response.Error(c, apperr.ErrForbidden)
	case errors.As(err, &appErr):
		// 门禁这类「带计数的业务拒绝」直接透出原文案，别被兜底成系统错误。
		response.Error(c, appErr)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// registerSupplierSubmissionRoutes 挂载供应商申报的正式接口。
func registerSupplierSubmissionRoutes(protected *gin.RouterGroup, deps Deps, idempotent gin.HandlerFunc) {
	handler := &supplierSubmissionHandler{svc: deps.SupplierSubmissions}
	// 发起测试/重测是「每次点击都是一次新命令」：语义键必须带上客户端的幂等键，
	// 否则 body 固定不变（categories 就那两个值），第二次点击之后全被当成重复请求回放，
	// 页面上表现为「再点一次没反应」。见 middleware.ClientKeyBizKey。
	perClick := idempotent
	if deps.IdemStore != nil {
		perClick = middleware.Idempotency(deps.IdemStore, middleware.ClientKeyBizKey)
	}
	group := protected.Group("/submissions")
	group.GET("", handler.list)
	// 静态段先于参数段注册，避免 /model-options 被 /:id 吞掉。
	group.GET("/model-options", handler.modelOptions)
	// 导入相关是静态段，必须排在 /:id 之前，否则会被当成 id。
	group.GET("/filter-options", handler.filterOptions)
	group.GET("/import/template", handler.importTemplate)
	group.POST("/import/parse", handler.importParse)
	group.GET("/:id", handler.detail)
	group.GET("/:id/rows/:rowId/test", handler.rowTest)
	group.POST("/:id/rows/:rowId/test", perClick, handler.rowTest)
	// 写接口一律挂幂等（红线）。
	group.POST("", idempotent, handler.create)
	group.PUT("/:id", idempotent, handler.update)
	group.POST("/:id/tests", perClick, handler.batchTests)
	group.POST("/:id/submit", idempotent, handler.submit)
	group.POST("/:id/withdraw", idempotent, handler.withdraw)
}
