import type { Pinia } from "pinia";
import {
  createRouter,
  createWebHashHistory,
  type RouteRecordRaw,
} from "vue-router";
import ModulePlaceholderView from "../views/ModulePlaceholderView.vue";
import LoginView from "../views/LoginView.vue";
import { installRouterGuards } from "./guards";
import { internalRoutes } from "./internal";
import { supplierRoutes } from "./supplier";
import type { PermissionCode } from "../domain/permissions";
import type { PortalType } from "../stores/session";

declare module "vue-router" {
  interface RouteMeta {
    requiresAuth?: boolean;
    requiresApproval?: boolean;
    portal?: PortalType;
    routeKey?: string;
    requiredPermission?: PermissionCode | null;
    title?: string;
    description?: string;
    menu?: boolean;
    order?: number;
    legacyMock?: boolean;
  }
}

const routes: RouteRecordRaw[] = [
  { path: "/", redirect: "/internal/workbench" },
  { path: "/models", redirect: "/internal/models" },
  { path: "/sync", redirect: "/internal/official-prices" },
  // 客户自助门户已下线（2026-10-30 产品裁决：平台不提供客户登录）。
  // 账号置 SUSPENDED + features.customer_portal=false + 前端不挂 /customer 路由，三处一致。
  // 要恢复：这里加回 customerRoutes，后端 features.customer_portal 置 true，账号改回 ACTIVE。
  ...([internalRoutes, supplierRoutes] as RouteRecordRaw[]),
  {
    path: "/internal/login",
    component: LoginView,
    meta: { title: "用户登录", portal: "internal" },
  },
  {
    path: "/supplier/login",
    component: LoginView,
    meta: { title: "供应商登录", portal: "supplier" },
  },
  {
    path: "/403",
    component: ModulePlaceholderView,
    meta: { title: "无权访问", description: "当前身份没有访问该页面的功能权限。" },
  },
  {
    path: "/configuration-error",
    component: ModulePlaceholderView,
    meta: { title: "配置错误", description: "非开发构建禁止关闭认证，请检查 VITE_AUTH_ENABLED。" },
  },
  {
    path: "/:pathMatch(.*)*",
    component: ModulePlaceholderView,
    meta: { title: "页面不存在", description: "请检查访问地址或从门户菜单重新进入。" },
  },
];

export function createAppRouter(pinia: Pinia) {
  const router = createRouter({ history: createWebHashHistory(), routes });
  installRouterGuards(router, pinia);
  return router;
}
