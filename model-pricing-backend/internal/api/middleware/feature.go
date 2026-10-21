// Package middleware 的模块开关拦截。
//
// 背景：原型 v0.19 重构后，旧系统有一批模块（审批/成本四因子/定价策略/涨价传导/
// 供应商报价审批动作/特价/汇率锁/客户门户）不再使用。按"只隐藏、不删代码"的纪律，
// 用配置开关 + 路由 pattern 精确拦截，关闭时返回 404（与"未注册"同形）。
package middleware

import (
	"github.com/gin-gonic/gin"

	"model_bss/internal/infra/config"
	"model_bss/pkg/apperr"
)

// featureRule 描述"某模块关闭时，哪些路由要 404"。
// method 为空表示不限方法；path 必须是注册时的完整 pattern（c.FullPath()）。
type featureRule struct {
	feature string
	method  string
	path    string
}

// featureRules 是"模块 → 路由 pattern"的清单。
// 原则：只关原型用不到的动作，同前缀下要保留的接口不能误伤
// （例如 model-applications 的提交/查询保留，只关 decision）。
var featureRules = []featureRule{
	// 审批 / 变更单（保留 model-applications 的提交与查询）
	{"approval", "POST", "/api/internal/approvals/:id/decision"},
	{"approval", "GET", "/api/internal/change-requests"},
	{"approval", "GET", "/api/internal/change-requests/:id"},
	{"approval", "POST", "/api/internal/model-applications/:id/decision"},

	// 成本四因子 / 成本基线 / 机会 / 对比 / 锁主供
	{"cost", "GET", "/api/internal/cost/baselines"},
	{"cost", "GET", "/api/internal/cost/baselines/:sku/history"},
	{"cost", "GET", "/api/internal/cost/params"},
	{"cost", "PUT", "/api/internal/cost/params"},
	{"cost", "POST", "/api/internal/cost/baselines/:sku/lock-primary"},
	{"cost", "GET", "/api/internal/cost/opportunities"},
	{"cost", "GET", "/api/internal/cost/baselines/:sku/compare"},

	// 旧官方价同步（新方案是人工核验 + 自动拉取，属后续新增）
	{"legacy_price_sync", "POST", "/api/internal/price-sync/jobs"},
	{"legacy_price_sync", "GET", "/api/internal/price-sync/jobs"},
	{"legacy_price_sync", "POST", "/api/internal/staging-prices"},
	{"legacy_price_sync", "GET", "/api/internal/staging-prices"},
	{"legacy_price_sync", "POST", "/api/internal/staging-prices/confirm"},

	// 定价策略（原型用采购系数/销售系数替代）
	{"pricing_policy", "GET", "/api/internal/pricing/policies"},
	{"pricing_policy", "POST", "/api/internal/pricing/policies"},
	{"pricing_policy", "PUT", "/api/internal/pricing/policies/:id"},

	// 涨价传导
	{"price_upconduction", "GET", "/api/internal/price-upconduction"},
	{"price_upconduction", "POST", "/api/internal/price-upconduction/generate"},
	{"price_upconduction", "POST", "/api/internal/price-upconduction/:id/decide"},

	// 供应商报价的审批/异常/追溯动作（保留 POST /quotes、history、:id、withdraw）
	{"supplier_quote_approval", "GET", "/api/internal/quotes/pending"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/activate-due"},
	{"supplier_quote_approval", "GET", "/api/internal/quotes/:id/diff"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/:id/approve"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/:id/reject"},
	{"supplier_quote_approval", "GET", "/api/internal/quotes/expiring"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/scan"},
	{"supplier_quote_approval", "GET", "/api/internal/quotes/anomalies"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/retro-effective"},
	{"supplier_quote_approval", "POST", "/api/internal/quotes/:id/confirm-remove"},

	// 客户特价审批 / 刷新
	{"special_price", "POST", "/api/internal/customer-quotes/:id/special-price"},
	{"special_price", "POST", "/api/internal/customer-quotes/:id/refresh"},

	// 财务：汇率 / 账户 / 汇率锁
	{"finance_legacy", "GET", "/api/internal/finance/fx-rates"},
	{"finance_legacy", "GET", "/api/internal/finance/accounts"},
	{"finance_legacy", "GET", "/api/internal/finance/accounts/:customerId"},
	{"finance_legacy", "GET", "/api/internal/finance/fx-lock"},
	{"finance_legacy", "POST", "/api/internal/finance/fx-lock"},

	// 客户自助门户（登录/登出保留：不在拦截清单里）
	{"customer_portal", "GET", "/api/customer/profile"},
	{"customer_portal", "GET", "/api/customer/price-book"},
	{"customer_portal", "GET", "/api/customer/quotes"},
	{"customer_portal", "POST", "/api/customer/quotes/:id/accept"},
	{"customer_portal", "GET", "/api/customer/billing"},
	{"customer_portal", "GET", "/api/customer/notifications"},
	{"customer_portal", "GET", "/api/customer/home"},
}

// FeatureGate 按模块开关拦截路由：模块关闭时返回 404（与"未注册"同形）。
// 用 c.FullPath()（注册 pattern）精确匹配，避免误伤同前缀下仍要保留的接口。
func FeatureGate(f config.Features) gin.HandlerFunc {
	return func(c *gin.Context) {
		fp := c.FullPath()
		if fp == "" {
			c.Next()
			return
		}
		method := c.Request.Method
		for _, r := range featureRules {
			if r.path != fp {
				continue
			}
			if r.method != "" && r.method != method {
				continue
			}
			if !f.IsEnabled(r.feature) {
				abortError(c, apperr.ErrNotFound)
				return
			}
		}
		c.Next()
	}
}
