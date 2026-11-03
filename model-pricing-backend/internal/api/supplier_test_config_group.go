// Package api 的 supplier_test_config_group.go：供应商准入测试**配置组**接口（000052）。
//
// 为什么要有这层：000046 把测试 URL/Key 收成「主体级一条」，而原型申报页是**多组**——
// 卡片式列出若干配置组（每套一条 URL/Key），模型表格里每行挂一组，提交门槛按组校验。
// 本文件只做 HTTP 边界：参数校验、越权防护（supplier_id 只从登录态取）、错误码翻译。
//
// 三条纪律：
//  1. **永不回显 Key**：DTO 只有 has_api_key；
//  2. 停用只改 enabled，不物理删除（历史上挂在该组上的行与报告要能继续读）；
//  3. 修改某一组只刷新**挂在该组上**的未提交行快照（多组最核心的价值：
//     改 A 组不该让 B 组的测试结果失效）。
package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 与迁移 000052 的列宽一致。
const (
	testConfigGroupNameMaxLen = 64
	testConfigGroupURLMaxLen  = 512
)

var (
	errTestConfigGroupNameRequired = apperr.New(apperr.ErrInvalidParams.Code, "请填写配置组名称", 400)
	errTestConfigGroupNameTooLong  = apperr.New(apperr.ErrInvalidParams.Code, "配置组名称不能超过 64 个字符", 400)
	errTestConfigGroupURLRequired  = apperr.New(apperr.ErrInvalidParams.Code, "请填写测试接口 URL", 400)
	errTestConfigGroupURLInvalid   = apperr.New(apperr.ErrInvalidParams.Code, "测试接口 URL 必须是 http/https 地址", 400)
	errTestConfigGroupURLTooLong   = apperr.New(apperr.ErrInvalidParams.Code, "测试接口 URL 超出长度上限", 400)
	errTestConfigGroupKeyTooLong   = apperr.New(apperr.ErrInvalidParams.Code, "测试 API Key 超出长度上限", 400)
)

