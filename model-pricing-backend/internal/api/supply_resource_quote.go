// Package api 的 supply_resource_quote.go：客户报价「资源口径」的供应域适配器。
//
// 职责：把供应域的资源事实（可报价判定、官方价项、采购系数）翻译成客户域要的形状。
// 边界：
//   - 可报价判定沿用供应域 ResourceQuoteReadiness（不在这里重算，避免两处口径漂移）。
//   - 采购成本 = 官方价项金额 × 采购系数，由客户域的纯函数算（这里只给原始事实）。
package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"model_bss/internal/domain/customer"
	"model_bss/internal/domain/supply"
)

// supplyResourceQuoteAdapter 实现 customer.ResourceQuoteStore。
type supplyResourceQuoteAdapter struct {
	reader *supply.ResourceReader
}

// NewSupplyResourceQuoteAdapter 构造。reader 为 nil 时资源口径报价不可用。
// 装配点：cmd/server/main.go（供应域依赖就绪后注入客户域服务）。
func NewSupplyResourceQuoteAdapter(reader *supply.ResourceReader) *supplyResourceQuoteAdapter {
	return &supplyResourceQuoteAdapter{reader: reader}
}

var _ customer.ResourceQuoteStore = (*supplyResourceQuoteAdapter)(nil)

// ListResourceQuoteCandidates 列出可报价的资源候选（供应域判定可报价的那些）。
func (a *supplyResourceQuoteAdapter) ListResourceQuoteCandidates(ctx context.Context, keyword string, page, size int) (*customer.ResourceQuoteCandidatePage, error) {
	if a == nil || a.reader == nil {
		return nil, customer.ErrResourceStoreUnavailable
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	// 供应域按 flat 视图取资源；只保留 quote_readiness.eligible 的（可报价）。
	// 分页在过滤之后做，保证 total 与 list 一致。
	result, err := a.reader.List(ctx, supply.ResourceListQuery{
		View: "flat", Keyword: strings.TrimSpace(keyword), Page: 1, Size: 100,
		SortBy: "id", Order: "asc", NullsLast: true, TieBreakBy: "id",
	})
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	all := make([]customer.ResourceQuoteCandidate, 0, len(result.Resources))
	for _, snapshot := range result.Resources {
		if snapshot == nil || !snapshot.QuoteReadiness.Eligible {
			continue
		}
		candidate, cerr := resourceQuoteCandidate(snapshot)
		if cerr != nil {
			return nil, cerr
		}
		all = append(all, candidate)
	}
	total := int64(len(all))
	start := (page - 1) * size
	if start > len(all) {
		start = len(all)
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	list := all[start:end]
	if list == nil {
		list = []customer.ResourceQuoteCandidate{}
	}
	return &customer.ResourceQuoteCandidatePage{List: list, Total: total, Page: page, Size: size}, nil
}

// LoadResourceQuoteItems 按 (resource_id, price_item_key) 给采购成本口径。
//
// 资源不可报价 → customer.ErrResourceNotQuotable；资源上没有该价项 → customer.ErrResourcePriceItem。
func (a *supplyResourceQuoteAdapter) LoadResourceQuoteItems(ctx context.Context, refs []customer.ResourceQuoteItemInput) (map[string]customer.ResourceQuoteItem, error) {
	out := map[string]customer.ResourceQuoteItem{}
	if a == nil || a.reader == nil {
		return nil, customer.ErrResourceStoreUnavailable
	}
	if len(refs) == 0 {
		return out, nil
	}
	// 同一资源只读一次快照（一条报价里同一资源可能有多个价项）。
	ids := make([]int64, 0, len(refs))
	seen := map[int64]bool{}
	for _, ref := range refs {
		if !seen[ref.ResourceID] {
			seen[ref.ResourceID] = true
			ids = append(ids, ref.ResourceID)
		}
	}
	for _, id := range ids {
		snapshot, err := a.reader.Get(ctx, strconv.FormatInt(id, 10), "")
		if err != nil {
			if errors.Is(err, supply.ErrNotFound) {
				return nil, customer.ErrResourceNotQuotable
			}
			return nil, fmt.Errorf("get resource %d: %w", id, err)
		}
		if snapshot == nil || !snapshot.QuoteReadiness.Eligible {
			return nil, customer.ErrResourceNotQuotable
		}
		items := map[string]supply.PriceItem{}
		for _, item := range snapshot.OfficialPriceItems {
			items[item.ItemKey] = item
		}
		for _, ref := range refs {
			if ref.ResourceID != id {
				continue
			}
			item, ok := items[ref.PriceItemKey]
			if !ok {
				return nil, customer.ErrResourcePriceItem
			}
			cost, cerr := customer.ResourceUnitCost(item.Amount, snapshot.Coefficient)
			if cerr != nil {
				return nil, fmt.Errorf("resource %d unit cost: %w", id, cerr)
			}
			key := fmt.Sprintf("%d:%s", ref.ResourceID, ref.PriceItemKey)
			out[key] = customer.ResourceQuoteItem{
				ResourceID: ref.ResourceID, PriceItemKey: ref.PriceItemKey,
				Amount: item.Amount, Coefficient: snapshot.Coefficient,
				UnitCost: cost.StringFixed(8), Currency: item.Currency,
				ModelName: snapshot.ModelName, SupplierName: snapshot.SupplierName,
			}
		}
	}
	return out, nil
}

// resourceQuoteCandidate 把资源快照翻成候选行，并带上各价项的采购成本。
func resourceQuoteCandidate(snapshot *supply.ResourceSnapshot) (customer.ResourceQuoteCandidate, error) {
	resourceID, err := strconv.ParseInt(snapshot.ID, 10, 64)
	if err != nil || resourceID <= 0 {
		return customer.ResourceQuoteCandidate{}, fmt.Errorf("资源 ID %q 不是正整数", snapshot.ID)
	}
	supplierID, err := strconv.ParseInt(snapshot.SupplierID, 10, 64)
	if err != nil || supplierID <= 0 {
		return customer.ResourceQuoteCandidate{}, fmt.Errorf("供应商 ID %q 不是正整数", snapshot.SupplierID)
	}
	candidate := customer.ResourceQuoteCandidate{
		ResourceID: resourceID, ModelName: snapshot.ModelName, OfficialID: snapshot.OfficialID,
		Company: snapshot.Company, ModelType: snapshot.ModelType,
		SupplierID: supplierID, SupplierName: snapshot.SupplierName,
		Region: snapshot.Region, SupplierModel: snapshot.SupplierModelID,
		Coefficient: snapshot.Coefficient, PriceItems: []customer.ResourcePriceItem{},
	}
	for _, item := range snapshot.OfficialPriceItems {
		cost, cerr := customer.ResourceUnitCost(item.Amount, snapshot.Coefficient)
		if cerr != nil {
			return customer.ResourceQuoteCandidate{}, fmt.Errorf("资源 %d 价项 %s 成本: %w", resourceID, item.ItemKey, cerr)
		}
		candidate.PriceItems = append(candidate.PriceItems, customer.ResourcePriceItem{
			ItemKey: item.ItemKey, Label: item.Label, TaskType: item.TaskType,
			Amount: item.Amount, Currency: item.Currency, UnitCode: item.UnitCode,
			UnitQuantity: item.UnitQuantity, UnitCost: cost.StringFixed(8),
		})
	}
	return candidate, nil
}
