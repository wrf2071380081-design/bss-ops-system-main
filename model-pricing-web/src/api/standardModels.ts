import { apiRequest } from "./http";
import { buildStandardModelSave, normalizeStandardModel, standardModelVersion } from "./standardModels.contract";
import type { StandardModelData, StandardModelDTO, StandardModelPage, StandardModelQuery } from "./standardModels.types";

const base = "/internal/standard-models";
function normalizePage(page: StandardModelPage): StandardModelPage {
  if (!page || !Array.isArray(page.list) || !Number.isSafeInteger(page.total) || page.total < 0 || !Number.isSafeInteger(page.page) || page.page < 1 || !Number.isSafeInteger(page.size) || page.size < 1 || page.size > 100) throw new Error("标准模型列表响应不符合约定，请重新查询。");
  return { ...page, list: page.list.map(model => normalizeStandardModel(model)) };
}
export const standardModelsApi = {
  async records(id: string, signal?: AbortSignal): Promise<{ action: string; reason: string; operator_id: string; request_id: string; occurred_at: string }[]> {
    const response = await apiRequest<{ list: Record<string, unknown>[] }>({ url: "/internal/operation-logs", params: { entityType: "standard_model", entityId: standardModelVersion(id), page: 1, size: 100 }, signal });
    if (!response || !Array.isArray(response.list)) throw new Error("操作记录暂时无法加载，请重试。");
    return response.list.map(row => ({ action: String(row.action ?? ""), reason: String(row.reason ?? ""), operator_id: String(row.operator_id ?? ""), request_id: String(row.request_id ?? ""), occurred_at: String(row.occurred_at ?? row.created_at ?? "") }));
  },
  async list(params: StandardModelQuery, signal?: AbortSignal) {
    return normalizePage(await apiRequest<StandardModelPage>({ url: base, params, signal }));
  },
  async get(id: string, signal?: AbortSignal) {
    return normalizeStandardModel(await apiRequest<StandardModelDTO>({ url: `${base}/${standardModelVersion(id)}`, signal }), id);
  },
  async versions(id: string, page: number, signal?: AbortSignal) {
    return normalizePage(await apiRequest<StandardModelPage>({ url: `${base}/${standardModelVersion(id)}/versions`, params: { page, size: 20 }, signal }));
  },
  async save(data: StandardModelData, reason: string, lifecycle: string, current?: StandardModelDTO) {
    const body = buildStandardModelSave(data, reason, current?.revision);
    const url = current ? `${base}/${standardModelVersion(current.id)}` : base;
    return normalizeStandardModel(await apiRequest<StandardModelDTO>({ method: current ? "PUT" : "POST", url, data: body, idempotency: { scope: url, payload: body, lifecycle } }));
  },
  async verify(current: StandardModelDTO, reason: string, lifecycle: string) {
    if (!reason.trim()) throw new Error("请填写核验说明。");
    const url = `${base}/${standardModelVersion(current.id)}/verify`;
    const body = { expected_revision: standardModelVersion(current.revision), checked_at: current.checked_at, reason: reason.trim() };
    return normalizeStandardModel(await apiRequest<StandardModelDTO>({ method: "POST", url, data: body, idempotency: { scope: url, payload: body, lifecycle } }));
  },
};