// TestConfigGroupDTO 是对外的配置组（**不含 Key**）。
type TestConfigGroupDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	HasAPIKey bool   `json:"has_api_key"`
	Enabled   bool   `json:"enabled"`
	SortOrder int    `json:"sort_order"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func buildTestConfigGroupDTO(group supplypg.TestConfigGroup) TestConfigGroupDTO {
	dto := TestConfigGroupDTO{
		ID:        strconv.FormatInt(group.ID, 10),
		Name:      group.Name,
		BaseURL:   group.BaseURL,
		HasAPIKey: group.HasAPIKey,
		Enabled:   group.Enabled,
		SortOrder: group.SortOrder,
	}
	if !group.UpdatedAt.IsZero() {
		dto.UpdatedAt = group.UpdatedAt.Format(time.RFC3339)
	}
	return dto
}

// SupplierTestConfigGroupService 配置组的读写服务。
type SupplierTestConfigGroupService struct {
	store *supplypg.TestConfigGroupStore
}

// NewSupplierTestConfigGroupService 构造。
func NewSupplierTestConfigGroupService(store *supplypg.TestConfigGroupStore) *SupplierTestConfigGroupService {
	if store == nil {
		return nil
	}
	return &SupplierTestConfigGroupService{store: store}
}

// List 列出主体的全部组（含停用，前端要能展示并重新启用）。
func (s *SupplierTestConfigGroupService) List(ctx context.Context, supplierID int64) ([]TestConfigGroupDTO, error) {
	groups, err := s.store.List(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	out := make([]TestConfigGroupDTO, 0, len(groups))
	for _, group := range groups {
		out = append(out, buildTestConfigGroupDTO(group))
	}
	return out, nil
}

// Create 新建一组。名称重复 → 409。
func (s *SupplierTestConfigGroupService) Create(ctx context.Context, supplierID int64, name, baseURL string, apiKey *string, updatedBy int64) (*TestConfigGroupDTO, error) {
	trimmedName, trimmedURL, err := validateGroupInput(name, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	group, err := s.store.Create(ctx, supplierID, trimmedName, trimmedURL, apiKey, updatedBy)
	if err != nil {
		return nil, err
	}
	dto := buildTestConfigGroupDTO(*group)
	return &dto, nil
}

// Update 改名 / 换 URL / 换 Key / 启停；改完只刷新该组上的未提交行。
func (s *SupplierTestConfigGroupService) Update(ctx context.Context, supplierID, id int64, patch supplypg.TestConfigGroupPatch, updatedBy int64) (*TestConfigGroupDTO, error) {
	if patch.Name != nil || patch.BaseURL != nil || patch.APIKey != nil {
		name := ""
		if patch.Name != nil {
			name = *patch.Name
		}
		baseURL := ""
		if patch.BaseURL != nil {
			baseURL = *patch.BaseURL
		}
		trimmedName, trimmedURL, err := validateGroupInput(name, baseURL, patch.APIKey)
		if err != nil {
			return nil, err
		}
		if patch.Name != nil {
			patch.Name = &trimmedName
		}
		if patch.BaseURL != nil {
			patch.BaseURL = &trimmedURL
		}
	}
	group, err := s.store.Update(ctx, supplierID, id, patch, updatedBy)
	if err != nil {
		return nil, err
	}
	// 只有「会影响发单目标」的字段（URL/Key）才需要刷快照；改名不改指纹。
	if patch.BaseURL != nil || patch.APIKey != nil {
		if err := s.store.RefreshOpenRowSnapshotsByGroup(ctx, supplierID, id); err != nil {
			return nil, err
		}
	}
	dto := buildTestConfigGroupDTO(*group)
	return &dto, nil
}

// Disable 停用一组（不删）。
//
// 不拦「停用最后一个组」：停用是配置动作，「先停旧组再建新组」是正常操作序列；
// 兜底在提交门槛（没有可用组就提交不了）。
func (s *SupplierTestConfigGroupService) Disable(ctx context.Context, supplierID, id int64, updatedBy int64) (*TestConfigGroupDTO, error) {
	group, err := s.store.Disable(ctx, supplierID, id, updatedBy)
	if err != nil {
		return nil, err
	}
	dto := buildTestConfigGroupDTO(*group)
	return &dto, nil
}

// validateGroupInput 只校验**传了**的字段（Update 是局部更新）。
func validateGroupInput(name, baseURL string, apiKey *string) (string, string, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return "", "", errTestConfigGroupNameRequired
	}
	if len([]rune(trimmedName)) > testConfigGroupNameMaxLen {
		return "", "", errTestConfigGroupNameTooLong
	}
	trimmedURL := strings.TrimSpace(baseURL)
	if trimmedURL == "" {
		return "", "", errTestConfigGroupURLRequired
	}
	if len(trimmedURL) > testConfigGroupURLMaxLen {
		return "", "", errTestConfigGroupURLTooLong
	}
	// 复用主体凭据的同一把尺子（含 `demo` 放行）。
	if !validTestBaseURL(trimmedURL) {
		return "", "", errTestConfigGroupURLInvalid
	}
	if apiKey != nil && len(strings.TrimSpace(*apiKey)) > credentialKeyMaxLen {
		return "", "", errTestConfigGroupKeyTooLong
	}
	return trimmedName, trimmedURL, nil
}

// writeTestConfigGroupError 把仓储错误翻译成可读的 HTTP 错误。
func writeTestConfigGroupError(c *gin.Context, err error) {
	var appErr apperr.Error
	switch {
	case errors.Is(err, supplypg.ErrTestConfigGroupNameTaken):
		response.Error(c, apperr.New(apperr.ErrConflict.Code, err.Error(), 409))
	case errors.Is(err, supplypg.ErrTestConfigGroupNotFound):
		response.Error(c, apperr.New(apperr.ErrNotFound.Code, err.Error(), 404))
	case errors.As(err, &appErr):
		response.Error(c, appErr)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// ---- handler ----

// SupplierTestConfigGroupHandler 是配置组的 HTTP 入口。
type SupplierTestConfigGroupHandler struct {
	svc *SupplierTestConfigGroupService
}

// NewSupplierTestConfigGroupHandler 构造。
func NewSupplierTestConfigGroupHandler(svc *SupplierTestConfigGroupService) *SupplierTestConfigGroupHandler {
	return &SupplierTestConfigGroupHandler{svc: svc}
}

// list 列出本主体的配置组。
//
//	@Summary	供应商准入测试配置组（列表）
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/test-config-groups [get]
func (h *SupplierTestConfigGroupHandler) list(c *gin.Context) {
	supplierID, _, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), supplierID)
	if err != nil {
		writeTestConfigGroupError(c, err)
		return
	}
	response.Success(c, gin.H{"list": list})
}

type createTestConfigGroupRequest struct {
	Name    string  `json:"name"`
	BaseURL string  `json:"base_url"`
	APIKey  *string `json:"api_key"`
}

// create 新建一组。
//
//	@Summary	供应商准入测试配置组（新建）
//	@Tags		供应商门户
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/test-config-groups [post]
func (h *SupplierTestConfigGroupHandler) create(c *gin.Context) {
	supplierID, accountID, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	var req createTestConfigGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	dto, err := h.svc.Create(c.Request.Context(), supplierID, req.Name, req.BaseURL, req.APIKey, accountID)
	if err != nil {
		writeTestConfigGroupError(c, err)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

type patchTestConfigGroupRequest struct {
	Name    *string `json:"name"`
	BaseURL *string `json:"base_url"`
	// api_key 三态：不传=不改；传 ""=清空；传值=覆盖。
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

// update 局部更新一组（改名/换 URL/换 Key/启停）。
//
//	@Summary	供应商准入测试配置组（修改/启停）
//	@Tags		供应商门户
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Router		/api/supplier/test-config-groups/{id} [patch]
func (h *SupplierTestConfigGroupHandler) update(c *gin.Context) {
	supplierID, accountID, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	id, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var req patchTestConfigGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	if req.Name == nil && req.BaseURL == nil && req.APIKey == nil && req.Enabled == nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "没有需要修改的字段", 400))
		return
	}
	dto, err := h.svc.Update(c.Request.Context(), supplierID, id, supplypg.TestConfigGroupPatch{
		Name: req.Name, BaseURL: req.BaseURL, APIKey: req.APIKey, Enabled: req.Enabled,
	}, accountID)
	if err != nil {
		writeTestConfigGroupError(c, err)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

// disable 停用一组（不删除）。
//
//	@Summary	供应商准入测试配置组（停用）
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Router		/api/supplier/test-config-groups/{id}/disable [post]
func (h *SupplierTestConfigGroupHandler) disable(c *gin.Context) {
	supplierID, accountID, ok := supplierIDFromLogin(c)
	if !ok {
		return
	}
	id, err := positiveID(c.Param("id"))
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	dto, err := h.svc.Disable(c.Request.Context(), supplierID, id, accountID)
	if err != nil {
		writeTestConfigGroupError(c, err)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

// registerSupplierTestConfigGroupRoutes 挂载配置组路由。
//
// 鉴权与 /supplier/test-credential 一致：只走 AuthN（供应商门户零内部权限点），
// supplier_id 只从登录态解析；写接口挂幂等（红线 5）。
func registerSupplierTestConfigGroupRoutes(protected *gin.RouterGroup, deps Deps, idempotent gin.HandlerFunc) {
	if deps.SupplierTestConfigGroups == nil {
		return
	}
	handler := NewSupplierTestConfigGroupHandler(deps.SupplierTestConfigGroups)
	protected.GET("/test-config-groups", handler.list)
	protected.POST("/test-config-groups", idempotent, handler.create)
	protected.PATCH("/test-config-groups/:id", idempotent, handler.update)
	protected.POST("/test-config-groups/:id/disable", idempotent, handler.disable)
}
