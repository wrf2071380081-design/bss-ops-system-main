import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import { buildResourceQuery, resourceRegionLabels, validateResourceGroup, validateResourceRow } from "./resources.contract";
import type { ResourceFilterOptions, ResourceGroup, ResourcePage, ResourceQuery, ResourceRow } from "./resources.types";

function page<T>(value: ResourcePage<unknown>, expected: ResourceQuery, validate: (row: unknown) => T): ResourcePage<T> {
  if (!value || value.view !== expected.view || !Array.isArray(value.list) || !Number.isSafeInteger(value.total) || value.total < value.list.length || value.page !== expected.page || value.size !== expected.size || value.list.length > value.size) throw new Error("资源列表响应不符合约定，请重新查询。");
  return { ...value, list: value.list.map(row => validate(row)) };
}
export const resourcesApi = {
  async groups(query: Partial<ResourceQuery>, signal?: AbortSignal): Promise<ResourcePage<ResourceGroup>> {
    const params = buildResourceQuery({ ...query, view: "grouped" });
    return page(await apiRequest<ResourcePage<unknown>>({ url: "/internal/resources", params, signal }), params, validateResourceGroup);
  },
  async rows(query: ResourceQuery, signal?: AbortSignal): Promise<ResourcePage<ResourceRow>> {
    const params = buildResourceQuery({ ...query, view: "flat" });
    const result = page(await apiRequest<ResourcePage<unknown>>({ url: "/internal/resources", params, signal }), params, validateResourceRow);
    if (result.list.some(row => (params.standard_model_id && row.standard_model_id !== params.standard_model_id) || (params.region && row.region !== params.region) || (params.supplier_id && row.supplier_id !== params.supplier_id))) throw new Error("来源响应与筛选范围不一致，请重新查询。");
    return result;
  },
  async get(id: string, region: string, signal?: AbortSignal): Promise<ResourceRow> {
    const result = validateResourceRow(await apiRequest<unknown>({ url: `/internal/resources/${standardModelVersion(id)}`, params: { region }, signal }), id);
    if (region && result.region !== region) throw new Error("资源详情与所选区域不一致，请重新查询。");
    return result;
  },
  async options(keyword: string, region: string, signal?: AbortSignal): Promise<ResourceFilterOptions> {
    const result = await apiRequest<ResourceFilterOptions>({ url: "/internal/resources/filter-options", params: { keyword, region }, signal });
    if (!result || !Array.isArray(result.companies) || result.companies.some(name => typeof name !== "string") || !Array.isArray(result.suppliers) || typeof result.has_more !== "boolean" || result.companies.length > 50 || result.suppliers.length > 50) throw new Error("筛选选项暂时无法加载，请重试。");
    for (const supplier of result.suppliers) { standardModelVersion(supplier.id); if (typeof supplier.name !== "string" || !supplier.name.trim() || !resourceRegionLabels[supplier.region] || (region && region !== supplier.region)) throw new Error("供应商选项与区域不一致，请重试。"); }
    return result;
  },
};
