import { standardModelVersion } from "./standardModels.contract.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";

export function buildPlanExecution(plan: PlanSummary, confirmed: boolean, completionNote: string, reason: string) {
  standardModelVersion(plan.id); standardModelVersion(plan.revision);
  const allowed = plan.kind === "PRICE_CHANGE" ? plan.permissions.can_execute && !!plan.assignee_id : plan.is_initiator && plan.permissions.can_execute && ["ONLINE", "OFFLINE"].includes(plan.kind);
  if (!allowed || plan.status !== "PENDING" || BigInt(plan.revision) >= 9223372036854775807n) throw new Error("当前计划状态或权限不允许登记完成，请重新加载核对。");
  if (!confirmed) throw new Error("请确认已在外部平台完成本次操作。");
  if (!completionNote.trim()) throw new Error("请填写实际完成依据。");
  if (!reason.trim()) throw new Error("请填写登记原因。");
  return { expected_revision: plan.revision, confirmed: true, completion_note: completionNote.trim(), reason: reason.trim() };
}

export function validatePlanExecutionReceipt(value: unknown, plan: PlanSummary, blocked = false) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("执行回执缺失，请重新加载计划核对结果。");
  const result = value as Record<string, unknown>;
  for (const key of ["id", "resource_id", "region", "kind", "created_by", "expected_resource_revision"] as const)
    if (result[key] !== plan[key]) throw new Error("执行回执与当前计划不一致，请重新加载核对。");
  if (result.revision !== (BigInt(standardModelVersion(plan.revision)) + 1n).toString() || result.status !== (blocked ? "BLOCKED" : "EXECUTED") || result.reason !== plan.reason || result.due_at !== plan.due_at || result.created_at !== plan.created_at ||
      (blocked ? result.executed_at !== null : typeof result.executed_at !== "string" || !Number.isFinite(Date.parse(result.executed_at))))
    throw new Error("执行结果或修订号不符合预期，请重新加载计划核对。");
  if (plan.kind === "PRICE_CHANGE" && (result.assignee_id !== plan.assignee_id || result.supplier_model_id !== plan.supplier_model_id || result.before_supplier_model_id !== plan.before_supplier_model_id || result.after_supplier_model_id !== plan.after_supplier_model_id || result.before_coefficient !== plan.before_coefficient || result.after_coefficient !== plan.after_coefficient || (!blocked && (result.executed_by !== plan.assignee_id || typeof result.completion_note !== "string" || !result.completion_note.trim()))))
    throw new Error("改价计划执行回执与指定办理人、接口模型 ID 或系数不一致，请重新加载核对。");
  return structuredClone(result);
}
