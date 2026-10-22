import { standardModelVersion } from "./standardModels.contract";
import { apiRequest } from "./http";
import type { SupplierModelOptionDTO, SupplierSubmissionDTO, SupplierSubmissionListParams, SupplierSubmissionPageDTO, SupplierSubmissionTestDTO, SupplierSubmissionWrite, SupplierTestCategory, SupplierTestJobDTO } from "./supplierSubmissions.types";

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
      if (!item.name?.trim() || !item.official_id?.trim() || !item.company?.trim()) throw new Error("标准模型目录字段不完整，请重新查询。");
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
  async testStatus(id: string, rowID: string, category: SupplierTestCategory, signal?: AbortSignal) {
    const result = await apiRequest<SupplierTestJobDTO>({ url: `${base}/${submissionID(id)}/rows/${submissionID(rowID)}/test`, params: { category }, signal });
    standardModelVersion(result.job_id);
    validateTest(result, category);
    return result;
  },
};
