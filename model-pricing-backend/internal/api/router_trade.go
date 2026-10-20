package api

import (
	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
)

// registerTradeRoutes 注册 C（客户 / 报价 / 留痕）的路由。
//
// 归属 C 的路径（新增一律写在本文件，不要改 router.go）：
//   - /api/internal/customers*         客户创建 / 详情 / 销售归属
//   - /api/internal/customer-links*    客户开通 / 状态 / 配置历史
//   - /api/internal/quotes*            报价 / 毛利 / 版本 / 导出
//   - /api/internal/operation-logs*    操作记录（按 entityType/entityId）
//   - /api/internal/assignments*       绑定变更（销售/采购/办理人）
//
// 说明：旧的 /customers、/customer-quotes 仍留在 router.go（遗留路由），迁移时再搬过来。
func registerTradeRoutes(g *gin.RouterGroup, protected *gin.RouterGroup, portal string, deps Deps) {
	if portal != "internal" {
		return
	}

	// F0 工作台：统一收件箱 + 人员选择 + 操作记录 + 公告已读。
	if deps.WorkbenchInboxSvc != nil {
		wh := NewWorkbenchInboxHandler(deps.WorkbenchInboxSvc)
		protected.GET("/workbench/inbox", wh.Inbox)
		protected.GET("/workbench/people", wh.People)
		protected.GET("/workbench/operations", wh.Operations)
		protected.POST("/workbench/notices/:id/read",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			wh.MarkNoticeRead,
		)
	}

	// 新路由加在这里。
}
