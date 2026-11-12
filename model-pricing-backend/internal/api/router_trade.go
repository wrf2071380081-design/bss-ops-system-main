package api

import (
	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/pkg/perm"
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
		protected.GET("/workbench/operations/options", wh.OperationOptions)
		protected.POST("/workbench/notices/:id/read",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			wh.MarkNoticeRead,
		)
	}

	// 客户「接入模型」绑定（G4 地基，迁移 000049）：
	//   - 读投影给客户详情「接入模型」Tab；
	//   - 建绑定在开通完成时被内部调用，同时也开放手工登记（排障/补录）；
	//   - 状态动作（禁用/恢复/确认停用）必须带 expected_revision。
	// 权限沿用客户域口径：读 M8:V、写 M8:E，不新造权限码。
	if deps.CustomerResourceLinkSvc != nil {
		rlh := NewCustomerResourceLinkHandler(deps.CustomerResourceLinkSvc)
		protected.GET("/customers/:id/resource-links",
			middleware.RequirePerm(perm.M8View),
			rlh.ListCustomerResourceLinks,
		)
		protected.POST("/resource-links",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			rlh.CreateResourceLink,
		)
		protected.POST("/resource-links/:id/actions",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			rlh.ActResourceLink,
		)
	}

	// 客户创建与基础档案（G4-B1）：此前 customer_profile / legal_subject 只有读路径，
	// 「完整开通」没有对象可挂。写接口必须幂等；读接口走数据域过滤。
	if deps.CustomerRegistrySvc != nil {
		crh := NewCustomerRegistryHandler(deps.CustomerRegistrySvc)
		protected.POST("/customers",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			crh.CreateCustomer,
		)
		protected.GET("/customers/:id",
			middleware.RequirePerm(perm.M8View),
			crh.GetCustomerProfile,
		)
		// 主体资料维护（联系人 / 税率 / 网关用户名 / 备注）。
		protected.PUT("/customers/:id/profile",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			crh.UpdateCustomerProfile,
		)
	}

	// 客户开通流程（G4-B2）：发起开通 / 读进度 / 完成节点。
	// 客户侧独立实现（路线 B）：只共用 onboarding_flow / onboarding_node 表结构与审计写法，
	// 供应商流程代码一行不动。
	if deps.CustomerOnboardingSvc != nil {
		coh := NewCustomerOnboardingHandler(deps.CustomerOnboardingSvc)
		protected.POST("/customers/:id/onboarding",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			coh.StartOnboarding,
		)
		protected.POST("/customers/:id/onboarding/models",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			coh.AddOnboardingModels,
		)
		// 兜底：审批已派活但历史客户没有流程时，按待办责任人补建（000063 之前的单）。
		protected.POST("/customers/:id/onboarding/ensure",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			coh.EnsureOnboardingFlow,
		)
		protected.GET("/customers/:id/onboarding",
			middleware.RequirePerm(perm.M8View),
			coh.GetOnboarding,
		)
		protected.POST("/customer-nodes/:id/complete",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			coh.CompleteCustomerNode,
		)
	}

	// 客户操作记录（B3a）：operation_log 的双实体时间线（customer_profile + onboarding_flow）。
	// 需要两个依赖：RegistryService 做 404/403 判定（与客户档案同口径），
	// CustomerWorkspaceRuntime 做只读查询。任一为 nil 就不挂载。
	if deps.CustomerRegistrySvc != nil && deps.CustomerWorkspace != nil {
		olh := NewCustomerOperationLogHandler(deps.CustomerRegistrySvc, deps.CustomerWorkspace)
		protected.GET("/customers/:id/operation-logs",
			middleware.RequirePerm(perm.M8View),
			olh.ListCustomerOperationLogs,
		)
	}

	// 新路由加在这里。
}
