import { apiFileDownloadRequest, apiRequest } from "./http";
import { normalizeF12Contracts, normalizeF12Credits, normalizeF12CreditInput, normalizeF12LatestContract } from "./f12SupplierRecords.contract";
import type { F12ContractRecord, F12SubjectType } from "./f12SupplierRecords.contract";
import { normalizeF12Readiness } from "./f12Readiness.contract";

const defaultPage = 1, defaultSize = 20;
const contractsURL = "/internal/contracts", creditsURL = "/internal/credits", attachmentsURL = "/internal/f12/attachments", readinessURL = "/internal/f12/readiness";

export interface F12FlowWriteContext {
  flowID: string;
  moduleKey: string;
  expectedRevision: number;
}

function flowFields(context?: F12FlowWriteContext) {
  if (!context) return {};
  if (!/^[1-9]\d*$/.test(context.flowID) || !context.moduleKey.trim() || !Number.isSafeInteger(context.expectedRevision) || context.expectedRevision < 1 || context.expectedRevision > 2147483647) {
    throw new Error("流程资料缺少有效的主体流程、办理模块或当前修订号。");
  }
  return { source_flow_id: context.flowID, source_module_key: context.moduleKey.trim(), expected_flow_revision: String(context.expectedRevision) };
}

async function fileBase64(file: File): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  return btoa(binary);
}

function normalizeAttachment(value: unknown, type: F12SubjectType, subjectID: string, purpose: "CONTRACT" | "CREDIT") {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("附件响应格式不正确。");
  const row = value as Record<string, unknown>;
  if (typeof row.file_id !== "string" || !/^f12:v1:[a-f0-9]{32}$/.test(row.file_id) || row.subject_type !== type || row.subject_id !== subjectID || row.purpose !== purpose ||
      typeof row.file_name !== "string" || !row.file_name || typeof row.uploaded_by !== "string" || !/^[1-9]\d*$/.test(row.uploaded_by) ||
      !["application/pdf", "image/png", "image/jpeg"].includes(String(row.content_type)) || !Number.isSafeInteger(row.size) || Number(row.size) < 1 || Number(row.size) > 2 * 1024 * 1024) {
    throw new Error("附件响应与当前主体或文件类型不匹配。");
  }
  return { id: row.file_id, name: row.file_name };
}

function assertF12WriteReceipt(value: unknown, expected: { subjectType: F12SubjectType; subjectID: string; version: number; status?: string; key?: string }) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("合同/授信写入回执格式不正确。");
  const row = value as Record<string, unknown>;
  if (row.subject_type !== expected.subjectType || row.subject_id !== expected.subjectID || typeof row.created_by !== "string" || !/^[1-9]\d*$/.test(row.created_by) || String(row.version) !== String(expected.version) ||
      (expected.status && row.status !== expected.status) || (expected.key && row.contract_key !== expected.key)) throw new Error("合同/授信写入回执与当前操作不匹配。");
}

