import test from "node:test";
import assert from "node:assert/strict";
import { buildPlanCreate, validateCreatedPlan } from "../src/api/planCreate.contract.ts";
const resource = () => ({ id: "11", region: "CN", state: "ADMITTED", ever_admitted: true, revision: "2", permissions: { can_edit: true } });
test("create plan binds resource revision and derives direction without accepting actor", () => {
  assert.deepEqual(buildPlanCreate(resource(), " 维护 ", "2027-01-01T00:00:00Z"), { resource_id: "11", expected_resource_revision: "2", kind: "OFFLINE", due_at: "2027-01-01T00:00:00.000Z", reason: "维护" });
  assert.equal(buildPlanCreate({ ...resource(), state: "OFFLINE" }, "恢复", "2027-01-01T00:00:00Z").kind, "ONLINE");
  for (const value of [{ ...resource(), permissions: { can_edit: false } }, { ...resource(), ever_admitted: false }, { ...resource(), state: "PENDING_ACCESS" }]) assert.throws(() => buildPlanCreate(value, "原因", "2027-01-01T00:00:00Z"));
  assert.throws(() => buildPlanCreate(resource(), " ", "2027-01-01T00:00:00Z"));
  assert.throws(() => buildPlanCreate(resource(), "原因", "invalid"));
});
test("create receipt must match resource direction region initial version and basis", () => {
  const r = resource(), body = buildPlanCreate(r, "维护", "2027-01-01T00:00:00Z");
  const result = { id: "81", resource_id: "11", region: "CN", kind: "OFFLINE", status: "PENDING", revision: "1", expected_resource_revision: "2", created_by: "7", created_at: "2026-10-20T00:00:00Z", due_at: body.due_at, reason: body.reason };
  assert.equal(validateCreatedPlan(result, r, body).id, "81");
  for (const change of [{ resource_id: "12" }, { region: "OVERSEAS" }, { expected_resource_revision: "3" }, { status: "EXECUTED" }, { revision: "2" }]) assert.throws(() => validateCreatedPlan({ ...result, ...change }, r, body));
});
