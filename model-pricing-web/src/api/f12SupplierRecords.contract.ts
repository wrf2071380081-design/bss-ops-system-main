import Decimal from "decimal.js";
import { assertContractId } from "../domain/contractId.ts";

export type F12SubjectType = "SUPPLIER" | "CUSTOMER";
export type F12CreditDirection = "SUPPLIER_TO_US" | "US_TO_CUSTOMER";

// Keep the pre-upload form check consistent with the F12 decimal(20,2) write contract.
export function normalizeF12CreditInput(rawAmount: string, rawCurrency: string) {
  const amount = rawAmount.trim(), currency = rawCurrency.trim().toUpperCase();
  if (!/^(0|[1-9]\d{0,17})(\.\d{1,2})?$/.test(amount) || !new Decimal(amount).greaterThan(0)) {
    throw new Error("授信额度须大于 0，整数最多 18 位、小数最多 2 位。");
  }
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error("币种须为三位英文字母，例如 CNY 或 USD。");
  return { amount: new Decimal(amount).toFixed(2), currency };
}

export interface F12ContractRecord {
  id: string; subject_type: F12SubjectType; subject_id: string;
  status: "PROCESSING" | "ARCHIVED"; version: number; has_attachment: boolean;
  contract_key?: string; document_name?: string; document_no?: string; file_id?: string; reason?: string; created_by?: string; is_latest?: boolean;
  source_flow_id?: string; source_module_key?: string; source_flow_revision?: number;
  /** Derived for display from trusted flow source + authenticated created_by; never accepted from the API input. */
  actual_operator_id?: string;
  signed_at: string | null; created_at: string; updated_at: string;
}
export interface F12CreditRecord {
  id: string; subject_type: F12SubjectType; subject_id: string; direction: F12CreditDirection;
  amount: string; currency: string; version: number; effective_at: string | null; created_at: string;
  document_name?: string; document_no?: string; file_id?: string; reason?: string; created_by?: string;
  source_flow_id?: string; source_module_key?: string; source_flow_revision?: number;
  /** Derived for display from trusted flow source + authenticated created_by; never accepted from the API input. */
  actual_operator_id?: string;
}
export interface F12CurrentCredit {
  direction: F12CreditDirection; currency: string;
  status: "AVAILABLE" | "AMBIGUOUS_VERSION" | "INVALID_LEGACY_AMOUNT";
  latest_version?: number;
  record?: F12CreditRecord;
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("合同/授信响应格式不正确。");
  return value as Record<string, unknown>;
}
function id(value: unknown): string {
  assertContractId(value);
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) throw new Error("合同/授信 ID 响应格式不正确。");
  return value;
}
function timestamp(value: unknown, nullable = false): string | null {
  if (nullable && value === null) return null;
  if (typeof value !== "string" || Number.isNaN(Date.parse(value))) throw new Error("合同/授信时间格式不正确。");
  return value;
}
function version(value: unknown): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error("合同/授信版本格式不正确。");
  return Number(value);
}

function expectedDirection(subjectType: F12SubjectType): F12CreditDirection {
  return subjectType === "CUSTOMER" ? "US_TO_CUSTOMER" : "SUPPLIER_TO_US";
}

function assertSubject(actualType: unknown, actualID: unknown, subjectType: F12SubjectType, subjectID?: string) {
  if (actualType !== subjectType || (subjectID !== undefined && actualID !== subjectID)) throw new Error("合同/授信主体与当前页面不匹配。");
}

function optionalText(row: Record<string, unknown>, key: string): string | undefined {
  const text = row[key];
  if (text !== undefined && typeof text !== "string") throw new Error(`合同/授信记录 ${key} 格式不正确。`);
  return typeof text === "string" && text ? text : undefined;
}

