import assert from "node:assert/strict";
import test from "node:test";
import { normalizeF12Readiness } from "../src/api/f12Readiness.contract.ts";

const expected = { subjectType: "SUPPLIER" as const, subjectID: "21", flowID: "55", moduleKey: "CONTRACT" };

test("F12 readiness accepts only the requested subject and returns exact current evidence references", () => {
  const result = normalizeF12Readiness({
    subject_type: "SUPPLIER", subject_id: "21", source_flow_id: "55", source_module_key: "CONTRACT",
    source_flow_revision: "7", required_items: [{ item: "CONTRACT" }, { item: "CREDIT" }], ready: true,
    contracts: [{ contract_id: "101", contract_key: "901", version: "2" }],
    credits: [
      { credit_line_id: "202", version: "1", currency: "CNY", amount: "800.00" },
      { credit_line_id: "203", version: "1", currency: "USD", amount: "100.00" },
    ], missing_reasons: [],
  }, expected);
  assert.equal(result.source_flow_revision, 7);
  assert.equal(result.contracts[0]?.contract_key, "901");
  assert.deepEqual(result.credits.map(row => row.currency), ["CNY", "USD"]);
  assert.equal(result.ready, true);
});

test("F12 readiness preserves precise missing currency reasons", () => {
  const result = normalizeF12Readiness({
    subject_type: "SUPPLIER", subject_id: "21", source_flow_id: "55", source_module_key: "CONTRACT",
    source_flow_revision: "8", required_items: [{ item: "CONTRACT" }, { item: "CREDIT", currency: "USD" }], ready: false,
    contracts: [{ contract_id: "101", contract_key: "901", version: "2" }], credits: [], missing_reasons: ["CREDIT_REQUIRED:USD"],
  }, expected);
  assert.equal(result.ready, false);
  assert.deepEqual(result.missing_reasons, ["CREDIT_REQUIRED:USD"]);
});

test("F12 readiness rejects mismatched identity and contradictory ready projections", () => {
  const base = {
    subject_type: "SUPPLIER", subject_id: "21", source_flow_id: "55", source_module_key: "CONTRACT",
    source_flow_revision: "7", required_items: [{ item: "CONTRACT" }], ready: true,
    contracts: [{ contract_id: "101", contract_key: "901", version: "2" }], credits: [], missing_reasons: [],
  };
  assert.throws(() => normalizeF12Readiness({ ...base, subject_id: "22" }, expected), /主体或办理流程/);
  assert.throws(() => normalizeF12Readiness({ ...base, contracts: [] }, expected), /阻断原因与结果/);
  assert.throws(() => normalizeF12Readiness({ ...base, ready: false, missing_reasons: ["CREDIT_REQUIRED"] }, expected), /阻断原因与结果/);
  assert.throws(() => normalizeF12Readiness({
    ...base, required_items: [{ item: "CREDIT" }, { item: "CREDIT", currency: "USD" }],
    contracts: [], credits: [{ credit_line_id: "202", version: "1", currency: "USD", amount: "100.00" }],
  }, expected), /通用授信条件/);
});
