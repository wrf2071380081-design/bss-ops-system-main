// Package api 的 supplier_credential.go：供应商**主体级**准入测试凭据（000044）。
//
// 原型（2026-10-27 版）把测试 URL / Key 收在申报页顶部，一句
// 「此 URL／Key 仅由供应商本人维护，用于本主体准入测试、供申报模型共用；
// 不用于平台运营测试。」——所以这是**主体资源**，不是申报行字段，
// 单独一条 GET/PUT 比塞进 submission 的写接口更贴合语义，也让「改凭据」
// 有一个明确的副作用点（刷新未提交申报的快照与指纹）。
//
// 鉴权：供应商门户不挂 RequirePerm（SUPPLIER 角色零内部权限点），只走 AuthN；
// supplier_id 只从登录态解析，绝不接受请求参数指定。
package api

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

var (
	errCredentialBaseURLRequired = apperr.New(apperr.ErrInvalidParams.Code, "请填写测试接口 URL", 400)
	errCredentialBaseURLInvalid  = apperr.New(apperr.ErrInvalidParams.Code, "测试接口 URL 必须是 http/https 地址", 400)
	errCredentialKeyTooLong      = apperr.New(apperr.ErrInvalidParams.Code, "测试 API Key 超出长度上限", 400)
)

// 与 submission_row.test_api_key_enc 列宽一致（varchar(512)）。
const credentialKeyMaxLen = 512

// SupplierCredentialService 是主体级测试凭据的读写服务。
type SupplierCredentialService struct {
	store *supplypg.SupplierCredentialStore
}

// NewSupplierCredentialService 构造服务。
func NewSupplierCredentialService(store *supplypg.SupplierCredentialStore) *SupplierCredentialService {
	return &SupplierCredentialService{store: store}
}

// supplierTestCredentialDTO 是 GET/PUT 的统一响应体。
//
// 字段名与前端 supplierTestCredential.ts 逐字一致；**永远不回显 Key**，
// 只给 has_api_key，避免前端把凭据留在内存里（原型同样只显示占位符）。
type supplierTestCredentialDTO struct {
	BaseURL    string `json:"base_url"`
	HasAPIKey  bool   `json:"has_api_key"`
	Configured bool   `json:"configured"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

func buildCredentialDTO(record *supplypg.SupplierTestCredential) supplierTestCredentialDTO {
	if record == nil {
		return supplierTestCredentialDTO{}
	}
	dto := supplierTestCredentialDTO{
		BaseURL:    record.BaseURL,
		HasAPIKey:  strings.TrimSpace(record.APIKey) != "",
		Configured: strings.TrimSpace(record.BaseURL) != "",
	}
	if !record.UpdatedAt.IsZero() {
		dto.UpdatedAt = record.UpdatedAt.Format(time.RFC3339)
	}
	return dto
}

// Get 读取本主体凭据；未登记时返回空状态（不是 404——前端首屏就要渲染表单）。
func (s *SupplierCredentialService) Get(ctx context.Context, supplierID int64) (*supplierTestCredentialDTO, error) {
	record, err := s.store.Get(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	dto := buildCredentialDTO(record)
	return &dto, nil
}

// Save 写入本主体凭据。
//
// 副作用：保存成功后把未提交申报行的快照列与指纹刷成新值——
// 否则供应商改了地址，草稿里还挂着旧指纹，测试结果的「失效」判定会失真。
func (s *SupplierCredentialService) Save(ctx context.Context, supplierID int64, baseURL string, apiKey *string, accountID int64) (*supplierTestCredentialDTO, error) {
	trimmedURL := strings.TrimSpace(baseURL)
	if trimmedURL == "" {
		return nil, errCredentialBaseURLRequired
	}
	if !validTestBaseURL(trimmedURL) {
		return nil, errCredentialBaseURLInvalid
	}
	if apiKey != nil && len(strings.TrimSpace(*apiKey)) > credentialKeyMaxLen {
		return nil, errCredentialKeyTooLong
	}
	record, err := s.store.Upsert(ctx, supplierID, trimmedURL, apiKey, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.store.RefreshOpenRowSnapshots(ctx, supplierID); err != nil {
		return nil, err
	}
	dto := buildCredentialDTO(record)
	return &dto, nil
}

// validTestBaseURL 只接受绝对 http/https 地址。
//
// 与前端保持同一口径；区别是后端放行 `demo`（平台内置演示模型，
// 不连真实服务，联调与演示环境都在用）。
func validTestBaseURL(raw string) bool {
	if strings.EqualFold(raw, "demo") {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

// ---- handler ----

// SupplierCredentialHandler 是主体级测试凭据的 HTTP 入口。
type SupplierCredentialHandler struct {
	svc *SupplierCredentialService
}

// NewSupplierCredentialHandler 构造。
func NewSupplierCredentialHandler(svc *SupplierCredentialService) *SupplierCredentialHandler {
	return &SupplierCredentialHandler{svc: svc}
}

// supplierIDFromLogin 解析本请求对应的 supplier.id；失败时已写好响应。
func supplierIDFromLogin(c *gin.Context) (int64, int64, bool) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.OperatorType != "SUPPLIER" || op.OperatorID <= 0 {
		response.Error(c, apperr.ErrUnauthorized)
		return 0, 0, false
	}
	return op.OperatorID, op.AccountID, true
}

func writeCredentialError(c *gin.Context, err error) {
	var appErr apperr.Error
	switch {
	case errors.As(err, &appErr):
		response.Error(c, appErr)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// get 读主体测试凭据。
//
//	@Summary	供应商主体测试凭据（读）
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/test-credential [get]
func (h *SupplierCredentialHandler) get(c *gin.Context) {
	supplierID, _, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	dto, err := h.svc.Get(c.Request.Context(), supplierID)
	if err != nil {
		writeCredentialError(c, err)
		return
	}
	response.Success(c, dto)
}

// registerSupplierCredentialRoutes 挂载主体级测试凭据的正式接口。
//
// 路径独立于 /submissions：凭据是主体资源，生命周期比单张申报长。
// 写接口挂幂等（红线）；两个接口都不挂 RequirePerm（供应商门户零内部权限点）。
func registerSupplierCredentialRoutes(protected *gin.RouterGroup, deps Deps, idempotent gin.HandlerFunc) {
	handler := NewSupplierCredentialHandler(deps.SupplierCredentials)
	protected.GET("/test-credential", handler.get)
	protected.PUT("/test-credential", idempotent, handler.save)
}

// saveRequest 是 PUT 的入参。
//
// api_key 不传（或传 null）= 留空保留已保存值；传空串 = 显式清空。
type saveCredentialRequest struct {
	BaseURL string  `json:"base_url"`
	APIKey  *string `json:"api_key"`
}

// save 保存主体测试凭据（PUT，幂等）。
//
//	@Summary	供应商主体测试凭据（写）
//	@Tags		供应商门户
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/test-credential [put]
func (h *SupplierCredentialHandler) save(c *gin.Context) {
	supplierID, accountID, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	var body saveCredentialRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	dto, err := h.svc.Save(c.Request.Context(), supplierID, body.BaseURL, body.APIKey, accountID)
	if err != nil {
		writeCredentialError(c, err)
		return
	}
	response.Success(c, dto)
}
