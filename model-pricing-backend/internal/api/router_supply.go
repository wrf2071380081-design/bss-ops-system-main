package api

import (
	"github.com/gin-gonic/gin"
)

// registerSupplyRoutes 注册 A（主数据与供应侧）的路由。
//
// 归属 A 的路径（新增一律写在本文件，不要改 router.go）：
//   - /api/internal/standard-models*        标准模型库（列表/详情/增改/核验/拉取差异）
//   - /api/internal/resources*              供应来源 / 模型广场（列表/详情/增改/状态）
//   - /api/internal/suppliers*              供应商扩展（正式配置/资质/税率/币种）
//   - /api/internal/contracts*、/credits*    合同与授信
//   - /api/internal/plans*、/notices*        上下架计划 / 公告
//
// 说明：旧的 /models、/suppliers 仍留在 router.go（遗留路由），迁移时再搬过来。
func registerSupplyRoutes(g *gin.RouterGroup, protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "internal" {
		return
	}
	// 新路由加在这里（示例，取消注释并替换 handler 即可）：
	// protected.GET("/standard-models", middleware.RequirePerm(perm.M1View), h.ListStandardModels)
}
