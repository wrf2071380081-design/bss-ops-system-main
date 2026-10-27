import Decimal from "decimal.js";
import { abilityLabels, copyStandardModel, factLabels, modelTypeLabels, unitLabels } from "./standardModels.contract.ts";
import { modelParameterLabel } from "./standardModelPresentation.ts";
import type { StandardAbility, StandardFact, StandardModelData } from "./standardModels.types.ts";

export interface ModelNotes { modalities: string; capacity: string; abilities: string; api: string; lifecycle: string }
const keys = ["context_window", "max_input_tokens", "max_output_tokens", "resolution", "duration_seconds", "frame_rate", "thinking", "tool_call", "structured_outputs", "streaming", "cache", "vision", "protocol", "endpoint", "rpm", "tpm", "rpd", "rate_limit", "access_conditions", "release_date", "version", "snapshot", "lifecycle", "status", "deprecation"];
function label(key: string, rows: { key: string }[]) {
  const name = modelParameterLabel(key);
  return rows.filter(row => modelParameterLabel(row.key) === name).length > 1 ? `${name} [${key}]` : name;
}
const multiline = (value: string) => value.replaceAll("\n", "\n  ");
function factLine(row: StandardFact, rows: StandardFact[]) {
  const content = row.disclosure === "DISCLOSED" ? row.value : `${factLabels[row.disclosure]}${row.value ? `（${row.value}）` : ""}`;
  return `${label(row.key, rows)}：${multiline(content)}`;
}
function abilityLine(row: StandardAbility, rows: StandardAbility[]) {
  const state = row.state === "SUPPORTED" ? "✓" : row.state === "UNSUPPORTED" ? "×" : `：${abilityLabels[row.state]}`;
  return `${label(row.key, rows)} ${state}${row.description ? `（${multiline(row.description)}）` : ""}`;
}
export function modelNotes(model: StandardModelData): ModelNotes {
  return {
    modalities: [model.input_modalities.length ? `输入：${model.input_modalities.map(value => modelTypeLabels[value]).join("、")}` : "", model.output_modalities.length ? `输出：${model.output_modalities.map(value => modelTypeLabels[value]).join("、")}` : ""].filter(Boolean).join("\n"),
    capacity: model.capacity.map(row => factLine(row, model.capacity)).join("\n"),
    abilities: model.abilities.map(row => abilityLine(row, model.abilities)).join("\n"),
    api: model.api_access.map(row => factLine(row, model.api_access)).join("\n"),
    lifecycle: model.lifecycle.map(row => factLine(row, model.lifecycle)).join("\n"),
  };
}
function lines(text: string) {
  const result: string[] = [];
  for (const line of text.split(/\r?\n/)) {
    if (/^\s+/.test(line) && result.length) result[result.length - 1] += `\n${line.replace(/^ {1,2}/, "")}`;
    else if (line.trim()) result.push(line.trim());
  }
  return result;
}
function findKey(name: string, existing: { key: string }[]) {
  const normalized = name.trim();
  const match = existing.find(row => label(row.key, existing) === normalized || row.key === normalized || modelParameterLabel(row.key) === normalized);
  return match?.key || keys.find(key => key === normalized || modelParameterLabel(key) === normalized);
}
function newKey(used: Set<string>, existing: { key: string }[], prefix: string) {
  let index = 1; while (used.has(`${prefix}_${index}`) || existing.some(row => row.key === `${prefix}_${index}`)) index++;
  return `${prefix}_${index}`;
}
function parseFacts(text: string, existing: StandardFact[]) {
  const used = new Set<string>();
  return lines(text).map(line => {
    const unchanged = existing.find(row => factLine(row, existing) === multiline(line));
    const delimiter = line.search(/[：:]/);
    const knownKey = delimiter < 0 ? undefined : findKey(line.slice(0, delimiter), existing);
    const key = unchanged?.key || knownKey || newKey(used, existing, "note");
    if (used.has(key)) throw new Error("资料项重复，请合并同一项目的说明。"); used.add(key);
    if (unchanged) return { ...unchanged };
    const content = knownKey ? line.slice(delimiter + 1).trim() : line;
    const disclosure = /^(未披露|未核实)(?:[（(]([\s\S]*)[）)])?$/.exec(content);
    return { key, disclosure: disclosure ? disclosure[1] === "未披露" ? "UNDISCLOSED" : "UNVERIFIED" : "DISCLOSED", value: disclosure ? disclosure[2] || "" : content };
  });
}
function parseAbilities(text: string, existing: StandardAbility[]) {
  const used = new Set<string>();
  return lines(text).map(line => {
    const unchanged = existing.find(row => abilityLine(row, existing) === multiline(line));
    const match = /^(.*?)(?:\s*[：:]?\s*)(✓|√|×|✗|不支持|支持|未披露|未核实)(?:\s*[（(]([\s\S]*)[）)]|\s*(.*))$/.exec(line);
    const knownKey = findKey(match ? match[1]! : line, existing);
    const key = unchanged?.key || knownKey || newKey(used, existing, "ability");
    if (used.has(key)) throw new Error("核心能力重复，请合并同一能力的说明。"); used.add(key);
    if (unchanged) return { ...unchanged };
    const marker = match?.[2];
    const state = marker === "✓" || marker === "√" || marker === "支持" ? "SUPPORTED" : marker === "×" || marker === "✗" || marker === "不支持" ? "UNSUPPORTED" : marker === "未披露" ? "UNDISCLOSED" : "UNVERIFIED";
    return { key, state, description: match ? (match[3] || match[4] || "").trim() : line };
  });
}
export function applyModelNotes(model: StandardModelData, notes: ModelNotes): StandardModelData {
  const result = copyStandardModel(model), before = modelNotes(model);
  if (notes.modalities !== before.modalities) {
    result.input_modalities = []; result.output_modalities = [];
    const directions = new Set<string>();
    for (const line of notes.modalities.split(/\r?\n/).filter(line => line.trim())) {
      const match = /^\s*(输入|输出)\s*[：:]\s*(.*?)\s*$/.exec(line);
      if (!match || directions.has(match[1]!)) throw new Error("输入／输出模态请分别写为“输入：文本、图片”和“输出：文本”，每类一行。");
      directions.add(match[1]!);
      const modalities = match[2]!.split(/[、，,/／;；]/).map(value => value.trim()).filter(Boolean).map(name => {
        const key = Object.keys(modelTypeLabels).find(key => key === name || modelTypeLabels[key] === name);
        if (!key) throw new Error(`当前不支持“${name}”模态，请使用文本、图片、视频或音频。`);
        return key;
      });
      result[match[1] === "输入" ? "input_modalities" : "output_modalities"] = [...new Set(modalities)];
    }
  }
  if (notes.capacity !== before.capacity) result.capacity = parseFacts(notes.capacity, model.capacity);
  if (notes.api !== before.api) result.api_access = parseFacts(notes.api, model.api_access);
  if (notes.lifecycle !== before.lifecycle) result.lifecycle = parseFacts(notes.lifecycle, model.lifecycle);
  if (notes.abilities !== before.abilities) result.abilities = parseAbilities(notes.abilities, model.abilities);
  return result;
}
const unitNames: Record<string, string> = { TOKEN: "tokens", TOKEN_HOUR: "tokens·小时", IMAGE: "张", SECOND: "秒", MINUTE: "分钟", REQUEST: "次", CHARACTER: "字符" };
export function priceUnitText(price: { unit_code: string; unit_quantity: string }) {
  if (new Decimal(price.unit_quantity || "1").eq(1000000) && ["TOKEN", "TOKEN_HOUR"].includes(price.unit_code)) return `百万 ${unitNames[price.unit_code]}`;
  return `${new Decimal(price.unit_quantity || "1").eq(1) ? "" : `${price.unit_quantity} `}${unitNames[price.unit_code] || unitLabels[price.unit_code] || price.unit_code}`;
}
export function parsePriceUnit(text: string) {
  const value = text.trim();
  const million = /^百万\s*(tokens?|Token|TOKEN)(·小时)?$/i.exec(value);
  if (million) return { unit_code: million[2] ? "TOKEN_HOUR" : "TOKEN", unit_quantity: "1000000" };
  const match = /^(?:(\d+(?:\.\d{1,8})?)\s*)?(.+)$/.exec(value);
  if (!match) throw new Error("请选择有效计费单位，例如百万 tokens、张或秒。");
  const quantity = match[1] || "1", unit = match[2]!.trim();
  const code = Object.keys(unitNames).find(key => unitNames[key] === unit || unitLabels[key] === unit || key === unit) || (/^tokens?$/i.test(unit) ? "TOKEN" : undefined);
  if (!code || !new Decimal(quantity).gt(0)) throw new Error("请选择有效计费单位及大于零的单位数量。");
  return { unit_code: code, unit_quantity: quantity };
}
