package api

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	tpdomain "model_bss/internal/domain/testplatform"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// TestJobStarter 是测试平台发起端口（domain/testplatform.Service 实现）。
type TestJobStarter interface {
	Start(context.Context, tpdomain.CreateCommand) (*tpdomain.StartResult, error)
}

// ResourceTestTarget is an authorized server-side platform target for retests.
// SealedAPIKey is a historical field name; its value is the platform Key, not
// the supplier Key, and is never returned to the browser.
type ResourceTestTarget struct {
	Model, BaseURL, SealedAPIKey, SupplierID, SupplierName string
	TargetFingerprint                                      string
}

// ResourceTestTargetLoader verifies the authenticated current handler, then
// loads the platform target without accepting a client-side identity or Key.
type ResourceTestTargetLoader interface {
	TestTarget(ctx context.Context, actorID, resourceID string) (*ResourceTestTarget, error)
}

// TestCallbackReceiver 是测试平台回调接收端口（infra/testplatform.Receiver 实现）。
// body 必须是原始字节：签名按原文计算。
type TestCallbackReceiver interface {
	Receive(ctx context.Context, body []byte, signature string) (duplicate bool, err error)
}

// registerOpenCallbacks 注册开放接口的扩展回调（B：测试平台对接）。
// 注意：这里是 /api/open 组，**不经过 AuthN**；回调必须自行验签（X-Signature + event_id 幂等）。
// 归属 B 的路径：
//   - /api/open/test-callback       测试平台终态回调
func registerOpenCallbacks(g *gin.RouterGroup, deps Deps) {
	// 回调是"轮询为主、回调为辅"里的加速件（Q11）：未配置 webhook secret 时不注册，
	// 轮询仍能把结果收回，接口保持不可见而不是返回 500。
	if deps.TestCallbacks != nil {
		g.POST("/test-callback", (&testCallbackHandler{receiver: deps.TestCallbacks}).receive)
	}
}

// testCallbackHandler 接收平台终态回调。
// 这是 /api/open 组，**没有 AuthN**，唯一凭据就是 X-Signature 签名。
type testCallbackHandler struct{ receiver TestCallbackReceiver }

func (h *testCallbackHandler) receive(c *gin.Context) {
	// 上限 1MiB：与平台请求体上限一致，避免内存被撑爆。
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil || len(body) == 0 {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体不可读", 400))
		return
	}
	duplicate, err := h.receiver.Receive(c.Request.Context(), body, c.GetHeader("X-Signature"))
	switch {
	case err == nil:
		// 平台只要求 2xx；重复投递也回 2xx（at-least-once 语义）。
		response.Success(c, gin.H{"duplicate": duplicate})
	case errors.Is(err, tpdomain.ErrSignature):
		response.Error(c, apperr.New(apperr.ErrUnauthorized.Code, "回调验签未通过", 401))
	case errors.Is(err, tpdomain.ErrInvalidCommand):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "回调内容不合法", 400))
	case errors.Is(err, tpdomain.ErrNotFound):
		response.Error(c, apperr.New(apperr.ErrNotFound.Code, "回调对应任务不存在", 404))
	default:
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "回调处理失败", 500))
	}
}

// registerSubmissionRoutes 注册 B（申报 / 办理 / 测试）的路由。
//
// 归属 B 的路径（新增一律写在本文件，不要改 router.go）：
//   - /api/supplier/submissions*       申报（多行/草稿/提交/撤回/版本/参与测试）
//   - /api/supplier/test-jobs*         测试触发 / 进度
//   - /api/internal/submissions*       办理台账 / 详情 / 登记接入
//   - /api/internal/flows*             办理流程与节点（办理人）
//
// 说明：旧的供应商报价 /quotes* 仍留在 router.go（遗留路由），迁移时再搬过来。
func registerSubmissionRoutes(g *gin.RouterGroup, protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "internal" && portal != "supplier" {
		return
	}
	// 供应商门户：申报正式接口（B-1）。写接口一律挂幂等（红线）。
	// 注意：原 supply_chain.go 里的简化版 POST /submissions 已删除，
	// 同路径重复注册会让 gin 启动 panic。
	if portal == "supplier" && deps.SupplierSubmissions != nil && deps.IdemStore != nil {
		registerSupplierSubmissionRoutes(protected, deps, middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey))
	}
	// 供应商门户：主体级测试凭据（000046，原型对齐）。
	// 放在申报同一段：凭据是申报测试的前置条件，改它会刷新未提交申报的快照与指纹。
	if portal == "supplier" && deps.SupplierCredentials != nil && deps.IdemStore != nil {
		registerSupplierCredentialRoutes(protected, deps, middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey))
	}
	// 供应商门户：模型管理列表（2026-10-28）。
	// 走 supplypg 的窄查询（resource + 最近报告），不碰 A 线 ReadStore
	// 那套「buyer_id 非空 + facts 对齐」的严格契约。
	if portal == "supplier" && deps.SupplierModels != nil {
		registerSupplierModelRoutes(protected, deps.SupplierModels)
	}
	// 一期只开放内部触发：供应商侧自助发起要等申报多行（M3）。
	if portal == "internal" {
		handler := &submissionTestHandler{starter: deps.TestJobs, targets: deps.ResourceTestTargets}
		protected.GET("/resources/:id/tests/options", handler.options)
		protected.POST("/resources/:id/tests",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			handler.start,
		)
	}
}

