package customer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// fakeResourceStore 是资源口径报价的最小仓储替身。
type fakeResourceStore struct {
	fakeStore
	bases map[string]ResourceQuoteItem
	items map[string]ResourceQuoteItem
}

func (f *fakeResourceStore) ListResourceQuoteCandidates(_ context.Context, _, _ string, page, size int) (*ResourceQuoteCandidatePage, error) {
	return &ResourceQuoteCandidatePage{List: []ResourceQuoteCandidate{}, Total: 0, Page: page, Size: size}, nil
}
func (f *fakeResourceStore) LoadResourceQuoteItems(_ context.Context, refs []ResourceQuoteItemInput) (map[string]ResourceQuoteItem, error) {
	out := map[string]ResourceQuoteItem{}
	for _, ref := range refs {
		key := resourceQuoteKey(ref.ResourceID, ref.PriceItemKey)
		item, ok := f.bases[key]
		if !ok {
			return nil, ErrResourceNotQuotable
		}
		out[key] = item
	}
	return out, nil
}

func resourceStoreFixture() *fakeResourceStore {
	return &fakeResourceStore{
		fakeStore: fakeStore{
			scopeFound: true, scopeInScope: true,
			levelCode: "GLOBAL", ownerSalesID: 51,
			margin: decimal.RequireFromString("0.15"),
			genRes: &GeneratedQuote{ID: 19, CustomerID: 3, VersionNo: 1, Status: QuoteStatusDraft, QuoteType: QuoteTypeResource},
		},
		bases: map[string]ResourceQuoteItem{
			"16:image_generation": {
				ResourceID: 16, PriceItemKey: "image_generation",
				Amount: "0.04000000", Coefficient: "0.6800", UnitCost: "0.02720000",
				Currency: "USD", ModelName: "GPT Image 1", SupplierName: "青禾云",
				Region: "CN",
			},
		},
	}
}

