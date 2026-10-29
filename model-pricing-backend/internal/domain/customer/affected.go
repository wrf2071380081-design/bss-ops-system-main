// Package customer 的 affected.go：受影响客户查询。
//
// 场景：一条供应资源（供应商 × 标准模型 × 区域）改价或上下架时，谁的现有报价受影响。
//
// 口径（resource_id 落库后才有意义，迁移 000048）：
//   - 受影响客户 = 存在「包含该资源明细」的报价单的客户（customer_quote_item.resource_id）。
//   - 报价单状态：FORMAL / CONTRACT / EFFECTIVE / PENDING 等都算——只要客户手上有这张单，
//     改价就要通知；DRAFT 不算（销售还没给客户，不构成影响面）。
//   - 行级过滤：与客户列表同口径（applyCustomerOwnerScope），SELF 域只看自己负责的客户。
package customer

import (
	"context"
	"fmt"
)

// AffectedCustomer 是被某条供应资源影响到的客户及其报价单。
type AffectedCustomer struct {
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	LevelCode    string `json:"level_code"`
	OwnerSalesID int64  `json:"owner_sales_operator_id"`
	// Quotes 是该客户名下引用了这条资源的报价单。
	Quotes []AffectedQuote `json:"quotes"`
}

// AffectedQuote 是一张受影响的报价单。
type AffectedQuote struct {
	QuoteID    int64  `json:"quote_id"`
	VersionNo  int    `json:"version_no"`
	Status     string `json:"status"`
	QuoteType  string `json:"quote_type"`
	ValidUntil string `json:"valid_until,omitempty"`
	// PriceItemKey / UnitPrice 是命中该资源的价项与售价（不含成本与 floor）。
	PriceItemKey string `json:"price_item_key"`
	UnitPrice    string `json:"unit_price"`
	Currency     string `json:"currency"`
}

// AffectedCustomerResult 是受影响客户的查询结果。
type AffectedCustomerResult struct {
	ResourceID int64              `json:"resource_id"`
	List       []AffectedCustomer `json:"list"`
	Total      int64              `json:"total"`
}

// Store 增加的端口。
type AffectedCustomerStore interface {
	// ListCustomersAffectedByResource 按资源查受影响客户（含各自命中的报价单）。
	ListCustomersAffectedByResource(ctx context.Context, scope OwnerScope, resourceID int64) (*AffectedCustomerResult, error)
}

// ListCustomersAffectedByResource 查询被某条供应资源影响的客户。
//
// 数据域过滤在仓储层按 customer_profile.owner_sales_operator_id 收口，
// 与客户列表同口径——SELF 域只返回自己负责的客户。
func (s *Service) ListCustomersAffectedByResource(ctx context.Context, scope OwnerScope, resourceID int64) (*AffectedCustomerResult, error) {
	if s == nil || s.store == nil {
		return nil, ErrResourceStoreUnavailable
	}
	if resourceID <= 0 {
		return nil, fmt.Errorf("%w：resource_id 必须是正整数", ErrInvalidQuoteType)
	}
	return s.store.ListCustomersAffectedByResource(ctx, scope, resourceID)
}
