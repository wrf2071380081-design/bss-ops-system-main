// Package supplier 的 portal_profile.go：供应商门户「主体档案」只读视图（批次 7a）。
//
// 与内部门户档案（profile.go，M3，/api/internal/suppliers）的区别：
//   - 本文件是 **/api/supplier/profile**（供应商门户），看的是**自己**的主体；
//   - 身份只从登录态解析（subject_operator.id → subject_id → supplier_profile.id），
//     绝不接受请求参数指定 supplier_id；
//   - 供应商门户**不挂 RequirePerm**（SUPPLIER 角色零内部权限点），隔离靠身份解析。
//
// ⚠️ 批次 7a 范围：**只做 GET**。PUT 本批不实现（见报告 §遗留）。
//
// ⚠️ 字段口径（批次 7a 实测核对结果，不要回退）：
// 前端原 DTO 里的 code / shortName / contactEmail / registeredAddressMasked /
// serviceRegions 在 supplier_profile、legal_subject、subject_operator 三张表里
// **都没有对应列**，全库也无其他来源。按任务书「不要在后端返回假字段」，
// 这些字段已从前端 DTO 删除，后端也不返回。
package supplier

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PortalProfile 是供应商门户「主体档案」响应体（camelCase，与前端 DTO 逐字一致）。
//
// 不含任何成本/毛利/平台售价键（DTO 层物理不含，无需运行时 fieldmask.Apply）。
type PortalProfile struct {
	ID                   string    `json:"id"`
	LegalName            string    `json:"legalName"`
	RegistrationNoMasked string    `json:"registrationNoMasked"`
	Status               string    `json:"status"`
	QualificationStatus  string    `json:"qualificationStatus"`
	ContactName          string    `json:"contactName"`
	ContactPhoneMasked   string    `json:"contactPhoneMasked"`
	SettlementCurrency   string    `json:"settlementCurrency"`
	PaymentTerms         string    `json:"paymentTerms"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

// PortalProfileRow 是仓储返回的单行（不含任何脱敏，脱敏在 service 层做）。
type PortalProfileRow struct {
	ID                 int64
	LegalName          string
	USCC               string
	Status             string
	QualStatus         string
	SettleType         string
	BillingCycle       int
	SettlementCurrency string
	ContactName        string
	ContactMobile      string
	UpdatedAt          time.Time
}

// PortalProfileStore 是门户档案只读数据源。
type PortalProfileStore interface {
	// GetPortalProfile 按 supplier_profile.id 取档案；不存在返回 (nil, nil)。
	GetPortalProfile(ctx context.Context, supplierID int64) (*PortalProfileRow, error)
}

// PortalProfileService 供应商门户档案只读服务。
type PortalProfileService struct {
	store PortalProfileStore
}

// NewPortalProfileService 构造。
func NewPortalProfileService(store PortalProfileStore) *PortalProfileService {
	return &PortalProfileService{store: store}
}

// Get 返回本主体的档案（已脱敏）。supplierID 必须来自登录态解析结果。
// 档案不存在时返回 (nil, nil)，由 handler 转 404。
func (s *PortalProfileService) Get(ctx context.Context, supplierID int64) (*PortalProfile, error) {
	row, err := s.store.GetPortalProfile(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &PortalProfile{
		ID:                   fmt.Sprintf("%d", row.ID),
		LegalName:            row.LegalName,
		RegistrationNoMasked: MaskRegistrationNo(row.USCC),
		Status:               row.Status,
		QualificationStatus:  row.QualStatus,
		ContactName:          row.ContactName,
		ContactPhoneMasked:   MaskPhone(row.ContactMobile),
		SettlementCurrency:   row.SettlementCurrency,
		PaymentTerms:         PaymentTermsOf(row.SettleType, row.BillingCycle),
		UpdatedAt:            row.UpdatedAt,
	}, nil
}

// ------------------------------------------------------------
// 脱敏与推导
//
// pkg/fieldmask 做的是「键剔除」（响应里没有这个 key），**做不了脱敏**。
// 脱敏必须单独实现，口径与前端 src/mocks/handlers/supplierAccount.ts 的
// maskPhone 保持一致（前 3 后 4 中间打星）。
// ------------------------------------------------------------

// MaskPhone 手机号脱敏：保留前 3 位与后 4 位，中间打星。
// 长度不足 7 位（异常数据）时整体返回 "***"——与前端 maskPhone 的兜底一致。
func MaskPhone(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 7 {
		if v == "" {
			return ""
		}
		return "***"
	}
	return v[:3] + "****" + v[len(v)-4:]
}

// MaskRegistrationNo 统一社会信用代码（USCC，18 位）脱敏：保留前 4 位与后 2 位，
// 中间按真实长度打星（18 位 → 4 + 12 星 + 2）。
//
// 与前端 mock 里写死的 `9144**********8X`（中间固定 10 星，共 16 字符）略有不同——
// mock 是手写的固定串，这里按真实长度生成，18 位 USCC 会得到 18 字符的结果。
// 长度不足 8 位时整体打星，避免把过短的异常值几乎原样透出。
func MaskRegistrationNo(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) < 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + strings.Repeat("*", len(v)-6) + v[len(v)-2:]
}

// PaymentTermsOf 把 supplier_profile 的 settle_type + billing_cycle 推导成可读账期。
//
// 这是**可验证推导**（两个字段都是真实列），不是编造：
//   - PREPAID → "预付"（billing_cycle 对预付无意义，不展示）
//   - MONTHLY → "月结 {billing_cycle} 天"
//   - 未知 settle_type → 原值透传（不猜）
func PaymentTermsOf(settleType string, billingCycle int) string {
	switch strings.ToUpper(strings.TrimSpace(settleType)) {
	case "PREPAID":
		return "预付"
	case "MONTHLY":
		return fmt.Sprintf("月结 %d 天", billingCycle)
	default:
		return settleType
	}
}
