import test from "node:test";
import assert from "node:assert/strict";
import { validatePlanSummary } from "../src/api/supplyWorkbench.contract.ts";
const planFixture = () => ({ id: "81", resource_id: "11", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "7", creator_name: "甲", revision: "1", expected_resource_revision: "2", current_resource_revision: "2", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "维护", is_initiator: false, read_only: true, precondition_matches: true, blocked_reasons: [] });
test("plan permissions default closed and execute is limited to the initiator's pending state plan", () => {
  const fixture = planFixture();
  assert.deepEqual(validatePlanSummary(fixture, "81").permissions, { can_revise: false, can_cancel: false, can_execute: false });
  assert.throws(() => validatePlanSummary({ ...fixture, permissions: { can_revise: true, can_cancel: true, can_execute: false } }, "81"));
  const own = { ...fixture, is_initiator: true, permissions: { can_revise: true, can_cancel: true, can_execute: false } };
  assert.equal(validatePlanSummary(own, "81").permissions.can_revise, true);
  assert.equal(validatePlanSummary({ ...own, permissions: { ...own.permissions, can_execute: true } }, "81").permissions.can_execute, true);
  assert.throws(() => validatePlanSummary({ ...own, status: "CANCELED" }, "81"));
  assert.throws(() => validatePlanSummary({ ...own, kind: "PRICE_CHANGE" }, "81"));
  assert.throws(() => validatePlanSummary({ ...fixture, permissions: { can_revise: false, can_cancel: false, can_execute: true } }, "81"));
  assert.throws(() => validatePlanSummary({ ...own, status: "BLOCKED", permissions: { can_revise: false, can_cancel: true, can_execute: true } }, "81"));
});
