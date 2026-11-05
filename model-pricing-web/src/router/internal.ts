import type { RouteRecordRaw } from "vue-router";
import AppLayout from "../layouts/AppLayout.vue";
import StandardModelList from "../views/internal/models/StandardModelList.vue";
import ModelPlaza from "../views/internal/resources/ModelPlaza.vue";
import ModelPlazaDetail from "../views/internal/resources/ModelPlazaDetail.vue";
import ModelApplicationReview from "../views/internal/models/ModelApplicationReview.vue";
import Sync from "../modules/pricing/views/OfficialPriceSync.vue";
import ModulePlaceholderView from "../views/ModulePlaceholderView.vue";
import SupplierQuoteList from "../views/internal/supplierQuotes/SupplierQuoteList.vue";
import AlertList from "../views/internal/alerts/AlertList.vue";
import AuditLogList from "../views/internal/audit/AuditLogList.vue";
import CurrentCostBaselineList from "../views/internal/cost/CurrentCostBaselineList.vue";
import PriceBookList from "../views/internal/pricing/PriceBookList.vue";
import PricingPolicyList from "../views/internal/pricing/PricingPolicyList.vue";
import SupplierWorkspace from "../views/internal/suppliers/SupplierWorkspace.vue";
import SupplierAccessModels from "../views/internal/suppliers/SupplierAccessModels.vue";
import SupplierSubmissionReview from "../views/internal/suppliers/SupplierSubmissionReview.vue";
import SupplierEntityList from "../views/internal/suppliers/SupplierEntityList.vue";
import CustomerWorkspace from "../views/internal/customers/CustomerWorkspace.vue";
import CustomerDetail from "../views/internal/customers/CustomerDetail.vue";
import FinanceCenter from "../views/internal/finance/FinanceCenter.vue";
import InternalWorkbench from "../views/internal/workbench/InternalWorkbench.vue";
import OrgPermissionCenter from "../views/internal/orgPermissions/OrgPermissionCenter.vue";
import IntegrationCenter from "../views/internal/integration/IntegrationCenter.vue";
import { ROUTE_REQUIREMENTS } from "../domain/permissions";

const page = (
  path: string,
  routeKey: string,
  title: string,
  description: string,
  order: number,
): RouteRecordRaw => ({
  path,
  component: ModulePlaceholderView,
  meta: {
    requiresAuth: true,
    portal: "internal",
    routeKey,
    requiredPermission: ROUTE_REQUIREMENTS[routeKey],
    title,
    description,
    menu: true,
    order,
  },
});

