import test from "node:test";
import assert from "node:assert/strict";
import { buildModelFilters, validateModelOptions } from "../src/api/modelFilters.contract.ts";
test("model filter wire encoding preserves commas, bigint IDs and empty dimensions", () => {
  const q = buildModelFilters({ page: 1, size: 20, companies: ["Company, LLC", "OpenAI"], model_types: ["TEXT", "IMAGE"], supplier_ids: ["9007199254740993"], regions: ["CN", "OVERSEAS"] });
  assert.deepEqual(JSON.parse(String(q.companies)), ["Company, LLC", "OpenAI"]);
  assert.deepEqual(JSON.parse(String(q.supplier_ids)), ["9007199254740993"]);
  assert.equal(buildModelFilters({ page: 1, size: 20, companies: [] }).companies, undefined);
  assert.throws(() => buildModelFilters({ page: 1, size: 20, regions: ["cn"] }));
  assert.throws(() => buildModelFilters({ page: 1, size: 20, company: "A", companies: ["B"] }));
  assert.throws(() => buildModelFilters({ page: 1, size: 20, companies: ["A", " A "] }));
});
test("model options validate independent directory and selected region", () => {
  const fixture = { companies: ["No resources company"], suppliers: [{ id: "21", name: "甲", region: "CN" }], has_more: true };
  assert.equal(validateModelOptions(fixture, ["CN"]).has_more, true);
  assert.throws(() => validateModelOptions(fixture, ["OVERSEAS"]));
  assert.throws(() => validateModelOptions({ ...fixture, companies: ["A", "A"] }, []));
  assert.throws(() => validateModelOptions({ ...fixture, suppliers: [fixture.suppliers[0], fixture.suppliers[0]] }, []));
});
