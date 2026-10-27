import assert from "node:assert/strict";
import { test } from "node:test";
import { aggregateSources, compareValues, emptySourceFilters, filterSources, validateSourceFilters } from "../src/domain/modelPlaza.ts";
import type { ResourceRow } from "../src/api/resources.types.ts";

const row = (fields: Partial<ResourceRow> = {}) => ({ id: "1", standard_model_id: "11", model_name: "模型甲", official_id: "official-a", company: "模型公司", model_type: "TEXT", supplier_id: "21", supplier_name: "供应商甲", supplier_model_id: "vendor-a", region: "CN", coefficient: "0.7000", state: "PENDING_ACCESS", quote_readiness: { eligible: false }, test: { status: "COMPLETED", metrics: { connection_ms: "0", rps: "12", tps: "9007199254740993" } }, ...fields }) as ResourceRow;

test("同维度多选取并集，跨维度取交集，搜索包含供应商及接口模型ID", () => {
  const rows = [row(), row({ id: "2", supplier_id: "22", supplier_name: "供应商乙", region: "OVERSEAS" })];
  assert.equal(filterSources(rows, { ...emptySourceFilters(), supplier_ids: ["21", "22"], regions: ["CN"] }).length, 1);
  assert.equal(filterSources(rows, { ...emptySourceFilters(), keyword: "供应商乙" })[0]?.id, "2");
  assert.equal(filterSources(rows, { ...emptySourceFilters(), keyword: "vendor-a" }).length, 2);
});
test("数值区间包含真实0，排除缺失和旧测试目标，精确比较大数", () => {
  const rows = [row(), row({ id: "2", test: { status: "NOT_TESTED", metrics: { connection_ms: null, rps: null, tps: null } } as ResourceRow["test"] }), row({ id: "3", test: { status: "STALE", metrics: { connection_ms: "0", rps: "12", tps: "1" } } as ResourceRow["test"] })];
  assert.deepEqual(filterSources(rows, { ...emptySourceFilters(), connection_max: "0" }).map(r => r.id), ["1"]);
  assert.equal(filterSources(rows, { ...emptySourceFilters(), tps_min: "9007199254740994" }).length, 0);
  assert.throws(() => validateSourceFilters({ ...emptySourceFilters(), coefficient_min: "0.9", coefficient_max: "0.5" }), /下限/);
  assert.throws(() => validateSourceFilters({ ...emptySourceFilters(), rps_min: "abc" }), /数字|数值/);
});
test("广场只聚合已有来源，按筛选后的来源计算系数和状态，不把已接入等同可报价", () => {
  const rows = [row(), row({ id: "2", coefficient: "0.5000", region: "OVERSEAS" })];
  const group = aggregateSources(rows)[0]!;
  assert.equal(group.supplier_count, 1);
  assert.equal(group.resource_count, 2);
  assert.equal(group.coefficient_min, "0.5000");
  assert.equal(group.state_label, "资源待接入");
  const admitted = aggregateSources([row({ state: "ADMITTED" })])[0]!;
  assert.equal(admitted.state_label, "有可用资源");
  assert.equal(admitted.available_count, 0);
  assert.equal(aggregateSources([row({ state: "OFFLINE" })])[0]!.state_label, "无可用资源");
  assert.deepEqual(aggregateSources([]), []);
});
test("空值在升降序都排末尾，大数和相等值排序稳定", () => {
  assert.equal(compareValues(null, "0.7", "desc", true), 1);
  assert.equal(compareValues("0.7", null, "asc", true), -1);
  assert.equal(compareValues(null, null, "desc", true), 0);
  assert.equal(compareValues("9007199254740993", "9007199254740992", "desc", true), -1);
});
