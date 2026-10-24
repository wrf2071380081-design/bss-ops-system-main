import test from "node:test";
import assert from "node:assert/strict";
import { emptySourceFilters, filterSources } from "../src/api/sourceFilters";
import { validateResourceReport } from "../src/api/resourceReports";
import type { ResourceRow } from "../src/api/resources.types";

test("source ranges preserve zero, exclude missing/stale metrics, and reject inverted bounds", () => {
  const row = { supplier_id: "21", state: "ADMITTED", coefficient: "0.5000", test: { status: "COMPLETED", metrics: { connection_ms: "0", rps: "12.5", tps: null } } } as ResourceRow;
  const filters = emptySourceFilters();
  filters.ranges.connection_ms = { min: "0", max: "0" };
  assert.deepEqual(filterSources([row, { ...row, test: { ...row.test, status: "STALE" } }], filters), [row]);
  filters.ranges.tps.min = "0";
  assert.equal(filterSources([row], filters).length, 0);
  filters.ranges.rps = { min: "20", max: "10" };
  assert.throws(() => filterSources([], filters));
});
const report = { id: "51", resource_id: "31", status: "COMPLETED", created_at: "2026-10-22T01:00:00Z", tested_at: "2026-10-22T01:00:30Z", test_item: "stress", operator_name: "测评人员甲", source: "", scene: "压力组合场景", finished_at: "2026-10-22T01:01:00Z", target_matches: true,
  connectivity_passed: true, pressure_passed: false, metrics: { connection_ms: "0", rps: "12.5", tps: "0" }, cache_stats: { status: "PROVIDED", report_id: "51", hit_tokens: "0", input_tokens: "100", scene: "无缓存" } };
test("report projection checks resource/report identity and never retains provider credentials", () => {
  const result = validateResourceReport({ ...report, api_key: "must-not-reach-view", raw_payload: { token: "private" } }, "31");
  assert.equal(result.tested_at, report.tested_at);
  assert.equal(result.test_item, "stress");
  assert.equal(result.operator_name, "测评人员甲");
  assert.equal(result.source, "");
  assert.equal(result.scene, "压力组合场景");
  assert.equal(result.metrics.connection_ms, "0");
  assert.equal(result.pressure_passed, false);
  assert.equal("api_key" in result, false);
  assert.equal("raw_payload" in result, false);
  assert.throws(() => validateResourceReport(report, "32"));
  assert.throws(() => validateResourceReport({ ...report, cache_stats: { ...report.cache_stats, report_id: "52" } }, "31"));
  assert.throws(() => validateResourceReport({ ...report, metrics: { ...report.metrics, rps: "NaN" } }, "31"));
  assert.throws(() => validateResourceReport({ ...report, source: "GUESSED" }, "31"));
  assert.throws(() => validateResourceReport({ ...report, tested_at: "unknown" }, "31"));
});
