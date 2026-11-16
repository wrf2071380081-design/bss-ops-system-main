// Package api 的 supplier_platform_service.go：供应商「生产平台服务商」关联接口（000069）。
//
// 这一步解决的是 2026-11 版本最核心的口径变化：供应商主体不再固定归属国内或海外。
// 一个主体可以在国内、海外各有一个生产平台服务商账号，两条记录靠人工登记 + 人工确认建立；
// 部署节点由每次申报自行选择，客户注册地不参与匹配。
//
// 三条纪律：
//  1. 登记/确认都是"事实记录"，**不触发**审批、不改模型或资源状态；
//  2. 同平台同标识全局唯一（存储层唯一索引兜底，冲突翻译成 409）；
//  3. 越权防护：supplier_id 只从路径取，且由 requireSupplyInternalStaff 组保护。
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

const (
	platformServiceNameMaxLen = 128
	platformServiceIDMaxLen   = 128
)

var (
	errPlatformServicePlatformInvalid = apperr.New(apperr.ErrInvalidParams.Code, "所属平台只能是国内或海外", 400)
	errPlatformServiceNameRequired    = apperr.New(apperr.ErrInvalidParams.Code, "请填写服务商名称", 400)
	errPlatformServiceNameTooLong     = apperr.New(apperr.ErrInvalidParams.Code, "服务商名称不能超过 128 个字符", 400)
	errPlatformServiceIDRequired      = apperr.New(apperr.ErrInvalidParams.Code, "请填写固定服务商标识", 400)
	errPlatformServiceIDTooLong       = apperr.New(apperr.ErrInvalidParams.Code, "固定服务商标识不能超过 128 个字符", 400)
)

