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

// ResourceTestTarget 是资源自身登记的测试目标（内部复测 T-10 用）。
type ResourceTestTarget struct {
	Model, BaseURL, SealedAPIKey, SupplierID, SupplierName string
}

// ResourceTestTargetLoader 读取资源测试目标。
type ResourceTestTargetLoader interface {
	TestTarget(ctx context.Context, resourceID string) (*ResourceTestTarget, error)
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
	// 一期只开放内部触发：供应商侧自助发起要等申报多行（M3）。
	if portal == "internal" && deps.TestJobs != nil && deps.IdemStore != nil {
		handler := &submissionTestHandler{starter: deps.TestJobs, targets: deps.ResourceTestTargets}
		protected.POST("/resources/:id/tests",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			handler.start,
		)
	}
}

// startTestRequest 是内部发起一次测试的入参。
//
// 一期由调用方直接给模型与凭据，用于联调真实链路；
// M3 改为从资源/供应商主体取共用测试凭据（Q5），届时本结构只留 category 与 params。
type startTestRequest struct {
	Category string         `json:"category"`
	Model    string         `json:"model"`
	BaseURL  string         `json:"base_url"`
	Protocol string         `json:"protocol"`
	APIKey   string         `json:"api_key"`
	Owner    string         `json:"owner"`
	Params   map[string]any `json:"params"`
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
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	category := tpdomain.Category(strings.TrimSpace(body.Category))
	if !category.Valid() {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "category 只支持 baseline 或 stress", 400))
		return
	}
	model, baseURL, apiKey := strings.TrimSpace(body.Model), strings.TrimSpace(body.BaseURL), strings.TrimSpace(body.APIKey)
	supplierName := ""
	// 内部复测（T-10）：不填目标就用资源上登记的那一套，避免人工重复录入。
	if model == "" || baseURL == "" {
		if h.targets == nil {
			response.Error(c, apperr.New(apperr.ErrSystem.Code, "资源测试配置不可用", 503))
			return
		}
		target, err := h.targets.TestTarget(c.Request.Context(), resourceID)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		if model == "" {
			model = target.Model
		}
		if baseURL == "" {
			baseURL = target.BaseURL
		}
		supplierName = target.SupplierName
		if apiKey == "" {
			if baseURL == "demo" {
				apiKey = "demo-key"
			} else {
				// 路线 A：资源上存的就是原样 Key。
				if strings.TrimSpace(target.SealedAPIKey) == "" {
					response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "该资源未保存测试 Key", 400))
					return
				}
				apiKey = target.SealedAPIKey
			}
		}
	}
	if model == "" || baseURL == "" {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "model 与 base_url 必填", 400))
		return
	}
	owner := strings.TrimSpace(body.Owner)
	if owner == "" {
		owner = supplierName
	}
	if owner == "" {
		owner = "resource-" + resourceID
	}
	// 平台幂等键与 HTTP 幂等键同源：同一笔业务重试必须命中同一个平台任务。
	result, err := h.starter.Start(c.Request.Context(), tpdomain.CreateCommand{
		ResourceID:     resourceID,
		ActorID:        strconv.FormatInt(operator.StaffID, 10),
		Category:       category,
		Owner:          owner,
		Target:         tpdomain.Target{Model: model, BaseURL: baseURL, Protocol: body.Protocol, APIKey: apiKey},
		Params:         body.Params,
		IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")),
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
