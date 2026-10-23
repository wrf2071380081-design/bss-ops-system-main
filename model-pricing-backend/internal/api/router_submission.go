package api

import (
	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
)

// registerOpenCallbacks 注册开放接口的扩展回调（B：测试平台对接）。
// 注意：这里是 /api/open 组，**不经过 AuthN**；回调必须自行验签（X-Signature + event_id 幂等）。
// 归属 B 的路径：
//   - /api/open/test-callback       测试平台终态回调
func registerOpenCallbacks(g *gin.RouterGroup, deps Deps) {
	// 新路由加在这里。
}

// registerSubmissionRoutes 注册 B（申报 / 办理 / 测试）的路由。
//
// 归属 B 的路径（新增一律写在本文件，不要改 router.go）：
//   - /api/supplier/submissions*       申报（多行/草稿/提交/撤回/版本/参与测试）
//   - /api/supplier/test-jobs*         测试触发 / 进度
//   - /api/internal/submissions*       办理台账 / 详情 / 登记接入
//   - /api/internal/flows*             办理流程与节点（办理人）
//
// 说明：旧的供应商报价 /quotes* 仍留在 router.go（遗留路由），迁移时再搬过来。
func registerSubmissionRoutes(g *gin.RouterGroup, protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "internal" && portal != "supplier" {
		return
	}
	// 供应商门户：申报正式接口（B-1）。写接口一律挂幂等（红线）。
	// 注意：原 supply_chain.go 里的简化版 POST /submissions 已删除，
	// 同路径重复注册会让 gin 启动 panic。
	if portal == "supplier" && deps.SupplierSubmissions != nil && deps.IdemStore != nil {
		registerSupplierSubmissionRoutes(protected, deps, middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey))
	}
	// 测试触发 / 办理台账：依赖测试平台分支，随 B-2/M3 推进。
}
