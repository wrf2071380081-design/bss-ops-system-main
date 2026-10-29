package customer

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

// fakeResourceStore 是资源口径报价的最小仓储替身。
type fakeResourceStore struct {
	fakeStore
	bases map[string]ResourceQuoteItem
	items map[string]ResourceQuoteItem
}

func (f *fakeResourceStore) ListResourceQuoteCandidates(_ context.Context, _ string, page, size int) (*ResourceQuoteCandidatePage, error) {
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
		Items:      []ResourceQuoteItemInput{{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.04000000"}},
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
		Items:      []ResourceQuoteItemInput{{ResourceID: 16, PriceItemKey: "image_generation", UnitPrice: "0.03000000"}},
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
