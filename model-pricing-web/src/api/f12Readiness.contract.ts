import Decimal from "decimal.js";
import type { F12SubjectType } from "./f12SupplierRecords.contract";

export interface F12ReadinessRequest {
  subjectType: F12SubjectType;
  subjectID: string;
  flowID: string;
  moduleKey: string;
}

export interface F12ReadinessRequirement {
  item: "CONTRACT" | "CREDIT";
  currency?: string;
}

export interface F12ReadinessSnapshot {
  subject_type: F12SubjectType;
  subject_id: string;
  source_flow_id: string;
  source_module_key: string;
  source_flow_revision: number;
  required_items: F12ReadinessRequirement[];
  ready: boolean;
  contracts: Array<{ contract_id: string; contract_key: string; version: number }>;
  credits: Array<{ credit_line_id: string; version: number; currency: string; amount: string }>;
  missing_reasons: string[];
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("F12 资料齐备状态响应格式不正确。");
  return value as Record<string, unknown>;
}

function positiveID(value: unknown): string {
  const id = String(value ?? "");
  if (!/^[1-9]\d*$/.test(id) || id.length > 19 || (id.length === 19 && id > "9223372036854775807")) throw new Error("F12 资料齐备状态包含无效编号。");
  return id;
}

function positiveVersion(value: unknown): number {
  const raw = String(value ?? "");
  if (!/^[1-9]\d*$/.test(raw)) throw new Error("F12 资料齐备状态包含无效版本号。");
  const version = Number(raw);
  if (!Number.isSafeInteger(version) || version > 2147483647) throw new Error("F12 资料齐备状态包含超出范围的版本号。");
  return version;
}

function normalizeRequirement(value: unknown): F12ReadinessRequirement {
  const row = object(value);
  const item = row.item;
  const currency = row.currency === undefined ? "" : String(row.currency);
  if (item === "CONTRACT") {
    if (currency) throw new Error("合同齐备条件不能包含币种。");
    return { item };
  }
  if (item !== "CREDIT" || (currency && !/^[A-Z]{3}$/.test(currency))) throw new Error("F12 授信齐备条件不正确。");
  return { item, ...(currency ? { currency } : {}) };
}

/** Validate the server-owned readiness projection against the requested subject and flow. */
export function normalizeF12Readiness(value: unknown, expected: F12ReadinessRequest): F12ReadinessSnapshot {
  const row = object(value);
  const revision = positiveVersion(row.source_flow_revision);
  if (row.subject_type !== expected.subjectType || String(row.subject_id) !== expected.subjectID ||
      String(row.source_flow_id) !== expected.flowID || row.source_module_key !== expected.moduleKey || typeof row.ready !== "boolean" ||
      !Array.isArray(row.required_items) || row.required_items.length === 0 || !Array.isArray(row.contracts) ||
      !Array.isArray(row.credits) || !Array.isArray(row.missing_reasons)) {
    throw new Error("F12 资料齐备状态与当前主体或办理流程不匹配。");
  }

  const required = row.required_items.map(normalizeRequirement);
  const requirementKeys = new Set<string>();
  for (const item of required) {
    const key = `${item.item}:${item.currency || ""}`;
    if (requirementKeys.has(key)) throw new Error("F12 资料齐备状态包含重复条件。");
    requirementKeys.add(key);
  }
  if (required.some(item => item.item === "CREDIT" && !item.currency) && required.some(item => item.item === "CREDIT" && Boolean(item.currency))) {
    throw new Error("通用授信条件不能与指定币种条件并存。");
  }

  const contracts = row.contracts.map(value => {
    const contract = object(value);
    return { contract_id: positiveID(contract.contract_id), contract_key: positiveID(contract.contract_key), version: positiveVersion(contract.version) };
  });
  if (new Set(contracts.map(contract => contract.contract_key)).size !== contracts.length) throw new Error("F12 资料齐备状态包含重复合同档案。");

  const credits = row.credits.map(value => {
    const credit = object(value);
    const currency = String(credit.currency ?? "");
    const amount = String(credit.amount ?? "");
    let parsedAmount: Decimal;
    try { parsedAmount = new Decimal(amount); } catch { throw new Error("F12 资料齐备状态包含无效授信金额。"); }
    if (!/^[A-Z]{3}$/.test(currency) || !parsedAmount.isFinite() || !parsedAmount.isPositive()) throw new Error("F12 资料齐备状态包含无效授信资料。");
    return { credit_line_id: positiveID(credit.credit_line_id), version: positiveVersion(credit.version), currency, amount };
  });
  if (new Set(credits.map(credit => credit.currency)).size !== credits.length) throw new Error("F12 资料齐备状态包含重复授信币种。");

  const missingReasons = row.missing_reasons.map(reason => String(reason));
  const creditRequirements = required.filter(item => item.item === "CREDIT");
  const expectedMissing = new Set<string>();
  if (required.some(item => item.item === "CONTRACT") && contracts.length === 0) expectedMissing.add("CONTRACT_REQUIRED");
  if (creditRequirements.length === 1 && !creditRequirements[0]?.currency && credits.length === 0) expectedMissing.add("CREDIT_REQUIRED");
  for (const item of creditRequirements) {
    if (item.currency && !credits.some(credit => credit.currency === item.currency)) expectedMissing.add(`CREDIT_REQUIRED:${item.currency}`);
  }
  if ((!required.some(item => item.item === "CONTRACT") && contracts.length > 0) ||
      credits.some(credit => !creditRequirements.some(item => !item.currency || item.currency === credit.currency))) {
    throw new Error("F12 资料齐备状态包含未要求的资料。");
  }
  if (missingReasons.some(reason => reason !== "CONTRACT_REQUIRED" && !/^CREDIT_REQUIRED(?::[A-Z]{3})?$/.test(reason)) ||
      new Set(missingReasons).size !== missingReasons.length || missingReasons.length !== expectedMissing.size ||
      missingReasons.some(reason => !expectedMissing.has(reason)) || row.ready !== (expectedMissing.size === 0)) {
    throw new Error("F12 资料齐备状态的阻断原因与结果不一致。");
  }

  return {
    subject_type: expected.subjectType, subject_id: expected.subjectID, source_flow_id: expected.flowID,
    source_module_key: expected.moduleKey, source_flow_revision: revision, required_items: required,
    ready: row.ready, contracts, credits, missing_reasons: missingReasons,
  };
}
