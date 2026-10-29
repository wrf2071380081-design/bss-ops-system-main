package repo

import (
	"context"
	"fmt"

	"model_bss/internal/domain/customer"
)

// affectedQuoteRow 是命中某条资源的报价单明细行。
type affectedQuoteRow struct {
	CustomerID   int64   `gorm:"column:customer_id"`
	CustomerName string  `gorm:"column:customer_name"`
	LevelCode    string  `gorm:"column:level_code"`
	OwnerSalesID int64   `gorm:"column:owner_sales_operator_id"`
	QuoteID      int64   `gorm:"column:quote_id"`
	VersionNo    int     `gorm:"column:version_no"`
	QuoteStatus  string  `gorm:"column:quote_status"`
	QuoteType    string  `gorm:"column:quote_type"`
	ValidUntil   *string `gorm:"column:valid_until"`
	PriceItemKey *string `gorm:"column:price_item_key"`
	UnitPrice    string  `gorm:"column:unit_price"`
	Currency     string  `gorm:"column:currency"`
}

// ListCustomersAffectedByResource 实现 customer.Store.ListCustomersAffectedByResource。
//
// 口径见 internal/domain/customer/affected.go：
//   - 只统计含该资源明细、且报价单不是 DRAFT 的行（DRAFT 还没给客户，不构成影响面）。
//   - 行级过滤沿用客户列表口径（applyCustomerOwnerScope）。
func (r *CustomerRepo) ListCustomersAffectedByResource(ctx context.Context, scope customer.OwnerScope, resourceID int64) (*customer.AffectedCustomerResult, error) {
	var rows []affectedQuoteRow
	tx := r.txOf(ctx).Table("customer_quote_item cqi").
		Joins("JOIN customer_quote cq ON cq.id = cqi.customer_quote_id").
		Joins("JOIN customer_profile cp ON cp.id = cq.customer_id").
		Joins("JOIN legal_subject ls ON ls.id = cp.subject_id")
	tx = applyCustomerOwnerScope(tx, scope)
	if err := tx.Where("cqi.resource_id = ? AND cq.status <> 'DRAFT'", resourceID).
		Select(`cp.id AS customer_id, ls.legal_name AS customer_name, cp.level_code AS level_code,
			cp.owner_sales_operator_id AS owner_sales_operator_id,
			cq.id AS quote_id, cq.version_no AS version_no, cq.status AS quote_status, cq.quote_type AS quote_type,
			cq.valid_until::text AS valid_until, cqi.price_item_key AS price_item_key,
			cqi.unit_price AS unit_price, cqi.currency AS currency`).
		Order("cp.id ASC, cq.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list customers affected by resource %d: %w", resourceID, err)
	}
	result := &customer.AffectedCustomerResult{ResourceID: resourceID, List: []customer.AffectedCustomer{}}
	index := map[int64]int{}
	for _, row := range rows {
		pos, ok := index[row.CustomerID]
		if !ok {
			result.List = append(result.List, customer.AffectedCustomer{
				CustomerID: row.CustomerID, CustomerName: row.CustomerName,
				LevelCode: row.LevelCode, OwnerSalesID: row.OwnerSalesID,
				Quotes: []customer.AffectedQuote{},
			})
			pos = len(result.List) - 1
			index[row.CustomerID] = pos
		}
		quote := customer.AffectedQuote{
			QuoteID: row.QuoteID, VersionNo: row.VersionNo, Status: row.QuoteStatus,
			QuoteType: row.QuoteType, UnitPrice: row.UnitPrice, Currency: row.Currency,
		}
		if row.ValidUntil != nil {
			quote.ValidUntil = *row.ValidUntil
		}
		if row.PriceItemKey != nil {
			quote.PriceItemKey = *row.PriceItemKey
		}
		result.List[pos].Quotes = append(result.List[pos].Quotes, quote)
	}
	result.Total = int64(len(result.List))
	return result, nil
}
