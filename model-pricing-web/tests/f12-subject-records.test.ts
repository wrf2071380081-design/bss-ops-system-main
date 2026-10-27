import assert from "node:assert/strict";
import test from "node:test";
import { normalizeF12Contracts, normalizeF12Credits, normalizeF12CreditInput, normalizeF12LatestContract } from "../src/api/f12SupplierRecords.contract.ts";

test("F12 validates credit amount and currency before starting attachment upload", () => {
  assert.deepEqual(normalizeF12CreditInput(" 500 ", " usd "), { amount: "500.00", currency: "USD" });
  assert.deepEqual(normalizeF12CreditInput("999999999999999999.99", "CNY"), { amount: "999999999999999999.99", currency: "CNY" });
  for (const amount of ["0", "-1", "1.001", "1e3", "01", "1000000000000000000"]) {
    assert.throws(() => normalizeF12CreditInput(amount, "CNY"), /额度/);
  }
  assert.throws(() => normalizeF12CreditInput("1", "CN"), /币种/);
});

const customerContract = {
  id: "101", subject_type: "CUSTOMER", subject_id: "21", status: "ARCHIVED", version: 2,
  has_attachment: true, signed_at: null, created_at: "2026-10-23T00:00:00Z", updated_at: "2026-10-23T00:00:00Z",
};
const customerCredit = {
  id: "202", subject_type: "CUSTOMER", subject_id: "21", direction: "US_TO_CUSTOMER", amount: "800.00",
  currency: "CNY", version: 3, effective_at: null, created_at: "2026-10-23T00:00:00Z",
};

test("F12 normalizes customer contract and credit projections without supplier direction assumptions", () => {
  const contracts = normalizeF12Contracts({ list: [customerContract], total: 1, page: 1, size: 20 }, 1, 20, "CUSTOMER", "21");
  const credits = normalizeF12Credits({
    list: [customerCredit], total: 1, page: 1, size: 20,
    current_credit: [{ direction: "US_TO_CUSTOMER", currency: "CNY", status: "AVAILABLE", record: customerCredit }],
  }, 1, 20, "CUSTOMER", "21");
  assert.equal(contracts.list[0]?.subject_type, "CUSTOMER");
  assert.equal(credits.list[0]?.direction, "US_TO_CUSTOMER");
  assert.equal(credits.current[0]?.record?.amount, "800.00");
});

test("F12 customer projection rejects another subject and supplier credit direction", () => {
  assert.throws(() => normalizeF12Contracts({ list: [customerContract], total: 1, page: 1, size: 20 }, 1, 20, "CUSTOMER", "22"), /主体|不匹配/);
  assert.throws(() => normalizeF12Credits({
    list: [{ ...customerCredit, direction: "SUPPLIER_TO_US" }], total: 1, page: 1, size: 20, current_credit: [],
  }, 1, 20, "CUSTOMER", "21"));
});

test("F12 displays flow-verified creator as actual handler and preserves source and reason", () => {
  const flowContract = normalizeF12Contracts({ list: [{
    ...customerContract, created_by: "8", source_flow_id: "55", source_module_key: "CONTRACT",
    source_flow_revision: "4", reason: "首次办理合同", contract_key: "901", is_latest: true,
  }], total: 1, page: 1, size: 20 }, 1, 20, "CUSTOMER", "21").list[0]!;
  const flowCredit = normalizeF12Credits({
    list: [{ ...customerCredit, created_by: "9", source_flow_id: "55", source_module_key: "CREDIT", source_flow_revision: "6", reason: "首次授信" }],
    total: 1, page: 1, size: 20, current_credit: [],
  }, 1, 20, "CUSTOMER", "21").list[0]!;
  assert.equal(flowContract.actual_operator_id, "8");
  assert.equal(flowContract.source_flow_revision, 4);
  assert.equal(flowContract.reason, "首次办理合同");
  assert.equal(flowCredit.actual_operator_id, "9");
  assert.equal(flowCredit.source_module_key, "CREDIT");
});

test("F12 rejects incomplete or malformed trusted flow source projections", () => {
  assert.throws(() => normalizeF12Contracts({
    list: [{ ...customerContract, source_flow_id: "55", source_module_key: "CONTRACT" }], total: 1, page: 1, size: 20,
  }, 1, 20, "CUSTOMER", "21"), /流程来源/);
  assert.throws(() => normalizeF12Credits({
    list: [{ ...customerCredit, source_flow_id: "55", source_module_key: "CREDIT", source_flow_revision: "01" }],
    total: 1, page: 1, size: 20, current_credit: [],
  }, 1, 20, "CUSTOMER", "21"), /修订号/);
});

test("F12 archive lookup validates latest version independently of the visible history page", () => {
  const latest = { ...customerContract, id: "109", status: "PROCESSING", contract_key: "99", is_latest: true, version: 9 };
  const page = { list: [latest], total: 1, page: 1, size: 2 };
  assert.equal(normalizeF12LatestContract(page, "CUSTOMER", "21", "99")?.version, 9);
  assert.equal(normalizeF12LatestContract({ list: [], total: 0, page: 1, size: 2 }, "CUSTOMER", "21", "99"), null);
  assert.throws(() => normalizeF12LatestContract(page, "SUPPLIER", "21", "99"), /主体/);
  assert.throws(() => normalizeF12LatestContract(page, "CUSTOMER", "21", "100"), /不匹配/);
  assert.throws(() => normalizeF12LatestContract({ ...page, list: [{ ...latest, is_latest: false }] }, "CUSTOMER", "21", "99"), /不匹配/);
  assert.throws(() => normalizeF12LatestContract({ ...page, list: [latest, { ...latest, id: "110" }], total: 2 }, "CUSTOMER", "21", "99"), /重复/);
});
