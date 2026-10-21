import { modelTypeLabels, standardModelVersion } from "./standardModels.contract.ts";
import type { ModelFilterOptions, StandardModelQuery } from "./standardModels.types.ts";
function invalid(): never { throw new Error("模型筛选条件或选项不符合约定，请重新加载。"); }
function values(source: unknown, validate: (value: string) => boolean): string[] {
  if (!Array.isArray(source) || source.length > 50) invalid();
  const list = source.map(value => { if (typeof value !== "string" || !value.trim() || !validate(value.trim())) invalid(); return value.trim(); });
  if (new Set(list).size !== list.length) invalid();
  return list;
}
export function buildModelFilters(query: StandardModelQuery): Record<string, string | number | undefined> {
  const { companies, model_types, supplier_ids, regions, ...base } = query;
  if ((base.company && companies?.length) || (base.model_type && model_types?.length)) invalid();
  const result: Record<string, string | number | undefined> = { ...base };
  const specs: [string, unknown, (value: string) => boolean][] = [
    ["companies", companies, value => [...value].length <= 200], ["model_types", model_types, value => !!modelTypeLabels[value]],
    ["supplier_ids", supplier_ids, value => { standardModelVersion(value); return true; }], ["regions", regions, value => ["CN", "OVERSEAS"].includes(value)],
  ];
  for (const [key, source, validate] of specs) if (source !== undefined) { const list = values(source, validate); if (list.length) result[key] = JSON.stringify(list); }
  return result;
}
export function validateModelOptions(value: unknown, regions: string[]): ModelFilterOptions {
  if (!value || typeof value !== "object") invalid();
  const result = value as ModelFilterOptions;
  const companies = values(result.companies, value => [...value].length <= 200);
  if (!Array.isArray(result.suppliers) || result.suppliers.length > 50 || typeof result.has_more !== "boolean") invalid();
  const seen = new Set<string>();
  const suppliers = result.suppliers.map(supplier => {
    if (!supplier || typeof supplier.name !== "string" || !supplier.name.trim() || !["CN", "OVERSEAS"].includes(supplier.region) || (regions.length && !regions.includes(supplier.region))) invalid();
    standardModelVersion(supplier.id); if (seen.has(supplier.id)) invalid(); seen.add(supplier.id); return { ...supplier };
  });
  return { companies, suppliers, has_more: result.has_more };
}