// TestGenerateResourceQuoteWritesResourceItems 锁住资源口径出单：
// 落库明细带 resource_id + price_item_key（sku_id 留空），floor 按资源成本算。
func TestGenerateResourceQuoteWritesResourceItems(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	res, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items:      []ResourceQuoteItemInput{{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.04000000", Coefficient: "1"}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.QuoteType != QuoteTypeResource {
		t.Fatalf("报价类型 = %+v, want RESOURCE", res)
	}
	if len(store.lastGenInput.Items) != 1 {
		t.Fatalf("落库明细数 = %d, want 1", len(store.lastGenInput.Items))
	}
	item := store.lastGenInput.Items[0]
	if item.ResourceID != 16 || item.PriceItemKey != "image_generation" {
		t.Fatalf("落库明细 = %+v, want resource 16 / image_generation", item)
	}
	if item.SKUID != 0 {
		t.Fatalf("资源口径明细不应带 sku_id，实际 = %d", item.SKUID)
	}
	// floor = 0.0272 ÷ (1 − 0.15) = 0.032
	if got := item.FloorPrice.StringFixed(8); got != "0.03200000" {
		t.Fatalf("floor = %s, want 0.03200000", got)
	}
	if item.Currency != "USD" {
		t.Fatalf("币种 = %s, want USD", item.Currency)
	}
}

// TestGenerateResourceQuoteRejectsBelowFloor 锁住红线：低于 floor 的资源报价必须被拒，
// 且错误能被 errors.Is(err, ErrBelowFloor) 命中（前端要拿到违规明细）。
func TestGenerateResourceQuoteRejectsBelowFloor(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	_, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items:      []ResourceQuoteItemInput{{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.03000000", Coefficient: "0.75"}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-2")
	if err == nil {
		t.Fatal("低于 floor 的报价应当被拒")
	}
	if !errors.Is(err, ErrBelowFloor) {
		t.Fatalf("错误 = %v, 应命中 ErrBelowFloor", err)
	}
	var floorErr *ResourceQuoteFloorError
	if !errors.As(err, &floorErr) || len(floorErr.Violations) != 1 {
		t.Fatalf("应带一条违规明细，实际 = %+v", floorErr)
	}
	if floorErr.Violations[0].ResourceID != 16 || floorErr.Violations[0].FloorPrice != "0.03200000" {
		t.Fatalf("违规明细 = %+v", floorErr.Violations[0])
	}
}

// TestGenerateResourceQuoteRejectsDuplicateAndUnknown 锁住两条输入红线：
// 同一资源同一价项重复 → 拒绝；资源不可报价 → 拒绝。
func TestGenerateResourceQuoteRejectsDuplicateAndUnknown(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	_, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items: []ResourceQuoteItemInput{
			{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.04"},
			{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.05"},
		},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-3")
	if !errors.Is(err, ErrResourceDuplicate) {
		t.Fatalf("重复价项的错误 = %v, want ErrResourceDuplicate", err)
	}

	_, err = svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items:      []ResourceQuoteItemInput{{ResourceID: 99, PriceItemKey: "text_input", UnitPrice: "0.04"}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-4")
	if !errors.Is(err, ErrResourceNotQuotable) {
		t.Fatalf("未知资源的错误 = %v, want ErrResourceNotQuotable", err)
	}
}

// TestGenerateResourceQuoteRequiresCoefficient 系数是报价单主数据，缺失即拒。
// 没有系数，导出时只能从单价倒推——官方价一变就还原不出客户承诺的报价。
func TestGenerateResourceQuoteRequiresCoefficient(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	_, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items:      []ResourceQuoteItemInput{{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.04000000"}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-coef-1")
	if err == nil || !strings.Contains(err.Error(), "报价系数必填") {
		t.Fatalf("缺系数应被拒，err=%v", err)
	}
}

// TestGenerateResourceQuoteRejectsCoefficientMismatch 系数与单价严重不一致时拒绝，
// 避免「导出按系数、收钱按单价」两边对不上。官方价 0.04 × 0.5 = 0.02，
// 单价却给 0.04（差 100%）。
func TestGenerateResourceQuoteRejectsCoefficientMismatch(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	_, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items: []ResourceQuoteItemInput{{
			ResourceID: 16, PriceItemKey: "image_generation",
			UnitPrice: "0.04000000", Coefficient: "0.5",
		}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-coef-2")
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("系数与单价不一致应被拒，err=%v", err)
	}
}

// TestGenerateResourceQuoteStoresCoefficient 落库明细必须带上级数原值。
func TestGenerateResourceQuoteStoresCoefficient(t *testing.T) {
	store := resourceStoreFixture()
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	if _, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items: []ResourceQuoteItemInput{{
			ResourceID: 16, PriceItemKey: "image_generation",
			UnitPrice: "0.04000000", Coefficient: "1",
		}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-coef-3"); err != nil {
		t.Fatalf("err=%v", err)
	}
	if got := store.lastGenInput.Items[0].QuotationCoefficient; got != "1" {
		t.Fatalf("落库系数 = %q, want 1", got)
	}
}

// TestGenerateResourceQuoteRejectsRegionMismatch 海外客户不能报国内资源（反之亦然）。
// 前端候选已按区域过滤，这道校验防的是绕过界面直接调接口。
func TestGenerateResourceQuoteRejectsRegionMismatch(t *testing.T) {
	store := resourceStoreFixture()
	store.customerRegion = "OVERSEAS" // 资源 fixture 在 CN
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	_, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items: []ResourceQuoteItemInput{{
			ResourceID: 16, PriceItemKey: "image_generation",
			UnitPrice: "0.04000000", Coefficient: "1",
		}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-region-1")
	if !errors.Is(err, ErrResourceRegionMismatch) {
		t.Fatalf("跨区域报价应被拒，err=%v", err)
	}
}

// TestGenerateResourceQuoteSameRegionPasses 同区域正常放行。
func TestGenerateResourceQuoteSameRegionPasses(t *testing.T) {
	store := resourceStoreFixture()
	store.customerRegion = "CN"
	svc := NewService(store, nil).WithResourceQuoteStore(store)
	if _, err := svc.GenerateResourceQuote(context.Background(), GenerateResourceQuoteInput{
		CustomerID: 3,
		Items: []ResourceQuoteItemInput{{
			ResourceID: 16, PriceItemKey: "image_generation",
			UnitPrice: "0.04000000", Coefficient: "1",
		}},
	}, OwnerScope{DataScope: "ALL", StaffID: 51}, 51, "req-region-2"); err != nil {
		t.Fatalf("同区域应放行，err=%v", err)
	}
}