// PlatformServiceDTO 对外的一行平台服务商记录。
type PlatformServiceDTO struct {
	ID          string `json:"id"`
	SupplierID  string `json:"supplier_id"`
	Platform    string `json:"platform"`
	ServiceName string `json:"service_name"`
	Identifier  string `json:"identifier"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at,omitempty"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
}

func buildPlatformServiceDTO(item supplypg.PlatformService) PlatformServiceDTO {
	dto := PlatformServiceDTO{
		ID:          strconv.FormatInt(item.ID, 10),
		SupplierID:  strconv.FormatInt(item.SupplierID, 10),
		Platform:    item.Platform,
		ServiceName: item.ServiceName,
		Identifier:  item.Identifier,
		Status:      item.Status,
	}
	if !item.CreatedAt.IsZero() {
		dto.CreatedAt = item.CreatedAt.Format(time.RFC3339)
	}
	if item.ConfirmedAt != nil {
		dto.ConfirmedAt = item.ConfirmedAt.Format(time.RFC3339)
	}
	return dto
}

// SupplierPlatformService 平台服务商关联的读写服务。
type SupplierPlatformService struct {
	store *supplypg.PlatformServiceStore
}

// NewSupplierPlatformService 构造。
func NewSupplierPlatformService(store *supplypg.PlatformServiceStore) *SupplierPlatformService {
	if store == nil {
		return nil
	}
	return &SupplierPlatformService{store: store}
}

// List 列出该主体的全部记录（含待核对，前端要能展示"确认关联"按钮）。
func (s *SupplierPlatformService) List(ctx context.Context, supplierID int64) ([]PlatformServiceDTO, error) {
	items, err := s.store.List(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformServiceDTO, 0, len(items))
	for _, item := range items {
		out = append(out, buildPlatformServiceDTO(item))
	}
	return out, nil
}

// Create 登记一条记录（默认待核对）。
func (s *SupplierPlatformService) Create(ctx context.Context, supplierID int64, platform, serviceName, identifier string, staffID int64) (*PlatformServiceDTO, error) {
	normalizedPlatform, trimmedName, trimmedID, err := validatePlatformServiceInput(platform, serviceName, identifier)
	if err != nil {
		return nil, err
	}
	item, err := s.store.Create(ctx, supplierID, normalizedPlatform, trimmedName, trimmedID, staffID)
	if err != nil {
		return nil, err
	}
	dto := buildPlatformServiceDTO(*item)
	return &dto, nil
}

// Confirm 确认关联（待核对 → 已关联）。
func (s *SupplierPlatformService) Confirm(ctx context.Context, supplierID, id, staffID int64) (*PlatformServiceDTO, error) {
	item, err := s.store.Confirm(ctx, supplierID, id, staffID)
	if err != nil {
		return nil, err
	}
	dto := buildPlatformServiceDTO(*item)
	return &dto, nil
}

// Remove 移除一条记录（软删）。
func (s *SupplierPlatformService) Remove(ctx context.Context, supplierID, id int64) error {
	return s.store.Remove(ctx, supplierID, id)
}

// validatePlatformServiceInput 归一化平台枚举并做长度校验。
func validatePlatformServiceInput(platform, serviceName, identifier string) (string, string, string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(platform))
	switch normalized {
	case "国内", "国内平台", "CN":
		normalized = "CN"
	case "海外", "海外平台", "OVERSEAS":
		normalized = "OVERSEAS"
	default:
		return "", "", "", errPlatformServicePlatformInvalid
	}
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return "", "", "", errPlatformServiceNameRequired
	}
	if len([]rune(name)) > platformServiceNameMaxLen {
		return "", "", "", errPlatformServiceNameTooLong
	}
	id := strings.TrimSpace(identifier)
	if id == "" {
		return "", "", "", errPlatformServiceIDRequired
	}
	if len([]rune(id)) > platformServiceIDMaxLen {
		return "", "", "", errPlatformServiceIDTooLong
	}
	return normalized, name, id, nil
}

func writePlatformServiceError(c *gin.Context, err error) {
	var appErr apperr.Error
	switch {
	case errors.Is(err, supplypg.ErrPlatformServiceTaken):
		response.Error(c, apperr.New(apperr.ErrConflict.Code, err.Error(), 409))
	case errors.Is(err, supplypg.ErrPlatformServiceNotFound):
		response.Error(c, apperr.New(apperr.ErrNotFound.Code, err.Error(), 404))
	case errors.As(err, &appErr):
		response.Error(c, appErr)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// ---- handler ----

type platformServiceHandler struct {
	svc *SupplierPlatformService
}

func (h *platformServiceHandler) supplierID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return 0, false
	}
	return id, true
}

func (h *platformServiceHandler) list(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	list, err := h.svc.List(c.Request.Context(), supplierID)
	if err != nil {
		writePlatformServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"list": list})
}

type createPlatformServiceRequest struct {
	Platform    string `json:"platform"`
	ServiceName string `json:"service_name"`
	Identifier  string `json:"identifier"`
}

func (h *platformServiceHandler) create(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	var req createPlatformServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	dto, err := h.svc.Create(c.Request.Context(), supplierID, req.Platform, req.ServiceName, req.Identifier, op.OperatorID)
	if err != nil {
		writePlatformServiceError(c, err)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

func (h *platformServiceHandler) confirm(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	serviceID, err := strconv.ParseInt(c.Param("serviceId"), 10, 64)
	if err != nil || serviceID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	dto, err := h.svc.Confirm(c.Request.Context(), supplierID, serviceID, op.OperatorID)
	if err != nil {
		writePlatformServiceError(c, err)
		return
	}
	middleware.SetIdempotencyResult(c, dto)
	response.Success(c, dto)
}

func (h *platformServiceHandler) remove(c *gin.Context) {
	supplierID, ok := h.supplierID(c)
	if !ok {
		return
	}
	serviceID, err := strconv.ParseInt(c.Param("serviceId"), 10, 64)
	if err != nil || serviceID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	if err := h.svc.Remove(c.Request.Context(), supplierID, serviceID); err != nil {
		writePlatformServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"id": strconv.FormatInt(serviceID, 10)})
}
