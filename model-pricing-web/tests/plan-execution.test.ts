import test from "node:test";
import assert from "node:assert/strict";
import { buildPlanExecution, validatePlanExecutionReceipt } from "../src/api/planExecution.contract.ts";
import type { PlanSummary } from "../src/api/supplyWorkbench.types.ts";

const plan = { id: "81", resource_id: "32", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "7", creator_name: "甲", revision: "9007199254740993", expected_resource_revision: "1", current_resource_revision: "1", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "维护", is_initiator: true, read_only: true, precondition_matches: true, blocked_reasons: [], permissions: { can_revise: true, can_cancel: true, can_execute: true } } satisfies PlanSummary;
test("execution requires initiator capability, external confirmation and evidence, preserving bigint revision", () => {
  assert.deepEqual(buildPlanExecution(plan, true, " 工单 OFF-81 ", " 登记 "), { expected_revision: "9007199254740993", confirmed: true, completion_note: "工单 OFF-81", reason: "登记" });
  for (const value of [{ ...plan, is_initiator: false }, { ...plan, status: "EXECUTED" }, { ...plan, permissions: { ...plan.permissions, can_execute: false } }]) assert.throws(() => buildPlanExecution(value, true, "工单", "登记"));
  assert.throws(() => buildPlanExecution(plan, false, "工单", "登记"));
  assert.throws(() => buildPlanExecution(plan, true, " ", "登记"));
});
test("execution receipts preserve plan basis and distinguish committed BLOCKED from completion", () => {
  const result = { ...plan, revision: "9007199254740994", status: "EXECUTED", executed_at: "2026-10-22T01:00:00Z" };
  assert.equal(validatePlanExecutionReceipt(result, plan).status, "EXECUTED");
  const blocked = { ...result, status: "BLOCKED", executed_at: null };
  assert.equal(validatePlanExecutionReceipt(blocked, plan, true).status, "BLOCKED");
  assert.throws(() => validatePlanExecutionReceipt(blocked, plan));
  for (const patch of [{ resource_id: "33" }, { created_by: "8" }, { expected_resource_revision: "2" }, { revision: plan.revision }]) assert.throws(() => validatePlanExecutionReceipt({ ...result, ...patch }, plan));
});
