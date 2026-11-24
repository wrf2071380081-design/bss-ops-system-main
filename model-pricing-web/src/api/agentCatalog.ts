import { apiRequest } from "./http";

export const catalogVendors = {
  deepseek: "DeepSeek", openai: "OpenAI", google: "Google",
  aliyun: "阿里云", zhipu: "智谱", anthropic: "Anthropic",
} as const;
export type Vendor = keyof typeof catalogVendors;
export type Run = { runId: string; status: string; run?: { startedAt?: string; finishedAt?: string }; pendingModelIds: string[] };
export type Task = { status: string; result: Run | { runs: { vendor: Vendor; crawl: Run | null }[] } | null };
export type Source = { vendor: Vendor; runId: string };
export type ModelCrawlStatus = {state:'COMPLETE'|'PARTIAL'|'UNAVAILABLE'|'NOT_OBSERVED'|'RUNNING'|'UNKNOWN'; attemptedAt?:string; lastCompleteAt?:string};
export type Plan = { revision?:number;
  id: string; active: boolean; localTime: string; recurrence: string; validFrom: string | null; validUntil: string | null;
  history: { vendor: Vendor; at: string; run: string }[];
};
export type ReviewRow = { id: string; name: string; pending: boolean; change?: string };
export type Difference = { field: string; old: unknown; new: unknown; kind?: string };
export type ReviewDetail = {
  pendingModelIds: string[]; canReview?: boolean;
  latestAttempt?: { id: string; result?: { models: {
    id: string; name: string; coverage?:string; bss: Record<string, unknown>; draft?: { overrides?: Record<string, unknown> };
    specEvidence?: Record<string, { state?: string; sourceUrl?: string; quote?: string }>;
    fieldEvidence?: Record<string, { state?: string; sourceUrl?: string; quote?: string }>;
    reviewDiff?: { baseline?: { kind: string; startedAt: string }; previous_batch?: { status: string; fields?: Difference[] } };
  }[] } };
  liveBss?: {
    id?: string; revision?: string; status: string; fields: Difference[]; checkedAt?: string;
    current?: Record<string, unknown>; official?: Record<string, unknown>;
  };
};
const url = "/internal/agent-catalog";
export const agentCatalog = {
  modelStatus: (ids:string[], signal?:AbortSignal) => apiRequest<Record<string,ModelCrawlStatus>>({url:url+'/model-status',params:{ids:ids.join(',')},signal}),
  start: (requestId: string, vendors: Vendor[], signal?: AbortSignal) =>
    apiRequest<{ sessionId: string }>({ url: url + "/tasks", method: "POST", data: { kind: "start", action: "deepseek-crawl", requestId, vendors }, signal }),
  task: (session: string, signal?: AbortSignal) =>
    apiRequest<{ task: Task | null }>({ url: url + "/tasks", params: { session }, signal }),
  plans: (signal?: AbortSignal) =>
    apiRequest<{ canManage: boolean; plans: Plan[] }>({ url: url + "/schedules", signal }),
  savePlan: (data: Record<string, string>) =>
    apiRequest({ url: url + "/schedules", method: "POST", data: new URLSearchParams(data), headers: { "Content-Type": "application/x-www-form-urlencoded" } }),
  index: (source: Source, page: number, signal: AbortSignal) =>
    apiRequest<{ rows: ReviewRow[]; pagination?: { total: number; pageSize: number } }>({
      url: url + "/review", params: { ...source, view: "index", page }, signal,
    }),
  detail: (source: Source, modelId: string, signal: AbortSignal) =>
    apiRequest<ReviewDetail>({ url: url + "/review", params: { ...source, view: "model", modelId }, signal }),
  review: (source: Source, modelId: string, action: "approve" | "draft", values: Record<string, unknown>, expected?: { id: string | null; revision: string | null }) =>
    apiRequest<{ bssSync?: { model_id: string; status: string }[] }>({
      url: url + "/review", method: "POST",
      data: { ...source, modelId, action, overrides: values, ...(expected ? { partial: true, expected } : {}) },
    }),
};
