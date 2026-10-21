import { standardModelVersion } from "./standardModels.contract.ts";
export interface PlanResource { id: string; region: string; state: string; ever_admitted: boolean; revision: string; permissions: { can_edit: boolean } }
export function canCreateStatePlan(resource: PlanResource): boolean { return resource.permissions.can_edit && resource.ever_admitted && ["ADMITTED", "OFFLINE"].includes(resource.state); }
export function buildPlanCreate(resource: PlanResource, reason: string, dueAt: string) {
  if (!canCreateStatePlan(resource)) throw new Error("当前资源状态或权限不允许创建上下架计划。");
  if (!reason.trim() || !dueAt || !Number.isFinite(Date.parse(dueAt))) throw new Error("请填写原因和有效的预计时间。");
  return { resource_id: standardModelVersion(resource.id), expected_resource_revision: standardModelVersion(resource.revision), kind: resource.state === "OFFLINE" ? "ONLINE" : "OFFLINE", due_at: new Date(dueAt).toISOString(), reason: reason.trim() };
}
export function validateCreatedPlan(value: unknown, resource: PlanResource, body: ReturnType<typeof buildPlanCreate>) {
  const p = value as { id: string; resource_id: string; region: string; kind: string; status: string; revision: string; expected_resource_revision: string; created_by: string; created_at: string; due_at: string; reason: string };
  if (!p) throw new Error("计划响应为空，请重新核对。");
  standardModelVersion(p.id); standardModelVersion(p.created_by);
  if (p.resource_id !== resource.id || p.region !== resource.region || p.kind !== body.kind || p.status !== "PENDING" || p.revision !== "1" || p.expected_resource_revision !== resource.revision || p.reason !== body.reason || !Number.isFinite(Date.parse(p.created_at)) || Date.parse(p.due_at) !== Date.parse(body.due_at)) throw new Error("计划响应与提交依据不一致，请刷新核对。");
  return { ...p };
}
