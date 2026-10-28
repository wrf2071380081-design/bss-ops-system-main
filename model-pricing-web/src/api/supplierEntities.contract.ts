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
  tax_rate: string | null;
  settlement_currency: "CNY" | "USD";
  reason: string;
  expected_revision?: string;
}

export interface SupplierEntityDTO {
  id: string;
  buyer_id: string;
  name: string;
  aliases: string[];
  aliases_supported: boolean;
  region: "CN" | "OVERSEAS";
  registration_no: string;
  contact: string;
  phone: string;
  tax_rate: string | null;
  settlement_currency: "CNY" | "USD";
  established: boolean;
  status: "ACTIVE" | "FROZEN";
  revision: string;
  created_at: string;
  updated_at: string;
  can_edit: boolean;
}

export interface SupplierQualificationDTO {
  id: string;
  supplier_id: string;
  name: string;
  qualification_type?: string;
  valid_from?: string;
  valid_to?: string;
  review_status?: string;
  revision?: string;
  created_by?: string;
  has_attachment: boolean;
  can_download: boolean;
  file_name?: string;
  uploaded_at: string;
}

export interface SupplierQualificationPageDTO {
  list: SupplierQualificationDTO[];
  total: number;
  page: number;
  size: number;
  metadata_supported: boolean;
}

export interface SupplierQualificationMetadata {
  qualification_type: string;
  valid_from: string;
  valid_to: string;
}

export interface SupplierTestConfigDTO {
  supplier_id: string;
  configured: boolean;
  base_url: string;
  has_api_key: boolean;
  reference?: string;
  revision: string;
}

export function normalizeSupplierTestConfig(value: unknown, supplierID: string): SupplierTestConfigDTO {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商测试配置响应格式不正确。");
  const row = value as Record<string, unknown>;
  if ("api_key" in row || "key" in row || "secret" in row || "storage_path" in row || row.supplier_id !== supplierID ||
      typeof row.configured !== "boolean" || typeof row.base_url !== "string" || typeof row.has_api_key !== "boolean" ||
      typeof row.revision !== "string" || !/^(0|[1-9]\d*)$/.test(row.revision) ||
      (row.reference !== undefined && (typeof row.reference !== "string" || row.reference.length > 256))) {
    throw new Error("供应商测试配置响应不符合安全字段契约。");
  }
  if (!row.configured) {
    if (row.revision !== "0" || row.base_url !== "" || row.has_api_key || row.reference) throw new Error("未配置状态包含已有凭据数据。");
    return { supplier_id: supplierID, configured: false, base_url: "", has_api_key: false, revision: "0" };
  }
  let parsed: URL;
  try { parsed = new URL(row.base_url); } catch { throw new Error("供应商测试URL格式不正确。"); }
  if (!(parsed.protocol === "https:" || parsed.protocol === "http:") || !parsed.hostname || parsed.username || parsed.password || parsed.hash || !row.has_api_key || !/^[1-9]\d*$/.test(row.revision)) {
    throw new Error("供应商测试配置不符合约定。");
  }
  return { supplier_id: supplierID, configured: true, base_url: row.base_url, has_api_key: true,
    ...(typeof row.reference === "string" && row.reference ? { reference: row.reference } : {}), revision: row.revision };
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
  const taxInput = form.tax_rate_percent.trim();
  let taxRate: string | null = null;
  if (taxInput) {
    let percent: Decimal;
    try { percent = new Decimal(taxInput); } catch { throw new Error("税率需填写 0～100 之间的百分比。"); }
    if (!percent.isFinite() || percent.isNegative() || percent.greaterThan(100)) throw new Error("税率需填写 0～100 之间的百分比。");
    if (percent.decimalPlaces() > 2) throw new Error("当前数据库最多支持 0.01 个百分点精度。");
    taxRate = percent.div(100).toFixed(8);
  }
  const body: SupplierEntitySaveBody = {
    name, region: form.region, registration_no: form.registration_no.trim(), contact: form.contact.trim(), phone: form.phone.trim(),
    tax_rate: taxRate, settlement_currency: form.settlement_currency, reason: reason.trim(),
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
  const aliases = row.aliases === undefined ? [] : row.aliases;
  if (typeof row.name !== "string" || !row.name.trim() || (row.region !== "CN" && row.region !== "OVERSEAS") ||
      typeof row.aliases_supported !== "boolean" ||
      (row.settlement_currency !== "CNY" && row.settlement_currency !== "USD") ||
      typeof row.registration_no !== "string" || typeof row.contact !== "string" || typeof row.phone !== "string" ||
      !(row.tax_rate === null || (typeof row.tax_rate === "string" && /^(0|1)(\.\d{1,8})?$/.test(row.tax_rate) && !new Decimal(row.tax_rate).greaterThan(1))) ||
      !Array.isArray(aliases) || aliases.some(alias => typeof alias !== "string" || !alias.trim()) ||
      typeof row.established !== "boolean" || (row.status !== "ACTIVE" && row.status !== "FROZEN") ||
      typeof row.can_edit !== "boolean" || typeof row.created_at !== "string" || typeof row.updated_at !== "string" ||
      Number.isNaN(Date.parse(row.created_at)) || Number.isNaN(Date.parse(row.updated_at)) || Date.parse(row.updated_at) < Date.parse(row.created_at)) {
    throw new Error("供应商主体响应不符合字段契约。");
  }
  return { id, buyer_id: buyerID, name: row.name, aliases: [...aliases], aliases_supported: row.aliases_supported, region: row.region, registration_no: row.registration_no,
    contact: row.contact, phone: row.phone, tax_rate: row.tax_rate === null ? null : new Decimal(row.tax_rate).toFixed(8),
    settlement_currency: row.settlement_currency, established: row.established, status: row.status,
    revision, created_at: row.created_at, updated_at: row.updated_at, can_edit: row.can_edit };
}

export function normalizeSupplierEntityPage(value: unknown, page: number, size: number) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商主体列表响应格式不正确。");
  const result = value as Record<string, unknown>;
  if (typeof result.aliases_supported !== "boolean" || !Array.isArray(result.list) || !Number.isSafeInteger(result.total) || Number(result.total) < 0 || result.page !== page || result.size !== size) {
    throw new Error("供应商主体列表分页信息不符合约定。");
  }
  const list = result.list.map(row => {
    const normalized = normalizeSupplierEntity(row);
    if (normalized.aliases_supported !== result.aliases_supported) throw new Error("供应商曾用名结构状态不一致。");
    return normalized;
  });
  return { list, total: Number(result.total), page, size, aliases_supported: result.aliases_supported };
}

