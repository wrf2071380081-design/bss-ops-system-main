import test from "node:test";
import assert from "node:assert/strict";
import { resourceStateAction, buildResourceStateBody, validateResourceStateReceipt } from "../src/api/resourceState.contract.ts";
const row = () => ({ id: "11", state: "ADMITTED", ever_admitted: true, revision: "2", permissions: { can_edit: true } });
test("direct state registration requires bound permission, actual operation and evidence", () => {
  assert.equal(resourceStateAction(row()), "OFFLINE");
  assert.equal(resourceStateAction({ ...row(), state: "OFFLINE" }), "ONLINE");
  assert.equal(resourceStateAction({ ...row(), permissions: { can_edit: false } }), null);
  assert.equal(resourceStateAction({ ...row(), state: "PENDING_ACCESS", ever_admitted: false }), null);
  const body = buildResourceStateBody(row(), true, " 平台记录 ", " 维护 ");
  assert.deepEqual(body, { expected_revision: "2", action: "OFFLINE", external_confirmed: true, completion_note: "平台记录", reason: "维护" });
  assert.throws(() => buildResourceStateBody(row(), false, "记录", "原因"));
  assert.throws(() => buildResourceStateBody(row(), true, " ", "原因"));
});
test("state receipt matches requested object, transition and next exact revision", () => {
  const receipt = { id: "11", state: "OFFLINE", revision: "3", ever_admitted: true, updated_at: "2026-10-20T01:00:00Z" };
  assert.equal(validateResourceStateReceipt(receipt, row(), "OFFLINE").revision, "3");
  for (const change of [{ id: "12" }, { state: "ADMITTED" }, { revision: 3 }, { revision: "4" }, { ever_admitted: false }]) assert.throws(() => validateResourceStateReceipt({ ...receipt, ...change }, row(), "OFFLINE"));
});
