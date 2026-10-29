package customer

import (
	"testing"

	"github.com/shopspring/decimal"
)

// TestResourceUnitCostIsOfficialPriceTimesCoefficient 锁住资源口径成本公式：
// 采购成本 = 官方价项金额 × 采购系数（供应域口径），不是定价域 cost_baseline 那套。
func TestResourceUnitCostIsOfficialPriceTimesCoefficient(t *testing.T) {
	cases := []struct {
		name        string
		amount      string
		coefficient string
		want        string
	}{
		{"官方价乘系数", "2.00000000", "0.7400", "1.48000000"},
		{"系数为一即官方价", "8.00000000", "1.0000", "8.00000000"},
		{"小数金额", "0.04000000", "0.6800", "0.02720000"},
	}
	for _, c := range cases {
		got, err := ResourceUnitCost(c.amount, c.coefficient)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.StringFixed(8) != c.want {
			t.Fatalf("%s: got %s want %s", c.name, got.StringFixed(8), c.want)
		}
	}
	for _, bad := range [][3]string{{"非法金额", "abc", "0.7"}, {"非法系数", "2.0", "xyz"}, {"负系数", "2.0", "-0.5"}} {
		if _, err := ResourceUnitCost(bad[1], bad[2]); err == nil {
			t.Fatalf("%s: 期望报错，实际通过", bad[0])
		}
	}
}

// TestBuildResourceQuoteFloorMatchesSKUFormula 锁住资源口径 floor 与 SKU 口径同一公式：
// floor = 成本 ÷ (1 − 最低毛利)，两者只是「成本来源」不同。
func TestBuildResourceQuoteFloorMatchesSKUFormula(t *testing.T) {
	cost := decimal.RequireFromString("1.48000000")
	margin := decimal.RequireFromString("0.15")
	got, err := BuildResourceQuoteFloor(cost, margin)
	if err != nil {
		t.Fatal(err)
	}
	if got.StringFixed(8) != "1.74117647" {
		t.Fatalf("floor = %s, want 1.74117647", got.StringFixed(8))
	}
	// 与 SKU 口径的 cost.Floor 同一实现，结果必须逐位相同。
	skuFloor, err := CalcFloor(cost, margin)
	if err != nil {
		t.Fatal(err)
	}
	if !skuFloor.Equal(got) {
		t.Fatalf("资源口径 floor %s 与 SKU 口径 %s 不一致", got, skuFloor)
	}
}
