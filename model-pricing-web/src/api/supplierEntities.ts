import { apiFileRequest, apiRequest } from "./http";
import { buildSupplierEntitySave, normalizeSupplierEntity, normalizeSupplierEntityPage, normalizeSupplierQualification, normalizeSupplierQualificationPage, normalizeSupplierTestConfig, type SupplierEntityDTO, type SupplierEntityForm, type SupplierQualificationDTO, type SupplierQualificationMetadata, type SupplierTestConfigDTO } from "./supplierEntities.contract";
import { normalizeSupplierMergePreview } from "./supplierMerge.contract";

const base = "/internal/supplier-entities";

async function qualificationFileBase64(file: File): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return btoa(binary);
}

export const supplierEntitiesApi = {
  async list(params: { page: number; size: number; keyword?: string; region?: string }, signal?: AbortSignal) {
    return normalizeSupplierEntityPage(await apiRequest<unknown>({ url: base, params, signal }), params.page, params.size);
  },
  async get(id: string, signal?: AbortSignal) {
    return normalizeSupplierEntity(await apiRequest<unknown>({ url: `${base}/${id}`, signal }));
  },
  async qualifications(id: string, page = 1, size = 20, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ url: `${base}/${id}/qualifications`, params: { page, size }, signal });
    return normalizeSupplierQualificationPage(value, id, page, size);
  },
  async testConfig(id: string, signal?: AbortSignal): Promise<SupplierTestConfigDTO> {
    return normalizeSupplierTestConfig(await apiRequest<unknown>({ url: `${base}/${id}/test-config`, signal }), id);
  },
  async saveTestConfig(id: string, input: { baseURL: string; apiKey: string; expectedRevision: string; reason: string }, lifecycle: string): Promise<SupplierTestConfigDTO> {
    const url = `${base}/${id}/test-config`;
    const body = { base_url: input.baseURL.trim(), api_key: input.apiKey, expected_revision: input.expectedRevision, reason: input.reason.trim() };
    return normalizeSupplierTestConfig(await apiRequest<unknown>({ method: "PUT", url, data: body,
      idempotency: { scope: url, payload: body, lifecycle },
    }), id);
  },
  async uploadQualification(id: string, input: { name: string; file: File; reason: string; metadata?: SupplierQualificationMetadata }, lifecycle: string) {
    const url = `${base}/${id}/qualifications`;
    const body = { name: input.name.trim(), file_name: input.file.name, content_base64: await qualificationFileBase64(input.file), reason: input.reason.trim(),
      ...(input.metadata ? { qualification_type: input.metadata.qualification_type, valid_from: input.metadata.valid_from || "", valid_to: input.metadata.valid_to || "" } : {}) };
    return normalizeSupplierQualification(await apiRequest<unknown>({ method: "POST", url, data: body,
      idempotency: { scope: url, payload: body, lifecycle },
    }), id);
  },
  async updateQualification(id: string, qualificationID: string, input: SupplierQualificationMetadata & { name: string; expected_revision: string; reason: string }, lifecycle: string): Promise<SupplierQualificationDTO> {
    const url = `${base}/${id}/qualifications/${qualificationID}`;
    const body = { name: input.name.trim(), qualification_type: input.qualification_type, valid_from: input.valid_from || "", valid_to: input.valid_to || "",
      expected_revision: input.expected_revision, reason: input.reason.trim() };
    return normalizeSupplierQualification(await apiRequest<unknown>({ method: "PUT", url, data: body,
      idempotency: { scope: url, payload: body, lifecycle },
    }), id);
  },
  async qualificationFile(id: string, qualificationID: string) {
    return apiFileRequest({ url: `${base}/${id}/qualifications/${qualificationID}/file` });
  },
  async mergePreview(sourceID: string, targetID: string, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ method: "POST", url: `${base}/${sourceID}/merge-preview`, data: { target_supplier_id: targetID }, signal });
    return normalizeSupplierMergePreview(value, sourceID, targetID);
  },
  async save(form: SupplierEntityForm, reason: string, lifecycle: string, current?: SupplierEntityDTO) {
    const body = buildSupplierEntitySave(form, reason, current?.revision);
    const url = current ? `${base}/${current.id}` : base;
    return normalizeSupplierEntity(await apiRequest<unknown>({ method: current ? "PUT" : "POST", url, data: body,
      idempotency: { scope: url, payload: body, lifecycle },
    }));
  },
};
