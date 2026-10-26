import { standardModelVersion } from "./standardModels.contract.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";
export type PlanMaintenanceAction = "revise" | "cancel";
function authorize(plan: PlanSummary, action: PlanMaintenanceAction, reason: string) {
  standardModelVersion(plan.id); standardModelVersion(plan.revision);
  const allowedKind = action === "revise" ? ["ONLINE", "OFFLINE"].includes(plan.kind) : ["ONLINE", "OFFLINE", "PRICE_CHANGE"].includes(plan.kind);
  if (BigInt(plan.revision) >= 9223372036854775807n || !plan.is_initiator || !allowedKind || !(action === "revise" ? plan.status === "PENDING" && plan.permissions.can_revise : ["PENDING", "BLOCKED"].includes(plan.status) && plan.permissions.can_cancel)) throw new Error("当前计划状态或权限不允许此操作，请重新加载核对。");
  if (!reason.trim()) throw new Error("请填写原因。");
  return { expected_revision: plan.revision, reason: reason.trim() };
}
export function buildPlanRevision(plan: PlanSummary, reason: string, dueAt: string) {
  const body = authorize(plan, "revise", reason);
  if (!dueAt || !Number.isFinite(Date.parse(dueAt))) throw new Error("请填写有效的预计时间。");
  return { ...body, due_at: new Date(dueAt).toISOString() };
}
export function buildPlanCancellation(plan: PlanSummary, reason: string) { return authorize(plan, "cancel", reason); }
export function validateMaintainedPlan(value: unknown, plan: PlanSummary, body: { expected_revision: string; reason: string; due_at?: string }, action: PlanMaintenanceAction) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("计划响应为空，请重新加载核对。");
  const result = value as Record<string, unknown>;
  for (const key of ["id", "resource_id", "region", "kind", "created_by", "expected_resource_revision"] as const) if (result[key] !== plan[key]) throw new Error("计划响应与原计划不一致，请重新加载核对。");
  if (result.revision !== (BigInt(body.expected_revision) + 1n).toString() || result.status !== (action === "revise" ? "PENDING" : "CANCELED") || result.reason !== (action === "revise" ? body.reason : plan.reason) || typeof result.due_at !== "string" || Date.parse(result.due_at) !== Date.parse(action === "revise" ? body.due_at! : plan.due_at) || typeof result.created_at !== "string" || Date.parse(result.created_at) !== Date.parse(plan.created_at) || result.executed_at !== plan.executed_at) throw new Error("计划响应与提交依据不一致，请重新加载核对。");
  if (plan.kind === "PRICE_CHANGE" && (result.assignee_id !== plan.assignee_id || result.supplier_model_id !== plan.supplier_model_id || result.before_supplier_model_id !== plan.before_supplier_model_id || result.after_supplier_model_id !== plan.after_supplier_model_id || result.before_coefficient !== plan.before_coefficient || result.after_coefficient !== plan.after_coefficient)) throw new Error("改价计划取消回执改变了办理人、接口模型 ID 或系数快照，请重新加载核对。");
  return structuredClone(result);
}