export const f12SubjectRecordsApi = {
  async readiness(subjectType: F12SubjectType, subjectID: string, context: Pick<F12FlowWriteContext, "flowID" | "moduleKey">, signal?: AbortSignal) {
    const validBigint = (value: string) => /^[1-9]\d{0,18}$/.test(value) && (value.length < 19 || value <= "9223372036854775807");
    if (!validBigint(subjectID) || !validBigint(context.flowID) || !context.moduleKey.trim()) {
      throw new Error("资料齐备查询缺少有效的主体或办理流程。");
    }
    const expected = { subjectType, subjectID, flowID: context.flowID, moduleKey: context.moduleKey.trim() };
    const value = await apiRequest<unknown>({ url: readinessURL, params: {
      subject_type: subjectType, subject_id: subjectID, flow_id: expected.flowID, module_key: expected.moduleKey,
    }, signal });
    return normalizeF12Readiness(value, expected);
  },
  async contracts(subjectType: F12SubjectType, subjectID: string, signal?: AbortSignal, page = defaultPage, size = defaultSize) {
    const value = await apiRequest<unknown>({ url: contractsURL, params: { subject_type: subjectType, subject_id: subjectID, page, size }, signal });
    return normalizeF12Contracts(value, page, size, subjectType, subjectID);
  },
  async latestContract(subjectType: F12SubjectType, subjectID: string, contractKey: string, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ url: contractsURL, params: {
      subject_type: subjectType, subject_id: subjectID, contract_key: contractKey, latest_only: "true", page: 1, size: 2,
    }, signal });
    return normalizeF12LatestContract(value, subjectType, subjectID, contractKey);
  },
  async credits(subjectType: F12SubjectType, subjectID: string, signal?: AbortSignal, page = defaultPage, size = defaultSize) {
    const value = await apiRequest<unknown>({ url: creditsURL, params: { subject_type: subjectType, subject_id: subjectID, page, size }, signal });
    return normalizeF12Credits(value, page, size, subjectType, subjectID);
  },
  async uploadAttachment(subjectType: F12SubjectType, subjectID: string, purpose: "CONTRACT" | "CREDIT", file: File, reason: string, lifecycle: string, flowContext?: F12FlowWriteContext) {
    if (file.size < 1 || file.size > 2 * 1024 * 1024) throw new Error("附件必须在 1 字节到 2 MiB 之间。");
    const body = { subject_type: subjectType, subject_id: subjectID, purpose, ...flowFields(flowContext),
      file_name: file.name, content_base64: await fileBase64(file), reason: reason.trim() };
    return normalizeAttachment(await apiRequest<unknown>({ method: "POST", url: attachmentsURL, data: body,
      idempotency: { scope: attachmentsURL, payload: body, lifecycle },
    }), subjectType, subjectID, purpose);
  },
  attachmentFile(subjectType: F12SubjectType, subjectID: string, fileID: string, purpose: "CONTRACT" | "CREDIT") {
    return apiFileDownloadRequest({ url: `${attachmentsURL}/${encodeURIComponent(fileID)}`, params: { subject_type: subjectType, subject_id: subjectID, purpose } });
  },
  async createContract(input: { subjectType: F12SubjectType; subjectID: string; documentName: string; documentNo: string; fileID: string; reason: string }, lifecycle: string, flowContext?: F12FlowWriteContext) {
    const body = { subject_type: input.subjectType, subject_id: input.subjectID, document_name: input.documentName.trim(), document_no: input.documentNo.trim(), file_id: input.fileID, ...flowFields(flowContext), reason: input.reason.trim() };
    const row = await apiRequest<unknown>({ method: "POST", url: contractsURL, data: body, idempotency: { scope: contractsURL, payload: body, lifecycle } });
    assertF12WriteReceipt(row, { subjectType: input.subjectType, subjectID: input.subjectID, version: 1, status: "PROCESSING" });
  },
  async reviseContract(contractKey: string, expectedVersion: number, input: { subjectType: F12SubjectType; subjectID: string; documentName: string; documentNo: string; fileID: string; reason: string }, lifecycle: string, flowContext?: F12FlowWriteContext) {
    const url = `${contractsURL}/${contractKey}/versions`;
    const body = { subject_type: input.subjectType, subject_id: input.subjectID, document_name: input.documentName.trim(), document_no: input.documentNo.trim(), file_id: input.fileID,
      ...flowFields(flowContext), expected_latest_version: String(expectedVersion), reason: input.reason.trim() };
    const row = await apiRequest<unknown>({ method: "POST", url, data: body, idempotency: { scope: url, payload: body, lifecycle } });
    assertF12WriteReceipt(row, { subjectType: input.subjectType, subjectID: input.subjectID, version: expectedVersion + 1, key: contractKey });
  },
  async archiveContract(record: F12ContractRecord, reason: string, lifecycle: string, flowContext?: F12FlowWriteContext) {
    if (!record.contract_key) throw new Error("当前旧结构没有稳定合同档案键，暂不能归档。");
    const url = `${contractsURL}/${record.contract_key}/archive`;
    const body = { ...flowFields(flowContext), expected_latest_version: String(record.version), reason: reason.trim() };
    const row = await apiRequest<unknown>({ method: "POST", url, data: body, idempotency: { scope: url, payload: body, lifecycle } });
    assertF12WriteReceipt(row, { subjectType: record.subject_type, subjectID: record.subject_id, version: record.version + 1, status: "ARCHIVED", key: record.contract_key });
  },
  async addCreditVersion(input: { subjectType: F12SubjectType; subjectID: string; direction: string; amount: string; currency: string; documentName: string; documentNo: string; fileID: string; expectedVersion: number; reason: string }, lifecycle: string, flowContext?: F12FlowWriteContext) {
    const credit = normalizeF12CreditInput(input.amount, input.currency);
    const body = { subject_type: input.subjectType, subject_id: input.subjectID, direction: input.direction, ...credit,
      document_name: input.documentName.trim(), document_no: input.documentNo.trim(), file_id: input.fileID, ...flowFields(flowContext), expected_latest_version: String(input.expectedVersion), reason: input.reason.trim() };
    const row = await apiRequest<unknown>({ method: "POST", url: creditsURL, data: body, idempotency: { scope: creditsURL, payload: body, lifecycle } });
    assertF12WriteReceipt(row, { subjectType: input.subjectType, subjectID: input.subjectID, version: input.expectedVersion + 1 });
  },
};

// Keep the supplier-named facade for existing callers while F12 is shared with CUSTOMER.
export const f12SupplierRecordsApi = {
  contracts: (supplierID: string, signal?: AbortSignal) => f12SubjectRecordsApi.contracts("SUPPLIER", supplierID, signal),
  credits: (supplierID: string, signal?: AbortSignal) => f12SubjectRecordsApi.credits("SUPPLIER", supplierID, signal),
};
