import test from "node:test";
import assert from "node:assert/strict";
import { emptyStandardModel, buildStandardModelSave, validateStandardModel, formatStandardPrice, normalizeStandardModel, standardModelVersion } from "../src/api/standardModels.contract.ts";
import type { StandardModelDTO } from "../src/api/standardModels.types.ts";

test("standard model draft saves a complete replacement without ownership or response metadata", () => {
  const body = buildStandardModelSave({ ...emptyStandardModel(), name: "待补资料", maintainer_id: "99", status: "ACTIVE", permissions: { can_edit: true } }, "人工补录", "9007199254740993");
  assert.equal(body.expected_revision, "9007199254740993");
  assert.equal(body.name, "待补资料");
  assert.deepEqual(body.price_items, []);
  assert.equal(Object.keys(body).length, 15);
  assert.equal("maintainer_id" in body, false);
  assert.equal("permissions" in body, false);
  assert.equal("status" in body, false);
  assert.deepEqual(validateStandardModel(emptyStandardModel()), []);
});

const price = () => ({ item_key: "text_input", label: "输入", task_type: "TEXT_INPUT", amount: "0", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准请求", dimensions: {}, source_url: "https://vendor.example/pricing" });
test("prices preserve decimal strings and zero, reject invalid precision, units and key duplicates", () => {
  const draft = emptyStandardModel(); draft.price_items = [price()];
  assert.deepEqual(validateStandardModel(draft), []);
  assert.match(formatStandardPrice(draft.price_items[0]!), /0 USD/);
  for (const amount of ["1e-8", "0.000000001", "1000000000000", "-1", "NaN"]) {
    draft.price_items = [{ ...price(), amount }];
    assert.ok(validateStandardModel(draft).some(item => item.path === "price_items[0].amount"), amount);
  }
  draft.price_items = [{ ...price(), amount: "999999999999.12345678" }];
  const body = buildStandardModelSave(draft, "核对官方价格");
  assert.equal(body.price_items[0]!.amount, "999999999999.12345678");
  draft.price_items = [price(), price()];
  assert.ok(validateStandardModel(draft).some(item => item.path.endsWith("item_key")));
  draft.price_items = [{ ...price(), unit_quantity: "0", unit_code: "UNKNOWN" }];
  assert.ok(validateStandardModel(draft).some(item => item.path.endsWith("unit_quantity")));
  assert.ok(validateStandardModel(draft).some(item => item.path.endsWith("unit_code")));
});

test("request snapshots cannot mutate draft and source URLs reject credentials", () => {
  const draft = emptyStandardModel(); draft.price_items = [{ ...price(), dimensions: { context_tier: "long" } }];
  const body = buildStandardModelSave(draft, "核对来源");
  body.price_items[0]!.dimensions.context_tier = "changed";
  assert.equal(draft.price_items[0]!.dimensions.context_tier, "long");
  for (const url of ["javascript:alert(1)", "https://user:pass@vendor.example", "https://vendor.example?api_key=secret"]) {
    draft.sources = [{ label: "官方", url, source_kind: "OFFICIAL_DOCS" }];
    assert.ok(validateStandardModel(draft).some(item => item.path === "sources[0].url"));
  }
});

test("provided facts validate structural truth without interpreting undisclosed as unsupported", () => {
  const draft = emptyStandardModel();
  draft.capacity = [{ key: "context_window", disclosure: "UNDISCLOSED", value: "" }];
  draft.abilities = [{ key: "tool_call", state: "UNSUPPORTED", description: "" }];
  assert.deepEqual(validateStandardModel(draft), []);
  draft.capacity[0]!.disclosure = "DISCLOSED";
  assert.ok(validateStandardModel(draft).some(item => item.path.endsWith(".value")));
  assert.throws(() => buildStandardModelSave(draft, "核对"));
  assert.throws(() => buildStandardModelSave(emptyStandardModel(), " "));
  for (const value of ["0", "01", "9223372036854775808", 1]) assert.throws(() => standardModelVersion(value));
  assert.equal(standardModelVersion("9223372036854775807"), "9223372036854775807");
});

test("wire responses reject wrong identity and numeric prices, normalize backend null draft arrays", () => {
  const dto: StandardModelDTO = { ...emptyStandardModel(), id: "9007199254740993", maintainer_id: "7", revision: "1", version_no: "1", status: "ACTIVE", verify_status: "INCOMPLETE", checked_by: "", created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", missing_fields: ["name"], permissions: { can_edit: true, can_verify: false }, quote_readiness: { ready: false, reasons: ["INCOMPLETE"], standard_model_version_no: "1" } };
  assert.throws(() => normalizeStandardModel(dto, "8"));
  assert.throws(() => normalizeStandardModel({ ...dto, price_items: [{ ...price(), amount: 0 }] } as unknown as StandardModelDTO));
  assert.throws(() => normalizeStandardModel({ ...dto, verify_status: "APPROVED" }));
  const normalized = normalizeStandardModel({ ...dto, price_items: null, capacity: null } as unknown as StandardModelDTO, dto.id);
  assert.deepEqual(normalized.price_items, []);
  assert.deepEqual(normalized.capacity, []);
});
