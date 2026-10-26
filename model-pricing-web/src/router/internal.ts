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
import SupplierEntityList from "../views/internal/suppliers/SupplierEntityList.vue";
import CustomerWorkspace from "../views/internal/customers/CustomerWorkspace.vue";
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
