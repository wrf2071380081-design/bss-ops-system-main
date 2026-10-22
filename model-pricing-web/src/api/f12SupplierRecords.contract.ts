import Decimal from "decimal.js";
import { assertContractId } from "../domain/contractId.ts";

export interface F12ContractRecord {
  id: string; subject_type: "SUPPLIER"; subject_id: string;
  status: "PROCESSING" | "ARCHIVED"; version: number; has_attachment: boolean;
  signed_at: string | null; created_at: string; updated_at: string;
}
export interface F12CreditRecord {
  id: string; subject_type: "SUPPLIER"; subject_id: string; direction: "SUPPLIER_TO_US";
  amount: string; currency: string; version: number; effective_at: string | null; created_at: string;
}
export interface F12CurrentCredit {
  direction: "SUPPLIER_TO_US"; currency: string;
  status: "AVAILABLE" | "AMBIGUOUS_VERSION" | "INVALID_LEGACY_AMOUNT";
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

export function normalizeF12Contract(value: unknown): F12ContractRecord {
  const row = object(value);
  if (row.subject_type !== "SUPPLIER" || (row.status !== "PROCESSING" && row.status !== "ARCHIVED") || typeof row.has_attachment !== "boolean") throw new Error("合同记录不符合 v36 只读契约。");
  const subjectID = id(row.subject_id), createdAt = timestamp(row.created_at)!, updatedAt = timestamp(row.updated_at)!;
  if (Date.parse(updatedAt) < Date.parse(createdAt)) throw new Error("合同记录时间顺序不正确。");
  return { id: id(row.id), subject_type: "SUPPLIER", subject_id: subjectID, status: row.status, version: version(row.version), has_attachment: row.has_attachment,
    signed_at: timestamp(row.signed_at, true), created_at: createdAt, updated_at: updatedAt };
}

export function normalizeF12Credit(value: unknown): F12CreditRecord {
  const row = object(value);
  if (row.subject_type !== "SUPPLIER" || row.direction !== "SUPPLIER_TO_US" || typeof row.amount !== "string" || !/^-?(0|[1-9]\d*)(\.\d{1,2})?$/.test(row.amount) ||
      typeof row.currency !== "string" || !/^[A-Z]{3}$/.test(row.currency)) throw new Error("授信记录不符合 v36 只读契约。");
  const amount = new Decimal(row.amount);
  if (!amount.isFinite()) throw new Error("授信金额格式不正确。");
  return { id: id(row.id), subject_type: "SUPPLIER", subject_id: id(row.subject_id), direction: "SUPPLIER_TO_US", amount: amount.toFixed(2), currency: row.currency,
    version: version(row.version), effective_at: timestamp(row.effective_at, true), created_at: timestamp(row.created_at)! };
}

function page<T>(value: unknown, validate: (row: unknown) => T, expectedPage: number, expectedSize: number) {
  const result = object(value);
  if (!Array.isArray(result.list) || !Number.isSafeInteger(result.total) || Number(result.total) < 0 || result.page !== expectedPage || result.size !== expectedSize || result.list.length > expectedSize || Number(result.total) < result.list.length) throw new Error("合同/授信分页响应不正确。");
  return { list: result.list.map(validate), total: Number(result.total), page: expectedPage, size: expectedSize };
}
export function normalizeF12Contracts(value: unknown, pageNo: number, size: number) { return page(value, normalizeF12Contract, pageNo, size); }

export function normalizeF12Credits(value: unknown, pageNo: number, size: number) {
  const raw = object(value);
  const rows = page(value, normalizeF12Credit, pageNo, size);
  if (!Array.isArray(raw.current_credit)) throw new Error("当前授信投影不正确。");
  const current = raw.current_credit.map((value): F12CurrentCredit => {
    const row = object(value);
    if (row.direction !== "SUPPLIER_TO_US" || typeof row.currency !== "string" || !/^[A-Z]{3}$/.test(row.currency) ||
        !["AVAILABLE", "AMBIGUOUS_VERSION", "INVALID_LEGACY_AMOUNT"].includes(String(row.status))) throw new Error("当前授信状态不正确。");
    const status = row.status as F12CurrentCredit["status"];
    if (status === "AVAILABLE") {
      const record = normalizeF12Credit(row.record);
      if (record.direction !== row.direction || record.currency !== row.currency || !new Decimal(record.amount).isPositive()) throw new Error("当前授信金额与维度不一致。");
      return { direction: row.direction, currency: row.currency, status, record };
    }
    if (row.record !== undefined) throw new Error("异常版本不可附带当前授信金额。");
    return { direction: row.direction, currency: row.currency, status };
  });
  const dimensions = new Set<string>();
  for (const row of current) { const key = `${row.direction}:${row.currency}`; if (dimensions.has(key)) throw new Error("当前授信维度重复。"); dimensions.add(key); }
  return { ...rows, current };
}
