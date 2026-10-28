import { normalizeModelOperationDetail } from "./standardModelPresentation";
import { apiRequest } from "./http";
import { buildModelFilters, validateModelOptions } from "./modelFilters.contract";
import { buildStandardModelSave, normalizeStandardModel, standardModelVersion } from "./standardModels.contract";
import type { StandardModelData, StandardModelDTO, StandardModelPage, StandardModelQuery } from "./standardModels.types";

const base = "/internal/standard-models";
function normalizePage(page: StandardModelPage): StandardModelPage {
  if (!page || !Array.isArray(page.list) || !Number.isSafeInteger(page.total) || page.total < 0 || !Number.isSafeInteger(page.page) || page.page < 1 || !Number.isSafeInteger(page.size) || page.size < 1 || page.size > 100) throw new Error("标准模型列表响应不符合约定，请重新查询。");
  return { ...page, list: page.list.map(model => normalizeStandardModel(model)) };
}
export const standardModelsApi = {
  async options(keyword: string, regions: string[], signal?: AbortSignal) {
    const params = buildModelFilters({ page: 1, size: 20, regions });
    return validateModelOptions(await apiRequest<unknown>({ url: `${base}/filter-options`, params: { keyword, regions: params.regions }, signal }), regions);
  },
  async records(id: string, signal?: AbortSignal): Promise<{ action: string; reason: string; operator_name: string; created_at: string }[]> {
    const response = await apiRequest<{ list: Record<string, unknown>[] }>({ url: `${base}/${standardModelVersion(id)}/operations`, params: { page: 1, size: 100 }, signal });
    if (!response || !Array.isArray(response.list)) throw new Error("操作记录暂时无法加载，请重试。");
    return response.list.map(row => ({ action: String(row.action ?? ""), reason: String(row.reason ?? ""), operator_name: String(row.operator_name ?? ""), created_at: String(row.created_at ?? "") }));
  },
  async operation(id: string, operationID: string, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ url: `${base}/${standardModelVersion(id)}/operations/${standardModelVersion(operationID)}`, signal });
    return normalizeModelOperationDetail(value, id, operationID);
  },
  async list(params: StandardModelQuery, signal?: AbortSignal) {
    return normalizePage(await apiRequest<StandardModelPage>({ url: base, params: buildModelFilters(params), signal }));
  },
  async get(id: string, signal?: AbortSignal) {
    return normalizeStandardModel(await apiRequest<StandardModelDTO>({ url: `${base}/${standardModelVersion(id)}`, signal }), id);
  },
  async versions(id: string, page: number, signal?: AbortSignal) {
    return normalizePage(await apiRequest<StandardModelPage>({ url: `${base}/${standardModelVersion(id)}/versions`, params: { page, size: 20 }, signal }));
  },
  async manualSave(data: StandardModelData, reason: string, lifecycle: string, current?: StandardModelDTO) {
    const body = buildStandardModelSave(data, reason, current?.revision);
    const url = current ? `${base}/${standardModelVersion(current.id)}/manual-completion` : `${base}/manual-completion`;
    return normalizeStandardModel(await apiRequest<StandardModelDTO>({ method: current ? "PUT" : "POST", url, data: body, idempotency: { scope: url, payload: body, lifecycle } }));
  },
};
