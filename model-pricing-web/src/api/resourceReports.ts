import { apiFileRequest, apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import { resourceCacheDisplay, resourceMetricDisplay } from "./resources.contract";
import type { ResourceCache } from "./resources.types";

// A/B integration proposal: no report body or storage URL is copied into A.
export interface ResourceReport {
  id: string; resource_id: string; status: "RUNNING" | "COMPLETED" | "FAILED";
  created_at: string; finished_at: string | null; target_matches: boolean;
  connectivity_passed: boolean | null; pressure_passed: boolean | null;
  metrics: { connection_ms: string | null; rps: string | null; tps: string | null };
  cache_stats: ResourceCache;
}
export interface ResourceReportDetail extends ResourceReport {
  files: { id: string; name: string; can_download: boolean }[];
}
function invalid(): never { throw new Error("报告响应与资源不匹配或字段不完整，请重新加载。"); }
export function validateResourceReport(value: unknown, resourceID: string): ResourceReport {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid();
  const row = value as ResourceReport;
  standardModelVersion(row.id);
  if (row.resource_id !== resourceID || !["RUNNING", "COMPLETED", "FAILED"].includes(row.status) || typeof row.target_matches !== "boolean") invalid();
  const time = (value: unknown) => typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value) && /(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value));
  if (!time(row.created_at) || (row.finished_at !== null && !time(row.finished_at)) || (row.status !== "RUNNING" && row.finished_at === null)) invalid();
  if (![row.connectivity_passed, row.pressure_passed].every(value => value === null || typeof value === "boolean") || (row.status === "COMPLETED" && (row.connectivity_passed === null || row.pressure_passed === null))) invalid();
  if (!row.metrics || !row.cache_stats || row.cache_stats.report_id !== row.id) invalid();
  for (const key of ["connection_ms", "rps", "tps"] as const) {
    const metric = row.metrics[key];
    if (metric !== null && (typeof metric !== "string" || !/^\d+(?:\.\d+)?$/.test(metric))) invalid();
    resourceMetricDisplay(metric, "COMPLETED");
  }
  if (!["PROVIDED", "NOT_PROVIDED", "NOT_APPLICABLE", "NOT_TESTED"].includes(row.cache_stats.status)) invalid();
  if (row.cache_stats.scene !== undefined && typeof row.cache_stats.scene !== "string") invalid();
  resourceCacheDisplay(row.cache_stats, row.cache_stats.status === "NOT_TESTED" ? "NOT_TESTED" : "COMPLETED");
  // Explicit safe projection. Never forward credentials or provider payloads.
  return { id: row.id, resource_id: resourceID, status: row.status, created_at: row.created_at, finished_at: row.finished_at, target_matches: row.target_matches,
    connectivity_passed: row.connectivity_passed, pressure_passed: row.pressure_passed,
    metrics: { connection_ms: row.metrics.connection_ms, rps: row.metrics.rps, tps: row.metrics.tps },
    cache_stats: { status: row.cache_stats.status, report_id: row.id, ...(row.cache_stats.status === "PROVIDED" ? { hit_tokens: row.cache_stats.hit_tokens, input_tokens: row.cache_stats.input_tokens } : {}), scene: row.cache_stats.scene },
  };
}
const base = (resourceID: string) => `/internal/resources/${standardModelVersion(resourceID)}/test-reports`;
export const resourceReportsApi = {
  async list(resourceID: string, page: number, signal?: AbortSignal) {
    const result = await apiRequest<{ list: unknown[]; total: number; page: number; size: number }>({ url: base(resourceID), params: { page, size: 10 }, signal });
    if (!result || !Array.isArray(result.list) || result.page !== page || result.size !== 10 || !Number.isSafeInteger(result.total) || result.total < result.list.length || result.list.length > 10) invalid();
    const list = result.list.map(value => validateResourceReport(value, resourceID));
    if (new Set(list.map(row => row.id)).size !== list.length) invalid();
    return { ...result, list };
  },
  async detail(resourceID: string, reportID: string, signal?: AbortSignal): Promise<ResourceReportDetail> {
    const result = await apiRequest<ResourceReportDetail>({ url: `${base(resourceID)}/${standardModelVersion(reportID)}`, signal });
    const report = validateResourceReport(result, resourceID);
    if (report.id !== reportID || !Array.isArray(result.files)) invalid();
    const seen = new Set<string>();
    const files = result.files.map(file => {
      standardModelVersion(file.id);
      if (seen.has(file.id) || typeof file.name !== "string" || !file.name.trim() || typeof file.can_download !== "boolean") invalid();
      seen.add(file.id);
      return { id: file.id, name: file.name, can_download: file.can_download };
    });
    return { ...report, files };
  },
  async file(resourceID: string, reportID: string, fileID: string) {
    return apiFileRequest({ url: `${base(resourceID)}/${standardModelVersion(reportID)}/files/${standardModelVersion(fileID)}` });
  },
};
