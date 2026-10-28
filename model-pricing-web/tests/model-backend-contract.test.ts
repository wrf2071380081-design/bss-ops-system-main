import assert from "node:assert/strict";
import { test } from "node:test";
import { buildResourceQuery, resourceQueryWire } from "../src/api/resources.contract.ts";
import { emptyStandardModel } from "../src/api/standardModels.contract.ts";
import { normalizeModelOperationDetail } from "../src/api/standardModelPresentation.ts";
import { emptySourceFilters, sourceResourceQuery } from "../src/domain/modelPlaza.ts";

test("广场将多选、精确区间、排序和页码传到服务器，详情保留模型范围", () => {
  const filters = { ...emptySourceFilters(), companies: ["Company, LLC", "示例公司"], supplier_ids: ["9007199254740993", "21"], connection_max: "0", tps_min: "9007199254740993" };
  const query = buildResourceQuery(sourceResourceQuery(filters, 2, 20, "admitted_at", "desc", "11"));
  const wire = resourceQueryWire(query);
  assert.equal(wire.companies, '["Company, LLC","示例公司"]');
  assert.equal(wire.supplier_ids, '["9007199254740993","21"]');
  assert.equal(wire.connection_max, "0");
  assert.equal(wire.tps_min, "9007199254740993");
  assert.equal(wire.standard_model_id, "11");
  assert.equal(wire.page, 2);
  assert.equal(wire.sort_by, "admitted_at");
  assert.equal("regions" in wire, false);
  for (const value of [{ supplier_ids: ["21"], supplier_id: "21" }, { regions: ["INVALID"] }, { connection_min: "1", connection_max: "0" }, { rps_max: "NaN" }, { coefficient_max: "1.01" }]) assert.throws(() => buildResourceQuery(value));
});

test("模型日志详情保留历史名称和字符串大ID，拒绝跨模型及错误快照", () => {
  const modelID = "9007199254740993", logID = "9007199254740995";
  const snapshot = { id: modelID, data: { ...emptyStandardModel(), name: "历史模型", api_key: "hidden" }, version_no: "2", revision: "3", verify_status: "INCOMPLETE", password: "hidden" };
  const row = { id: logID, entity_id: modelID, model_name: "历史模型", action: "UPDATE", created_at: "2026-10-26T01:00:00Z", before: null, after: snapshot, token: "hidden" };
  const result = normalizeModelOperationDetail(row, modelID, logID);
  assert.equal(result.id, logID);
  assert.equal(result.model_name, "历史模型");
  assert.equal(result.after?.data.name, "历史模型");
  assert.equal(JSON.stringify(result).includes("hidden"), false);
  assert.throws(() => normalizeModelOperationDetail(row, "12", logID));
  assert.throws(() => normalizeModelOperationDetail({ ...row, after: { ...snapshot, id: "12" } }, modelID, logID));
  assert.throws(() => normalizeModelOperationDetail({ ...row, id: 9007199254740995 }, modelID, logID));
  assert.equal(normalizeModelOperationDetail({ ...row, after: null }, modelID, logID).after, null);
});
