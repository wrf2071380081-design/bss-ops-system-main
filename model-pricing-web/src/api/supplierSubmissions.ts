import { standardModelVersion } from "./standardModels.contract";
import { apiFileRequest, apiRequest } from "./http";
import type {
  SupplierBatchTestResultDTO, SupplierImportPreviewDTO, SupplierModelOptionDTO,
  SupplierSubmissionDTO, SupplierSubmissionFilterOptionsDTO, SupplierSubmissionListParams,
  SupplierSubmissionPageDTO, SupplierSubmissionTestDTO, SupplierSubmissionWrite,
  SupplierTestCategory, SupplierTestJobDTO,
} from "./supplierSubmissions.types";

const base = "/supplier/submissions";
function submissionID(value: string) { standardModelVersion(value); return encodeURIComponent(value); }
function validatePage(value: SupplierSubmissionPageDTO, params: SupplierSubmissionListParams): SupplierSubmissionPageDTO {
  if (!value || !Array.isArray(value.list) || !Number.isSafeInteger(value.total) || value.total < 0 || value.page !== params.page || value.size !== params.size || value.list.length > value.size) throw new Error("申报列表响应不符合约定，请重新加载。");
  return value;
}
function validateTest(value: SupplierSubmissionTestDTO | undefined, category: SupplierTestCategory) {
  if (value && (value.category !== category || typeof value.status !== "string" || typeof value.tested !== "boolean" || typeof value.passed !== "boolean")) throw new Error("测试状态响应不符合约定，请重新查询。");
}

export const supplierSubmissionsApi = {
  async list(params: SupplierSubmissionListParams, signal?: AbortSignal) {
    return validatePage(await apiRequest<SupplierSubmissionPageDTO>({ url: base, params, signal }), params);
  },
  async get(id: string, signal?: AbortSignal) {
    return apiRequest<SupplierSubmissionDTO>({ url: `${base}/${submissionID(id)}`, signal });
  },
  async modelOptions(keyword: string, signal?: AbortSignal) {
    const response = await apiRequest<{ list: SupplierModelOptionDTO[] }>({ url: `${base}/model-options`, params: { keyword: keyword.trim(), page: 1, size: 20 }, signal });
    if (!response || !Array.isArray(response.list)) throw new Error("标准模型目录响应不符合约定，请重新查询。");
    for (const item of response.list) {
      standardModelVersion(item.id);
      // Incomplete official data may be saved and used for resource handling.
      // Identity remains mandatory; missing metadata must not reject the list.
      if (typeof item.name !== 'string' || typeof item.official_id !== 'string' || typeof item.company !== 'string') throw new Error("标准模型目录字段不完整，请重新查询。");
    }
    return response.list;
  },
  create(data: SupplierSubmissionWrite, lifecycle: string) {
    return apiRequest<SupplierSubmissionDTO>({ method: "POST", url: base, data, idempotency: { scope: "supplier:submission:create", payload: data, lifecycle } });
  },
  update(id: string, data: SupplierSubmissionWrite, lifecycle: string) {
    const url = `${base}/${submissionID(id)}`;
    return apiRequest<SupplierSubmissionDTO>({ method: "PUT", url, data, idempotency: { scope: url, payload: data, lifecycle } });
  },
  submit(id: string, expectedRevision: string, reason: string, lifecycle: string) {
    const url = `${base}/${submissionID(id)}/submit`;
    const data = { expected_revision: expectedRevision, reason: reason.trim() };
    return apiRequest<SupplierSubmissionDTO>({ method: "POST", url, data, idempotency: { scope: url, payload: data, lifecycle } });
  },
  withdraw(id: string, expectedRevision: string, reason: string, lifecycle: string) {
    const url = `${base}/${submissionID(id)}/withdraw`;
    const data = { expected_revision: expectedRevision, reason: reason.trim() };
    return apiRequest<SupplierSubmissionDTO>({ method: "POST", url, data, idempotency: { scope: url, payload: data, lifecycle } });
  },
  async startTest(id: string, rowID: string, category: SupplierTestCategory, lifecycle: string) {
    const url = `${base}/${submissionID(id)}/rows/${submissionID(rowID)}/test`;
    const data = { category };
    const result = await apiRequest<SupplierTestJobDTO>({ method: "POST", url, data, idempotency: { scope: `${url}:${category}`, payload: data, lifecycle } });
    standardModelVersion(result.job_id);
    validateTest(result, category);
    return result;
  },
  async batchTest(id: string, categories: SupplierTestCategory[], lifecycle: string) {
    const url = `${base}/${submissionID(id)}/tests`;
    const data = { categories };
    const result = await apiRequest<SupplierBatchTestResultDTO>({
      method: "POST", url, data, idempotency: { scope: `${url}:${categories.join("+")}`, payload: data, lifecycle },
    });
    if (!result || !Array.isArray(result.items) || typeof result.started !== "number") {
      throw new Error("批量测试响应不符合约定，请重新查询进度。");
    }
    return result;
  },
  async filterOptions(signal?: AbortSignal) {
    const result = await apiRequest<SupplierSubmissionFilterOptionsDTO>({ url: `${base}/filter-options`, signal });
    if (!result || !Array.isArray(result.companies)) throw new Error("筛选选项响应不符合约定。");
    return result;
  },
  importTemplate() {
    return apiFileRequest({ url: `${base}/import/template` });
  },
  async parseImport(file: File) {
    const form = new FormData();
    form.append("file", file);
    const result = await apiRequest<SupplierImportPreviewDTO>({ method: "POST", url: `${base}/import/parse`, data: form });
    if (!result || !Array.isArray(result.rows)) throw new Error("导入解析响应不符合约定。");
    return result;
  },
  async testStatus(id: string, rowID: string, category: SupplierTestCategory, signal?: AbortSignal) {
    const result = await apiRequest<SupplierTestJobDTO>({ url: `${base}/${submissionID(id)}/rows/${submissionID(rowID)}/test`, params: { category }, signal });
    standardModelVersion(result.job_id);
    validateTest(result, category);
    return result;
  },
};
