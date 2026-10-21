import test from "node:test";
import assert from "node:assert/strict";
import * as contract from "../src/api/planMaintenance.contract.ts";
import type { PlanSummary } from "../src/api/supplyWorkbench.types.ts";
const plan = (): PlanSummary => ({ id: "81", resource_id: "32", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "7", creator_name: "采购甲", revision: "9007199254740993", expected_resource_revision: "1", current_resource_revision: "2", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "维护", is_initiator: true, read_only: true, precondition_matches: false, blocked_reasons: ["RESOURCE_CHANGED"], permissions: { can_revise: true, can_cancel: true, can_execute: false } });
test("revise sends only expected plan revision, reason and time; cancel sends revision and reason", () => {
  assert.deepEqual(contract.buildPlanRevision(plan(), " 调整维护 ", "2027-01-02T09:00:00+08:00"), { expected_revision: "9007199254740993", reason: "调整维护", due_at: "2027-01-02T01:00:00.000Z" });
  assert.deepEqual(contract.buildPlanCancellation(plan(), " 不再维护 "), { expected_revision: "9007199254740993", reason: "不再维护" });
  assert.throws(() => contract.buildPlanRevision(plan(), " ", "2027-01-02T01:00:00Z"));
  assert.throws(() => contract.buildPlanRevision(plan(), "维护", "bad"));
  assert.throws(() => contract.buildPlanCancellation(plan(), " "));
});
test("maintenance requires initiator, state kind and explicit capability; blocked only permits cancellation", () => {
  for (const patch of [{ is_initiator: false }, { kind: "PRICE_CHANGE" }, { status: "EXECUTED" }, { permissions: { can_revise: false, can_cancel: false, can_execute: false } }]) {
    const p = { ...plan(), ...patch };
    assert.throws(() => contract.buildPlanRevision(p, "维护", "2027-01-02T01:00:00Z"));
    assert.throws(() => contract.buildPlanCancellation(p, "撤销"));
  }
  const blocked = { ...plan(), status: "BLOCKED", permissions: { can_revise: false, can_cancel: true, can_execute: false } };
  assert.throws(() => contract.buildPlanRevision(blocked, "维护", "2027-01-02T01:00:00Z"));
  assert.equal(contract.buildPlanCancellation(blocked, "撤销").reason, "撤销");
});
test("maintenance receipt preserves plan identity, creator, resource basis and exact next revision", () => {
  const p = plan(), body = contract.buildPlanRevision(p, "调整", "2027-01-02T01:00:00Z");
  const receipt = { ...p, revision: "9007199254740994", reason: body.reason, due_at: body.due_at };
  assert.equal(contract.validateMaintainedPlan(receipt, p, body, "revise").revision, "9007199254740994");
  for (const patch of [{ id: "82" }, { created_by: "8" }, { resource_id: "33" }, { region: "OVERSEAS" }, { kind: "ONLINE" }, { revision: "9007199254740993" }, { expected_resource_revision: "2" }, { created_at: "2026-10-21T01:00:00Z" }, { status: "EXECUTED" }, { reason: "wrong" }, { due_at: "bad" }]) assert.throws(() => contract.validateMaintainedPlan({ ...receipt, ...patch }, p, body, "revise"));
  const cancel = contract.buildPlanCancellation(p, "撤销");
  // Cancellation reason belongs to the operation log; original plan reason remains intact.
  assert.equal(contract.validateMaintainedPlan({ ...p, revision: "9007199254740994", status: "CANCELED" }, p, cancel, "cancel").status, "CANCELED");
  assert.throws(() => contract.validateMaintainedPlan({ ...p, revision: "9007199254740994", status: "CANCELED", due_at: "2028-01-01T01:00:00Z" }, p, cancel, "cancel"));
});
