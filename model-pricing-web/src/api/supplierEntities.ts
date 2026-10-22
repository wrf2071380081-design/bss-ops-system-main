import { apiRequest } from "./http";
import { buildSupplierEntitySave, normalizeSupplierEntity, normalizeSupplierEntityPage, type SupplierEntityDTO, type SupplierEntityForm } from "./supplierEntities.contract";

const base = "/internal/supplier-entities";

export const supplierEntitiesApi = {
  async list(params: { page: number; size: number; keyword?: string; region?: string }, signal?: AbortSignal) {
    return normalizeSupplierEntityPage(await apiRequest<unknown>({ url: base, params, signal }), params.page, params.size);
  },
  async get(id: string, signal?: AbortSignal) {
    return normalizeSupplierEntity(await apiRequest<unknown>({ url: `${base}/${id}`, signal }));
  },
  async save(form: SupplierEntityForm, reason: string, lifecycle: string, current?: SupplierEntityDTO) {
    const body = buildSupplierEntitySave(form, reason, current?.revision);
    const url = current ? `${base}/${current.id}` : base;
    return normalizeSupplierEntity(await apiRequest<unknown>({ method: current ? "PUT" : "POST", url, data: body,
      idempotency: { scope: url, payload: body, lifecycle },
    }));
  },
};
