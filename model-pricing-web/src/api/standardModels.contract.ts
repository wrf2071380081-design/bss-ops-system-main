import Decimal from "decimal.js";
import type { StandardModelData, StandardModelDTO, StandardPrice } from "./standardModels.types.ts";

export const modelTypeLabels: Record<string, string> = { TEXT: "文本", IMAGE: "图片", VIDEO: "视频", AUDIO: "音频" };
export const factLabels: Record<string, string> = { DISCLOSED: "已披露", UNDISCLOSED: "未披露", UNVERIFIED: "未核实" };
export const abilityLabels: Record<string, string> = { SUPPORTED: "支持", UNSUPPORTED: "不支持", UNDISCLOSED: "未披露", UNVERIFIED: "未核实" };
export const taskLabels: Record<string, string> = {
  TEXT_INPUT: "文本输入", TEXT_OUTPUT: "文本输出", CACHE_INPUT: "缓存输入", CACHE_STORAGE: "缓存存储",
  IMAGE_INPUT: "图片输入", IMAGE_OUTPUT: "图片输出", IMAGE_GENERATION: "图片生成",
  VIDEO_INPUT: "视频输入", VIDEO_OUTPUT: "视频输出", VIDEO_GENERATION: "视频生成",
  AUDIO_INPUT: "音频输入", AUDIO_OUTPUT: "音频输出", AUDIO_TRANSCRIPTION: "音频转写", AUDIO_RECOGNITION: "语音识别",
};
export const unitLabels: Record<string, string> = { TOKEN: "Token", TOKEN_HOUR: "Token·小时", IMAGE: "张", SECOND: "秒", MINUTE: "分钟", REQUEST: "次", CHARACTER: "字符" };
export const dimensionLabels: Record<string, string> = { context_tier: "上下文档位", resolution: "分辨率", duration_seconds: "时长（秒）", quality: "质量", cache_type: "缓存类型", modality: "模态", region:"适用地区", deployment:"部署方式", valid_from:"生效日期", valid_to:"失效日期", period:"计费时段", account_tier:"账号等级" };
export const sourceLabels: Record<string, string> = { OFFICIAL_SITE: "官网", OFFICIAL_DOCS: "官方文档", OFFICIAL_PRICING: "官方价格" };
export const fieldLabels: Record<string, string> = { company: "模型公司", name: "模型名称", official_id: "官方 ID", model_type: "模型类型", input_modalities: "输入模态", output_modalities: "输出模态", capacity: "容量规格", abilities: "核心能力", price_items: "官方价格", api_access: "API / 访问条件", lifecycle: "发布 / 生命周期", sources: "官方来源", checked_at: "核对日期" };
export function standardFieldLabel(path: string): string { return (fieldLabels[path.split(/[.[]/)[0]!] || path) + (/[.[]/.test(path) ? `（${path}）` : ""); }
export function emptyStandardModel(): StandardModelData {
  return { company: "", name: "", official_id: "", model_type: "", input_modalities: [], output_modalities: [], capacity: [], abilities: [], price_items: [], api_access: [], lifecycle: [], sources: [], checked_at: "" };
}
export function standardModelVersion(value: unknown): string {
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value) || BigInt(value) > 9223372036854775807n) throw new Error("标识或版本必须是有效的正整数字符串。");
  return value;
}
export function isOfficialURL(value: string): boolean {
  try {
    if (/\s/.test(value)) return false;
    decodeURI(value);
    const url = new URL(value);
    if (!["https:", "http:"].includes(url.protocol) || !url.hostname || url.username || url.password) return false;
    return ![...url.searchParams.keys()].some(key => ["key", "api_key", "apikey", "token", "access_token", "password", "secret", "authorization", "signature", "sig", "x-amz-signature", "credential"].includes(key.toLowerCase()));
  } catch { return false; }
}
function validDecimal(value: unknown, positive: boolean): boolean {
  if (typeof value !== "string" || !/^\d+(?:\.\d{1,8})?$/.test(value) || value.split(".")[0]!.replace(/^0+/, "").length > 12) return false;
  const amount = new Decimal(value);
  return !positive || amount.gt(0);
}
export function shanghaiToday(): string {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(new Date());
  return ["year", "month", "day"].map(key => parts.find(part => part.type === key)!.value).join("-");
}
export interface StandardFieldIssue { path: string; message: string }
export function validateStandardModel(data: StandardModelData): StandardFieldIssue[] {
  const issues: StandardFieldIssue[] = [];
  const issue = (path: string, message: string) => { issues.push({ path, message }); };
  for (const key of ["company", "name", "official_id", "model_type", "checked_at"] as const) if (typeof data[key] !== "string") issue(key, "请填写文本");
  if (data.model_type && !modelTypeLabels[data.model_type]) issue("model_type", "请选择支持的模型类型");
  for (const key of ["input_modalities", "output_modalities"] as const) {
    if (!Array.isArray(data[key]) || data[key].some(value => !modelTypeLabels[value]) || new Set(data[key]).size !== data[key].length) issue(key, "请选择不重复的有效模态");
  }
  for (const key of ["capacity", "api_access", "lifecycle"] as const) {
    const seen = new Set<string>();
    for (const [index, fact] of data[key].entries()) {
      const path = `${key}[${index}]`;
      if (!/^[a-z][a-z0-9_]*$/.test(fact.key) || seen.has(fact.key)) issue(path + ".key", "资料标识需为唯一的小写英文标识");
      seen.add(fact.key);
      if (fact.source_url && !isOfficialURL(fact.source_url)) issue(path + ".source_url", "请填写不含凭据的证据网址");
      if (fact.semantics && !['SHARED_WINDOW','SEPARATE_INPUT_OUTPUT','INPUT_ONLY','UNSPECIFIED'].includes(fact.semantics)) issue(path + '.semantics','请选择有效的 Token 定义');
      if (fact.unit && !unitLabels[fact.unit]) issue(path + '.unit','请选择有效单位');
      if (!factLabels[fact.disclosure]) issue(path + ".disclosure", "请选择披露状态");
      if (fact.disclosure === "DISCLOSED" && !fact.value.trim()) issue(path + ".value", "已披露资料需填写具体内容");
    }
  }
  const abilities = new Set<string>();
  for (const [index, ability] of data.abilities.entries()) {
    if (!/^[a-z][a-z0-9_]*$/.test(ability.key) || abilities.has(ability.key)) issue(`abilities[${index}].key`, "能力标识需唯一且有效");
    abilities.add(ability.key);
    if (ability.source_url && !isOfficialURL(ability.source_url)) issue(`abilities[${index}].source_url`,"请填写不含凭据的证据网址");
    if (!abilityLabels[ability.state]) issue(`abilities[${index}].state`, "请选择能力状态");
  }
  const prices = new Set<string>();
  const priceScopes=new Map<string,string>();
  for (const [index, price] of data.price_items.entries()) {
    const path = `price_items[${index}].`;
    if (!/^[a-z][a-z0-9_]*$/.test(price.item_key) || prices.has(price.item_key)) issue(path + "item_key", "价格项标识需唯一且有效");
    prices.add(price.item_key);
    if (!price.label.trim()) issue(path + "label", "请填写价格项名称");
    if (!taskLabels[price.task_type]) issue(path + "task_type", "请选择任务类型");
    if (!validDecimal(price.amount, false)) issue(path + "amount", "金额需为非负十进制字符串，最多12位整数和8位小数");
    if (!/^[A-Z]{3}$/.test(price.currency)) issue(path + "currency", "币种需为三位大写字母");
    if (!unitLabels[price.unit_code]) issue(path + "unit_code", "请选择计费单位");
    if (!validDecimal(price.unit_quantity, true)) issue(path + "unit_quantity", "单位数量需大于零，最多8位小数");
    if (!price.condition.trim()) issue(path + "condition", "请说明价格适用条件");
    for (const [key, value] of Object.entries(price.dimensions)) if (!dimensionLabels[key] || !value.trim()) issue(path + "dimensions." + key, "价格维度无效或内容为空");
    if (price.source_url && !isOfficialURL(price.source_url)) issue(path + "source_url", "请填写不含凭据的 HTTP(S) 来源");
    // 同一 task_type 可以有多档计费项（缓存写入 5 分钟 / 1 小时、缓存读取等只有名称不同），
    // 所以判重必须带上计费项名称，否则合法档位会被当成「同一条件两个价」而无法保存。
    const scope=JSON.stringify([price.task_type,price.label.trim(),price.currency,price.unit_code,price.unit_quantity,price.condition,Object.entries(price.dimensions??{}).sort(([a],[b])=>a.localeCompare(b))]);
    if(validDecimal(price.amount,false)) {const amount=new Decimal(price.amount).toFixed();if(priceScopes.has(scope)&&priceScopes.get(scope)!==amount)issue(path+'amount','同一计费项与计费条件存在不同价格，请核对后只采纳一条');priceScopes.set(scope,amount);}
  }
  for (const [index, source] of data.sources.entries()) {
    if (!source.label.trim()) issue(`sources[${index}].label`, "请填写来源名称");
    if (!sourceLabels[source.source_kind]) issue(`sources[${index}].source_kind`, "请选择官方来源类型");
    if (!isOfficialURL(source.url)) issue(`sources[${index}].url`, "请填写不含凭据的 HTTP(S) 来源");
  }
  if (data.checked_at) {
    const parsed = new Date(data.checked_at + "T00:00:00Z");
    if (!/^\d{4}-\d{2}-\d{2}$/.test(data.checked_at) || Number.isNaN(parsed.valueOf()) || parsed.toISOString().slice(0, 10) !== data.checked_at || data.checked_at > shanghaiToday()) issue("checked_at", "请选择不晚于今天的真实日期（上海时区）");
  }
  return issues;
}
export function copyStandardModel(data: StandardModelData): StandardModelData {
  const result = emptyStandardModel();
  for (const key of ["company", "name", "official_id", "model_type", "checked_at"] as const) result[key] = data[key] ?? "";
  for (const key of ["input_modalities", "output_modalities", "capacity", "abilities", "price_items", "api_access", "lifecycle", "sources"] as const) Object.assign(result, { [key]: JSON.parse(JSON.stringify(data[key] ?? [])) });
  for (const price of result.price_items) price.dimensions ??= {};
  return result;
}
export function buildStandardModelSave<T extends StandardModelData>(data: T, reason: string, revision?: string): StandardModelData & { reason: string; expected_revision?: string } {
  const copy = copyStandardModel(data);
  const issues = validateStandardModel(copy);
  if (issues.length) throw new Error(`${standardFieldLabel(issues[0]!.path)}：${issues[0]!.message}`);
  if (!reason.trim()) throw new Error("请填写操作说明。");
  return { ...copy, reason: reason.trim(), ...(revision === undefined ? {} : { expected_revision: standardModelVersion(revision) }) };
}
export function formatStandardPrice(price: StandardPrice): string {
  return `${price.label}：${price.amount} ${price.currency} / ${price.unit_quantity} ${unitLabels[price.unit_code] || price.unit_code}`;
}
export function normalizeStandardModel(model: StandardModelDTO, expectedID?: string): StandardModelDTO {
  // 降级行：后端解析不了这一行的资料（快照/价格/领域校验没过），只保证 id 与展示字段可信。
  // 这里刻意不跑契约校验——否则一行坏数据会让整个列表抛错，「单行降级」就白做了。
  const loadError = typeof model?.load_error === "string" ? model.load_error.trim() : "";
  if (loadError) {
    standardModelVersion(model.id);
    if (expectedID !== undefined && model.id !== expectedID) throw new Error("返回的模型与请求对象不一致，请重新查询。");
    return {
      ...model,
      ...copyStandardModel(model),
      id: model.id,
      maintainer_id: "",
      revision: "0",
      version_no: "0",
      status: model.status === "INACTIVE" ? "INACTIVE" : "ACTIVE",
      verify_status: "INCOMPLETE",
      checked_by: "",
      created_at: model.created_at ?? "",
      updated_at: model.updated_at ?? "",
      missing_fields: [],
      quote_readiness: { ready: false, reasons: ["UNREADABLE_ROW"], standard_model_version_no: "0" },
      permissions: { can_edit: false, can_verify: false },
      load_error: loadError,
    };
  }
  standardModelVersion(model.id); standardModelVersion(model.revision); standardModelVersion(model.version_no);
  standardModelVersion(model.maintainer_id);
  if (expectedID !== undefined && model.id !== expectedID) throw new Error("返回的模型与请求对象不一致，请重新查询。");
  if (!["ACTIVE", "INACTIVE"].includes(model.status) || !["INCOMPLETE", "UNVERIFIED", "VERIFIED"].includes(model.verify_status)) throw new Error("模型状态不符合约定，请重新查询。");
  if (typeof model.permissions?.can_edit !== "boolean" || typeof model.permissions?.can_verify !== "boolean" || !Array.isArray(model.missing_fields) || typeof model.quote_readiness?.ready !== "boolean") throw new Error("模型响应缺少操作资格或完整性资料，请重新查询。");
  const normalized = { ...model, ...copyStandardModel(model) };
  if (validateStandardModel(normalized).length) throw new Error("模型资料响应不符合字段约定，请重新查询。");
  return normalized;
}
