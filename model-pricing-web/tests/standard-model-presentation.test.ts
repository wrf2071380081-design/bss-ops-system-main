import assert from "node:assert/strict";
import { test } from "node:test";
import { modelParameterLabel, modelPriceGroups, modelOperationLabel, normalizeModelOperationPage } from "../src/api/standardModelPresentation.ts";

test("模型参数使用业务名称，价格展示去掉尾零并保留极小值和原币种单位", () => {
  assert.equal(modelParameterLabel("context_window"), "上下文");
  assert.equal(modelParameterLabel("tool_call"), "Function Calling");
  const price = { item_key: "input", label: "输入", task_type: "TEXT_INPUT", amount: "0.00000000", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准请求", dimensions: {}, source_url: "" };
  const groups = modelPriceGroups([price, { ...price, item_key: "output", label: "输出", amount: "0.00000001" }, { ...price, item_key: "image", label: "图片", currency: "CNY", unit_code: "IMAGE", unit_quantity: "1", amount: "0.25000000" }]);
  assert.equal(groups.length, 2);
  assert.deepEqual(groups[0]?.lines, ["输入 $0", "输出 $0.00000001"]);
  assert.equal(groups[0]?.unit, "USD / 1M tokens");
  assert.deepEqual(groups[1]?.lines, ["图片 ¥0.25"]);
  assert.equal(groups[1]?.unit, "CNY / 张");
});
test("不同价格条件和维度不合并，操作名称转换为业务描述", () => {
  const price = { item_key: "input", label: "输入", task_type: "TEXT_INPUT", amount: "2", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "短上下文", dimensions: { context_tier: "≤200K" }, source_url: "" };
  assert.equal(modelPriceGroups([price, { ...price, item_key: "long", condition: "长上下文", dimensions: { context_tier: ">200K" } }]).length, 2);
  assert.equal(modelOperationLabel("CREATE"), "新增标准模型");
  assert.equal(modelOperationLabel("MANUAL_SAVE"), "手动补全标准模型");
});
test("日志保留对象和安全摘要，拒绝跨业务对象和不完整分页，不透传快照或凭据", () => {
  const row = { id: "9007199254740993", entity_type: "standard_model", entity_id: "11", action: "UPDATE", operator_name: "维护人", reason: "核对价格", created_at: "2026-10-26T01:00:00Z", api_key: "should-not-appear", before_json: { secret: "hidden" } };
  const page = { list: [row], page: 1, size: 100, total: 1 };
  const result = normalizeModelOperationPage(page, 1);
  assert.equal(result.list[0]?.entity_id, "11");
  assert.equal(JSON.stringify(result).includes("should-not-appear"), false);
  assert.equal("before_json" in result.list[0]!, false);
  assert.throws(() => normalizeModelOperationPage({ ...page, list: [{ ...row, entity_type: "resource" }] }, 1));
  assert.throws(() => normalizeModelOperationPage({ ...page, page: 2 }, 1));
});
