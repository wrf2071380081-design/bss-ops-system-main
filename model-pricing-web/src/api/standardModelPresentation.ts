import Decimal from "decimal.js";
import { dimensionLabels, standardModelVersion, unitLabels } from "./standardModels.contract.ts";
import { copyStandardModel, validateStandardModel } from "./standardModels.contract.ts";
import type { StandardModelData, StandardPrice } from "./standardModels.types.ts";

const parameterLabels: Record<string, string> = {
  audio_generation:'语音生成',image_generation:'图像生成',code_execution:'代码执行',file_search:'文件检索',live_api:'实时 API',url_context:'网址理解',web_search:'联网搜索',maps_grounding:'地图信息检索',
  native_context_length:'原生上下文',extended_context_length:'扩展上下文',weight_version:'权重版本',api_version:'API 版本',weight_lifecycle:'权重生命周期',api_lifecycle:'API 生命周期',confirmed_aliases:'已确认别名',retirement_date:'退役日期',lifecycle_status:'生命周期',account_tier:'账号等级',region:'适用地区',deployment:'部署方式',openai_base_url:'OpenAI 兼容地址',anthropic_base_url:'Anthropic 兼容地址',concurrency_limit:'并发上限',
  context: "上下文", context_window: "上下文", context_length: "上下文", max_input_tokens: "最大输入", max_output_tokens: "最大输出",
  max_input: "最大输入", max_output: "最大输出", resolution: "分辨率", duration: "时长", duration_seconds: "时长（秒）", frame_rate: "帧率",
  thinking: "Thinking", reasoning: "Thinking", tool_call: "Function Calling", tool_calling: "Function Calling", function_call: "Function Calling", function_calling: "Function Calling",
  tool_calls: "工具调用", json_output: "JSON 输出", anthropic_api: "Anthropic API", responses_api: "Responses API",
  fim_completion_beta: "FIM 补全（Beta）", chat_prefix_completion_beta: "对话前缀续写（Beta）",
  structured_outputs: "Structured Outputs", structured_output: "Structured Outputs", streaming: "Streaming", cache: "Cache", caching: "Cache", vision: "视觉理解",
  protocol: "协议", endpoint: "API 端点", api: "API", rpm: "RPM", tpm: "TPM", rpd: "RPD", rate_limit: "限流", rate_limits: "限流", access: "访问条件", access_conditions: "访问条件",
  release: "发布日期", release_date: "发布日期", version: "版本", snapshot: "快照", lifecycle: "生命周期", status: "状态", deprecation: "弃用信息", deprecated_at: "弃用日期",
};
export const modelParameterLabel = (key: string) => parameterLabels[key] || key.replaceAll("_", " ");
const semanticsLabels:Record<string,string>={SHARED_WINDOW:'输入输出共享窗口',SEPARATE_INPUT_OUTPUT:'输入输出分别计限',INPUT_ONLY:'仅公布输入上限'};
export function modelFactText(fact: import('./standardModels.types.ts').StandardFact):string {
  if(fact.disclosure!=='DISCLOSED')return fact.disclosure==='UNDISCLOSED'?'未披露':'待核实';
  const value=/^\d+$/.test(fact.value)?Number(fact.value).toLocaleString('en-US'):fact.value;
  const scopes:Record<string,string>={API:'当前 API',WEIGHTS:'开放权重',NATIVE:'原生窗口',EXTENDED:'扩展窗口'};
  return [[value,fact.unit?unitLabels[fact.unit]??fact.unit:''].filter(Boolean).join(' '),semanticsLabels[fact.semantics??''],scopes[fact.scope??'']??fact.scope,fact.condition].filter(Boolean).join(' · ');
}
export interface ModelPriceGroup { key: string; lines: string[]; unit: string; condition: string; dimensions: string[] }
export function modelPriceGroups(prices: StandardPrice[]): ModelPriceGroup[] {
  const groups = new Map<string, ModelPriceGroup>();
  for (const price of prices) {
    const quantity = new Decimal(price.unit_quantity).toFixed();
    const dimensions = Object.entries(price.dimensions || {}).sort(([a], [b]) => a.localeCompare(b));
    const key = JSON.stringify([price.currency, price.unit_code, quantity, price.condition, dimensions]);
    if (!groups.has(key)) {
      let unit = `${quantity === "1" ? "" : `${quantity} `}${unitLabels[price.unit_code] || price.unit_code}`;
      if (["TOKEN", "TOKEN_HOUR"].includes(price.unit_code) && quantity === "1000000") unit = `1M tokens${price.unit_code === "TOKEN_HOUR" ? "·小时" : ""}`;
      groups.set(key, { key, lines: [], unit: `${price.currency} / ${unit}`, condition: price.condition, dimensions: dimensions.map(([key, value]) => `${dimensionLabels[key] || key}：${value}`) });
    }
    const symbol = price.currency === "USD" ? "$" : price.currency === "CNY" ? "¥" : `${price.currency} `;
    groups.get(key)!.lines.push(`${price.label} ${symbol}${new Decimal(price.amount).toFixed()}`);
  }
  return [...groups.values()];
}
const operationLabels: Record<string, string> = {
  CREATE: "新增标准模型", UPDATE: "修改标准模型", UPDATE_NOOP: "保存标准模型（无变化）", VERIFY: "人工核验标准模型", VERIFY_NOOP: "人工核验标准模型（无变化）",
  MANUAL_CREATE: "新增标准模型",
  MANUAL_SAVE: "手动补全标准模型", MANUAL_SAVE_NOOP: "手动补全标准模型（无变化）",
  // 抓取类（对齐交互原型「标准模型库 · 操作记录」）：失败记录的对象是「系统记录」，没有模型名。
  MODEL_CRAWL_MANUAL_FAILED: "手动抓取尝试失败",
  MODEL_CRAWL_MANUAL_FAILED_SIM: "手动抓取尝试失败（模拟）",
  MODEL_CRAWL_AUTO_FAILED_SIM: "自动抓取尝试失败（模拟）",
};
export const modelOperationLabel = (action: string) => operationLabels[action] || action;
export interface ModelOperation { id: string; entity_id: string; action: string; operator_name: string; reason: string; created_at: string; model_name: string }
function operationID(value: unknown) {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return standardModelVersion(String(value));
  return standardModelVersion(value);
}
export function normalizeModelOperationPage(value: unknown, page: number): { list: ModelOperation[]; total: number } {
  const invalid = () => { throw new Error("模型操作记录响应不符合约定，请重新加载。"); };
  if (!value || typeof value !== "object") return invalid();
  const result = value as { list: Record<string, unknown>[]; total: number; page: number; size: number };
  if (!Array.isArray(result.list) || result.page !== page || result.size !== 100 || !Number.isSafeInteger(result.total) || result.total < result.list.length || result.list.length > 100) return invalid();
  return { total: result.total, list: result.list.map(row => {
    if (!row || (row.entity_type !== undefined && row.entity_type !== "standard_model") || typeof row.action !== "string" || !row.action || typeof row.created_at !== "string" || !Number.isFinite(Date.parse(row.created_at)) || (row.operator_name != null && typeof row.operator_name !== "string") || (row.reason != null && typeof row.reason !== "string")) return invalid();
    return { id: operationID(row.id), entity_id: operationID(row.entity_id), action: row.action, operator_name: typeof row.operator_name === "string" ? row.operator_name : "", reason: typeof row.reason === "string" ? row.reason : "", created_at: row.created_at, model_name: typeof row.model_name === "string" ? row.model_name : "" };
  }) };
}

