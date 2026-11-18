// supply_manual_test.go：内部手工登记测试结果（临时方案，等测试平台可用后退役）。
//
// 为什么需要它：申报提交只做本地模拟连通检查（不产证据），准入门禁要真实 test_report，
// 中间没有可用入口，业务流在"提交审批"这一步必然卡住。这里补一个内部登记入口，
// 产出的报告 source=MANUAL_SIMULATED，与平台实测报告可区分。
package api

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

type manualTestRequest struct {
	Category     string `json:"category"`
	ConnectionMS string `json:"connection_ms"`
	RPS          string `json:"rps"`
	TPS          string `json:"tps"`
	Verdict      string `json:"verdict"`
	Note         string `json:"note"`
}

// NewManualTestStore 构造手工测试登记仓储（供 main 注入，失败返回 nil → 路由不注册）。
func NewManualTestStore(db *sql.DB) *supplypg.ManualTestStore {
	store, err := supplypg.NewManualTestStore(db, "public")
	if err != nil {
		return nil
	}
	return store
}

type submissionTestRecorder struct {
	store *supplypg.ManualTestStore
}

// record 给某个申报行登记一次测试结果（连通 / 压力 / 两者）。
//
//	@Summary	手工登记测试结果（内部，模拟用）
//	@Router		/api/internal/submission-rows/{rowId}/manual-test-report [post]
func (h *submissionTestRecorder) record(c *gin.Context) {
	if h == nil || h.store == nil {
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试登记未启用", 503))
		return
	}
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	rowID, err := strconv.ParseInt(c.Param("rowId"), 10, 64)
	if err != nil || rowID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var req manualTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	category := strings.ToUpper(strings.TrimSpace(req.Category))
	if category == "" {
		category = "BOTH"
	}
	if category != "CONNECTIVITY" && category != "STRESS" && category != "BOTH" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "测试类别只能是 CONNECTIVITY / STRESS / BOTH", 400))
		return
	}
	verdict := strings.ToLower(strings.TrimSpace(req.Verdict))
	if verdict == "" {
		verdict = "pass"
	}
	if verdict != "pass" && verdict != "fail" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "结论只能是 pass 或 fail", 400))
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请填写测试说明（例如：测试平台未接入，按线下实测结果登记）", 400))
		return
	}
	reportID, err := h.store.Record(c.Request.Context(), supplypg.ManualTestInput{
		RowID:        rowID,
		Category:     category,
		ConnectionMS: strings.TrimSpace(req.ConnectionMS),
		RPS:          strings.TrimSpace(req.RPS),
		TPS:          strings.TrimSpace(req.TPS),
		Verdict:      verdict,
		Note:         note,
		ActorID:      op.OperatorID,
		RequestID:    c.GetString("request_id"),
	})
	if err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, err.Error(), 400))
		return
	}
	result := gin.H{"report_id": reportID, "verdict": verdict, "source": "MANUAL_SIMULATED"}
	middleware.SetIdempotencyResult(c, result)
	response.Success(c, result)
}
