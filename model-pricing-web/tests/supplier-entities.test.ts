import assert from "node:assert/strict";
import test from "node:test";
import { buildSupplierEntitySave, normalizeSupplierEntity } from "../src/api/supplierEntities.contract.ts";

test("supplier entity save converts percent to ratio and omits buyer assignment", () => {
  const body = buildSupplierEntitySave({
    name: "示例供应商", region: "CN", registration_no: "", contact: "联系人", phone: "",
    tax_rate_percent: "6.25", settlement_currency: "CNY",
  }, "供应商建档");
  assert.equal(body.tax_rate, "0.06250000");
  assert.equal("buyer_id" in body, false);
  assert.equal("established" in body, false);
  assert.equal(body.reason, "供应商建档");
});

test("supplier entity response preserves canonical bigint strings and edit authority", () => {
  const profile = normalizeSupplierEntity({
    id: "9007199254740993", buyer_id: "7", name: "示例供应商", region: "CN", registration_no: "",
    contact: "", phone: "", tax_rate: "0.06000000", settlement_currency: "CNY", established: false,
    status: "ACTIVE", revision: "1", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  });
  assert.equal(profile.id, "9007199254740993");
  assert.equal(profile.can_edit, true);
  assert.throws(() => normalizeSupplierEntity({ id: 9007199254740993 }), /格式不正确|安全范围/);
});

test("supplier tax percent refuses precision lost by the v36 native column", () => {
  assert.throws(() => buildSupplierEntitySave({
    name: "示例供应商", region: "CN", registration_no: "", contact: "", phone: "",
    tax_rate_percent: "6.251", settlement_currency: "CNY",
  }, "建档"), /0.01/);
});
