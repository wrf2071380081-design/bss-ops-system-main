import { apiRequest } from "./http";
import { standardModelVersion, modelTypeLabels } from "./standardModels.contract";
import type { SupplierModelFilterOptionsDTO, SupplierModelListParams, SupplierModelPageDTO, SupplierModelResourceDTO } from "./supplierModels.types";

function validateRow(value: unknown): SupplierModelResourceDTO {
  if (!value || typeof value !== "object") throw new Error("模型列表响应不符合约定，请重新查询。");
  const row = value as SupplierModelResourceDTO;
  const coefficient = typeof row.coefficient === "string" && /^\d+(?:\.\d+)?$/.test(row.coefficient) ? Number(row.coefficient) : Number.NaN;
  standardModelVersion(row.id);
  standardModelVersion(row.standard_model_id);
  standardModelVersion(row.revision);
  if (!row.model_name?.trim() || !row.official_id?.trim() || !row.supplier_model_id?.trim() ||
      typeof row.company !== "string" || !modelTypeLabels[row.model_type] ||
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
        !Number.isSafeInteger(result.hidden_total) || result.hidden_total < 0 ||
        result.page !== params.page || result.size !== params.size || result.list.length > params.size) {
      throw new Error("模型列表分页响应不符合约定，请重新查询。");
    }
    return { ...result, list: result.list.map(validateRow) };
  },
  /** 筛选条候选值：按全部自有资源返回，不随当前列表为空而变化。 */
  async filterOptions(signal?: AbortSignal): Promise<SupplierModelFilterOptionsDTO> {
    const result = await apiRequest<SupplierModelFilterOptionsDTO>({ url: "/supplier/models/filter-options", signal });
    if (!result || !Array.isArray(result.companies) || !Array.isArray(result.model_types) || !Array.isArray(result.states)) {
      throw new Error("模型筛选候选值响应不符合约定，请重新加载。");
    }
    return result;
  },
  // 2026-10-28：删掉 priceChange。它一直打向不存在的 /supplier/models/:id/price-change；
  // 价格变更改走申报主链路（kind=PRICE_CHANGE），由 SupplierSubmissionNew 创建 + 提交。
};
