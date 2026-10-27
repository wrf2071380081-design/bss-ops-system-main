import { standardModelVersion } from "./standardModels.contract.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";

export const supplierOperationCategories = ["supplier", "supplier_qualification", "supplier_formal_config", "contract", "credit_line", "resource", "plan"] as const;
export type SourceOperationType = (typeof supplierOperationCategories)[number];
export const supplierOperationCategoryLabels: Record<SourceOperationType, string> = {
  supplier: "主体档案", supplier_qualification: "资质", supplier_formal_config: "正式配置", contract: "合同", credit_line: "授信", resource: "资源", plan: "计划",
};
export interface SourceOperation {
  id: string; entity_type: SourceOperationType; entity_id: string;
  resource_id: string; supplier_id: string; supplier_name: string; supplier_model_id: string;
  region: "CN" | "OVERSEAS"; action: string; operator_name: string; reason: string; created_at: string;
}
export interface SourceOperationPage { list: SourceOperation[]; total: number; page: number; size: number }
function invalid(): never { throw new Error("供应来源操作记录响应不符合约定，请重新加载。"); }
export function validateSourceOperations(value: unknown, page: number, size: number, expectedSupplierID?: string): SourceOperationPage {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid();
  const data = value as SourceOperationPage;
  if (!Array.isArray(data.list) || data.page !== page || data.size !== size || !Number.isSafeInteger(data.total) || data.total < data.list.length || data.list.length > size || ((page - 1) * size >= data.total && data.list.length)) invalid();
  const seen = new Set<string>();
  const list = data.list.map(row => {
    if (!row || typeof row !== "object") invalid();
    for (const key of ["id", "entity_id", "supplier_id"] as const) standardModelVersion(row[key]);
    const resourceOperation = row.entity_type === "resource" || row.entity_type === "plan";
    if (resourceOperation) standardModelVersion(row.resource_id);
    else if (!expectedSupplierID || row.resource_id !== "" || row.supplier_model_id !== "") invalid();
    if (seen.has(row.id) || !supplierOperationCategories.includes(row.entity_type) || (row.entity_type === "resource" && row.entity_id !== row.resource_id) ||
      (row.entity_type === "supplier" && row.entity_id !== row.supplier_id) || !["CN", "OVERSEAS"].includes(row.region) || (expectedSupplierID && row.supplier_id !== expectedSupplierID)) invalid();
    for (const key of ["supplier_name", "supplier_model_id", "action", "operator_name", "reason", "created_at"] as const) if (typeof row[key] !== "string") invalid();
    if (!row.action || !Number.isFinite(Date.parse(row.created_at))) invalid();
    seen.add(row.id);
    // Explicit safe projection: never retain provider snapshots or credentials.
    return { id: row.id, entity_type: row.entity_type, entity_id: row.entity_id, resource_id: row.resource_id, supplier_id: row.supplier_id, supplier_name: row.supplier_name, supplier_model_id: row.supplier_model_id, region: row.region, action: row.action, operator_name: row.operator_name, reason: row.reason, created_at: row.created_at };
  });
  return { list, total: data.total, page, size };
}
export function assertPlanContext(plan: PlanSummary, resourceID?: string, region?: string): PlanSummary {
  if ((resourceID && plan.resource_id !== resourceID) || (region && plan.region !== region)) throw new Error("该计划不属于当前供应资源，请核对链接后重新打开。");
  return plan;
}
export function sourceOperationLocation(row: SourceOperation): { path: string; query: Record<string, string> } {
  if (row.entity_type !== "resource" && row.entity_type !== "plan") {
    return { path: `/internal/suppliers/${standardModelVersion(row.supplier_id)}/models`, query: { tab: "profile", section: row.entity_type } };
  }
  return { path: `/internal/model-plaza/${standardModelVersion(row.resource_id)}`, query: { region: row.region, tab: row.entity_type === "plan" ? "plans" : "records", ...(row.entity_type === "plan" ? { plan_id: standardModelVersion(row.entity_id) } : {}) } };
}

export function supplierOperationLabel(row: SourceOperation) {
  const actions: Record<string, string> = {
    CREATE: "新建", UPDATE: "维护", UPDATE_NOOP: "维护（无变更）", DISABLE: "停用", DISABLE_NOOP: "停用（无变更）",
    UPLOAD: "上传", ARCHIVE: "归档", REVISE: "修订", CANCEL: "取消", EXECUTE: "登记实际完成", BLOCK: "执行阻断",
    ADMIT: "登记接入", BIND_FORMAL_CONFIG: "绑定正式配置", ONLINE: "上架", OFFLINE: "下架", CHANGE_COEFFICIENT: "批准改价", CHANGE_COEFFICIENT_NOOP: "同价确认", APPLY_APPROVED_PRICE: "应用批准改价",
    CONFIG_COMPLETE:'登记配置完成',CANCEL_ACCESS:'取消接入',
  };
  const action = actions[row.action] || "业务变更";
  return `${supplierOperationCategoryLabels[row.entity_type]} · ${action}`;
}
