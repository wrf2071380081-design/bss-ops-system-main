// Package api 负责应用 HTTP 路由的装配。
//
// 中间件顺序（gin r.Use 即执行顺序）：
//
//	Recovery → CORS → RequestID → AccessLog → [受保护组] AuthN → DataScope → AuthZ → handler
//
// AuthZ 依赖 AuthN 注入的会话快照，故必须排在 AuthN 之后。
package api

import (
	"os"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/cost"
	"model_bss/internal/domain/customer"
	"model_bss/internal/domain/finance"
	"model_bss/internal/domain/model"
	"model_bss/internal/domain/openapi"
	"model_bss/internal/domain/orgperm"
	"model_bss/internal/domain/price"
	"model_bss/internal/domain/pricing"
	"model_bss/internal/domain/supplier"
	"model_bss/internal/domain/workbench"
	"model_bss/internal/infra/config"
	infra_middleware "model_bss/internal/infra/middleware"
	"model_bss/pkg/perm"
)

// Deps 是路由装配所需的全部依赖。
type Deps struct {
	Config        *config.Config
	Log           *zap.Logger
	Health        Pinger
	AuthService   *auth.Service
	SessionReader auth.SessionReader
	IdemStore     middleware.IdempotencyStore
	// SupplyRoutes contains A-owned handlers assembled over the current schema.
	// It stays nil in environments where the A module is not configured.
	SupplyRoutes *SupplyDeps
	// TestJobs 是 B 线测试平台发起端口；未配置测试平台时保持 nil（路由不注册）。
	TestJobs TestJobStarter
	// TestCallbacks 是测试平台终态回调接收端口；未配置 webhook secret 时保持 nil。
	TestCallbacks TestCallbackReceiver
	// SupplierSubmissions 是供应商申报（B-1）服务；未配置时申报路由不注册。
	SupplierSubmissions *SupplierSubmissionService
	// SupplierCredentials 是供应商**主体级**准入测试凭据（000044，原型对齐）。
	// nil 时 /supplier/test-credential 不注册（同一条纪律：没接仓储就不暴露能力）。
	SupplierCredentials *SupplierCredentialService
	// ResourceTestTargets 供内部复测（T-10）读取资源自身测试目标。
	ResourceTestTargets ResourceTestTargetLoader
	ModelSvc            *model.Service
	SupplierSvc         *supplier.Service
	SupplierProfileSvc  *supplier.ProfileService
	SupplierQuoteSvc    *supplier.QuoteService
	SupplierImportSvc   *supplier.ImportService
	QuoteApproveSvc     *supplier.ApproveService
	QuoteLifecycleSvc   *supplier.LifecycleService
	CostReadSvc         *cost.ReadService
	CostParamSvc        *cost.ParamService
	CostLockSvc         *cost.LockService
	CostCompareSvc      *cost.CompareService
	PriceSyncSvc        *price.SyncService
	PriceConfirmSvc     *price.ConfirmService
	PricingSvc          *pricing.PricingService
	PricingPublishSvc   *pricing.PublishService
	// 阶段 8b-2：涨价传导决策队列（08-pricing.md §5）。
	PricingUpconductionSvc *pricing.UpconductionService
	// 阶段 9a：客户域（09-customer-quote.md §1/§2）。
	CustomerSvc *customer.Service
	// 阶段 9b：特价审批 + 刷新 + 导出（09-customer-quote.md §3/§4/§5）。
	CustomerSpecialSvc *customer.SpecialPriceService
	CustomerRefreshSvc *customer.RefreshService
	CustomerExportSvc  *customer.ExportService
	// 阶段 9c：客户门户（09-customer-quote.md §6）。
	CustomerPortalSvc *customer.PortalService
	// 阶段 10a：工作台（10-workbench-audit.md §2/§3）。
	WorkbenchSvc *workbench.Service
	// 2026-10-20：工作台统一收件箱（F0）。
	WorkbenchInboxSvc *workbench.InboxService
	// 阶段 10b：告警处理 + 审计日志（10-workbench-audit.md §4/§5）。
	AlertSvc *workbench.AlertService
	AuditSvc *workbench.AuditService
	// 阶段 11a：开放接口（11-open-api.md §1/§2）。
	OpenApiSvc     *openapi.Service
	OpenTokenStore middleware.OpenTokenStore
	// 批次 4：财务中心只读（12-finance.md §1/§2/§3）。
	FinanceReadSvc *finance.ReadService
	// 批次 6：汇率锁定读写（12-finance.md §4/§5）。
	FinanceLockSvc *finance.LockService
	// 阶段 12：组织与权限 M11 只读视图（批次 5）。
	OrgPermSvc *orgperm.Service
	// 2026-10-13：M11 账号管理写侧仓储。nil 时写接口整体不挂载
	// （与 OrgPermSvc 同一条纪律：没接仓储就不暴露能力）。
	StaffStore orgperm.StaffStore
	// 阶段 12：供应商门户主体档案只读视图（批次 7a，只做 GET）。
	SupplierPortalProfileSvc *supplier.PortalProfileService
	// 供应商门户「账户设置」（M2，2026-10-27）：读 supplier 表，明文读写。
	// nil 时路由整体不挂载（与其它模块同一条纪律：没接仓储就不暴露能力）。
	SupplierAccount *SupplierAccountService
	// UploadDir 是门户上传文件（企业证明）的落盘根目录。
	UploadDir string
	// Stage 2：供应商申报 → 领导审批 → mock 测试 → 登记接入。
	SupplyChain *SupplyChainRuntime
	// 客户管理合并页：客户列表派生列 / 客户报价 / 特价审批记录（纯只读）。
	CustomerWorkspace *CustomerWorkspaceRuntime
}

