import { standardModelVersion } from "./standardModels.contract.ts";
import type { ResourceRow } from "./resources.types.ts";
import type { PlanSummary } from "./supplyWorkbench.types.ts";

function units(value: string): bigint {
  if (!/^\d+(?:\.\d{1,4})?$/.test(value.trim())) throw new Error("采购系数须为大于0且不超过1的小数，最多4位。");
  const [whole, fraction = ""] = value.trim().split(".");
  return BigInt(whole) * 10000n + BigInt((fraction + "0000").slice(0, 4));
}
function fixed(value: bigint) { return `${value / 10000n}.${(value % 10000n).toString().padStart(4, "0")}`; }
function validateAmount(value: string) {
  const next = units(value);
  if (next <= 0n || next > 10000n) throw new Error("新采购系数须在0到1之间。");
  return fixed(next);
}
function validateAssignment(id: string) {
  standardModelVersion(id);
  if (BigInt(id) > 9223372036854775807n) throw new Error("请选择有效的内部办理人。");
}
function validateDue(dueAt: string) {
  if (!dueAt || !Number.isFinite(Date.parse(dueAt))) throw new Error("请填写有效的约定调整时间。");
}
function validateSupplierModelID(value: string) {
  const candidate = value.trim();
  if (!candidate || candidate.length > 128) throw new Error("请填写有效的供应商接口模型 ID（最多128个字符）。");
  return candidate;
}

export function buildPriceChangePlanCreate(resource: ResourceRow, supplierModelID: string, afterCoefficient: string, assigneeID: string, reason: string, dueAt: string) {
  if (!resource.permissions.can_edit) throw new Error("仅资源归属采购人可以创建内部改价计划。");
  if (!resource.ever_admitted || !["ADMITTED", "OFFLINE"].includes(resource.state)) throw new Error("只有已有上架历史的资源可以建立内部改价计划。");
  if (!reason.trim()) throw new Error("请填写改价说明。");
  validateAssignment(assigneeID); validateDue(dueAt);
  const targetModelID = validateSupplierModelID(supplierModelID);
  const targetCoefficient = validateAmount(afterCoefficient);
  if (targetModelID === resource.supplier_model_id && targetCoefficient === fixed(units(resource.coefficient))) throw new Error("供应商接口模型 ID 和采购系数至少需要调整一项。");
  return {
    resource_id: standardModelVersion(resource.id), supplier_model_id: targetModelID, expected_resource_revision: standardModelVersion(resource.revision),
    after_coefficient: targetCoefficient, assignee_id: assigneeID,
    due_at: new Date(dueAt).toISOString(), reason: reason.trim(),
  };
}

export function validateCreatedPriceChangePlan(value: unknown, resource: ResourceRow, body: ReturnType<typeof buildPriceChangePlanCreate>) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("改价计划响应为空，请重新加载资源核对。");
  const plan = value as Record<string, unknown>;
  if (typeof plan.id !== "string" || typeof plan.created_by !== "string" || typeof plan.assignee_id !== "string") throw new Error("改价计划响应缺少人员或计划标识。");
  standardModelVersion(plan.id); standardModelVersion(plan.created_by); standardModelVersion(plan.assignee_id);
  if (plan.resource_id !== resource.id || plan.supplier_model_id !== body.supplier_model_id || plan.before_supplier_model_id !== resource.supplier_model_id || plan.after_supplier_model_id !== body.supplier_model_id || plan.region !== resource.region || plan.kind !== "PRICE_CHANGE" || plan.status !== "PENDING" || plan.revision !== "1" ||
      plan.expected_resource_revision !== resource.revision || plan.before_coefficient !== fixed(units(resource.coefficient)) ||
      plan.after_coefficient !== body.after_coefficient || plan.assignee_id !== body.assignee_id || plan.reason !== body.reason ||
      typeof plan.created_at !== "string" || !Number.isFinite(Date.parse(plan.created_at)) || typeof plan.due_at !== "string" || Date.parse(plan.due_at) !== Date.parse(body.due_at))
    throw new Error("改价计划响应与提交依据不一致，请刷新核对。");
  return { ...plan };
}

export function buildPriceChangePlanRevision(plan: PlanSummary, supplierModelID: string, afterCoefficient: string, assigneeID: string, reason: string, dueAt: string) {
  if (!plan.is_initiator || !plan.permissions.can_revise || plan.kind !== "PRICE_CHANGE" || plan.status !== "PENDING") throw new Error("当前计划权限或状态不允许修改。");
  if (!plan.before_coefficient) throw new Error("改价计划缺少原始系数快照，请重新加载核对。");
  const targetModelID = validateSupplierModelID(supplierModelID);
  const targetCoefficient = validateAmount(afterCoefficient);
  if (!plan.before_supplier_model_id || !plan.before_coefficient) throw new Error("改价计划缺少修改前快照，请重新加载核对。");
  if (targetModelID === plan.before_supplier_model_id && targetCoefficient === fixed(units(plan.before_coefficient))) throw new Error("供应商接口模型 ID 和采购系数至少需要调整一项。");
  if (BigInt(plan.revision) >= 9223372036854775807n || !reason.trim()) throw new Error("请填写修改原因。");
  validateAssignment(assigneeID); validateDue(dueAt);
  return { expected_revision: plan.revision, supplier_model_id: targetModelID, after_coefficient: targetCoefficient, assignee_id: assigneeID, due_at: new Date(dueAt).toISOString(), reason: reason.trim() };
}

export function validateRevisedPriceChangePlan(value: unknown, plan: PlanSummary, body: ReturnType<typeof buildPriceChangePlanRevision>) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("修改回执缺失，请重新加载计划核对。");
  const result = value as Record<string, unknown>;
  if (result.id !== plan.id || result.resource_id !== plan.resource_id || result.supplier_model_id !== body.supplier_model_id || result.before_supplier_model_id !== plan.before_supplier_model_id || result.after_supplier_model_id !== body.supplier_model_id || result.kind !== "PRICE_CHANGE" || result.created_by !== plan.created_by || result.expected_resource_revision !== plan.expected_resource_revision ||
      result.revision !== (BigInt(body.expected_revision) + 1n).toString() || result.status !== "PENDING" || result.after_coefficient !== body.after_coefficient || result.assignee_id !== body.assignee_id || result.reason !== body.reason ||
      typeof result.due_at !== "string" || Date.parse(result.due_at) !== Date.parse(body.due_at) || result.before_coefficient !== plan.before_coefficient)
    throw new Error("修改回执与计划版本不一致，请重新加载核对。");
  return structuredClone(result);
}