// startTestRequest 是内部发起一次测试的入参。
//
// Only category is accepted. Platform identity, model, URL and quota credentials
// come from the authorized server provider; supplier self-test stays separate.
type startTestRequest struct {
	Category string `json:"category"`
}

type submissionTestHandler struct {
	starter TestJobStarter
	targets ResourceTestTargetLoader
}

func (h *submissionTestHandler) start(c *gin.Context) {
	operator := middleware.OperatorFrom(c)
	if operator == nil || operator.PortalType != "INTERNAL" || operator.OperatorType != "STAFF" || operator.StaffID <= 0 {
		supplyHTTPError(c, supply.ErrForbidden)
		return
	}
	resourceID := c.Param("id")
	if _, err := planPositiveID(resourceID); err != nil {
		supplyHTTPError(c, err)
		return
	}
	var body startTestRequest
	if err := decodeSupplyRequest(c, &body, []string{"category"}); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	category := tpdomain.Category(strings.TrimSpace(body.Category))
	if !category.Valid() {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 baseline 或 stress", 400))
		return
	}
	if h.targets == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	target, err := h.targets.TestTarget(c.Request.Context(), strconv.FormatInt(operator.StaffID, 10), resourceID)
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	if !h.retestReady(target) {
		writeTestStartError(c, tpdomain.ErrNotConfigured)
		return
	}
	owner := target.SupplierName
	if owner == "" {
		owner = "resource-" + resourceID
	}
	// 平台幂等键与 HTTP 幂等键同源：同一笔业务重试必须命中同一个平台任务。
	result, err := h.starter.Start(c.Request.Context(), tpdomain.CreateCommand{
		ResourceID:        resourceID,
		ActorID:           strconv.FormatInt(operator.StaffID, 10),
		Category:          category,
		Owner:             owner,
		Target:            tpdomain.Target{Model: target.Model, BaseURL: target.BaseURL, APIKey: target.SealedAPIKey},
		TargetFingerprint: target.TargetFingerprint,
		Source:            "INTERNAL",
		IdempotencyKey:    strings.TrimSpace(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeTestStartError(c, err)
		return
	}
	response.Success(c, gin.H{
		"job_id":          result.LocalJobID,
		"external_job_id": result.ExternalID,
		"status":          result.Status,
	})
}

func (h *submissionTestHandler) retestReady(target *ResourceTestTarget) bool {
	return h != nil && h.starter != nil && target != nil && strings.TrimSpace(target.Model) != "" && strings.TrimSpace(target.BaseURL) != "" && strings.TrimSpace(target.SealedAPIKey) != "" && strings.TrimSpace(target.TargetFingerprint) != ""
}
func (h *submissionTestHandler) options(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.PortalType != "INTERNAL" || op.OperatorType != "STAFF" || op.StaffID <= 0 {
		supplyHTTPError(c, supply.ErrForbidden)
		return
	}
	if _, err := planPositiveID(c.Param("id")); err != nil {
		supplyHTTPError(c, err)
		return
	}
	if h.targets == nil {
		supplyHTTPError(c, supply.ErrDependencyUnavailable)
		return
	}
	target, err := h.targets.TestTarget(c.Request.Context(), strconv.FormatInt(op.StaffID, 10), c.Param("id"))
	if err != nil {
		supplyHTTPError(c, err)
		return
	}
	ready := h.retestReady(target)
	reason := ""
	if !ready {
		reason = "平台内部测试接口或Inspector连接尚未配置，请联系部署人员。"
	}
	response.Success(c, gin.H{"can_start": ready, "reason": reason, "categories": []string{"baseline", "stress"}})
}

// writeTestStartError 把测试平台的失败翻译成对前端有意义的状态码。
func writeTestStartError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, tpdomain.ErrInvalidCommand):
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "发起测试的参数不完整", 400))
	case errors.Is(err, tpdomain.ErrNotConfigured):
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台未配置", 503))
	default:
		response.Error(c, apperr.New(apperr.ErrSystem.Code, "测试平台调用失败，请稍后重试", 502))
	}
}
