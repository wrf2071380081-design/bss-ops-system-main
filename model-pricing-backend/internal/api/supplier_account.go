// Package api 的 supplier_account.go：供应商门户「账户设置」（M2，2026-10-27）。
//
// 与 supplier_portal_profile.go（批次 7a，只读、打码、读旧模型 supplier_profile+legal_subject）的分工：
//   - 本文件服务于**页面「账户设置」**：读新模型 `supplier`（A/B 线共用那张表），字段明文、可写；
//   - 旧接口 `/supplier/profile` 保持原语义不动，避免已对接方返工。
//
// 鉴权：供应商门户**不挂 RequirePerm**（SUPPLIER 角色零内部权限点，挂了必然 403），只走 AuthN；
// 隔离靠「供应商身份只从登录态解析」——account.owner_type/owner_id → supplier.id
// （见 supplypg.AccountStore.ResolveSupplierID），绝不接受请求参数指定 supplier_id。
package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// 账户设置的业务错误（复用通用错误码，只改 message，前端直接展示）。
var (
	errAccountNameRequired  = apperr.New(apperr.ErrInvalidParams.Code, "企业资料必填", 400)
	errAccountFieldTooLong  = apperr.New(apperr.ErrInvalidParams.Code, "字段长度超出上限", 400)
	errAccountRegionInvalid = apperr.New(apperr.ErrInvalidParams.Code, "主体属性取值不合法", 400)
	errAccountRegionLocked  = apperr.New(apperr.ErrIdempotency.Code, "主体属性变更前须无资源、无申报", 409)
	errAccountNotFound      = apperr.New(apperr.ErrNotFound.Code, "账户档案不存在", 404)
	errLicenseMissing       = apperr.New(apperr.ErrInvalidParams.Code, "请选择企业证明文件", 400)
	errLicenseTooLarge      = apperr.New(apperr.ErrInvalidParams.Code, "企业证明不能超过 10 MB", 400)
	errLicenseType          = apperr.New(apperr.ErrInvalidParams.Code, "企业证明仅支持 pdf / png / jpg / jpeg / txt", 400)
)

// 字段上限与列宽一致（supplier.name varchar(128) 等），超长在应用层拦掉，别等数据库报错。
const (
	accountNameMaxLen    = 128
	accountRegNoMaxLen   = 64
	accountContactMaxLen = 64
	licenseMaxBytes      = 10 << 20
)

// SupplierAccountService 是账户设置的读/写服务。
type SupplierAccountService struct {
	store *supplypg.AccountStore
}

// NewSupplierAccountService 构造。
func NewSupplierAccountService(store *supplypg.AccountStore) *SupplierAccountService {
	return &SupplierAccountService{store: store}
}

// Resolve 把登录态解析成 supplier.id；解析不到返回 errAccountNotFound。
// handler 先解析一次，后面所有读写都只认这个 id，避免各方法重复解析出现不一致。
func (s *SupplierAccountService) Resolve(ctx context.Context, accountID, operatorID int64) (int64, error) {
	supplierID, err := s.store.ResolveSupplierID(ctx, accountID, operatorID)
	if err != nil {
		return 0, err
	}
	if supplierID <= 0 {
		return 0, errAccountNotFound
	}
	return supplierID, nil
}

// supplierLicenseDTO 企业证明。license 为 null 表示还没上传。
type supplierLicenseDTO struct {
	Name       string `json:"name"`
	FileRef    string `json:"fileRef"`
	UploadedAt string `json:"uploadedAt,omitempty"`
}

// supplierAccountDTO 是 GET/PUT 的统一响应体（camelCase，与前端 SupplierAccountDTO 逐字一致）。
type supplierAccountDTO struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	RegistrationNo   string              `json:"registrationNo"`
	Contact          string              `json:"contact"`
	Phone            string              `json:"phone"`
	Region           string              `json:"region"`
	RegionLocked     bool                `json:"regionLocked"`
	RegionLockReason string              `json:"regionLockReason,omitempty"`
	License          *supplierLicenseDTO `json:"license"`
	UpdatedAt        string              `json:"updatedAt"`
}

