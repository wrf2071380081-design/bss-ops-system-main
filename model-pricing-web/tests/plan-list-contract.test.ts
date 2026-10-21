import test from "node:test";
import assert from "node:assert/strict";
import { validatePlanPage } from "../src/api/planList.contract.ts";
const plan = () => ({ id: "81", resource_id: "11", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "7", creator_name: "甲", revision: "1", expected_resource_revision: "2", current_resource_revision: "2", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "维护", is_initiator: true, read_only: true, precondition_matches: true, blocked_reasons: [] });
test("plan page preserves original initiator and copies readonly resource projection", () => {
  const p = plan(), query = { resource_id: "11", region: "CN", page: 1, size: 10 };
  const page = validatePlanPage({ list: [p], total: 1, page: 1, size: 10 }, query);
  assert.equal(page.list[0].is_initiator, true); assert.equal(page.list[0].read_only, true);
  page.list[0].reason = "changed"; assert.equal(p.reason, "维护");
});
test("plan page refuses foreign scope duplicates wrong state and impossible pagination", () => {
  const p = plan(), query = { resource_id: "11", region: "CN", status: "PENDING", page: 1, size: 10 };
  const page = { list: [p], total: 1, page: 1, size: 10 };
  for (const value of [{ ...page, list: [{ ...p, resource_id: "12" }] }, { ...page, list: [{ ...p, region: "OVERSEAS" }] }, { ...page, list: [{ ...p, status: "CANCELED" }] }, { ...page, list: [p, p], total: 2 }, { ...page, total: 0 }, { ...page, page: 2 }]) assert.throws(() => validatePlanPage(value, query));
  assert.throws(() => validatePlanPage(page, { ...query, page: 2 }));
});