function flowSource(row: Record<string, unknown>, createdBy?: string) {
  const flowID = optionalText(row, "source_flow_id"), moduleKey = optionalText(row, "source_module_key");
  const rawRevision = row.source_flow_revision;
  if (rawRevision !== undefined && (typeof rawRevision !== "string" || !/^[1-9]\d*$/.test(rawRevision) || Number(rawRevision) > 2147483647)) {
    throw new Error("合同/授信流程来源修订号格式不正确。");
  }
  const revision = typeof rawRevision === "string" ? Number(rawRevision) : undefined;
  if (Boolean(flowID || moduleKey || revision) && (!flowID || !moduleKey || !revision)) throw new Error("合同/授信流程来源信息不完整。");
  return {
    ...(flowID ? { source_flow_id: flowID } : {}),
    ...(moduleKey ? { source_module_key: moduleKey } : {}),
    ...(revision ? { source_flow_revision: revision } : {}),
    ...(flowID && revision && createdBy ? { actual_operator_id: createdBy } : {}),
  };
}

export function normalizeF12Contract(value: unknown, subjectType: F12SubjectType = "SUPPLIER", expectedSubjectID?: string): F12ContractRecord {
  const row = object(value);
  assertSubject(row.subject_type, row.subject_id, subjectType, expectedSubjectID);
  if ((row.status !== "PROCESSING" && row.status !== "ARCHIVED") || typeof row.has_attachment !== "boolean") throw new Error("合同记录不符合 v36 只读契约。");
  const subjectID = id(row.subject_id), createdAt = timestamp(row.created_at)!, updatedAt = timestamp(row.updated_at)!;
  if (Date.parse(updatedAt) < Date.parse(createdAt)) throw new Error("合同记录时间顺序不正确。");
  const createdBy = optionalText(row, "created_by");
  if (createdBy && !/^[1-9]\d*$/.test(createdBy)) throw new Error("合同登记人 ID 格式不正确。");
  if (row.is_latest !== undefined && typeof row.is_latest !== "boolean") throw new Error("合同记录最新版本标识不正确。");
  return { id: id(row.id), subject_type: subjectType, subject_id: subjectID, status: row.status, version: version(row.version), has_attachment: row.has_attachment,
    ...(optionalText(row, "contract_key") ? { contract_key: optionalText(row, "contract_key") } : {}),
    ...(optionalText(row, "document_name") ? { document_name: optionalText(row, "document_name") } : {}),
    ...(optionalText(row, "document_no") ? { document_no: optionalText(row, "document_no") } : {}),
    ...(optionalText(row, "file_id") ? { file_id: optionalText(row, "file_id") } : {}),
    ...(optionalText(row, "reason") ? { reason: optionalText(row, "reason") } : {}),
    ...(createdBy ? { created_by: createdBy } : {}),
    ...flowSource(row, createdBy),
    ...(typeof row.is_latest === "boolean" ? { is_latest: row.is_latest } : {}),
    signed_at: timestamp(row.signed_at, true), created_at: createdAt, updated_at: updatedAt };
}

export function normalizeF12LatestContract(value: unknown, subjectType: F12SubjectType, subjectID: string, contractKey: string): F12ContractRecord | null {
  assertContractId(contractKey);
  const result = normalizeF12Contracts(value, 1, 2, subjectType, subjectID);
  if (result.total === 0 && result.list.length === 0) return null;
  if (result.total !== 1 || result.list.length !== 1) throw new Error("该合同存在重复最新版本，请先核对历史记录。");
  const row = result.list[0]!;
  if (row.contract_key !== contractKey || row.is_latest !== true) throw new Error("最新版本与当前合同档案不匹配。");
  return row;
}

