import type { RouteRecordRaw } from "vue-router";
import SupplierLayout from "../layouts/SupplierLayout.vue";
import SupplierPortalHome from "../views/supplier/SupplierPortalHome.vue";
import SupplierModels from "../views/supplier/SupplierModels.vue";
import SupplierQuoteList from "../views/supplier/SupplierQuoteList.vue";
import SupplierQuoteNew from "../views/supplier/SupplierQuoteNew.vue";
import SupplierQuoteImport from "../views/supplier/SupplierQuoteImport.vue";
import SupplierModelApplications from "../views/supplier/SupplierModelApplications.vue";
import SupplierAccountSettings from "../views/supplier/SupplierAccountSettings.vue";
import SupplierSubmissionNew from "../views/supplier/SupplierSubmissionNew.vue";
import SupplierSubmissionSimple from "../views/supplier/SupplierSubmissionSimple.vue";
import SupplierQualifications from "../views/supplier/SupplierQualifications.vue";
import SupplierReconciliation from "../views/supplier/SupplierReconciliation.vue";

export const supplierRoutes: RouteRecordRaw = {
  path: "/supplier",
  component: SupplierLayout,
  meta: { requiresAuth: true, portal: "supplier" },
  children: [
    { path: "", redirect: "/supplier/home" },
    { path: "quotes/:id/renew", component: SupplierQuoteNew, meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.quoteNew", title: "续报新版本", menu: false } },

    // 原型 IA：申报 / 模型管理 / 账户设置
    {
      path: "home",
      component: SupplierPortalHome,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.home", title: "申报", menu: true, order: 10 },
    },
    {
      path: "models",
      component: SupplierModels,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.models", title: "模型管理", menu: true, order: 20 },
    },
    {
      path: "profile",
      component: SupplierAccountSettings,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.profile", title: "账户设置", menu: true, order: 30 },
    },
    {
      path: "submissions/simple",
      component: SupplierSubmissionSimple,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.submissionSimple", title: "发起申报", menu: false, order: 36 },
    },
    {
      path: "submissions/new",
      component: SupplierSubmissionNew,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.submissionNew", title: "发起申报", menu: false, order: 35 },
    },

    // 旧报价路由保留（深链/按钮跳转用），不进左侧菜单
    {
      path: "quotes",
      component: SupplierQuoteList,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.quotes", title: "报价历史", menu: false, order: 40 },
    },
    {
      path: "quotes/new",
      component: SupplierQuoteNew,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.quoteNew", title: "新建报价", menu: false, order: 50 },
    },
    {
      path: "quotes/import",
      component: SupplierQuoteImport,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.quoteImport", title: "批量导入", menu: false, order: 60 },
    },
    {
      path: "model-applications",
      component: SupplierModelApplications,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.modelApplications", title: "新模型申请", menu: false, order: 70 },
    },
    {
      path: "qualifications",
      component: SupplierQualifications,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.qualifications", title: "资质管理", menu: false, order: 80 },
    },
    {
      path: "reconciliation",
      component: SupplierReconciliation,
      meta: { requiresAuth: true, portal: "supplier", routeKey: "supplier.reconciliation", title: "结算与对账", menu: false, order: 90 },
    },
  ],
};