function normalizeSupplierQualificationRow(value: unknown, supplierID: string): SupplierQualificationDTO {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商资质记录格式不正确。");
  const row = value as Record<string, unknown>;
  const optionalText = (key: string) => {
    const value = row[key];
    if (value !== undefined && value !== null && typeof value !== "string") throw new Error(`供应商资质 ${key} 格式不正确。`);
    return typeof value === "string" && value.trim() ? value : undefined;
  };
  const validFrom = optionalText("valid_from"), validTo = optionalText("valid_to");
  const validDate = (date: string) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
    const parsed = new Date(`${date}T00:00:00.000Z`);
    return !Number.isNaN(parsed.valueOf()) && parsed.toISOString().slice(0, 10) === date;
  };
  if ((validFrom && !validDate(validFrom)) || (validTo && !validDate(validTo)) ||
      (validFrom && validTo && validFrom > validTo)) throw new Error("供应商资质有效期格式不正确。");
  const revision = optionalText("revision"), createdBy = optionalText("created_by"), qualificationType = optionalText("qualification_type"), reviewStatus = optionalText("review_status");
  if ((revision && !/^[1-9]\d*$/.test(revision)) || (createdBy && !/^[1-9]\d*$/.test(createdBy))) throw new Error("供应商资质版本或登记人格式不正确。");
  if ("file_ref" in row || "storage_path" in row || "file_path" in row ||
      typeof row.id !== "string" || !/^[1-9]\d*$/.test(row.id) || row.supplier_id !== supplierID ||
      typeof row.name !== "string" || !row.name.trim() || typeof row.has_attachment !== "boolean" ||
      typeof row.can_download !== "boolean" || (row.can_download && !row.has_attachment) ||
      (row.file_name !== undefined && (typeof row.file_name !== "string" || !row.file_name.trim() || /[\\/\u0000-\u001f\u007f]/.test(row.file_name))) ||
      (row.can_download && typeof row.file_name !== "string") ||
      typeof row.uploaded_at !== "string" || Number.isNaN(Date.parse(row.uploaded_at))) {
    throw new Error("供应商资质记录不符合字段契约。");
  }
  return { id: row.id, supplier_id: row.supplier_id, name: row.name,
    ...(qualificationType ? { qualification_type: qualificationType } : {}), ...(validFrom ? { valid_from: validFrom } : {}), ...(validTo ? { valid_to: validTo } : {}),
    ...(reviewStatus ? { review_status: reviewStatus } : {}), ...(revision ? { revision } : {}), ...(createdBy ? { created_by: createdBy } : {}),
    has_attachment: row.has_attachment, can_download: row.can_download,
    ...(typeof row.file_name === "string" ? { file_name: row.file_name } : {}), uploaded_at: row.uploaded_at };
}

export function normalizeSupplierQualificationPage(value: unknown, supplierID: string, page: number, size: number) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商资质响应格式不正确。");
  const result = value as Record<string, unknown>;
  if (typeof result.metadata_supported !== "boolean") throw new Error("供应商资质结构能力状态不符合字段契约。");
  if (!Array.isArray(result.list) || !Number.isSafeInteger(result.total) || Number(result.total) < 0 || result.page !== page || result.size !== size) {
    throw new Error("供应商资质分页信息不符合约定。");
  }
  const list = result.list.map(item => normalizeSupplierQualificationRow(item, supplierID));
  return { list, total: Number(result.total), page, size, metadata_supported: result.metadata_supported };
}

export function normalizeSupplierQualification(value: unknown, supplierID: string): SupplierQualificationDTO {
  return normalizeSupplierQualificationRow(value, supplierID);
}