export function normalizeF12Credit(value: unknown, subjectType: F12SubjectType = "SUPPLIER", expectedSubjectID?: string): F12CreditRecord {
  const row = object(value);
  assertSubject(row.subject_type, row.subject_id, subjectType, expectedSubjectID);
  const direction = expectedDirection(subjectType);
  if (row.direction !== direction || typeof row.amount !== "string" || !/^-?(0|[1-9]\d*)(\.\d{1,2})?$/.test(row.amount) ||
      typeof row.currency !== "string" || !/^[A-Z]{3}$/.test(row.currency)) throw new Error("授信记录不符合 v36 只读契约。");
  const amount = new Decimal(row.amount);
  if (!amount.isFinite()) throw new Error("授信金额格式不正确。");
  const createdBy = optionalText(row, "created_by");
  if (createdBy && !/^[1-9]\d*$/.test(createdBy)) throw new Error("授信登记人 ID 格式不正确。");
  return { id: id(row.id), subject_type: subjectType, subject_id: id(row.subject_id), direction, amount: amount.toFixed(2), currency: row.currency,
    version: version(row.version), effective_at: timestamp(row.effective_at, true), created_at: timestamp(row.created_at)!,
    ...(optionalText(row, "document_name") ? { document_name: optionalText(row, "document_name") } : {}),
    ...(optionalText(row, "document_no") ? { document_no: optionalText(row, "document_no") } : {}),
    ...(optionalText(row, "file_id") ? { file_id: optionalText(row, "file_id") } : {}),
    ...(optionalText(row, "reason") ? { reason: optionalText(row, "reason") } : {}),
    ...(createdBy ? { created_by: createdBy } : {}),
    ...flowSource(row, createdBy) };
}

function page<T>(value: unknown, validate: (row: unknown) => T, expectedPage: number, expectedSize: number) {
  const result = object(value);
  if (!Array.isArray(result.list) || !Number.isSafeInteger(result.total) || Number(result.total) < 0 || result.page !== expectedPage || result.size !== expectedSize || result.list.length > expectedSize || Number(result.total) < result.list.length) throw new Error("合同/授信分页响应不正确。");
  return { list: result.list.map(validate), total: Number(result.total), page: expectedPage, size: expectedSize };
}
export function normalizeF12Contracts(value: unknown, pageNo: number, size: number, subjectType: F12SubjectType = "SUPPLIER", expectedSubjectID?: string) {
  return page(value, row => normalizeF12Contract(row, subjectType, expectedSubjectID), pageNo, size);
}

export function normalizeF12Credits(value: unknown, pageNo: number, size: number, subjectType: F12SubjectType = "SUPPLIER", expectedSubjectID?: string) {
  const raw = object(value);
  const rows = page(value, row => normalizeF12Credit(row, subjectType, expectedSubjectID), pageNo, size);
  const direction = expectedDirection(subjectType);
  if (!Array.isArray(raw.current_credit)) throw new Error("当前授信投影不正确。");
  const current = raw.current_credit.map((value): F12CurrentCredit => {
    const row = object(value);
    if (row.direction !== direction || typeof row.currency !== "string" || !/^[A-Z]{3}$/.test(row.currency) ||
        !["AVAILABLE", "AMBIGUOUS_VERSION", "INVALID_LEGACY_AMOUNT"].includes(String(row.status))) throw new Error("当前授信状态不正确。");
    const status = row.status as F12CurrentCredit["status"];
    const latestVersion = row.latest_version === undefined ? undefined : version(row.latest_version);
    if (status === "AVAILABLE") {
      const record = normalizeF12Credit(row.record, subjectType, expectedSubjectID);
      if (record.direction !== direction || record.currency !== row.currency || (latestVersion !== undefined && record.version !== latestVersion) || !new Decimal(record.amount).isPositive()) throw new Error("当前授信金额与维度不一致。");
      return { direction, currency: row.currency, status, ...(latestVersion !== undefined ? { latest_version: latestVersion } : { latest_version: record.version }), record };
    }
    if (row.record !== undefined) throw new Error("异常版本不可附带当前授信金额。");
    return { direction, currency: row.currency, status, ...(latestVersion !== undefined ? { latest_version: latestVersion } : {}) };
  });
  const dimensions = new Set<string>();
  for (const row of current) { const key = `${row.direction}:${row.currency}`; if (dimensions.has(key)) throw new Error("当前授信维度重复。"); dimensions.add(key); }
  return { ...rows, current };
}
