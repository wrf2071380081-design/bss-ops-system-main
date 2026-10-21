import { standardModelVersion } from "./standardModels.contract.ts";
import type { NoticePage, NoticeQuery, NoticeReadReceipt, NoticeView, PlanSummary, SupplyNotice } from "./supplyWorkbench.types.ts";

export const planKindLabels: Record<string, string> = { ONLINE: "上架", OFFLINE: "下架", PRICE_CHANGE: "调价" };
export const planStatusLabels: Record<string, string> = { PENDING: "待执行", EXECUTING: "执行中", EXECUTED: "已执行", CANCELED: "已取消", BLOCKED: "已阻断" };
function invalid(): never { throw new Error("公告或计划响应不符合约定，请重新加载。"); }
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid();
  return value as Record<string, unknown>;
}
function timestamp(value: unknown): value is string {
  return typeof value === "string" && /^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value));
}
export function noticeImpactText(notice: SupplyNotice): string {
  return notice.affected_status === "PENDING" ? "影响范围待计算" : `${notice.affected_customer_count} 个关联客户`;
}
export function validateNoticeView(value: unknown, expectedID?: string, selectedStaffID?: string): NoticeView {
  const view = object(value), notice = object(view.notice), permissions = object(view.permissions);
  standardModelVersion(notice.id); standardModelVersion(notice.resource_id); standardModelVersion(view.staff_id);
  if ((expectedID && notice.id !== expectedID) || (selectedStaffID && view.staff_id !== selectedStaffID)) invalid();
  if (!["CN", "OVERSEAS"].includes(String(notice.region)) || !timestamp(notice.published_at)) invalid();
  for (const key of ["resource_name", "kind", "title", "content", "reason"]) if (typeof notice[key] !== "string") invalid();
  if (!String(notice.title).trim() || !String(notice.kind).trim() || typeof view.staff_name !== "string" || typeof permissions.can_mark_read !== "boolean") invalid();
  if (notice.source_event_id !== undefined) standardModelVersion(notice.source_event_id);
  if (notice.affected_status === "PENDING") { if (notice.affected_customer_count !== undefined) invalid(); }
  else if (notice.affected_status === "READY") {
    if (typeof notice.affected_customer_count !== "string" || !/^(0|[1-9]\d*)$/.test(notice.affected_customer_count) || BigInt(notice.affected_customer_count) > 18446744073709551615n) invalid();
  } else invalid();
  if (view.read_status === "READ") { if (!timestamp(view.read_at) || permissions.can_mark_read) invalid(); }
  else if (view.read_status === "UNREAD") { if (view.read_at !== null) invalid(); }
  else invalid();
  return structuredClone(view) as unknown as NoticeView;
}
export function validateNoticePage(value: unknown, query: NoticeQuery): NoticePage {
  const page = object(value);
  if (!Array.isArray(page.list) || !Number.isSafeInteger(page.total) || (page.total as number) < 0 || page.page !== query.page || page.size !== query.size || page.list.length > query.size || (page.total as number) < page.list.length) invalid();
  if ((query.page - 1) * query.size >= (page.total as number) && page.list.length > 0) invalid();
  const seen = new Set<string>();
  const list = page.list.map(value => {
    const row = validateNoticeView(value), key = `${row.notice.id}:${row.staff_id}`;
    if (seen.has(key) || (query.resource_id && row.notice.resource_id !== query.resource_id) || (query.region && row.notice.region !== query.region) || (query.read_status && query.read_status !== "ALL" && row.read_status !== query.read_status)) invalid();
    seen.add(key); return row;
  });
  return { list, total: page.total as number, page: query.page, size: query.size };
}
export function validateReadReceipt(value: unknown, noticeID: string, staffID: string): NoticeReadReceipt {
  const receipt = object(value);
  if (receipt.notice_id !== noticeID || receipt.staff_id !== staffID || !timestamp(receipt.read_at) || typeof receipt.created !== "boolean") invalid();
  return { ...receipt } as unknown as NoticeReadReceipt;
}
export function validatePlanSummary(value: unknown, expectedID: string): PlanSummary {
  const plan = object(value);
  for (const key of ["id", "resource_id", "created_by", "revision", "expected_resource_revision", "current_resource_revision"]) standardModelVersion(plan[key]);
  if (plan.id !== expectedID || !["CN", "OVERSEAS"].includes(String(plan.region)) || !planKindLabels[String(plan.kind)] || !planStatusLabels[String(plan.status)]) invalid();
  if (plan.read_only !== true || typeof plan.is_initiator !== "boolean" || typeof plan.precondition_matches !== "boolean" || plan.precondition_matches !== (plan.expected_resource_revision === plan.current_resource_revision)) invalid();
  if (!Array.isArray(plan.blocked_reasons) || plan.blocked_reasons.some(reason => typeof reason !== "string") || (!plan.precondition_matches && !plan.blocked_reasons.includes("RESOURCE_CHANGED"))) invalid();
  if (!timestamp(plan.due_at) || !timestamp(plan.created_at) || (plan.executed_at !== null && !timestamp(plan.executed_at)) || (plan.status === "EXECUTED" && plan.executed_at === null)) invalid();
  if (typeof plan.creator_name !== "string" || typeof plan.reason !== "string" || !plan.reason.trim()) invalid();
  const permissions = plan.permissions === undefined ? { can_revise: false, can_cancel: false, can_execute: false } : object(plan.permissions);
  if (["can_revise", "can_cancel", "can_execute"].some(key => typeof permissions[key] !== "boolean")) invalid();
  if ((permissions.can_revise && (!plan.is_initiator || plan.status !== "PENDING" || !["ONLINE", "OFFLINE"].includes(String(plan.kind)))) || (permissions.can_cancel && (!plan.is_initiator || !["PENDING", "BLOCKED"].includes(String(plan.status)) || !["ONLINE", "OFFLINE"].includes(String(plan.kind)))) || permissions.can_execute) invalid();
  return structuredClone({ ...plan, permissions }) as unknown as PlanSummary;
}