type supplierAccountUpdateRequest struct {
	Name           string `json:"name"`
	RegistrationNo string `json:"registrationNo"`
	Contact        string `json:"contact"`
	Region         string `json:"region"`
}

type supplierOperationLogDTO struct {
	ID         string         `json:"id"`
	Action     string         `json:"action"`
	Object     string         `json:"object"`
	Operator   string         `json:"operator"`
	OperatedAt string         `json:"operatedAt"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type supplierOperationLogPage struct {
	List  []supplierOperationLogDTO `json:"list"`
	Total int64                     `json:"total"`
	Page  int                       `json:"page"`
	Size  int                       `json:"size"`
}

// actionLabels 把 operation_log 里的动作码翻成原型文案。
// 未知码原样返回——宁可露出英文码，也不要让前端空白。
var actionLabels = map[string]string{
	"UPDATE_PROFILE": "维护企业资料",
	"UPDATE_LICENSE": "更换企业证明",
}

// 合法主体属性（对齐 supplier.region 的 CHECK 约束口径）。
var validAccountRegions = map[string]struct{}{"CN": {}, "OVERSEAS": {}}

// normalizeAccountUpdate 做入场校验与裁剪，返回可直接落库的结构。
// 纯函数，单测覆盖（不依赖数据库）。
func normalizeAccountUpdate(in supplierAccountUpdateRequest) (supplypg.AccountUpdate, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return supplypg.AccountUpdate{}, errAccountNameRequired
	}
	if utf8.RuneCountInString(name) > accountNameMaxLen {
		return supplypg.AccountUpdate{}, errAccountFieldTooLong
	}
	registrationNo := strings.TrimSpace(in.RegistrationNo)
	if utf8.RuneCountInString(registrationNo) > accountRegNoMaxLen {
		return supplypg.AccountUpdate{}, errAccountFieldTooLong
	}
	contact := strings.TrimSpace(in.Contact)
	if utf8.RuneCountInString(contact) > accountContactMaxLen {
		return supplypg.AccountUpdate{}, errAccountFieldTooLong
	}
	region := strings.ToUpper(strings.TrimSpace(in.Region))
	if _, ok := validAccountRegions[region]; !ok {
		return supplypg.AccountUpdate{}, errAccountRegionInvalid
	}
	return supplypg.AccountUpdate{
		Name: name, RegistrationNo: registrationNo, Contact: contact, Region: region,
	}, nil
}

// regionLockReason 生成 label 后缀文案（原型：「主体属性（有上架历史，已锁定）」）。
func regionLockReason(rec *supplypg.AccountRecord) string {
	if !supplypg.CurrentRegionLocked(rec) {
		return ""
	}
	if rec.EverAdmitted {
		return "有上架历史，已锁定"
	}
	return "已有资源或申报，已锁定"
}

func buildSupplierAccountDTO(rec *supplypg.AccountRecord) supplierAccountDTO {
	dto := supplierAccountDTO{
		ID:               fmt.Sprintf("%d", rec.ID),
		Name:             rec.Name,
		RegistrationNo:   rec.RegistrationNo,
		Contact:          rec.Contact,
		Phone:            rec.Phone,
		Region:           rec.Region,
		RegionLocked:     supplypg.CurrentRegionLocked(rec),
		RegionLockReason: regionLockReason(rec),
		UpdatedAt:        rec.UpdatedAt.Format(time.RFC3339),
	}
	if rec.LicenseName != "" || rec.LicenseRef != "" {
		license := &supplierLicenseDTO{Name: rec.LicenseName, FileRef: rec.LicenseRef}
		if rec.LicenseUploadedAt != nil {
			license.UploadedAt = rec.LicenseUploadedAt.Format(time.RFC3339)
		}
		dto.License = license
	}
	return dto
}

// Get 读账户资料。
func (s *SupplierAccountService) Get(ctx context.Context, supplierID int64) (*supplierAccountDTO, error) {
	rec, err := s.store.Load(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	dto := buildSupplierAccountDTO(rec)
	return &dto, nil
}

// Update 保存账户资料。锁定校验在仓储事务内完成（store.ErrRegionLocked）。
func (s *SupplierAccountService) Update(ctx context.Context, supplierID int64, in supplierAccountUpdateRequest, requestID string) (*supplierAccountDTO, error) {
	update, err := normalizeAccountUpdate(in)
	if err != nil {
		return nil, err
	}
	// 操作人取对接负责人：供应商侧没有 operation_log 的 staff 外键，只能落名字快照。
	rec, err := s.store.Update(ctx, supplierID, update, update.Contact, requestID)
	if err != nil {
		return nil, err
	}
	dto := buildSupplierAccountDTO(rec)
	return &dto, nil
}

// Logs 分页读账户留痕。
func (s *SupplierAccountService) Logs(ctx context.Context, supplierID int64, page, size int) (*supplierOperationLogPage, error) {
	rec, err := s.store.Load(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.store.ListLogs(ctx, supplierID, page, size)
	if err != nil {
		return nil, err
	}
	list := make([]supplierOperationLogDTO, 0, len(rows))
	for _, row := range rows {
		label := actionLabels[row.Action]
		if label == "" {
			label = row.Action
		}
		list = append(list, supplierOperationLogDTO{
			ID:         fmt.Sprintf("%d", row.ID),
			Action:     label,
			Object:     rec.Name,
			Operator:   row.Operator,
			OperatedAt: row.CreatedAt.Format(time.RFC3339),
			Payload:    row.Payload,
		})
	}
	return &supplierOperationLogPage{List: list, Total: total, Page: page, Size: size}, nil
}

// SaveLicense 登记企业证明（文件已由 handler 落盘）。
func (s *SupplierAccountService) SaveLicense(ctx context.Context, supplierID int64, name, fileRef, operatorName, requestID string) (*supplierLicenseDTO, error) {
	if err := s.store.SaveLicense(ctx, supplierID, name, fileRef, operatorName, requestID); err != nil {
		return nil, err
	}
	rec, err := s.store.Load(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	result := &supplierLicenseDTO{Name: rec.LicenseName, FileRef: rec.LicenseRef}
	if rec.LicenseUploadedAt != nil {
		result.UploadedAt = rec.LicenseUploadedAt.Format(time.RFC3339)
	}
	return result, nil
}

// ---- handler ----

// SupplierAccountHandler 是账户设置的 HTTP 入口。
type SupplierAccountHandler struct {
	svc       *SupplierAccountService
	uploadDir string
}

// NewSupplierAccountHandler 构造。uploadDir 为企业证明落盘根目录。
func NewSupplierAccountHandler(svc *SupplierAccountService, uploadDir string) *SupplierAccountHandler {
	return &SupplierAccountHandler{svc: svc, uploadDir: uploadDir}
}

// supplierOperator 取出供应商登录态；非供应商身份一律 401（与 7a 的 /profile 同口径）。
func supplierOperator(c *gin.Context) (*middleware.Operator, bool) {
	op := middleware.OperatorFrom(c)
	if op == nil || op.OperatorType != "SUPPLIER" || op.PortalType != "SUPPLIER" {
		response.Error(c, apperr.ErrUnauthorized)
		return nil, false
	}
	return op, true
}

// resolveSupplier 解析本请求对应的 supplier.id；失败时已写好响应，返回 ok=false。
func (h *SupplierAccountHandler) resolveSupplier(c *gin.Context, op *middleware.Operator) (int64, bool) {
	supplierID, err := h.svc.Resolve(c.Request.Context(), op.AccountID, op.OperatorID)
	if err != nil {
		writeAccountError(c, err)
		return 0, false
	}
	return supplierID, true
}

// writeAccountError 把领域/仓储错误翻译成对外错误码。
func writeAccountError(c *gin.Context, err error) {
	var appErr apperr.Error
	switch {
	case errors.Is(err, supplypg.ErrRegionLocked):
		response.Error(c, errAccountRegionLocked)
	case errors.Is(err, supplypg.ErrAccountMissing), errors.Is(err, supplypg.ErrAccountUnresolved):
		response.Error(c, errAccountNotFound)
	case errors.As(err, &appErr):
		response.Error(c, appErr)
	default:
		response.Error(c, apperr.ErrSystem)
	}
}

// get 读账户资料。
//
//	@Summary	供应商账户设置（读）
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/account [get]
func (h *SupplierAccountHandler) get(c *gin.Context) {
	op, ok := supplierOperator(c)
	if !ok {
		return
	}
	supplierID, ok := h.resolveSupplier(c, op)
	if !ok {
		return
	}
	dto, err := h.svc.Get(c.Request.Context(), supplierID)
	if err != nil {
		writeAccountError(c, err)
		return
	}
	response.Success(c, dto)
}

// update 保存账户资料（PUT）。
//
//	@Summary	供应商账户设置（写）
//	@Tags		供应商门户
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/account [put]
func (h *SupplierAccountHandler) update(c *gin.Context) {
	op, ok := supplierOperator(c)
	if !ok {
		return
	}
	var body supplierAccountUpdateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, errAccountNameRequired)
		return
	}
	supplierID, ok := h.resolveSupplier(c, op)
	if !ok {
		return
	}
	dto, err := h.svc.Update(c.Request.Context(), supplierID, body, c.GetString("request_id"))
	if err != nil {
		writeAccountError(c, err)
		return
	}
	response.Success(c, dto)
}

// logs 分页读操作记录。
//
//	@Summary	供应商账户操作记录
//	@Tags		供应商门户
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	api.APIResponse
//	@Router		/api/supplier/account/operation-logs [get]
func (h *SupplierAccountHandler) logs(c *gin.Context) {
	op, ok := supplierOperator(c)
	if !ok {
		return
	}
	page, err := positiveIntQuery(c, "page", 1, 10000)
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	size, err := positiveIntQuery(c, "size", 20, 100)
	if err != nil {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	supplierID, ok := h.resolveSupplier(c, op)
	if !ok {
		return
	}
	result, err := h.svc.Logs(c.Request.Context(), supplierID, page, size)
	if err != nil {
		writeAccountError(c, err)
		return
	}
	response.Success(c, result)
}

// 企业证明允许的扩展名（与前端 accept 保持一致）。
var licenseExts = map[string]struct{}{".pdf": {}, ".png": {}, ".jpg": {}, ".jpeg": {}, ".txt": {}}

// uploadLicense 上传/替换企业证明（multipart 字段名 file）。
//
// 落盘路径：<uploadDir>/supplier-license/<supplierID>/<时间戳><ext>，库里只存相对路径。
// 不做幂等：文件内容不适合作为幂等签名，重复上传按「替换」语义处理（旧记录保留，取最新一条）。
func (h *SupplierAccountHandler) uploadLicense(c *gin.Context) {
	op, ok := supplierOperator(c)
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		response.Error(c, errLicenseMissing)
		return
	}
	if fileHeader.Size > licenseMaxBytes {
		response.Error(c, errLicenseTooLarge)
		return
	}
	ext := strings.ToLower(path.Ext(fileHeader.Filename))
	if _, allowed := licenseExts[ext]; !allowed {
		response.Error(c, errLicenseType)
		return
	}
	supplierID, ok := h.resolveSupplier(c, op)
	if !ok {
		return
	}

	dir := path.Join("supplier-license", fmt.Sprintf("%d", supplierID))
	absDir := path.Join(h.uploadDir, dir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	stored := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	if err := c.SaveUploadedFile(fileHeader, path.Join(absDir, stored)); err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	fileRef := path.Join(dir, stored)
	operatorName := strings.TrimSpace(c.PostForm("operator_name"))
	result, err := h.svc.SaveLicense(c.Request.Context(), supplierID, fileHeader.Filename, fileRef, operatorName, c.GetString("request_id"))
	if err != nil {
		// 落盘成功但入库失败：删掉孤儿文件，别把存储变成垃圾场。
		_ = os.Remove(path.Join(absDir, stored))
		writeAccountError(c, err)
		return
	}
	response.Success(c, result)
}

// positiveIntQuery 读正整数查询参数；缺省用 def，越界直接判非法（不静默截断）。
func positiveIntQuery(c *gin.Context, key string, def, max int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 1 || value > max {
		return 0, fmt.Errorf("%s out of range", key)
	}
	return value, nil
}