export interface ModelOperationSnapshot { id: string; data: StandardModelData; version_no: string; revision: string; verify_status: string }
export interface ModelOperationDetail extends ModelOperation { before: ModelOperationSnapshot | null; after: ModelOperationSnapshot | null }
export function normalizeModelOperationDetail(value: unknown, modelID: string, operationID: string): ModelOperationDetail {
  if (!value || typeof value !== "object") throw new Error("模型日志详情无效。");
  const row = normalizeModelOperationPage({ list: [value], total: 1, page: 1, size: 100 }, 1).list[0]!;
  if (row.entity_id !== modelID || row.id !== operationID) throw new Error("日志与当前模型不匹配。");
  const raw = value as Record<string, unknown>;
  const snapshot = (value: unknown): ModelOperationSnapshot | null => {
    if (value === null) return null;
    if (!value || typeof value !== "object") throw new Error("历史资料快照无效。");
    const item = value as ModelOperationSnapshot;
    if (item.id !== modelID || !["INCOMPLETE", "UNVERIFIED", "VERIFIED"].includes(item.verify_status)) throw new Error("历史快照与模型不匹配。");
    standardModelVersion(item.version_no); standardModelVersion(item.revision);
    const data = copyStandardModel(item.data);
    if (validateStandardModel(data).length) throw new Error("历史资料快照不符合字段约定。");
    return { id: item.id, data, version_no: item.version_no, revision: item.revision, verify_status: item.verify_status };
  };
  return { ...row, before: snapshot(raw.before), after: snapshot(raw.after) };
}
