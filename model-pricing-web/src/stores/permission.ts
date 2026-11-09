import { defineStore } from "pinia";
import { ref } from "vue";
import { ROUTE_REQUIREMENTS, ROLE_REQUIREMENTS } from "../domain/permissions";

export const usePermissionStore = defineStore("permission", () => {
  const actionPermissionSet = ref<Set<string>>(new Set());
  const routePermissionSet = ref<Set<string>>(new Set());

  function rebuild(permissionCodes: string[], roleCodes: string[] = []) {
    actionPermissionSet.value = new Set(permissionCodes);
    // 角色门槛（AND）：ROLE_REQUIREMENTS 命中的 routeKey，除权限点外还须持有指定角色 code。
    // 账号管理只有 PLATFORM_ADMIN 能进——LEADER 持全部 60 个权限点（含 M11:E），
    // 只按权限点放行会让许澄 / 林舟看到菜单却整页 403。
    const roleGatePassed = (routeKey: string) => {
      const requiredRole = ROLE_REQUIREMENTS[routeKey];
      return !requiredRole || roleCodes.includes(requiredRole);
    };
    routePermissionSet.value = new Set(
      Object.entries(ROUTE_REQUIREMENTS)
        .filter(
          ([routeKey, requiredPermission]) =>
            roleGatePassed(routeKey) &&
            (requiredPermission === null ||
              actionPermissionSet.value.has(requiredPermission) ||
              (routeKey === "internal.priceBooks" && roleCodes.includes("FINANCE"))),
        )
        .map(([routeKey]) => routeKey),
    );
  }

  function clear() {
    actionPermissionSet.value = new Set();
    routePermissionSet.value = new Set();
  }

  const canAction = (permission: string) =>
    actionPermissionSet.value.has(permission);
  const canRoute = (routeKey: string) => routePermissionSet.value.has(routeKey);

  return {
    actionPermissionSet,
    routePermissionSet,
    rebuild,
    clear,
    canAction,
    canRoute,
  };
});
