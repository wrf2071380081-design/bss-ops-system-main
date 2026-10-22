import { standardModelVersion } from "./standardModels.contract.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";

export interface SourceOperation {
  id: string; entity_type: "resource" | "plan"; entity_id: string;
  resource_id: string; supplier_id: string; supplier_name: string; supplier_model_id: string;
  region: "CN" | "OVERSEAS"; action: string; operator_name: string; reason: string; created_at: string;
}
export interface SourceOperationPage { list: SourceOperation[]; total: number; page: number; size: number }
function invalid(): never { throw new Error("供应来源操作记录响应不符合约定，请重新加载。"); }
export function validateSourceOperations(value: unknown, page: number, size: number): SourceOperationPage {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid();
  const data = value as SourceOperationPage;
  if (!Array.isArray(data.list) || data.page !== page || data.size !== size || !Number.isSafeInteger(data.total) || data.total < data.list.length || data.list.length > size || ((page - 1) * size >= data.total && data.list.length)) invalid();
  const seen = new Set<string>();
  const list = data.list.map(row => {
    if (!row || typeof row !== "object") invalid();
    for (const key of ["id", "entity_id", "resource_id", "supplier_id"] as const) standardModelVersion(row[key]);
    if (seen.has(row.id) || !["resource", "plan"].includes(row.entity_type) || (row.entity_type === "resource" && row.entity_id !== row.resource_id) || !["CN", "OVERSEAS"].includes(row.region)) invalid();
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
export function sourceOperationLocation(row: SourceOperation) {
  return { path: `/internal/model-plaza/${standardModelVersion(row.resource_id)}`, query: { region: row.region, tab: row.entity_type === "plan" ? "plans" : "records", ...(row.entity_type === "plan" ? { plan_id: standardModelVersion(row.entity_id) } : {}) } };
}
