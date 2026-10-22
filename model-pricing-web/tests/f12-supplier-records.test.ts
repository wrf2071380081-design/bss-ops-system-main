import assert from "node:assert/strict";
import test from "node:test";
import { normalizeF12Contracts, normalizeF12Credits } from "../src/api/f12SupplierRecords.contract.ts";

const now = "2026-10-22T08:00:00Z";

test("F12 contract projection whitelists fields and preserves only attachment presence", () => {
  const result = normalizeF12Contracts({ list: [{ id: "8", subject_type: "SUPPLIER", subject_id: "21", status: "PROCESSING", version: 2,
    has_attachment: true, signed_at: null, created_at: now, updated_at: now, file_ref: "secret/path.pdf", note: "private" }], total: 1, page: 1, size: 20 }, 1, 20);
  assert.equal(result.list[0]?.has_attachment, true);
  assert.equal("file_ref" in result.list[0]!, false);
  assert.equal("note" in result.list[0]!, false);
  assert.throws(() => normalizeF12Contracts({ list: [], total: 0, page: 1, size: 20 }, 2, 20));
});

test("F12 credit projection keeps exact amounts and refuses ambiguous current value", () => {
  const result = normalizeF12Credits({ list: [{ id: "9", subject_type: "SUPPLIER", subject_id: "21", direction: "SUPPLIER_TO_US", amount: "80.00", currency: "CNY", version: 2, effective_at: null, created_at: now }],
    current_credit: [
      { direction: "SUPPLIER_TO_US", currency: "CNY", status: "AVAILABLE", record: { id: "9", subject_type: "SUPPLIER", subject_id: "21", direction: "SUPPLIER_TO_US", amount: "80.00", currency: "CNY", version: 2, effective_at: null, created_at: now } },
      { direction: "SUPPLIER_TO_US", currency: "USD", status: "AMBIGUOUS_VERSION" },
    ], total: 1, page: 1, size: 100 }, 1, 100);
  assert.equal(result.current[0]?.record?.amount, "80.00");
  assert.equal(result.current[1]?.status, "AMBIGUOUS_VERSION");
  assert.equal("record" in result.current[1]!, false);
  assert.throws(() => normalizeF12Credits({ list: [], current_credit: [{ direction: "SUPPLIER_TO_US", currency: "CNY", status: "AVAILABLE" }], total: 0, page: 1, size: 20 }, 1, 20));
});
