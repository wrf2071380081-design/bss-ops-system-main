import assert from "node:assert/strict";
import { test } from "node:test";
import { emptyStandardModel } from "../src/api/standardModels.contract.ts";
import { applyModelNotes, modelNotes, parsePriceUnit, priceUnitText } from "../src/api/standardModelEditor.ts";

test("维护文本回填与无修改保存不改变字段键、披露状态及多行内容", () => {
  const model = emptyStandardModel();
  model.input_modalities = ["TEXT", "IMAGE"]; model.output_modalities = ["TEXT"];
  model.capacity = [{ key: "context_window", disclosure: "DISCLOSED", value: "128000 tokens\nAPI 上限需单独核对" }, { key: "max_output_tokens", disclosure: "UNDISCLOSED", value: "官方尚未披露" }];
  model.abilities = [{ key: "tool_call", state: "SUPPORTED", description: "仅限特定版本" }, { key: "cache", state: "UNVERIFIED", description: "" }];
  const notes = modelNotes(model);
  assert.match(notes.modalities, /输入：文本、图片/);
  assert.match(notes.abilities, /Function Calling ✓/);
  assert.deepEqual(applyModelNotes(model, notes), model);
  const edited = { ...notes, capacity: notes.capacity.replace("128000", "256000") };
  const result = applyModelNotes(model, edited);
  assert.equal(result.capacity[0]?.key, "context_window");
  assert.equal(result.capacity[0]?.value, "256000 tokens\nAPI 上限需单独核对");
  assert.deepEqual(result.capacity[1], model.capacity[1]);
});
test("编辑原型文本保持未披露/未核实不同，不从说明文字推断能力通过", () => {
  const model = emptyStandardModel(), notes = modelNotes(model);
  const result = applyModelNotes(model, { ...notes, modalities: "输入：文本、图片\n输出：文本", capacity: "上下文：128000 tokens\n最大输出：未披露", abilities: "Thinking ×\nFunction Calling ✓（需 API Key）\nStreaming：未核实\nCache：未披露" });
  assert.deepEqual(result.input_modalities, ["TEXT", "IMAGE"]);
  assert.equal(result.capacity[1]?.disclosure, "UNDISCLOSED");
  assert.deepEqual(result.abilities.map(row => row.state), ["UNSUPPORTED", "SUPPORTED", "UNVERIFIED", "UNDISCLOSED"]);
  assert.equal(result.abilities[1]?.description, "需 API Key");
  assert.throws(() => applyModelNotes(model, { ...notes, modalities: "输入：未知模态" }), /模态/);
  assert.throws(() => applyModelNotes(model, { ...notes, capacity: "上下文：1\n上下文：2" }), /重复/);
});
test("任意补充说明有稳定标识，清空资料可保留待补齐状态", () => {
  const model = emptyStandardModel(), notes = modelNotes(model);
  const result = applyModelNotes(model, { ...notes, api: "Responses / Chat Completions\n限流：5000 RPM" });
  assert.equal(result.api_access.length, 2);
  assert.match(result.api_access[0]!.key, /^[a-z][a-z0-9_]*$/);
  assert.deepEqual(applyModelNotes(result, { ...modelNotes(result), api: "" }).api_access, []);
});
test("同名字段可区分，修改说明不会丢失价格维度、补充来源或篡改原对象", () => {
  const model = emptyStandardModel();
  model.capacity = [{ key: "context", disclosure: "DISCLOSED", value: "128K" }, { key: "context_window", disclosure: "UNVERIFIED", value: "独立规格" }];
  model.price_items = [{ item_key: "input", label: "输入", task_type: "TEXT_INPUT", amount: "0.00000001", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000", condition: "长上下文", dimensions: { context_tier: ">200k" }, source_url: "https://vendor.example/pricing" }];
  model.sources = [{ label: "官方价格", source_kind: "OFFICIAL_PRICING", url: "https://vendor.example/pricing" }, { label: "官方文档", source_kind: "OFFICIAL_DOCS", url: "https://vendor.example/docs" }];
  const notes = modelNotes(model);
  const result = applyModelNotes(model, { ...notes, capacity: notes.capacity.replace("128K", "256K") });
  assert.equal(result.capacity[0]?.key, "context");
  assert.equal(result.capacity[1]?.key, "context_window");
  assert.equal(model.capacity[0]?.value, "128K");
  assert.deepEqual(result.price_items, model.price_items);
  assert.deepEqual(result.sources, model.sources);
  result.price_items[0]!.dimensions.context_tier = "changed";
  assert.equal(model.price_items[0]?.dimensions.context_tier, ">200k");
});
test("主价格表单位可往返百万tokens、特殊单位及任意精确数量，不改变金额", () => {
  assert.equal(priceUnitText({ unit_code: "TOKEN", unit_quantity: "1000000" }), "百万 tokens");
  assert.deepEqual(parsePriceUnit("百万 tokens"), { unit_code: "TOKEN", unit_quantity: "1000000" });
  assert.deepEqual(parsePriceUnit("百万 tokens·小时"), { unit_code: "TOKEN_HOUR", unit_quantity: "1000000" });
  assert.deepEqual(parsePriceUnit("张"), { unit_code: "IMAGE", unit_quantity: "1" });
  assert.deepEqual(parsePriceUnit("1000.00000001 tokens"), { unit_code: "TOKEN", unit_quantity: "1000.00000001" });
  assert.throws(() => parsePriceUnit("未知单位"), /单位/);
  assert.throws(() => parsePriceUnit("0 tokens"), /单位/);
});
