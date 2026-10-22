import Decimal from "decimal.js";
import { assertContractId } from "../domain/contractId.ts";

export interface SupplierEntityForm {
  name: string;
  region: "CN" | "OVERSEAS";
  registration_no: string;
  contact: string;
  phone: string;
  tax_rate_percent: string;
  settlement_currency: "CNY" | "USD";
}

export interface SupplierEntitySaveBody {
  name: string;
  region: "CN" | "OVERSEAS";
  registration_no: string;
  contact: string;
  phone: string;
  tax_rate: string;
  settlement_currency: "CNY" | "USD";
  reason: string;
  expected_revision?: string;
}

export interface SupplierEntityDTO {
  id: string;
  buyer_id: string;
  name: string;
  region: "CN" | "OVERSEAS";
  registration_no: string;
  contact: string;
  phone: string;
  tax_rate: string;
  settlement_currency: "CNY" | "USD";
  established: boolean;
  status: "ACTIVE" | "FROZEN";
  revision: string;
  created_at: string;
  updated_at: string;
  can_edit: boolean;
}

const positiveBigint = (value: unknown): string => {
  assertContractId(value);
  const result = String(value);
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(result)) throw new Error("供应商 ID 需以字符串返回。");
  return result;
};

export function buildSupplierEntitySave(form: SupplierEntityForm, reason: string, revision?: string): SupplierEntitySaveBody {
  const name = form.name.trim();
  if (!name) throw new Error("请填写供应商名称。");
  if (!["CN", "OVERSEAS"].includes(form.region)) throw new Error("请选择国内或海外主体。");
  if (!["CNY", "USD"].includes(form.settlement_currency)) throw new Error("请选择支持的结算币种。");
  if (!reason.trim()) throw new Error("请填写操作说明。");
  let percent: Decimal;
  try { percent = new Decimal(form.tax_rate_percent); } catch { throw new Error("税率需填写 0～100 之间的百分比。"); }
  if (!percent.isFinite() || percent.isNegative() || percent.greaterThan(100)) throw new Error("税率需填写 0～100 之间的百分比。");
  if (percent.decimalPlaces() > 2) throw new Error("当前数据库最多支持 0.01 个百分点精度。");
  const body: SupplierEntitySaveBody = {
    name, region: form.region, registration_no: form.registration_no.trim(), contact: form.contact.trim(), phone: form.phone.trim(),
    tax_rate: percent.div(100).toFixed(8), settlement_currency: form.settlement_currency, reason: reason.trim(),
  };
  if (revision !== undefined) body.expected_revision = positiveBigint(revision);
  return body;
}

export function normalizeSupplierEntity(value: unknown): SupplierEntityDTO {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商主体响应格式不正确。");
  const row = value as Record<string, unknown>;
  const id = positiveBigint(row.id);
  const buyerID = positiveBigint(row.buyer_id);
  const revision = positiveBigint(row.revision);
  if (typeof row.name !== "string" || !row.name.trim() || (row.region !== "CN" && row.region !== "OVERSEAS") ||
      (row.settlement_currency !== "CNY" && row.settlement_currency !== "USD") ||
      typeof row.registration_no !== "string" || typeof row.contact !== "string" || typeof row.phone !== "string" ||
      typeof row.tax_rate !== "string" || !/^(0|1)(\.\d{1,8})?$/.test(row.tax_rate) || new Decimal(row.tax_rate).greaterThan(1) ||
      typeof row.established !== "boolean" || (row.status !== "ACTIVE" && row.status !== "FROZEN") ||
      typeof row.can_edit !== "boolean" || typeof row.created_at !== "string" || typeof row.updated_at !== "string" ||
      Number.isNaN(Date.parse(row.created_at)) || Number.isNaN(Date.parse(row.updated_at)) || Date.parse(row.updated_at) < Date.parse(row.created_at)) {
    throw new Error("供应商主体响应不符合字段契约。");
  }
  return { id, buyer_id: buyerID, name: row.name, region: row.region, registration_no: row.registration_no,
    contact: row.contact, phone: row.phone, tax_rate: new Decimal(row.tax_rate).toFixed(8),
    settlement_currency: row.settlement_currency, established: row.established, status: row.status,
    revision, created_at: row.created_at, updated_at: row.updated_at, can_edit: row.can_edit };
}

export function normalizeSupplierEntityPage(value: unknown, page: number, size: number) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商主体列表响应格式不正确。");
  const result = value as Record<string, unknown>;
  if (!Array.isArray(result.list) || !Number.isSafeInteger(result.total) || Number(result.total) < 0 || result.page !== page || result.size !== size) {
    throw new Error("供应商主体列表分页信息不符合约定。");
  }
  return { list: result.list.map(normalizeSupplierEntity), total: Number(result.total), page, size };
}
