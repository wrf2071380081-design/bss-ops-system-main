import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import type { SupplierModelListParams, SupplierModelPageDTO, SupplierModelResourceDTO } from "./supplierModels.types";

function validateRow(value: unknown): SupplierModelResourceDTO {
  if (!value || typeof value !== "object") throw new Error("模型列表响应不符合约定，请重新查询。");
  const row = value as SupplierModelResourceDTO;
  const coefficient = typeof row.coefficient === "string" && /^\d+(?:\.\d+)?$/.test(row.coefficient) ? Number(row.coefficient) : Number.NaN;
  standardModelVersion(row.id);
  standardModelVersion(row.standard_model_id);
  standardModelVersion(row.revision);
  if (!row.model_name?.trim() || !row.official_id?.trim() || !row.supplier_model_id?.trim() ||
      !["CN", "OVERSEAS"].includes(row.region) || !Number.isFinite(coefficient) || coefficient <= 0 || coefficient > 1 ||
      typeof row.ever_admitted !== "boolean" || !row.test || !row.quote_readiness ||
      typeof row.test.connectivity_tested !== "boolean" || typeof row.test.connectivity_passed !== "boolean" ||
      typeof row.test.stress_tested !== "boolean" || typeof row.test.stress_passed !== "boolean" ||
      typeof row.test.valid !== "boolean" || typeof row.quote_readiness.eligible !== "boolean" ||
      !Array.isArray(row.quote_readiness.blocked_reasons) || row.quote_readiness.blocked_reasons.some(reason => typeof reason !== "string")) {
    throw new Error("模型列表响应字段不完整，请重新查询。");
  }
  if (row.test.report_id) standardModelVersion(row.test.report_id);
  return row;
}

export const supplierModelsApi = {
  async list(params: SupplierModelListParams, signal?: AbortSignal): Promise<SupplierModelPageDTO> {
    const query = { ...params, keyword: params.keyword?.trim() || undefined, region: params.region || undefined, state: params.state || undefined };
    const result = await apiRequest<SupplierModelPageDTO>({ url: "/supplier/models", params: query, signal });
    if (!result || !Array.isArray(result.list) || !Number.isSafeInteger(result.total) || result.total < result.list.length ||
        result.page !== params.page || result.size !== params.size || result.list.length > params.size) {
      throw new Error("模型列表分页响应不符合约定，请重新查询。");
    }
    return { ...result, list: result.list.map(validateRow) };
  },
  priceChange(resourceID: string, expectedRevision: string, coefficient: string, reason: string, lifecycle: string) {
    standardModelVersion(resourceID);
    standardModelVersion(expectedRevision);
    const normalized = coefficient.trim();
    const amount = Number(normalized);
    if (!/^(?:0?\.\d+|1(?:\.0+)?)$/.test(normalized) || !Number.isFinite(amount) || amount <= 0 || amount > 1) {
      throw new Error("采购系数必须大于 0 且不超过 1。");
    }
    const url = `/supplier/models/${encodeURIComponent(resourceID)}/price-change`;
    const payload = { expected_revision: expectedRevision, coefficient: normalized, reason: reason.trim() };
    return apiRequest<unknown>({ method: "POST", url, data: payload, idempotency: { scope: url, payload, lifecycle } });
  },
};
