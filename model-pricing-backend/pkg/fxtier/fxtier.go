// Package fxtier 定义汇率锁定的固定 8 档白名单，以及规范化 / 校验函数。
//
// 单一来源：供应商报价（05-quotes.md §0.4）与财务汇率锁定（12-finance.md）共用本包，
// 避免同一份 8 档在两个域各写一遍而漂移。
//
// 匹配口径：按**数值等价**匹配——"6.8" / "6.80" / "6.800" 视为同一档，
// 命中后统一回显规范形式（如 "6.8"、"7.0"）。
package fxtier

import (
	"strings"

	"github.com/shopspring/decimal"
)

// tier 是单个档位的规范文本与数值。
type tier struct {
	Canonical string
	Value     decimal.Decimal
}

// tiers 是交换汇率的固定 8 档（§0.4）。顺序即下拉展示顺序（由低到高）。
var tiers = []tier{
	{"6.5", decimal.RequireFromString("6.5")},
	{"6.7", decimal.RequireFromString("6.7")},
	{"6.75", decimal.RequireFromString("6.75")},
	{"6.8", decimal.RequireFromString("6.8")},
	{"6.85", decimal.RequireFromString("6.85")},
	{"6.9", decimal.RequireFromString("6.9")},
	{"6.95", decimal.RequireFromString("6.95")},
	{"7.0", decimal.RequireFromString("7.0")},
}

// Tiers 返回 8 档的规范文本（新切片，调用方可安全修改而不影响内部状态）。
func Tiers() []string {
	out := make([]string, 0, len(tiers))
	for _, t := range tiers {
		out = append(out, t.Canonical)
	}
	return out
}

// Normalize 按数值等价匹配 8 档：命中返回规范形式与 true；未命中或无法解析返回 false。
func Normalize(raw string) (string, bool) {
	v, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	for _, t := range tiers {
		if v.Equal(t.Value) {
			return t.Canonical, true
		}
	}
	return "", false
}

// IsValid 判定 raw 是否落在 8 档内（数值等价）。
func IsValid(raw string) bool {
	_, ok := Normalize(raw)
	return ok
}

// Canonical 把库存的 fx_tier 文本（如 "6.800"/"6.80"）规范化为档位回显形式（如 "6.8"）；
// nil 保持 nil；不在 8 档内的历史值原样返回（不静默改值）。
func Canonical(raw *string) *string {
	if raw == nil {
		return nil
	}
	if canonical, ok := Normalize(*raw); ok {
		return &canonical
	}
	return raw
}
