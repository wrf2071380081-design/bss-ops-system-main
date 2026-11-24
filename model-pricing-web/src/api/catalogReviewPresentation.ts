import { modelParameterLabel } from "./standardModelPresentation.ts";

export interface CatalogDifference { field: string; old: unknown; new: unknown; kind?: string }

/** Keep a complete field comparison even when a model has no pending changes. */
export function catalogReviewRows(current: Record<string, unknown> = {}, official: Record<string, unknown> = {},
  differences: CatalogDifference[] = []): CatalogDifference[] {
  const changed = new Map(differences.map(row => [row.field, row]));
  return [...new Set([...Object.keys(current), ...Object.keys(official), ...changed.keys()])].map(field =>
    changed.get(field) ?? { field, old: current[field] ?? null, new: official[field] ?? null,
      kind: official[field] == null ? 'NO_CURRENT_EVIDENCE' : 'UNCHANGED' });
}
const labels: Record<string, string> = {
  name: "模型名称", details: "访问与规格详情", limits: "限制条件", tokenSemantics: "Token 预算定义",
  contextLength: "上下文", maxInput: "最大输入", maxOutput: "最大输出", version: "版本",
  inputModalities: "输入模态", outputModalities: "输出模态", methods: "调用方法", abilities: "核心能力",
  prices: "价格", priceItems: "条件价格", capability: "能力", retirement: "退役信息", aliases: "别名",
  sourceUrl: "来源地址", detailUrl: "详情地址", concurrencyLimit: "并发上限", concurrency_limit: "并发上限",
  openaiBaseUrl: "OpenAI 兼容地址", anthropicBaseUrl: "Anthropic 兼容地址",
  metadataSha256: "资料校验摘要", metadataFallbackSha256: "备用资料校验摘要", metadataSourceUrl: "资料来源",
};
export function catalogDifferenceLabel(field: string): string {
  return field.split(".").map(key => labels[key] ?? modelParameterLabel(key)).join(" / ");
}

export function catalogValue(value: unknown): string {
  if (value == null || value === "") return "—";
  if (typeof value === "boolean") return value ? "支持" : "不支持";
  if (Array.isArray(value)) return value.length ? value.map(catalogValue).join("、") : "—";
  if (typeof value === 'object' && 'value' in value) {
    const row = value as Record<string, unknown>;
    const number = typeof row.value === 'string' && /^\d+$/.test(row.value) ? Number(row.value).toLocaleString('en-US') : catalogValue(row.value);
    const semantics: Record<string,string> = {SHARED_WINDOW:'输入输出共享窗口',SEPARATE_INPUT_OUTPUT:'输入输出分别计限',INPUT_ONLY:'仅公布输入上限'};
    const quantity=[row.disclosure==='UNVERIFIED'?'待核实':number,row.unit==='TOKEN'?'Token':row.unit].filter(Boolean).join(' ');
    const scopes:Record<string,string>={API:'当前 API',WEIGHTS:'开放权重',NATIVE:'原生窗口',EXTENDED:'扩展窗口'};
    return [quantity,semantics[String(row.semantics)],scopes[String(row.scope)]??row.scope,row.condition].filter(v=>v!=null && v!=='').join(' · ');
  }
  if (typeof value === "object") return Object.entries(value).map(([key, item]) =>
    `${labels[key] ?? modelParameterLabel(key)}：${catalogValue(item)}`).join("；") || "—";
  return String(value);
}
/** Expand nested snapshots so reviewers can see the actual changed values. */
export function isTechnicalCatalogField(field: string): boolean {
  return field.split(".").some(key => ["metadataSha256", "metadataFallbackSha256", "metadataSourceUrl",
    "sha256", "evidenceId", "sourceUrl", "source_url", "detailUrl", "quote", "attempts", "fetchedAt", "observedAt"].includes(key));
}

/** Group historical assertions by parameter; never align them by array position. */
function limitHistory(value: unknown): Record<string, unknown[]> {
  const grouped: Record<string, unknown[]> = {};
  if (!Array.isArray(value)) return grouped;
  for (const item of value) {
    if (!item || typeof item !== 'object' || typeof item.field !== 'string') continue;
    const row: Record<string, unknown> = {};
    for (const key of Object.keys(item).sort()) {
      if (!isTechnicalCatalogField(key) && !['field','rawLabel','rawValue'].includes(key)) row[key] = item[key];
    }
    row.value = item.value == null ? item.rawValue : String(item.value);
    row.semantics ??= 'UNSPECIFIED';
    (grouped[item.field] ??= []).push(Object.fromEntries(Object.entries(row).sort(([a],[b])=>a.localeCompare(b))));
  }
  for (const key of Object.keys(grouped)) grouped[key] = [...new Set(grouped[key]!.map(row=>JSON.stringify(row)))].sort().map(row=>JSON.parse(row));
  return grouped;
}
export function expandCatalogDifferences(rows: CatalogDifference[], includeTechnical = false): CatalogDifference[] {
  const result: CatalogDifference[] = [];
  const object = (value: unknown): value is Record<string, unknown> =>
    value != null && typeof value === "object" && !Array.isArray(value);
  function visit(row: CatalogDifference) {
    if (!includeTechnical && isTechnicalCatalogField(row.field)) return;
    if (row.field === 'limits' && !includeTechnical && (Array.isArray(row.old) || Array.isArray(row.new))) {
      const before = limitHistory(row.old), after = limitHistory(row.new);
      for (const key of new Set([...Object.keys(before),...Object.keys(after)])) {
        if (JSON.stringify(before[key] ?? []) !== JSON.stringify(after[key] ?? [])) {
          result.push({...row,field:`limits.${key}`,old:before[key] ?? null,new:after[key] ?? null});
        }
      }
      return;
    }
    if (object(row.old) || object(row.new)) {
      const before = object(row.old) ? row.old : {};
      const after = object(row.new) ? row.new : {};
      // Preserve a scalar/object type change as a whole value.
      if ((row.old != null && !object(row.old)) || (row.new != null && !object(row.new))) {
        result.push(row);
        return;
      }
      for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) {
        visit({ ...row, field: `${row.field}.${key}`, old: before[key] ?? null, new: after[key] ?? null });
      }
    } else if (JSON.stringify(row.old) !== JSON.stringify(row.new)) result.push(row);
  }
  rows.forEach(visit);
  return result;
}
