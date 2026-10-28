import assert from "node:assert/strict";
import test from "node:test";
import { canManageF12Subject } from "../src/domain/f12Permissions.ts";

test("F12 ordinary subject permission applies when there is no flow context", () => {
  assert.equal(canManageF12Subject(true, false), true);
  assert.equal(canManageF12Subject(false, false), false);
  assert.equal(canManageF12Subject(false, false, true), false);
});

test("F12 flow handler can manage even without ordinary subject ownership", () => {
  assert.equal(canManageF12Subject(false, true, true), true);
});

test("F12 flow context requires current handler permission and overrides ordinary ownership", () => {
  assert.equal(canManageF12Subject(true, true, false), false);
  assert.equal(canManageF12Subject(true, true, undefined), false);
});
