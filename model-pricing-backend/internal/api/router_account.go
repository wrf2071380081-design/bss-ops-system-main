package api

import (
	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
)

// registerSupplierAccountRoutes 注册供应商门户「账户设置」路由（M2，2026-10-27）。
//
// 路径（都挂在 /api/supplier 下）：
//   - GET  /account                  读账户资料
//   - PUT  /account                  保存账户资料（挂幂等：写操作红线）
//   - GET  /account/operation-logs   操作记录分页
//   - POST /account/license          上传/替换企业证明（multipart，不做幂等）
//
// 只注册在供应商门户（portal == "supplier"）：
//   - 门户段不挂 RequirePerm（SUPPLIER 角色零内部权限点），隔离全靠登录态解析 supplier.id；
//   - 不接受任何请求参数指定 supplier_id，避免越权。
func registerSupplierAccountRoutes(protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "supplier" || deps.SupplierAccount == nil {
		return
	}
	handler := NewSupplierAccountHandler(deps.SupplierAccount, deps.UploadDir)

	protected.GET("/account", handler.get)
	// 写接口一律挂幂等（红线 5）；没配幂等库时宁可不暴露写能力。
	if deps.IdemStore != nil {
		protected.PUT("/account",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			handler.update,
		)
	}
	protected.GET("/account/operation-logs", handler.logs)
	protected.POST("/account/license", handler.uploadLicense)
}
