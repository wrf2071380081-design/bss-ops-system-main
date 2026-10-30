package config

import (
	"reflect"
	"testing"
)

// TestFeaturesEnabledNames 锁住「模块开关注解」的输出口径：
// 启动日志用它打印生效清单，全关时用它触发告警。开关名必须与
// middleware/feature.go 里的 featureRules 一致，写错会在日志里看不出来。
func TestFeaturesEnabledNames(t *testing.T) {
	all := Features{
		Approval:              true,
		Cost:                  true,
		LegacyPriceSync:       true,
		PricingPolicy:         true,
		PriceUpconduction:     true,
		SupplierQuoteApproval: true,
		SpecialPrice:          true,
		FinanceLegacy:         true,
		CustomerPortal:        true,
	}
	want := []string{
		"approval", "cost", "legacy_price_sync", "pricing_policy",
		"price_upconduction", "supplier_quote_approval", "special_price",
		"finance_legacy", "customer_portal",
	}
	if got := all.EnabledNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("全开时的模块清单不对: got %v want %v", got, want)
	}
	if !all.AnyEnabled() {
		t.Fatal("全开时 AnyEnabled 应为 true")
	}

	// 线上验收踩过的形态：配置文件没有 features 段 → 全部取零值。
	var zero Features
	if got := zero.EnabledNames(); len(got) != 0 {
		t.Fatalf("全关时不应有生效模块，实际 %v", got)
	}
	if zero.AnyEnabled() {
		t.Fatal("全关时 AnyEnabled 应为 false（启动日志据此告警）")
	}

	// 演示口径：只开官方报价同步与特价需要的三项。
	demo := Features{Approval: true, LegacyPriceSync: true, SpecialPrice: true}
	if got, want := demo.EnabledNames(), []string{"approval", "legacy_price_sync", "special_price"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("演示口径的模块清单不对: got %v want %v", got, want)
	}
}
