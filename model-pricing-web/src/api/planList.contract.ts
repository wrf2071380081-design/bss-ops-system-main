import { validatePlanSummary, planKindLabels, planStatusLabels } from "./supplyWorkbench.contract.ts";
import { standardModelVersion } from "./standardModels.contract.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";
export interface PlanQuery { resource_id: string; region: string; status?: string; kind?: string; page: number; size: number }
export interface PlanPage { list: PlanSummary[]; total: number; page: number; size: number }
export function validatePlanQuery(query: PlanQuery): PlanQuery {
  standardModelVersion(query.resource_id);
  if (!["CN", "OVERSEAS"].includes(query.region) || (query.status && !planStatusLabels[query.status]) || (query.kind && !planKindLabels[query.kind]) || !Number.isSafeInteger(query.page) || query.page < 1 || !Number.isSafeInteger(query.size) || query.size < 1 || query.size > 100 || query.page > Math.floor(Number.MAX_SAFE_INTEGER / query.size)) throw new Error("计划筛选或分页参数不正确。");
  return { ...query };
}
export function validatePlanPage(value: unknown, query: PlanQuery): PlanPage {
  validatePlanQuery(query);
  const page = value as PlanPage;
  const invalid = () => { throw new Error("计划列表响应不符合所选范围，请重新加载。"); };
  if (!page || !Array.isArray(page.list) || page.page !== query.page || page.size !== query.size || !Number.isSafeInteger(page.total) || page.total < page.list.length || page.list.length > query.size || ((query.page - 1) * query.size >= page.total && page.list.length)) invalid();
  const seen = new Set<string>();
  const list = page.list.map(value => {
    if (!value || typeof value.id !== "string") invalid();
    const plan = validatePlanSummary(value, value.id);
    if (seen.has(plan.id) || plan.resource_id !== query.resource_id || plan.region !== query.region || (query.status && plan.status !== query.status) || (query.kind && plan.kind !== query.kind)) invalid();
    seen.add(plan.id); return plan;
  });
  return { list, total: page.total, page: query.page, size: query.size };
}
