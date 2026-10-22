import Decimal from "decimal.js";
import type { ResourceRow } from "./resources.types";
export const sourceRanges = [
  { key: "coefficient", label: "采购系数" }, { key: "connection_ms", label: "连通耗时 · ms" },
  { key: "rps", label: "压力 RPS" }, { key: "tps", label: "压力 TPS" },
] as const;
export type SourceRangeKey = typeof sourceRanges[number]["key"];
export interface SourceFilters { supplierIDs: string[]; states: string[]; ranges: Record<SourceRangeKey, { min: string; max: string }> }
export function emptySourceFilters(): SourceFilters {
  return { supplierIDs: [], states: [], ranges: { coefficient: { min: "", max: "" }, connection_ms: { min: "", max: "" }, rps: { min: "", max: "" }, tps: { min: "", max: "" } } };
}
export function filterSources(rows: ResourceRow[], filters: SourceFilters) {
  const ranges = sourceRanges.map(({ key, label }) => {
    const input = filters.ranges[key];
    const parse = (value: string) => {
      if (!value.trim()) return undefined;
      if (!/^\d+(?:\.\d+)?$/.test(value.trim())) throw new Error(`${label}请输入非负数字。`);
      const number = new Decimal(value.trim());
      if (!number.isFinite() || number.lt(0) || (key === "coefficient" && number.gt(1))) throw new Error(`${label}范围不正确。`);
      return number;
    };
    const min = parse(input.min), max = parse(input.max);
    if (min && max && min.gt(max)) throw new Error(`${label}下限不能大于上限。`);
    return { key, min, max };
  });
  return rows.filter(row => (!filters.supplierIDs.length || filters.supplierIDs.includes(row.supplier_id)) &&
    (!filters.states.length || filters.states.includes(row.state)) && ranges.every(({ key, min, max }) => {
      if (!min && !max) return true;
      const value = key === "coefficient" ? row.coefficient : row.test.status === "COMPLETED" ? row.test.metrics[key] : null;
      if (value === null) return false;
      const number = new Decimal(value);
      return (!min || number.gte(min)) && (!max || number.lte(max));
    }));
}
