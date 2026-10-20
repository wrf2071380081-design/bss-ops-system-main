import { standardModelVersion } from "./standardModels.contract.ts";
export interface ResourceStateInput { id: string; state: string; ever_admitted: boolean; revision: string; permissions: { can_edit: boolean } }
export type ResourceStateAction = "OFFLINE" | "ONLINE";
export function resourceStateAction(row: ResourceStateInput): ResourceStateAction | null {
  if (!row.permissions.can_edit || !row.ever_admitted) return null;
  return row.state === "ADMITTED" ? "OFFLINE" : row.state === "OFFLINE" ? "ONLINE" : null;
}
export function buildResourceStateBody(row: ResourceStateInput, confirmed: boolean, note: string, reason: string) {
  const action = resourceStateAction(row);
  if (!action) throw new Error("当前资源状态或权限不允许登记上下架。");
  if (!confirmed || !note.trim() || !reason.trim()) throw new Error("请确认已完成实际平台操作，并填写完成依据和原因。");
  return { expected_revision: standardModelVersion(row.revision), action, external_confirmed: true, completion_note: note.trim(), reason: reason.trim() };
}
export function validateResourceStateReceipt(value: unknown, row: ResourceStateInput, action: ResourceStateAction) {
  const receipt = value as { id: string; state: string; revision: string; ever_admitted: boolean; updated_at: string };
  if (!receipt || receipt.id !== row.id || receipt.state !== (action === "OFFLINE" ? "OFFLINE" : "ADMITTED") || receipt.ever_admitted !== true || typeof receipt.revision !== "string" || receipt.revision !== String(BigInt(standardModelVersion(row.revision)) + 1n) || typeof receipt.updated_at !== "string" || !Number.isFinite(Date.parse(receipt.updated_at))) throw new Error("登记结果不符合约定，请刷新资源状态核对。");
  return { ...receipt };
}
