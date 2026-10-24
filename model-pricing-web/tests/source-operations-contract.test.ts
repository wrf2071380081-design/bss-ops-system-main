import test from "node:test";
import assert from "node:assert/strict";
import { validateSourceOperations } from "../src/api/sourceOperations.contract.ts";

const row = { id: "9", entity_type: "plan", entity_id: "8", resource_id: "2", supplier_id: "3", supplier_name: "供应商甲", supplier_model_id: "vendor-model-v1", region: "CN", action: "CREATE", operator_name: "采购甲", reason: "计划调整", created_at: "2026-10-22T08:00:00Z" };

test("supplier operation history validates every row belongs to the requested supplier", () => {
  assert.equal(validateSourceOperations({ list: [row], total: 1, page: 1, size: 20 }, 1, 20, "3").list[0]?.supplier_id, "3");
  assert.throws(() => validateSourceOperations({ list: [{ ...row, supplier_id: "4" }], total: 1, page: 1, size: 20 }, 1, 20, "3"));
});
