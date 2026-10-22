import Decimal from "decimal.js";
import { emptyStandardModel, modelTypeLabels, standardModelVersion, validateStandardModel } from "./standardModels.contract.ts";
import type { ResourceCache, ResourceGroup, ResourceQuery, ResourceRow } from "./resources.types.ts";

export const resourceStateLabels: Record<string, string> = { PENDING_TEST: "待测试", PENDING_ACCESS: "待接入", ADMITTED: "已接入", OFFLINE: "已下架" };
export const resourceRegionLabels: Record<string, string> = { CN: "国内", OVERSEAS: "海外" };
export const resourceBlockLabels: Record<string, string> = { RESOURCE_NOT_ADMITTED: "资源尚未接入或已下架", SUPPLIER_INACTIVE: "供应商已停用", SUPPLIER_REGION_MISMATCH: "供应商区域不匹配", MODEL_NOT_READY: "标准模型资料或核验尚未就绪", FORMAL_CONFIG_UNAVAILABLE: "正式接口配置尚未就绪", FORMAL_CONFIG_CHANGED: "正式配置已变更，需重新确认绑定", TESTS_REQUIRED: "尚无该资源的有效测试报告", TEST_TARGET_CHANGED: "测试目标已变化，原报告不再适用", TEST_EVIDENCE_INVALID: "测试依据已失效", TESTS_NOT_PASSED: "自身连通或压力测试尚未通过" };
const groupedSort = ["id", "name", "company", "model_type", "supplier_count", "resource_count", "available_count", "coefficient_min", "coefficient_max"];
const flatSort = ["id", "supplier_name", "region", "coefficient", "state", "updated_at", "connection_ms", "rps", "tps", "cache_hit_rate"];
function invalid(): never { throw new Error("供应资源响应不符合约定，请重新查询。"); }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== "object" || Array.isArray(value)) invalid(); return value as Record<string, unknown>; }
function coefficient(value: unknown): string {
  if (typeof value !== "string" || !/^\d+(?:\.\d{1,4})?$/.test(value) || !new Decimal(value).gt(0) || new Decimal(value).gt(1)) invalid();
  return value;
}
function integer(value: unknown): value is number { return Number.isSafeInteger(value) && (value as number) >= 0; }
function version(value: unknown) { return value === "0" ? "0" : standardModelVersion(value); }
export function buildResourceQuery(source: Partial<ResourceQuery>, expand?: { modelID: string; page?: number; size?: number; sort_by?: string; order?: string }): ResourceQuery {
  const view = expand ? "flat" : source.view || "grouped";
  const result: ResourceQuery = { view, page: expand ? expand.page ?? 1 : source.page ?? 1, size: expand ? expand.size ?? 20 : source.size ?? 20, sort_by: expand ? expand.sort_by || "coefficient" : source.sort_by || (view === "flat" ? "coefficient" : "name"), order: expand ? expand.order || "asc" : source.order || "asc" };
  for (const key of ["keyword", "company", "model_type", "supplier_id", "region", "state"] as const) if (source[key]) result[key] = source[key]!.trim();
  if (expand) result.standard_model_id = standardModelVersion(expand.modelID);
  else if (source.standard_model_id) result.standard_model_id = standardModelVersion(source.standard_model_id);
  if (!["grouped", "flat"].includes(view) || !Number.isSafeInteger(result.page) || result.page < 1 || !Number.isSafeInteger(result.size) || result.size < 1 || result.size > 100 || result.page > Math.floor(Number.MAX_SAFE_INTEGER / result.size)) throw new Error("分页参数不正确。");
  if (result.supplier_id) standardModelVersion(result.supplier_id);
  if ((result.region && !resourceRegionLabels[result.region]) || (result.state && !resourceStateLabels[result.state]) || (result.model_type && !modelTypeLabels[result.model_type]) || !["asc", "desc"].includes(result.order) || !(view === "grouped" ? groupedSort : flatSort).includes(result.sort_by)) throw new Error("筛选或排序参数不正确。");
  return result;
}
export function resourceMetricDisplay(value: string | null, status: string): string {
  if (status === "STALE") return "目标已变化";
  if (status === "NOT_TESTED") return "未测试";
  if (value === null) return "未提供";
  if (typeof value !== "string" || !/^\d+(?:\.\d+)?$/.test(value)) invalid();
  return value;
}
export function resourceCacheDisplay(cache: ResourceCache, testStatus: string): string {
  if (testStatus === "STALE") return "目标已变化";
  if (testStatus === "NOT_TESTED") return "未测试";
  if (cache.status === "NOT_PROVIDED") return "未提供";
  if (cache.status === "NOT_APPLICABLE") return "不适用";
  if (cache.status !== "PROVIDED" || typeof cache.hit_tokens !== "string" || typeof cache.input_tokens !== "string" || !/^\d+$/.test(cache.hit_tokens) || !/^\d+$/.test(cache.input_tokens)) invalid();
  const hit = BigInt(cache.hit_tokens), input = BigInt(cache.input_tokens);
  if (input === 0n || hit > input) invalid();
  const hundredths = (hit * 10000n * 2n + input) / (input * 2n);
  return `${hundredths / 100n}.${String(hundredths % 100n).padStart(2, "0")}%`;
}
export function validateResourceGroup(value: unknown): ResourceGroup {
  const row = object(value); standardModelVersion(row.id);
  for (const key of ["name", "official_id", "company", "model_type"]) if (typeof row[key] !== "string") invalid();
  if (row.model_type && !modelTypeLabels[String(row.model_type)]) invalid();
  for (const key of ["supplier_count", "resource_count", "available_count"]) if (!integer(row[key])) invalid();
  if ((row.resource_count as number) < 1 || (row.supplier_count as number) > (row.resource_count as number) || (row.available_count as number) > (row.resource_count as number)) invalid();
  if (new Decimal(coefficient(row.coefficient_min)).gt(coefficient(row.coefficient_max))) invalid();
  if (!Array.isArray(row.regions) || !row.regions.length || row.regions.some(region => !resourceRegionLabels[region])) invalid();
  const states = object(row.state_counts);
  if (Object.entries(states).some(([key, count]) => !resourceStateLabels[key] || !integer(count)) || Object.values(states).reduce<number>((total, count) => total + (count as number), 0) !== row.resource_count) invalid();
  return structuredClone(row) as unknown as ResourceGroup;
}
export function validateResourceRow(value: unknown, expectedID?: string): ResourceRow {
  const row = object(value);
  for (const key of ["id", "supplier_id", "standard_model_id", "buyer_id", "revision"]) standardModelVersion(row[key]);
  if (expectedID !== undefined && row.id !== expectedID) invalid();
  version(row.formal_config_revision);
  if (row.formal_config_id) standardModelVersion(row.formal_config_id);
  for (const key of ["supplier_name", "model_name", "official_id", "company", "model_type", "supplier_model_id", "created_at", "updated_at"]) if (typeof row[key] !== "string") invalid();
  if (!resourceRegionLabels[String(row.region)] || !resourceStateLabels[String(row.state)] || typeof row.ever_admitted !== "boolean") invalid();
  coefficient(row.coefficient);
  const test = object(row.test), cache = object(test.cache_stats), metrics = object(test.metrics), readiness = object(row.quote_readiness), permissions = object(row.permissions);
  if (!["NOT_TESTED", "STALE", "COMPLETED"].includes(String(test.status)) || ["connectivity_passed", "pressure_passed", "valid"].some(key => typeof test[key] !== "boolean") || typeof permissions.can_edit !== "boolean" || typeof readiness.eligible !== "boolean" || !Array.isArray(readiness.blocked_reasons) || readiness.blocked_reasons.some(reason => typeof reason !== "string")) invalid();
  if (test.report_id) { standardModelVersion(test.report_id); if (cache.report_id !== test.report_id || readiness.test_evidence_ref !== test.report_id) invalid(); }
  else if (test.status !== "NOT_TESTED" || cache.report_id || readiness.test_evidence_ref) invalid();
  if (test.status === "NOT_TESTED" && cache.status !== "NOT_TESTED") invalid();
  if (test.status === "COMPLETED" && !["PROVIDED", "NOT_PROVIDED", "NOT_APPLICABLE"].includes(String(cache.status))) invalid();
  if (readiness.resource_revision !== row.revision || readiness.formal_config_revision !== row.formal_config_revision) invalid();
  standardModelVersion(readiness.model_version_no);
  if (readiness.eligible && (readiness.blocked_reasons.length || row.state !== "ADMITTED" || !row.ever_admitted || test.status !== "COMPLETED" || !test.valid || !test.connectivity_passed || !test.pressure_passed)) invalid();
  resourceCacheDisplay(cache as unknown as ResourceCache, String(test.status));
  for (const key of ["connection_ms", "rps", "tps"]) resourceMetricDisplay(metrics[key] as string | null, String(test.status));
  if (!Array.isArray(row.official_price_items)) invalid();
  const officialPrices = row.official_price_items.map(value => {
    const price = object(value);
    return { ...price, dimensions: price.dimensions == null ? {} : object(price.dimensions) };
  });
  if (validateStandardModel({ ...emptyStandardModel(), price_items: officialPrices as ResourceRow["official_price_items"] }).length) invalid();
  return structuredClone({ ...row, official_price_items: officialPrices }) as unknown as ResourceRow;
}
