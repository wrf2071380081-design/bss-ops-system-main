import Decimal from "decimal.js";
import { filterSources as filterSourceRanges, type SourceFilters as RangeFilters } from "../api/sourceFilters.ts";
import type { ResourceGroup, ResourceRow, ResourceQuery } from "../api/resources.types.ts";

export interface SourceFilters {
  keyword: string; companies: string[]; model_types: string[]; supplier_ids: string[]; regions: string[]; states: string[];
  coefficient_min: string; coefficient_max: string; connection_min: string; connection_max: string;
  rps_min: string; rps_max: string; tps_min: string; tps_max: string;
}
export const emptySourceFilters = (): SourceFilters => ({ keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], states: [], coefficient_min: "", coefficient_max: "", connection_min: "", connection_max: "", rps_min: "", rps_max: "", tps_min: "", tps_max: "" });
export const rangeFields = ["coefficient", "connection", "rps", "tps"] as const;
export const rangeLabels = { coefficient: "采购系数", connection: "连通耗时 · ms", rps: "压力 RPS", tps: "压力 TPS" };
function rangeFilters(filters: SourceFilters): RangeFilters {
  return { supplierIDs: filters.supplier_ids, states: filters.states, ranges: {
    coefficient: { min: filters.coefficient_min, max: filters.coefficient_max },
    connection_ms: { min: filters.connection_min, max: filters.connection_max },
    rps: { min: filters.rps_min, max: filters.rps_max }, tps: { min: filters.tps_min, max: filters.tps_max },
  } };
}
export function validateSourceFilters(filters: SourceFilters) { filterSourceRanges([], rangeFilters(filters)); }
export function filterSources(rows: ResourceRow[], filters: SourceFilters) {
  const match = (selected: string[], value: string) => !selected.length || selected.includes(value);
  const keyword = filters.keyword.trim().toLocaleLowerCase();
  return filterSourceRanges(rows, rangeFilters(filters)).filter(row =>
    match(filters.companies, row.company) && match(filters.model_types, row.model_type) && match(filters.regions, row.region) &&
    (!keyword || [row.model_name, row.official_id, row.supplier_name, row.supplier_model_id].join(" ").toLocaleLowerCase().includes(keyword)));
}
export function compareValues(a: string | number | null | undefined, b: string | number | null | undefined, order: "asc" | "desc", numeric = false) {
  const emptyA = a == null || a === "", emptyB = b == null || b === "";
  if (emptyA || emptyB) return emptyA === emptyB ? 0 : emptyA ? 1 : -1;
  const result = numeric ? new Decimal(a).comparedTo(b) : String(a).localeCompare(String(b), "zh-CN", { numeric: true });
  return result * (order === "asc" ? 1 : -1);
}
export interface PlazaGroup extends ResourceGroup { state_label: string }
export function aggregateSources(rows: ResourceRow[]): PlazaGroup[] {
  const groups = new Map<string, { group: PlazaGroup; suppliers: Set<string> }>();
  for (const row of rows) {
    if (!groups.has(row.standard_model_id)) groups.set(row.standard_model_id, { group: { id: row.standard_model_id, name: row.model_name, official_id: row.official_id, company: row.company, model_type: row.model_type, supplier_count: 0, resource_count: 0, available_count: 0, coefficient_min: row.coefficient, coefficient_max: row.coefficient, regions: [], state_counts: {}, state_label: "" }, suppliers: new Set() });
    const { group, suppliers } = groups.get(row.standard_model_id)!;
    suppliers.add(row.supplier_id); group.supplier_count = suppliers.size; group.resource_count++;
    if (row.quote_readiness.eligible) group.available_count++;
    group.state_counts[row.state] = (group.state_counts[row.state] || 0) + 1;
    if (!group.regions.includes(row.region)) group.regions.push(row.region);
    if (new Decimal(row.coefficient).lt(group.coefficient_min)) group.coefficient_min = row.coefficient;
    if (new Decimal(row.coefficient).gt(group.coefficient_max)) group.coefficient_max = row.coefficient;
  }
  return [...groups.values()].map(({ group }) => ({ ...group, regions: group.regions.sort(), state_label: group.state_counts.ADMITTED ? "有可用资源" : group.state_counts.PENDING_TEST || group.state_counts.PENDING_ACCESS ? "资源待接入" : "无可用资源" }));
}

// Serialize compound filters through model and supplier return links.
export function encodeSourceFilters(filters: SourceFilters) { return JSON.stringify(filters); }
export function decodeSourceFilters(value: unknown): SourceFilters {
  const result = emptySourceFilters();
  if (typeof value !== "string" || !value) return result;
  const parsed = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("筛选参数无效。");
  for (const key of Object.keys(result) as (keyof SourceFilters)[]) {
    const value = parsed[key];
    if (value === undefined) continue;
    if (Array.isArray(result[key])) {
      if (!Array.isArray(value) || value.some(item => typeof item !== "string")) throw new Error("筛选参数无效。");
      (result[key] as string[]) = [...new Set(value)] as string[];
    } else {
      if (typeof value !== "string") throw new Error("筛选参数无效。");
      (result[key] as string) = value;
    }
  }
  validateSourceFilters(result);
  return result;
}

export function sourceResourceQuery(filters: SourceFilters, page: number, size: number, sort_by: string, order: string, modelID?: string): ResourceQuery {
  validateSourceFilters(filters);
  return { ...filters, page, size, sort_by, order, view: modelID ? "flat" : "grouped", ...(modelID ? { standard_model_id: modelID } : {}) };
}
