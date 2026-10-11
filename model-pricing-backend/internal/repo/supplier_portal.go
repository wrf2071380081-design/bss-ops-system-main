// Package repo 的 supplier_portal.go：供应商门户档案只读查询（批次 7a）。
//
// 只做 GET，不含任何写操作。
//
// 行级隔离（与内部门户不同，别串）：
//   - 本查询的过滤键是 **supplier_profile.id**，该 id 由登录态解析得到
//     （subject_operator.id → subject_id → supplier_profile.id，见 domain/supplier/identity.go），
//     **不是**内部门户的 owner_procurement_operator_id / owner_sales_operator_id。
//   - 因此这里不需要也不应该再拼 owner 过滤——重复过滤反而会把身份解析的语义搞糊。
package repo

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"model_bss/internal/domain/supplier"
)

var _ supplier.PortalProfileStore = (*SupplierRepo)(nil)

// portalProfileRow 平铺行模型（CLAUDE.md 坑位表：内嵌带 TableName 的行 + 别名列扫不上）。
type portalProfileRow struct {
	ID                 int64     `gorm:"column:id"`
	LegalName          string    `gorm:"column:legal_name"`
	USCC               string    `gorm:"column:uscc"`
	Status             string    `gorm:"column:status"`
	QualStatus         string    `gorm:"column:qual_status"`
	SettleType         string    `gorm:"column:settle_type"`
	BillingCycle       int       `gorm:"column:billing_cycle"`
	SettlementCurrency string    `gorm:"column:settlement_currency"`
	ContactName        string    `gorm:"column:contact_name"`
	ContactMobile      string    `gorm:"column:contact_mobile"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

// portalProfileQuery 门户档案查询构造器（单独抽出以便 DryRun 断言，见 supplier_portal_test.go）。
//
// 联系人取本主体下 is_primary=true 且 status='ACTIVE' 的 subject_operator。
// 用 LEFT JOIN：没有主操作员时档案本身仍要能返回（联系人两列为空串），不能整行丢失。
func portalProfileQuery(tx *gorm.DB, supplierID int64) *gorm.DB {
	return tx.Table("supplier_profile sp").
		Select(`sp.id, ls.legal_name, ls.uscc, sp.status, sp.qual_status,
			sp.settle_type, sp.billing_cycle, sp.settlement_currency, sp.updated_at,
			COALESCE(so.name, '') AS contact_name, COALESCE(so.mobile, '') AS contact_mobile`).
		Joins("JOIN legal_subject ls ON ls.id = sp.subject_id").
		Joins(`LEFT JOIN subject_operator so ON so.subject_id = sp.subject_id
			AND so.is_primary = true AND so.status = 'ACTIVE'`).
		Where("sp.id = ?", supplierID)
}

// GetPortalProfile 取本主体的门户档案；不存在返回 (nil, nil)。
func (r *SupplierRepo) GetPortalProfile(ctx context.Context, supplierID int64) (*supplier.PortalProfileRow, error) {
	if supplierID <= 0 {
		return nil, nil
	}
	var rows []portalProfileRow
	err := portalProfileQuery(r.txOf(ctx), supplierID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("get portal profile: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	return &supplier.PortalProfileRow{
		ID:                 row.ID,
		LegalName:          row.LegalName,
		USCC:               row.USCC,
		Status:             row.Status,
		QualStatus:         row.QualStatus,
		SettleType:         row.SettleType,
		BillingCycle:       row.BillingCycle,
		SettlementCurrency: row.SettlementCurrency,
		ContactName:        row.ContactName,
		ContactMobile:      row.ContactMobile,
		UpdatedAt:          row.UpdatedAt,
	}, nil
}
