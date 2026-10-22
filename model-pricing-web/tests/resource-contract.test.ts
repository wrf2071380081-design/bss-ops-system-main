import test from "node:test";
import assert from "node:assert/strict";
import { buildResourceQuery, resourceCacheDisplay, resourceMetricDisplay, validateResourceGroup, validateResourceRow } from "../src/api/resources.contract.ts";

test("group expansion keeps applied filters and resets resource pagination and sorting", () => {
  const original = { keyword: "模型", company: "示例公司", region: "CN", supplier_id: "9007199254740993", state: "ADMITTED", model_type: "TEXT", page: 3, size: 50, view: "grouped", sort_by: "supplier_count", order: "desc" };
  const flat = buildResourceQuery(original, { modelID: "11", page: 1, size: 20 });
  assert.deepEqual(flat, { ...original, standard_model_id: "11", view: "flat", sort_by: "coefficient", order: "asc", page: 1, size: 20 });
  assert.equal(original.page, 3);
  assert.equal(buildResourceQuery(original).view, "grouped");
  assert.throws(() => buildResourceQuery({ ...original, supplier_id: "01" }));
  assert.throws(() => buildResourceQuery(original, { modelID: "11", sort_by: "supplier_count" }));
});

test("cache formatting derives percentage once from original integer strings", () => {
  assert.equal(resourceCacheDisplay({ status: "PROVIDED", report_id: "51", hit_tokens: "1200", input_tokens: "2000" }, "COMPLETED"), "60.00%");
  assert.equal(resourceCacheDisplay({ status: "PROVIDED", report_id: "51", hit_tokens: "0", input_tokens: "2000" }, "COMPLETED"), "0.00%");
  assert.equal(resourceCacheDisplay({ status: "PROVIDED", report_id: "51", hit_tokens: "246899", input_tokens: "2000000", hit_rate: "0.123450" }, "COMPLETED"), "12.34%");
  assert.equal(resourceCacheDisplay({ status: "PROVIDED", report_id: "51", hit_tokens: "9007199254740993", input_tokens: "9007199254740993" }, "COMPLETED"), "100.00%");
  assert.equal(resourceCacheDisplay({ status: "NOT_PROVIDED" }, "COMPLETED"), "未提供");
  assert.equal(resourceCacheDisplay({ status: "NOT_APPLICABLE" }, "COMPLETED"), "不适用");
  assert.equal(resourceCacheDisplay({ status: "NOT_TESTED" }, "NOT_TESTED"), "未测试");
  assert.equal(resourceCacheDisplay({ status: "PROVIDED", hit_tokens: "1", input_tokens: "1" }, "STALE"), "目标已变化");
  assert.throws(() => resourceCacheDisplay({ status: "PROVIDED", hit_tokens: "3", input_tokens: "2" }, "COMPLETED"));
});

test("resource metrics distinguish exact zero from missing and preserve large values", () => {
  assert.equal(resourceMetricDisplay("0.00", "COMPLETED"), "0.00");
  assert.equal(resourceMetricDisplay("9007199254740993", "COMPLETED"), "9007199254740993");
  assert.equal(resourceMetricDisplay(null, "COMPLETED"), "未提供");
  assert.equal(resourceMetricDisplay(null, "NOT_TESTED"), "未测试");
  assert.equal(resourceMetricDisplay("12", "STALE"), "目标已变化");
  assert.throws(() => resourceMetricDisplay(0 as unknown as string, "COMPLETED"));
});

export const resourceGroupFixture = () => ({ id: "11", name: "示例模型", official_id: "example-v1", company: "示例公司", model_type: "TEXT", supplier_count: 2, resource_count: 2, available_count: 1, coefficient_min: "0.5000", coefficient_max: "0.7000", regions: ["CN"], state_counts: { ADMITTED: 2 } });
export const resourceRowFixture = () => ({ id: "9007199254740993", supplier_id: "21", supplier_name: "供应商甲", standard_model_id: "11", model_name: "示例模型", official_id: "example-v1", company: "示例公司", model_type: "TEXT", region: "CN", supplier_model_id: "vendor-model-v1", coefficient: "0.6800", buyer_id: "7", state: "ADMITTED", ever_admitted: true, revision: "1", formal_config_id: "91", formal_config_revision: "1", test: { report_id: "51", status: "COMPLETED", connectivity_passed: true, pressure_passed: true, valid: true, cache_stats: { status: "PROVIDED", report_id: "51", hit_tokens: "0", input_tokens: "2000" }, metrics: { connection_ms: "0.00", rps: "23.00000000", tps: "9007199254740993" } }, official_price_items: [], quote_readiness: { eligible: false, blocked_reasons: ["MODEL_NOT_READY"], resource_revision: "1", model_version_no: "1", formal_config_revision: "1", test_evidence_ref: "51" }, permissions: { can_edit: false }, created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z" });

test("native optional price dimensions do not prevent loading resource details", () => {
  const price = { item_key: "input", label: "文本输入", task_type: "TEXT_INPUT", amount: "2", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准计费", source_url: "https://openai.com" };
  for (const dimensions of [undefined, null]) {
    const row = validateResourceRow({ ...resourceRowFixture(), official_price_items: [{ ...price, dimensions }] });
    assert.deepEqual(row.official_price_items[0]?.dimensions, {});
  }
});
test("wire validation rejects numeric IDs, bad group counts and borrowed report cache", () => {
  assert.equal(validateResourceGroup(resourceGroupFixture()).resource_count, 2);
  assert.equal(validateResourceRow(resourceRowFixture()).id, "9007199254740993");
  assert.throws(() => validateResourceGroup({ ...resourceGroupFixture(), available_count: 3 }));
  assert.throws(() => validateResourceRow({ ...resourceRowFixture(), id: 9007199254740992 }));
  const wrong = resourceRowFixture(); wrong.test.cache_stats.report_id = "52";
  assert.throws(() => validateResourceRow(wrong));
});