export const internalRoutes: RouteRecordRaw = {
  path: "/internal",
  component: AppLayout,
  meta: { requiresAuth: true, portal: "internal" },
  children: [
    { path: "", redirect: "/internal/workbench" },
    {
      path: "workbench",
      component: InternalWorkbench,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.workbench",
        requiredPermission: ROUTE_REQUIREMENTS["internal.workbench"],
        title: "工作台",
        menu: true,
        order: 10,
      },
    },
    {
      path: "models",
      component: StandardModelList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.models",
        requiredPermission: ROUTE_REQUIREMENTS["internal.models"],
        title: "标准模型库",
        menu: true,
        order: 20,
      },
    },
    {
      path: "model-applications",
      component: ModelApplicationReview,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.modelApplications",
        requiredPermission: ROUTE_REQUIREMENTS["internal.modelApplications"],
        title: "模型申请审批",
        menu: false,
        order: 25,
      },
    },
    {
      path: "official-prices",
      component: Sync,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.officialPrices",
        requiredPermission: ROUTE_REQUIREMENTS["internal.officialPrices"],
        title: "官方价格同步",
        menu: false,
        order: 30,
      },
    },
    {
      path: "suppliers",
      component: SupplierWorkspace,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.suppliers",
        requiredPermission: ROUTE_REQUIREMENTS["internal.suppliers"],
        title: "供应商管理",
        menu: true,
        order: 40,
      },
    },
    {
      path: "supplier-entities",
      component: SupplierEntityList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.suppliers",
        requiredPermission: ROUTE_REQUIREMENTS["internal.suppliers"],
        title: "供应商主体档案",
        menu: false,
        order: 51,
      },
    },
    {
      // 申报审批独立页（原型：工作台待办 → 供应商管理 / {供应商} / 申报与审批记录 / {类型}申请）。
      // routeKey 用 internal.workbench（无模块权限点）：审批人只要能看到待办，就必须打得开这一页；
      // 挂 M3:V 会把「有 can_approve_supplier 但没有供应商模块权限」的账号挡在门外。
      path: "submissions/:submissionId",
      component: SupplierSubmissionReview,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.workbench",
        requiredPermission: ROUTE_REQUIREMENTS["internal.workbench"],
        title: "申报审批",
        menu: false,
        order: 53,
      },
    },
    {
      path: "suppliers/:supplierId/models",
      component: SupplierAccessModels,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.suppliers",
        requiredPermission: ROUTE_REQUIREMENTS["internal.suppliers"],
        title: "供应商接入模型",
        menu: false,
        order: 52,
      },
    },
    // 说明：该路由必须保持为独立入口，不要合并进 /internal/suppliers 的 Tab（不要再改成 redirect）。
    // 理由：本路由只要 M4:V，而 /internal/suppliers 要求 M3:V；补录员（RETRO_OP: M4:V + M4:P）
    // 没有 M3:V，合并后会被路由守卫打到 /403，「特权补录」对该角色直接不可达（G1 曾因此回归）。
    // 工作台待办也统一落到这里：待办卡的 routeKey 是 internal.supplierQuotes（M4:V），
    // 若指向供应商管理 Tab，只有 M4:V 的账号点开同样 403。
    // 有 M3:V 的账号另可从「供应商管理 · 供应商报价」Tab 进入嵌入版；两条路径共用 SupplierQuoteList。
    {
      path: "supplier-quotes",
      component: SupplierQuoteList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.supplierQuotes",
        requiredPermission: ROUTE_REQUIREMENTS["internal.supplierQuotes"],
        title: "供应商报价",
        menu: false,
        order: 50,
      },
    },
    {
      path: "cost",
      component: CurrentCostBaselineList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.cost",
        requiredPermission: ROUTE_REQUIREMENTS["internal.cost"],
        title: "成本管理",
        menu: false,
        order: 60,
      },
    },
    {
      path: "pricing/policies",
      component: PricingPolicyList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.pricingPolicies",
        requiredPermission: ROUTE_REQUIREMENTS["internal.pricingPolicies"],
        title: "定价策略",
        menu: false,
        order: 70,
      },
    },
    {
      path: "price-books",
      component: PriceBookList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.priceBooks",
        requiredPermission: ROUTE_REQUIREMENTS["internal.priceBooks"],
        title: "价目表",
        menu: false,
        order: 80,
      },
    },
    {
      path: "customers",
      component: CustomerWorkspace,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.customers",
        requiredPermission: ROUTE_REQUIREMENTS["internal.customers"],
        title: "客户管理",
        menu: true,
        order: 70,
      },
    },
    {
      // 客户详情整页（B3b）：routeKey **复用 internal.customers**（M8:V），
      // 不新增 routeKey —— 新增要同步 ROUTE_REQUIREMENTS，漏配会变成"无权限直接 403"。
      path: "customers/:id",
      component: CustomerDetail,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.customers",
        requiredPermission: ROUTE_REQUIREMENTS["internal.customers"],
        title: "客户详情",
        menu: false,
      },
    },
    {
      path: "finance",
      component: FinanceCenter,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.finance",
        requiredPermission: ROUTE_REQUIREMENTS["internal.finance"],
        title: "财务信息",
        menu: false,
        order: 100,
      },
    },
    {
      path: "alerts",
      component: AlertList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.alerts",
        requiredPermission: ROUTE_REQUIREMENTS["internal.alerts"],
        title: "告警中心",
        menu: false,
        order: 110,
      },
    },
    {
      path: "audit",
      component: AuditLogList,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.audit",
        requiredPermission: ROUTE_REQUIREMENTS["internal.audit"],
        title: "审计日志",
        menu: false,
        order: 120,
      },
    },
    {
      path: "org-permissions",
      component: OrgPermissionCenter,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.orgPermissions",
        requiredPermission: ROUTE_REQUIREMENTS["internal.orgPermissions"],
        title: "组织与权限",
        menu: false,
        order: 130,
      },
    },
    {
      path: "integration",
      component: IntegrationCenter,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.integration",
        requiredPermission: ROUTE_REQUIREMENTS["internal.integration"],
        title: "系统对接",
        menu: false,
        order: 140,
      },
    },
    {
      path: "model-plaza",
      component: ModelPlaza,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.modelPlaza",
        requiredPermission: ROUTE_REQUIREMENTS["internal.modelPlaza"],
        title: "模型广场",
        description: "按标准模型查看供应来源、采购系数与资源状态",
        menu: true,
        order: 30,
      },
    },
    {
      path: "model-detail/:id",
      component: ModelPlazaDetail,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.modelPlaza",
        requiredPermission: ROUTE_REQUIREMENTS["internal.modelPlaza"],
        title: "模型详情",
        description: "供应来源、测试指标与资源状态",
        menu: false,
        order: 32,
      },
    },
    {
      path: "model-plaza/:resourceId",
      component: ModelPlaza,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.modelPlaza",
        requiredPermission: ROUTE_REQUIREMENTS["internal.modelPlaza"],
        title: "模型资源详情",
        description: "打开工作台待办关联的资源计划",
        menu: false,
        order: 31,
      },
    },
    // 报价单已并入「客户管理」，保留路由兼容旧链接，不再出现在左菜单
    {
      path: "quotes",
      component: ModulePlaceholderView,
      meta: {
        requiresAuth: true,
        portal: "internal",
        routeKey: "internal.quotes",
        requiredPermission: ROUTE_REQUIREMENTS["internal.quotes"],
        title: "报价单",
        menu: false,
        order: 60,
      },
    },
  ],
};