// New 构建并返回应用使用的 Gin Engine。
func New(deps Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	if deps.Config.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS(deps.Config.Server.AllowOrigins))
	r.Use(infra_middleware.RequestID())
	r.Use(infra_middleware.AccessLog(deps.Log))
	// 模块开关（重构收敛）：原型 v0.19 用不到的旧模块默认关闭，命中路由返回 404。
	// 只隐藏入口，不删代码/表；configs 或 MODEL_BSS_FEATURES_* 可改回。
	r.Use(middleware.FeatureGate(deps.Config.Features))

	r.GET("/healthz", HealthzHandler(deps.Health))

	// 在线接口调试页（Swagger UI）：docs.enable 默认 true，生产显式关闭。
	// 放在 /api 业务路由之前注册，但路径不重叠（/swagger、/openapi），互不干扰。
	if deps.Config.Docs.Enable {
		registerDocs(r)
	}

	api := r.Group("/api")
	for _, portal := range []string{"internal", "supplier", "customer", "open"} {
		registerPortal(api, portal, deps)
	}

	return r
}

// registerPortal 注册单个门户的公开路由与受保护路由。
func registerPortal(api *gin.RouterGroup, portal string, deps Deps) {
	g := api.Group("/" + portal)

	// ---- 开放接口（11a）：独立鉴权，不挂 AuthN/RequirePerm/Idempotency ----
	if portal == "open" {
		// 公开路由：POST /auth/token（client_id+secret 换 token）
		if deps.OpenApiSvc != nil {
			h := NewOpenApiHandler(deps.OpenApiSvc)
			g.POST("/auth/token", h.IssueToken)
		}
		// 受保护路由：OpenAuthN（校验 open_api_token）
		protected := g.Group("")
		if deps.OpenTokenStore != nil {
			protected.Use(middleware.OpenAuthN(deps.OpenTokenStore))
		}
		if deps.OpenApiSvc != nil {
			h := NewOpenApiHandler(deps.OpenApiSvc)
			protected.GET("/aliases", h.GetAliases)
			protected.GET("/sellable-models", h.GetSellableModels)
			protected.GET("/routing/:sku", h.GetRouting)
			protected.GET("/price-book", h.GetPriceBook)
			protected.GET("/cost-snapshot", h.GetCostSnapshot)
			protected.GET("/events", h.PullEvents)
		}
		// 开放接口的扩展路由（如测试平台回调）加到 router_submission.go，别改本文件。
		registerOpenCallbacks(g, deps)
		return
	}

	// ---- 公开路由（不经过 AuthN） ----
	authGroup := g.Group("/auth")
	authGroup.POST("/login", LoginHandler(deps.AuthService, portal))
	// logout 放在公开组：登出需要携带 token，但会话即使已过期也应允许吊销。
	authGroup.POST("/logout", LogoutHandler(deps.AuthService))
	// 供应商：手机号+验证码登录 / 注册（短信未接入，验证码固定 1234）。
	if portal == "supplier" {
		authGroup.POST("/login-mobile", SupplierMobileLoginHandler(deps.AuthService))
		authGroup.POST("/register", SupplierRegisterHandler(deps.AuthService))
	}

	// ---- 受保护路由：AuthN → DataScope（顺序固定，AuthZ 在路由级追加） ----
	protected := g.Group("")
	protected.Use(middleware.AuthN(deps.SessionReader))
	protected.Use(middleware.DataScope())

	// 冒烟验证路由：仅当 MODEL_BSS_SMOKE=1 时挂载（生产默认关闭）。
	if os.Getenv("MODEL_BSS_SMOKE") == "1" {
		registerSmokeRoutes(protected, deps.IdemStore)
	}

	// 业务接口：模型管理 M1（全局主数据，不注入数据域行级过滤）。
	if portal == "internal" && deps.ModelSvc != nil {
		h := NewModelHandler(deps.ModelSvc)
		protected.GET("/models", middleware.RequirePerm(perm.M1View), h.ListModels)
		protected.GET("/models/options", middleware.RequirePerm(perm.M1View), h.ModelOptions)
		protected.POST("/models",
			middleware.RequirePerm(perm.M1Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			h.CreateModel,
		)
		protected.PUT("/models/:id", middleware.RequirePerm(perm.M1Edit), h.UpdateModel)

		// 阶段 4b-1：别名 / 查重 / 合并 / 批量 / 上架
		// 静态路由必须先于 /models/:id 注册，避免被路径参数吃掉。
		protected.GET("/models/aliases/suggest", middleware.RequirePerm(perm.M1View), h.SuggestAliases)
		protected.POST("/models/aliases/merge",
			middleware.RequirePerm(perm.M1Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			h.MergeAliases,
		)
		protected.POST("/models/batch", middleware.RequirePerm(perm.M1Edit), h.BatchModels)
		protected.POST("/models/:id/aliases", middleware.RequirePerm(perm.M1Edit), h.ReplaceAliases)
		// 上架也挂幂等（CLAUDE.md 坑位表：审批、发布、补录、汇率锁定、移交、退役都要）
		protected.POST("/models/:id/publish",
			middleware.RequirePerm(perm.M1Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			h.PublishModel,
		)

		// 阶段 4b-2：退役链路
		protected.GET("/models/:id/deprecation-impact", middleware.RequirePerm(perm.M1View), h.DeprecationImpact)
		protected.POST("/models/:id/deprecate",
			middleware.RequirePerm(perm.M1Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			h.DeprecateModel,
		)
		// 通用审批入口：**不声明模块权限点**。
		// 审批权由 approval_step.required_role 决定，由服务层校验（设计文档 §3.3：
		// 审批类操作"额外"校验 required_role 与当前操作员角色匹配）。
		// 若在此处硬编码 M1:A，会与 required_role 机制冲突——例如退役第 2 步的
		// required_role=PRICING_OP 而该角色只有 M1:V，合法审批人会被 403 挡死。
		// 本路由只要求已认证（AuthN），越权由服务层返回 403。
		protected.POST("/approvals/:id/decision",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			h.DecideApproval,
		)

		// 变更单与审批进度只读查询（联调 P1-4 / P1-5）。
		// 官方价变更单（PRICE_UP/PRICE_DOWN）与价目表发布（PRICE_BOOK_PUBLISH/ROLLBACK）
		// 统一走这里；不挂模块权限点——与审批动作同口径（审批权由 required_role 决定）。
		crh := NewChangeRequestHandler(deps.ModelSvc)
		protected.GET("/change-requests", crh.ListChangeRequests)
		protected.GET("/change-requests/:id", crh.GetChangeRequest)
	}

	// 业务接口：新模型申请审核（内部侧，属 M1 模型主数据域）。
	// 契约：设计文档 §8.5 接口清单；状态机 SUBMITTED → APPROVED/MERGED/REJECTED。
	if portal == "internal" && deps.SupplierSvc != nil {
		mah := NewModelApplicationHandler(deps.SupplierSvc)
		protected.GET("/model-applications",
			middleware.RequirePerm(perm.M1View), mah.ListApplications)
		protected.POST("/model-applications/:id/decision",
			middleware.RequirePerm(perm.M1Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			mah.DecideApplication,
		)
	}
	if portal == "internal" && deps.SupplierProfileSvc != nil {
		sph := NewSupplierProfileHandler(deps.SupplierProfileSvc)
		protected.GET("/suppliers", middleware.RequirePerm(perm.M3View), sph.ListProfiles)
		protected.GET("/suppliers/:id", middleware.RequirePerm(perm.M3View), sph.GetProfile)
	}

	// 业务接口：成本基线只读（06-cost.md §2/§3，内部 M5:V）。
	// 无归属过滤（设计 §3.2 决议：成本与比价类放开 ALL，红线 7——这里不挂任何数据域过滤）。
	// 字段剔除在 handler 内（op.FieldMask + fieldmask.Apply，000017 首次真正挂接）。
	if portal == "internal" && deps.CostReadSvc != nil {
		ch := NewCostHandler(deps.CostReadSvc)
		// 静态段 /cost/baselines 先注册；/cost/baselines/:sku/history 是子路径，互补冲突。
		protected.GET("/cost/baselines", middleware.RequirePerm(perm.M5View), ch.ListCostBaselines)
		protected.GET("/cost/baselines/:sku/history", middleware.RequirePerm(perm.M5View), ch.GetCostBaselineHistory)
	}

	// 业务接口：成本参数读写（06-cost.md §7，内部 M5:V 读 / M5:C 写 + 服务层角色收敛 PRICING_OP）。
	// PUT 挂幂等中间件（红线 5：影响成本的写操作必须过 Idempotency-Key）。
	//
	// ⚠️ 写权限点由 M5:E 改为 M5:C（批次 3a / 裁决 B2=甲）：
	//   M5:E 被 6 个角色共享，而它同时承载「成本参数配置 / 主供应商锁定 / 汇率锁定」三个不同动作，
	//   权限点层面无法区分。现按动作拆开：配置类挂 :C（仅 PRICING_OP），主供应商锁定保留 :E（仅 PROCUREMENT），
	//   汇率锁定挂 M10:E（仅 FINANCE，批次 6）。迁移 000030_split_m5_permission 同步改了 role_permission。
	//   服务层 CanEditCostParam 保留为防御性第二层，不是唯一防线。
	if portal == "internal" && deps.CostParamSvc != nil {
		ph := NewCostParamHandler(deps.CostParamSvc)
		protected.GET("/cost/params", middleware.RequirePerm(perm.M5View), ph.ListCostParams)
		protected.PUT("/cost/params",
			middleware.RequirePerm(perm.M5Config),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ph.UpdateCostParams,
		)
	}

	// 业务接口：手动锁定主供应商（06-cost.md §6，内部 M5:E + 服务层角色收敛 PROCUREMENT）。
	// 批次 3a 后 M5:E 只归 PROCUREMENT（+PLATFORM_ADMIN），语义收窄为「主供应商锁定」。
	// 幂等必挂（红线 5：产生业务新版本的写操作必须过 Idempotency-Key）。
	// {sku} 解析复用 ReadService（与 §3 历史同一段代码——绝不能自己再写一份）。
	if portal == "internal" && deps.CostLockSvc != nil && deps.CostReadSvc != nil {
		lh := NewCostLockHandler(deps.CostLockSvc, deps.CostReadSvc)
		protected.POST("/cost/baselines/:sku/lock-primary",
			middleware.RequirePerm(perm.M5Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			lh.LockPrimary,
		)
	}

	// 业务接口：§4 比价 + §5 议价机会（内部 M5:V，只读）。
	// 静态段 /cost/opportunities 必须先于 /cost/baselines/:sku/compare 注册——
	// gin 按注册序匹配，静态段后置会被 :sku 吃掉（6b-4 同款纪律：静态段先于路径参数）。
	if portal == "internal" && deps.CostCompareSvc != nil {
		cch := NewCostCompareHandler(deps.CostCompareSvc)
		protected.GET("/cost/opportunities", middleware.RequirePerm(perm.M5View), cch.Opportunities)
		protected.GET("/cost/baselines/:sku/compare", middleware.RequirePerm(perm.M5View), cch.Compare)
	}

	// 业务接口：官方价采集批次 + 暂存区（07-supplier-and-price-change.md §5/§6，内部 M2，阶段 7a）。
	// POST /staging-prices 是写接口，必挂幂等（红线 5）；官方价是全局主数据，无归属过滤。
	if portal == "internal" && deps.PriceSyncSvc != nil {
		psh := NewPriceSyncHandler(deps.PriceSyncSvc)
		protected.POST("/price-sync/jobs", middleware.RequirePerm(perm.M2Edit), psh.CreateSyncJob)
		protected.GET("/price-sync/jobs", middleware.RequirePerm(perm.M2View), psh.ListSyncJobs)
		protected.POST("/staging-prices",
			middleware.RequirePerm(perm.M2Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			psh.CreateStagingPrices,
		)
		protected.GET("/staging-prices", middleware.RequirePerm(perm.M2View), psh.ListStagingPrices)
	}

	// 业务接口：官方价确认入正式版本（07-supplier-and-price-change.md §7，内部 M2，阶段 7b）。
	// POST /staging-prices/confirm 是写接口，必挂幂等（红线 5）。
	// 审批动作复用 /approvals/:id/decision（ModelSvc.DecideApprovalByType 按 change_type 分发连锁回调）。
	if portal == "internal" && deps.PriceConfirmSvc != nil {
		pch := NewPriceConfirmHandler(deps.PriceConfirmSvc)
		protected.POST("/staging-prices/confirm",
			middleware.RequirePerm(perm.M2Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			pch.ConfirmStaging,
		)
	}

	// 业务接口：定价策略 + 生成价目表草稿（08-pricing.md §1/§2，内部 M6/M7，阶段 8a）。
	// 写接口（POST/PUT policies、POST price-books）必挂幂等（红线 5）。
	if portal == "internal" && deps.PricingSvc != nil {
		ph := NewPricingHandler(deps.PricingSvc)
		protected.GET("/pricing/policies", middleware.RequirePerm(perm.M6View), ph.ListPolicies)
		protected.POST("/pricing/policies",
			middleware.RequirePerm(perm.M6Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ph.CreatePolicy,
		)
		protected.PUT("/pricing/policies/:id",
			middleware.RequirePerm(perm.M6Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ph.UpdatePolicy,
		)
		protected.POST("/price-books",
			middleware.RequirePerm(perm.M7Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ph.GenerateDraft,
		)
	}

	// 业务接口：价目表发布 + 回滚（08-pricing.md §3/§4，内部 M7，阶段 8b-1）。
	// 写接口必挂幂等（红线 5）；审批复用 /approvals/:id/decision（DecideApprovalByType 按 change_type 分发）。
	if portal == "internal" && deps.PricingPublishSvc != nil {
		pph := NewPricingPublishHandler(deps.PricingPublishSvc)
		protected.POST("/price-books/:id/publish",
			middleware.RequirePerm(perm.M7Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			pph.Publish,
		)
		protected.POST("/price-books/:id/rollback",
			middleware.RequirePerm(perm.M7Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			pph.Rollback,
		)
	}

	// 业务接口：涨价传导决策队列（08-pricing.md §5，内部 M7，阶段 8b-2）。
	// 写接口挂幂等（红线 5）；查询挂 M7:V。
	if portal == "internal" && deps.PricingUpconductionSvc != nil {
		puh := NewPricingUpconductionHandler(deps.PricingUpconductionSvc)
		protected.GET("/price-upconduction",
			middleware.RequirePerm(perm.M7View),
			puh.ListQueue,
		)
		protected.POST("/price-upconduction/generate",
			middleware.RequirePerm(perm.M7Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			puh.GenerateQueue,
		)
		protected.POST("/price-upconduction/:id/decide",
			middleware.RequirePerm(perm.M7Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			puh.Decide,
		)
	}

	// 业务接口：客户列表 + 生成客户报价（09-customer-quote.md §1/§2，内部 M8/M9，阶段 9a）。
	// 行级过滤：客户列表 SELF/DEPT/ALL 按 owner_sales_operator_id（销售归属），
	// 移交仅主管（OPS_ADMIN/PLATFORM_ADMIN）且双确认；写接口必挂幂等（红线 5）。
	if portal == "internal" && deps.CustomerSvc != nil {
		ch := NewCustomerHandler(deps.CustomerSvc)
		protected.GET("/customers",
			middleware.RequirePerm(perm.M8View),
			ch.ListCustomers,
		)
		protected.GET("/customers/:id/quote-context",
			middleware.RequirePerm(perm.M9View),
			ch.GetQuoteContext,
		)
		protected.POST("/customers/:id/transfer",
			middleware.RequirePerm(perm.M8Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ch.TransferCustomer,
		)
		protected.POST("/customer-quotes",
			middleware.RequirePerm(perm.M9Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ch.GenerateQuote,
		)
		// 业务接口：9b 特价审批 + 刷新 + 导出（09-customer-quote.md §3/§4/§5）。
		// 与 9a 共用 /customer-quotes 路径前缀；/export 是 GET 直返文件流，不挂幂等。
		if deps.CustomerSpecialSvc != nil {
			csh := NewCustomerSpecialHandler(deps.CustomerSpecialSvc, deps.CustomerRefreshSvc, deps.CustomerExportSvc)
			protected.POST("/customer-quotes/:id/special-price",
				middleware.RequirePerm(perm.M9Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				csh.RequestSpecialPrice,
			)
			protected.POST("/customer-quotes/:id/refresh",
				middleware.RequirePerm(perm.M9Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				csh.RefreshQuote,
			)
			protected.GET("/customer-quotes/:id/export",
				middleware.RequirePerm(perm.M9View),
				csh.ExportQuote,
			)
		}
	}

	// 业务接口：报价审批与激活（05-quotes.md §7/§8/§9/§10/§14，内部 M4）。
	// 行级过滤在服务层按 OwnerScope 注入（报价单随供应商归属，设计 §3.2）。
	if portal == "internal" && deps.QuoteApproveSvc != nil {
		qh := NewQuoteHandler(deps.QuoteApproveSvc)
		// 静态路由必须先于 /quotes/:id 注册，避免被路径参数吃掉。
		protected.GET("/quotes/pending", middleware.RequirePerm(perm.M4View), qh.ListPendingQuotes)
		protected.POST("/quotes/activate-due", middleware.RequirePerm(perm.M4Edit), qh.ActivateDueQuotes)
		protected.GET("/quotes/:id/diff", middleware.RequirePerm(perm.M4View), qh.GetQuoteDiff)
		protected.POST("/quotes/:id/approve",
			middleware.RequirePerm(perm.M4Approve),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			qh.ApproveQuote,
		)
		// reject 也挂幂等（契约 §9 已补：409 不等于幂等语义，网络重试应返回首次结果）
		protected.POST("/quotes/:id/reject",
			middleware.RequirePerm(perm.M4Approve),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			qh.RejectQuote,
		)
	}

	// 业务接口：到期闭环/异常检测/特权补录（05-quotes.md §11/§12/§13，内部 M4）。
	// expiring 按 ownerSupplierScope 行级过滤；anomalies 不过滤（§3.2 放开比价决议，5d 裁决 8）。
	if portal == "internal" && deps.QuoteLifecycleSvc != nil {
		lh := NewQuoteLifecycleHandler(deps.QuoteLifecycleSvc, deps.QuoteApproveSvc)
		// 静态路由先于 /quotes/:id 注册
		protected.GET("/quotes/expiring", middleware.RequirePerm(perm.M4View), lh.ListExpiringQuotes)
		protected.POST("/quotes/scan", middleware.RequirePerm(perm.M4Edit), lh.ScanQuotes)
		protected.GET("/quotes/anomalies", middleware.RequirePerm(perm.M4View), lh.ListQuoteAnomalies)
		// retro-effective 集合路径（5d 裁决 1：supplier_id 入请求体，不带路径 id）
		protected.POST("/quotes/retro-effective",
			middleware.RequirePerm(perm.M4Privilege),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			lh.RetroEffectiveQuote,
		)
		protected.POST("/quotes/:id/confirm-remove",
			middleware.RequirePerm(perm.M4Edit),
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			lh.ConfirmRemoveQuote,
		)
	}

	// 业务接口：供应商门户（05-quotes.md §2 可报价 SKU 列表 / §3 提交报价 / §4 历史 / §5 详情）。
	// SUPPLIER 角色零内部权限点，不挂 RequirePerm；只走 AuthN + DataScope，
	// 隔离靠服务层按 supplier_id（登录态解析）硬过滤。
	if portal == "supplier" && deps.SupplierSvc != nil {
		h := NewSupplierHandler(deps.SupplierSvc, deps.SupplierQuoteSvc)
		protected.GET("/skus", h.ListSupplierSKUs)
		if deps.SupplierQuoteSvc != nil {
			protected.POST("/quotes",
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				h.SubmitQuote,
			)
			// 静态路由必须先于 /quotes/:id 注册，避免被路径参数吃掉。
			protected.GET("/quotes/history", h.ListQuoteHistory)
			protected.GET("/quotes/:id", h.GetQuote)
			// 撤回报价（产品反馈 2）：写操作挂幂等（红线 5）。
			// 同段其它路由一样不挂 RequirePerm——SUPPLIER 角色零内部权限点，
			// 隔离靠仓储层 UPDATE ... WHERE supplier_id = ?（登录态解析）。
			protected.POST("/quotes/:id/withdraw",
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				h.WithdrawQuote,
			)
		}
		// 5c 批量导入：模板下载（只读）/ 上传预览（不落库）/ 确认入库（挂幂等）
		if deps.SupplierImportSvc != nil {
			ih := NewSupplierImportHandler(deps.SupplierSvc, deps.SupplierImportSvc)
			protected.GET("/quotes/template", ih.DownloadQuoteTemplate)
			protected.POST("/quotes/import/preview", ih.PreviewQuoteImport)
			protected.POST("/quotes/import/confirm",
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				ih.ConfirmQuoteImport,
			)
		}
		// 新模型申请（供应商侧）：提交挂幂等；查询走行级过滤（仅本主体）。
		// 契约：设计文档 §8.5 接口清单 POST/GET /api/supplier/model-applications。
		{
			ah := NewSupplierApplicationHandler(deps.SupplierSvc)
			protected.POST("/model-applications",
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				ah.SubmitApplication,
			)
			protected.GET("/model-applications", ah.ListApplications)
		}
		// 主体档案（批次 7a）：只读 GET。同段其它路由一样**不挂 RequirePerm**
		// （SUPPLIER 角色零内部权限点），隔离靠身份从登录态解析——
		// subject_operator.id → subject_id → supplier_profile.id。
		// ⚠️ PUT /profile 本批不实现（可写字段在库里无列，见批次 7a 报告 §遗留）。
		if deps.SupplierPortalProfileSvc != nil {
			ph := NewSupplierPortalProfileHandler(deps.SupplierPortalProfileSvc, deps.SupplierSvc)
			protected.GET("/profile", ph.GetPortalProfile)
		}
	}

	// Stage 2：供应商申报（最少单行链路）。
	if portal == "supplier" && deps.SupplyChain != nil {
		// 申报写接口由 B 线的 registerSubmissionRoutes 正式接管
		// （原简化版 POST /submissions 已删除，避免同路径重复注册导致启动 panic）。
		sch := NewSupplyChainHandler(deps.SupplyChain)
		protected.GET("/standard-models", sch.ListStandardModels)
	}

	// 原生审批从工作台进入 /internal/flows/*，复用实际审批事务与指定办理人。
	// 旧 /approvals/*/approve 和 /simulate 保持下线，禁止模拟测试绕过准入。
	if portal == "internal" && deps.SupplyChain != nil {
		RegisterNativeSupplyFlowRoutes(protected, deps.SupplyChain, deps.IdemStore)
		sch := NewSupplyChainHandler(deps.SupplyChain)
		protected.GET("/approvals/submissions", sch.ListApprovals)
		protected.GET("/approvals/history", sch.ListApprovalHistory)
		protected.GET("/supply/suppliers", sch.ListSuppliers)
		// 兼容已有驳回入口；正向审批只注册到原生 flows。
		protected.POST("/approvals/submissions/:id/reject",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			sch.Reject,
		)
	}

	// 客户管理合并页：客户列表 / 客户报价 / 特价审批记录（只读）。
	if portal == "internal" && deps.CustomerWorkspace != nil {
		cwh := NewCustomerWorkspaceHandler(deps.CustomerWorkspace)
		protected.GET("/customer-workspace/customers", middleware.RequirePerm(perm.M8View), cwh.ListCustomers)
		protected.GET("/customer-workspace/quotes", middleware.RequirePerm(perm.M9View), cwh.ListQuotes)
		protected.GET("/customer-workspace/approvals", middleware.RequirePerm(perm.M9View), cwh.ListApprovals)
	}

	// 业务接口：客户门户（09-customer-quote.md §6）。
	// CUSTOMER 角色零内部权限点，不挂 RequirePerm；只走 AuthN + owner_id 行级过滤
	// （OperatorID == customer_id）。字段剔除裁决 6：DTO 物理不含成本/毛利 key，
	// 无需运行时 fieldmask.Apply。
	if portal == "customer" && deps.CustomerPortalSvc != nil {
		ph := NewCustomerPortalHandler(deps.CustomerPortalSvc)
		protected.GET("/price-book", ph.GetPriceBook)
		protected.GET("/quotes", ph.ListQuotes)
		// 接受报价是写操作，挂幂等（CLAUDE.md 红线 5：写操作幂等中间件）。
		protected.POST("/quotes/:id/accept",
			middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
			ph.AcceptQuote,
		)
		protected.GET("/billing", ph.GetBilling)
		protected.GET("/notifications", ph.ListNotifications)
		protected.GET("/home", ph.GetHome)
	}

	// 业务接口：工作台（10-workbench-audit.md §2 待办 + §3 指标卡）。
	// 裁决 10：不挂模块权限点——已认证即可，按角色自动裁剪。
	// PLATFORM_ADMIN 看全部，其他角色看自己的（assignee_id=operator_id）。
	// 成本/毛利剔除：SQL 层不 SELECT，DTO 物理不含 unit_cost/floor_price/margin。
	if portal == "internal" && deps.WorkbenchSvc != nil {
		wh := NewWorkbenchHandler(deps.WorkbenchSvc)
		protected.GET("/workbench/todos", wh.ListTodos)
		protected.GET("/workbench/metrics", wh.ListMetrics)
	}

	// 业务接口：告警处理（10-workbench-audit.md §4）+ 审计日志（§5）。
	// 权限：M12:V（查看）/ M12:E（处理），见 stage10b 裁决 1。
	if portal == "internal" && deps.AlertSvc != nil {
		ah := NewWorkbenchAlertHandler(deps.AlertSvc)
		protected.GET("/alerts", middleware.RequirePerm(perm.M12View), ah.ListAlerts)
		protected.POST("/alerts", middleware.RequirePerm(perm.M12Edit), middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey), ah.HandleAlert)
	}
	if portal == "internal" && deps.AuditSvc != nil {
		uh := NewWorkbenchAuditHandler(deps.AuditSvc)
		protected.GET("/audit-logs", middleware.RequirePerm(perm.M12View), uh.ListAuditLogs)
		protected.GET("/audit-logs/export", middleware.RequirePerm(perm.M12View), uh.ExportAuditLogs)
	}

	// 业务接口：财务中心只读三接口（12-finance.md §1/§2/§3，内部 M10:V，批次 4）。
	// 客户财务账户按 owner_sales_operator_id 做 SELF/DEPT/DEPT_SUB/ALL 行级过滤（红线 7）。
	// 静态段 /finance/accounts 先注册，再注册 /finance/accounts/:customerId。
	if portal == "internal" && deps.FinanceReadSvc != nil {
		fh := NewFinanceHandler(deps.FinanceReadSvc, deps.FinanceLockSvc)
		protected.GET("/finance/fx-rates", middleware.RequirePerm(perm.M10View), fh.ListFxRates)
		protected.GET("/finance/accounts", middleware.RequirePerm(perm.M10View), fh.ListAccounts)
		protected.GET("/finance/accounts/:customerId", middleware.RequirePerm(perm.M10View), fh.GetAccount)
		// 汇率锁定（批次 6）：GET M10:V；POST M10:E + 幂等 + 服务层仅 FINANCE。
		if deps.FinanceLockSvc != nil {
			protected.GET("/finance/fx-lock", middleware.RequirePerm(perm.M10View), fh.GetFxLock)
			protected.POST("/finance/fx-lock",
				middleware.RequirePerm(perm.M10Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				fh.PostFxLock,
			)
		}
	}

	// 业务接口：组织与权限 M11（批次 5 只读 + 2026-10-13 账号管理写侧）。
	// 只读两个挂 M11:V；写侧全部挂 M11:E，且服务层再按角色收敛到 PLATFORM_ADMIN
	// （见 StaffHandler.canManage / pkg/perm.CanManageStaff）。
	// 路径前缀 /org-permissions 由前端写死（src/api/orgPermissions.ts），不要改。
	if portal == "internal" && deps.OrgPermSvc != nil {
		oh := NewOrgPermissionHandler(deps.OrgPermSvc)
		protected.GET("/org-permissions/organizations", middleware.RequirePerm(perm.M11View), oh.ListOrganizations)
		protected.GET("/org-permissions/roles", middleware.RequirePerm(perm.M11View), oh.ListRoles)

		// 账号管理（M11:E + PLATFORM_ADMIN）。写接口必挂幂等（红线 5）。
		if deps.StaffStore != nil {
			sh := NewStaffHandler(deps.OrgPermSvc)
			protected.GET("/org-permissions/staff", middleware.RequirePerm(perm.M11Edit), sh.ListStaff)
			protected.GET("/org-permissions/staff/:id", middleware.RequirePerm(perm.M11Edit), sh.GetStaff)
			protected.POST("/org-permissions/staff",
				middleware.RequirePerm(perm.M11Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				sh.CreateStaff,
			)
			protected.PUT("/org-permissions/staff/:id",
				middleware.RequirePerm(perm.M11Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				sh.UpdateStaff,
			)
			protected.POST("/org-permissions/staff/:id/password",
				middleware.RequirePerm(perm.M11Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				sh.ResetStaffPassword,
			)
			protected.POST("/org-permissions/staff/:id/roles",
				middleware.RequirePerm(perm.M11Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				sh.GrantStaffRole,
			)
			protected.DELETE("/org-permissions/staff/:id/roles/:roleId",
				middleware.RequirePerm(perm.M11Edit),
				middleware.Idempotency(deps.IdemStore, middleware.DefaultBizKey),
				sh.RevokeStaffRole,
			)
		}
	}

	// ---- 重构并行开发：三条线各自的路由文件（各改各的，避免 router.go 冲突） ----
	// 新增路由一律加到对应文件；上面的遗留路由暂不搬，后续按需迁移。
	registerSupplyRoutes(g, protected, portal, deps)       // A：主数据与供应侧
	registerSubmissionRoutes(g, protected, portal, deps)   // B：申报/办理/测试
	registerTradeRoutes(g, protected, portal, deps)        // C：客户/报价/留痕
	registerSupplierAccountRoutes(protected, portal, deps) // 供应商门户：账户设置（M2）
}
