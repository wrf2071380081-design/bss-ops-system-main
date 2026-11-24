<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ElMessageBox } from "element-plus";
import { agentCatalog, catalogVendors, type Difference, type ReviewDetail, type ReviewRow, type Source } from "../../../api/agentCatalog";
import { modelParameterLabel } from "../../../api/standardModelPresentation";
import { dimensionLabels, modelTypeLabels } from "../../../api/standardModels.contract";
import { catalogDifferenceLabel, catalogValue, catalogReviewRows, expandCatalogDifferences, isTechnicalCatalogField } from "../../../api/catalogReviewPresentation";

const props = defineProps<{ sources: Source[] }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const groups: Record<string, string> = { name: "模型名称", model_type: "模型类型", input_modalities: "输入模态",
  output_modalities: "输出模态", capacity: "容量规格", abilities: "核心能力", price_items: "官方价格 / 计费单位",
  api_access: "API / 限流 / 访问条件", lifecycle: "版本 / 生命周期", sources: "官方来源" };
const changes: Record<string, string> = { NEW: "新增", CHANGED: "有变化", UNCHANGED: "无变化",
  NOT_OBSERVED: "本批未观察到", NO_BASELINE: "无历史基准" };
const index = ref(0), page = ref(1), rows = ref<ReviewRow[]>([]), modelId = ref("");
const range = ref('pending'), search = ref(''), choosing = ref(false);
const filtered = computed(() => rows.value.filter(row => (range.value === 'all' || (range.value === 'pending' ? row.pending : ['NEW','CHANGED'].includes(row.change ?? ''))) &&
  (!search.value.trim() || (row.name + ' ' + row.id).toLowerCase().includes(search.value.trim().toLowerCase()))));
const total = computed(() => filtered.value.length);
const visibleRows = computed(() => filtered.value.slice((page.value - 1) * 20, page.value * 20));
const pendingCount = computed(() => rows.value.filter(row => row.pending).length);
watch([range, search], () => { page.value = 1; }, {flush:'sync'});
watch(visibleRows, list => { if (!list.some(row => row.id === modelId.value)) modelId.value = list[0]?.id ?? ''; });
const requestedModel = ref('');
const detail = ref<ReviewDetail>(), busy = ref(false), loading = ref(false);
const error = ref(""), detailError = ref(""), notice = ref(""), revision = ref(0);
type Supplement = { name: string; model_type: string; input_modalities: string[]; output_modalities: string[] };
const supplements = ref<Record<string, Supplement>>({});
const picks = ref<Record<string, { fields: string[]; name: string; index: number; page: number; id: string }>>({});
const selectionKey = computed(() => JSON.stringify([props.sources[index.value]?.vendor, props.sources[index.value]?.runId, modelId.value]));
const selected = computed({ get: () => picks.value[selectionKey.value]?.fields ?? [], set: (fields: string[]) => {
  if (!fields.length) { delete picks.value[selectionKey.value]; return; }
  picks.value[selectionKey.value] = { fields, name: model.value?.name || modelId.value, index: index.value, page: page.value, id: modelId.value };
} });
const supplement = computed({ get: () => supplements.value[selectionKey.value] ?? { name: '', model_type: '', input_modalities: [], output_modalities: [] },
  set: (value: Supplement) => { supplements.value[selectionKey.value] = value; } });
const selectedModels = computed(() => Object.values(picks.value).filter(p => p.fields.length));
const totalSelected = computed(() => selectedModels.value.reduce((n, p) => n + p.fields.length, 0));
function countSelected(id: string) { return picks.value[JSON.stringify([source.value?.vendor, source.value?.runId, id])]?.fields.length ?? 0; }
async function requestClose(done?: () => void) {
  if (busy.value || choosing.value) return;
  if (totalSelected.value) {
    try { await ElMessageBox.confirm('尚有 ' + selectedModels.value.length + ' 个模型的字段未保存或提交。关闭后将丢弃本次勾选。', '保留审核进度', { confirmButtonText: '放弃并关闭', cancelButtonText: '继续核对', type: 'warning' }); }
    catch { return; }
  }
  if (done) done(); else emit('close');
}
function revisit(pick: { index: number; page: number; id: string }) {
  range.value = 'all'; search.value = '';
  requestedModel.value = pick.id;
  if (index.value === pick.index) { page.value = Math.floor(rows.value.findIndex(row => row.id === pick.id) / 20) + 1; requestedModel.value = ''; }
  index.value = pick.index; modelId.value = pick.id;
}
const source = computed(() => props.sources[index.value]);
const model = computed(() => detail.value?.latestAttempt?.result?.models[0]);
const live = computed(() => detail.value?.liveBss);
const previous = computed(() => model.value?.reviewDiff);
const historicalFields = computed(() => expandCatalogDifferences(previous.value?.previous_batch?.fields ?? []));
const technicalFields = computed(() => expandCatalogDifferences(previous.value?.previous_batch?.fields ?? [], true).filter(row => isTechnicalCatalogField(row.field)));
const historyStatus = computed(() => {
  const comparison = previous.value?.previous_batch;
  return comparison?.status === 'CHANGED' && comparison.fields && !historicalFields.value.length ? 'UNCHANGED' : comparison?.status;
});
const editable = computed(() => detail.value?.canReview === true && detail.value.pendingModelIds.includes(modelId.value) && !!live.value?.official &&
  (!live.value.id || !!live.value.revision));
const fieldGroups = computed(() => Object.entries(groups).map(([key, title]) => ({
  title, rows: catalogReviewRows(live.value?.current, live.value?.official, live.value?.fields)
    .filter(row => row.field.split(".")[0] === key),
})).filter(group => group.rows.length));
function evidence(field: string) {
  const [, key = field] = field.split(".");
  const aliases: Record<string, string> = { max_input: "maxInput", max_output: "maxOutput", context_length: "contextLength",
    price_items: "prices", input_modalities: "inputModalities", output_modalities: "outputModalities" };
  const entries = { ...model.value?.specEvidence, ...model.value?.fieldEvidence };
  return entries[aliases[key] ?? key] ?? entries[aliases[field.split(".")[0]] ?? field.split(".")[0]];
}
function canSelect(row: Difference) {
  return row.kind === "CHANGED" && row.new != null && evidence(row.field)?.state !== "CONFLICT";
}
const hasUpdates = computed(() => !live.value?.id || (live.value.fields ?? []).some(canSelect));
const selectableFields = computed(() => editable.value ? fieldGroups.value.flatMap(g => g.rows.filter(canSelect).map(r => r.field)) : []);
function selectAll() { selected.value = [...selectableFields.value]; }
async function selectModel(row: ReviewRow, checked: boolean) {
  const target = source.value;
  if (!target || choosing.value || busy.value) return;
  const key = JSON.stringify([target.vendor, target.runId, row.id]);
  if (!checked) { delete picks.value[key]; return; }
  if (selectedModels.value.length >= 50) { notice.value = '每批最多勾选 50 个型号。'; return; }
  choosing.value = true;
  try {
    const result = await agentCatalog.detail(target, row.id, new AbortController().signal);
    const candidate = result.latestAttempt?.result?.models[0], record = result.liveBss;
    if (result.latestAttempt?.id !== target.runId || candidate?.id !== row.id || !result.canReview || !result.pendingModelIds.includes(row.id) || !record?.official || record.id && !record.revision) throw Error('该型号暂时不能审核，请刷新结果。');
    const aliases: Record<string,string> = {context_length:'contextLength',max_input:'maxInput',max_output:'maxOutput',price_items:'prices',input_modalities:'inputModalities',output_modalities:'outputModalities'};
    const entries = {...candidate.specEvidence,...candidate.fieldEvidence};
    const fields = catalogReviewRows(record.current,record.official,record.fields).filter(item => {
      const [group,part=group]=item.field.split('.');
      return group in groups && item.kind === 'CHANGED' && item.new != null && (entries[aliases[part]??part]??entries[aliases[group]??group])?.state !== 'CONFLICT';
    }).map(item=>item.field);
    if (!fields.length) { notice.value = '该型号与 BSS 档案一致，没有可更新字段。'; return; }
    picks.value[key] = {fields,name:row.name || row.id,index:index.value,page:page.value,id:row.id};
  } catch(cause) { detailError.value = cause instanceof Error ? cause.message : '无法勾选该型号，请重试。'; }
  finally { choosing.value = false; }
}
function officialLink(field: string, value: unknown) {
  const row = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const url = evidence(field)?.sourceUrl ?? row.source_url ?? row.url;
  return typeof url === "string" && /^https:\/\//i.test(url) ? url : undefined;
}
function display(value: unknown): string {
  if (value == null || value === "") return "—";
  if (Array.isArray(value)) return value.map(item => modelTypeLabels[item as keyof typeof modelTypeLabels] ?? catalogValue(item)).join('、') || '—';
  if (typeof value === "object") {
    const item = value as Record<string, unknown>;
    if ("value" in item) return catalogValue(item);
    const states: Record<string, string> = { SUPPORTED: '支持', UNSUPPORTED: '不支持', UNDISCLOSED: '未披露', UNVERIFIED: '未核实' };
    return [item.value, item.label, states[String(item.state)] ?? item.state, item.description, item.amount, item.currency,
      item.unit_quantity && item.unit_code ? String(item.unit_quantity) + " " + String(item.unit_code) : item.unit_code,
      item.condition, item.dimensions && Object.entries(item.dimensions).map(([k, v]) => (dimensionLabels[k] || k) + "：" + v).join(" · "),
      item.url].filter(Boolean).join(" · ") || catalogValue(value);
  }
  return modelTypeLabels[String(value) as keyof typeof modelTypeLabels] ?? String(value);
}
function label(field: string) {
  const [group, key] = field.split(".");
  const value=live.value?.official?.[field] ?? live.value?.current?.[field];
  const title=value && typeof value === "object" ? (value as Record<string,unknown>).label : undefined;
  return typeof title === "string" && title ? title : key && group !== "sources" ? modelParameterLabel(key) : groups[group] ?? field;
}
watch([source, revision], async ([target], _, cleanup) => {
  const controller = new AbortController();
  cleanup(() => controller.abort());
  rows.value = []; page.value = 1; modelId.value = ""; error.value = "";
  if (!target) return;
  loading.value = true;
  try {
    const result = await agentCatalog.index(target, 1, controller.signal);
    if (controller.signal.aborted) return;
    const items = [...result.rows], pages = Math.ceil((result.pagination?.total ?? items.length) / (result.pagination?.pageSize ?? 20));
    if (pages > 100) throw Error('型号数量超过本次读取范围，请缩小厂商范围。');
    for (let number=2; number<=pages; number++) {
      const next = await agentCatalog.index(target, number, controller.signal);
      if (controller.signal.aborted) return;
      items.push(...next.rows);
    }
    rows.value = Array.from(new Map(items.map(row=>[row.id,row])).values());
    if (requestedModel.value) page.value = Math.max(1, Math.floor(filtered.value.findIndex(row=>row.id===requestedModel.value) / 20) + 1);
    modelId.value = visibleRows.value.some(row => row.id === requestedModel.value) ? requestedModel.value : visibleRows.value[0]?.id ?? "";
    requestedModel.value = '';
  } catch (cause) {
    if (!controller.signal.aborted) error.value = cause instanceof Error ? cause.message : "无法读取采集结果。";
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}, { immediate: true });
watch([source, modelId], async ([target, id], _, cleanup) => {
  const controller = new AbortController();
  cleanup(() => controller.abort());
  detail.value = undefined; detailError.value = "";
  if (!target || !id) return;
  try {
    const result = await agentCatalog.detail(target, id, controller.signal);
    if (controller.signal.aborted) return;
    if (result.latestAttempt?.id !== target.runId || result.latestAttempt.result?.models[0]?.id !== id) {
      throw Error("该型号在本批未观察到，或候选已更新。");
    }
    detail.value = result;
    selected.value = selected.value.filter(field => selectableFields.value.includes(field));
    const comparison = result.latestAttempt.result.models[0].reviewDiff?.previous_batch;
    if (comparison?.status === 'CHANGED' && comparison.fields && !expandCatalogDifferences(comparison.fields).length) {
      rows.value = rows.value.map(row => row.id === id ? { ...row, change: 'UNCHANGED' } : row);
    }
    const saved = result.latestAttempt.result.models[0].draft?.overrides;
    const original = result.liveBss?.official;
    const modalities = (key: string) => {
      const value = saved?.[key] ?? original?.[key];
      return Array.isArray(value) ? value.map(String) : [];
    };
    if (!supplements.value[selectionKey.value]) supplement.value = { name: String(saved?.name ?? original?.name ?? ""), model_type: String(saved?.model_type ?? original?.model_type ?? ""),
      input_modalities: modalities("input_modalities"), output_modalities: modalities("output_modalities") };
  } catch (cause) {
    if (!controller.signal.aborted) detailError.value = cause instanceof Error ? cause.message : "无法核对该型号。";
  }
});
async function save(action: "approve" | "draft") {
  const target = source.value, record = live.value, item = model.value;
  if (busy.value || !editable.value || !target || !record || !item || !record.official) return;
  notice.value = detailError.value = "";
  const values = { ...record.current };
  for (const key of selected.value) values[key] = record.official[key];
  const checkedAt = record.checkedAt || item.bss.checked_at;
  if (checkedAt) values.checked_at = checkedAt;
  if (!record.id) Object.assign(values, supplement.value);
  const hasModalities = (value: unknown) => Array.isArray(value) && value.length > 0;
  if (action === "approve" && (!values.name || !values.model_type || !hasModalities(values.input_modalities) || !hasModalities(values.output_modalities))) {
    detailError.value = "请先补齐模型名称、类型、输入和输出模态，或保存待审草稿。";
    return;
  }
  const data: Record<string, unknown> = { company: item.bss.company, official_id: item.id, capacity: [], abilities: [],
    price_items: [], api_access: [], lifecycle: [], sources: [] };
  for (const [key, value] of Object.entries(values)) {
    if (value == null) continue;
    const split = key.indexOf(".");
    if (split === -1) data[key] = value;
    else {
      const group = key.slice(0, split);
      const items = (data[group] ??= []) as unknown[];
      items.push(value);
    }
  }
  busy.value = true;
  try {
    const receipt = await agentCatalog.review(target, item.id, action, data,
      action === "approve" ? { id: record.id ?? null, revision: record.revision ?? null } : undefined);
    if (action === "approve") {
      if (!receipt.bssSync?.some(row => row.model_id === item.id && row.status === "WRITTEN")) throw Error("尚未取得 BSS 写入回执，请重新核对。");
      selected.value = [];
      notice.value = "BSS 已确认写入。";
      emit("saved");
      revision.value++;
    } else { selected.value = []; notice.value = "待审草稿已保存。"; }
  } catch (cause) {
    detailError.value = cause instanceof Error ? cause.message : "审核未完成，请重新核对。";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <el-dialog :model-value="true" title="官方模型采集 · 变更审核" width="min(1080px, 96vw)"
    :show-close="!busy" :close-on-click-modal="false" :close-on-press-escape="!busy" :before-close="requestClose" @close="emit('close')">
    <div class="review-controls"><el-select v-model="index" aria-label="厂商" :disabled="busy || choosing" @change="page = 1">
      <el-option v-for="(item, i) in sources" :key="item.runId" :label="catalogVendors[item.vendor]" :value="i" />
    </el-select><select v-model="range" aria-label="型号范围" :disabled="busy || choosing"><option value="changed">较上次新增与变更</option><option value="pending">全部待审</option><option value="all">全部型号</option></select>
      <el-input v-model="search" type="search" aria-label="搜索型号" placeholder="搜索型号名称 / 官方 ID" clearable :disabled="busy || choosing" />
      <el-button :disabled="busy || choosing" @click="revision++">刷新结果</el-button></div>
    <p class="range-summary">本批 {{ rows.length }} 个型号 · 待审 {{ pendingCount }} 个 · 当前显示 {{ total }} 个</p>
    <el-alert v-if="error || notice" :title="error || notice" :type="error ? 'error' : 'success'" :closable="false" show-icon />
    <div v-loading="loading" class="review-body">
      <aside aria-label="本批型号">
        <div v-for="row in visibleRows" :key="row.id" class="model-option" :class="{'active-model':modelId === row.id}">
        <input v-if="row.pending" type="checkbox" :aria-label="'勾选模型 ' + (row.name || row.id)" :checked="countSelected(row.id) > 0" :disabled="busy || choosing" @change="selectModel(row, ($event.target as HTMLInputElement).checked)" />
        <button type="button" :aria-pressed="modelId === row.id" :disabled="busy || choosing" @click="modelId = row.id">
          <strong>{{ row.name || row.id }}</strong><small>{{ row.pending ? '待审 · ' : '' }}{{ changes[row.change ?? ''] || '' }}</small>
          <span v-if="countSelected(row.id)" class="picked-badge">已选 {{ countSelected(row.id) }} 个字段 · 待提交</span>
        </button></div>
        <div v-if="!loading && !visibleRows.length && !error" class="empty-review"><p>{{ search.trim() ? '未找到匹配型号，请调整搜索条件。' : range === 'changed' ? '本次没有较上次新增或变更的型号。' : '当前范围暂无型号。' }}</p>
          <p v-if="range === 'changed' && rows.length">采集无变化不代表已审核，可切换到全部待审。</p>
          <el-button v-if="search.trim()" size="small" @click="search = ''">清空搜索</el-button>
          <el-button v-if="range !== 'pending' && pendingCount" size="small" @click="range = 'pending'; search = ''">查看全部待审（{{ pendingCount }}）</el-button></div>
        <el-pagination v-if="total > 20" v-model:current-page="page" :disabled="busy" :total="total" :page-size="20" layout="prev, next" />
      </aside>
      <main>
        <div v-if="model" class="model-heading"><h3>{{ model.name || model.id }}</h3>
          <div v-if="editable && selectableFields.length" class="field-actions">
            <span>本模型已选 {{ selected.length }} / {{ selectableFields.length }} 项</span>
            <el-button size="small" :disabled="busy || selected.length === selectableFields.length" @click="selectAll">全选当前模型字段</el-button>
            <el-button size="small" :disabled="busy || !selected.length" @click="selected = []">清空选择</el-button>
          </div>
        </div>
        <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" />
        <p v-if="live?.status === 'UNAVAILABLE'">BSS 当前值暂时无法核对。</p>
        <p v-else-if="model && live && !hasUpdates" class="review-message">本次采集的模型资料与 BSS 当前档案一致，暂无需要更新的字段。</p>
        <p v-if="model && live && live.status !== 'UNAVAILABLE'" class="comparison-help">{{ live.id ? '核对 BSS 当前档案与本次采集资料，勾选需要更新的字段。' : '本次首次建档，核对采集资料并补齐必填信息。' }}</p>
        <fieldset :disabled="busy || choosing">
          <section v-for="group in fieldGroups" :key="group.title" class="field-group">
            <h4>{{ group.title }}</h4>
            <table><thead><tr><th>采纳</th><th>字段</th><th>BSS 当前值</th><th>本次官网候选</th></tr></thead><tbody>
              <tr v-for="row in group.rows" :key="row.field" :class="{ 'changed-field': row.kind === 'CHANGED' }"><td><input v-if="editable && canSelect(row)" v-model="selected" type="checkbox" :value="row.field" :aria-label="'采纳' + label(row.field)" /><span v-else class="field-state">{{ row.kind === 'UNCHANGED' ? '一致' : row.kind === 'NO_CURRENT_EVIDENCE' ? '待补齐' : '待核对' }}</span></td>
                <td>{{ label(row.field) }}</td><td>{{ display(row.old) }}</td>
                <td>{{ row.kind === 'NO_CURRENT_EVIDENCE' ? '本批暂无证据' : display(row.new) }}
                  <p v-if="evidence(row.field)?.state === 'CONFLICT'">来源冲突 · 待核对</p>
                  <small v-if="evidence(row.field)?.quote">{{ evidence(row.field)?.quote }}</small>
                  <a v-if="officialLink(row.field, row.new)" :href="officialLink(row.field, row.new)" target="_blank" rel="noopener noreferrer">官方来源 ↗</a></td></tr>
            </tbody></table>
          </section>
          <section v-if="editable && !live?.id" class="field-group">
            <h4>新型号建档 · 核对或人工补录</h4>
            <el-form label-position="top"><el-form-item label="模型名称"><el-input v-model="supplement.name" /></el-form-item>
              <el-form-item label="模型类型"><el-select v-model="supplement.model_type"><el-option v-for="(title, key) in modelTypeLabels" :key="key" :label="title" :value="key" /></el-select></el-form-item>
              <el-form-item label="输入模态"><el-select v-model="supplement.input_modalities" multiple><el-option v-for="(title, key) in modelTypeLabels" :key="key" :label="title" :value="key" /></el-select></el-form-item>
              <el-form-item label="输出模态"><el-select v-model="supplement.output_modalities" multiple><el-option v-for="(title, key) in modelTypeLabels" :key="key" :label="title" :value="key" /></el-select></el-form-item></el-form>
          </section>
        </fieldset>
        <section v-if="previous?.baseline" class="field-group historical-diff">
          <h4>{{ previous.baseline.kind === 'PERSONAL' ? '与本人上次采集比较' : '与平台上次采集比较' }} · {{ historyStatus === 'UNCHANGED' ? '无业务变化' : changes[historyStatus ?? ''] || '待核对' }}</h4>
          <table v-if="historicalFields.length"><thead><tr><th>变化字段</th><th>上一批值</th><th>本次值</th></tr></thead><tbody>
            <tr v-for="row in historicalFields" :key="row.field"><td>{{ catalogDifferenceLabel(row.field) }}</td><td>{{ catalogValue(row.old) }}</td><td>{{ catalogValue(row.new) }}</td></tr>
          </tbody></table>
          <p v-else class="comparison-empty">{{ historyStatus === 'NEW' ? '上次采集没有该型号，本次首次发现。' : historyStatus === 'NOT_OBSERVED' ? '本批未观察到该型号。' : '与上次采集相比，模型规格、能力和价格均无变化。' }}</p>
        </section>
        <details v-if="technicalFields.length" class="technical-details"><summary>采集技术记录</summary>
          <p>本次资料校验信息有更新。</p>
          <dl v-for="row in technicalFields" :key="row.field"><dt>{{ catalogDifferenceLabel(row.field) }}</dt><dd>上次 {{ catalogValue(row.old) }}</dd><dd>本次 {{ catalogValue(row.new) }}</dd></dl>
        </details>
      </main>
    </div>
    <template #footer>
      <div class="review-footer"><div class="selection-overview">
        <el-popover v-if="totalSelected" placement="top-start" :width="340" trigger="click">
          <template #reference><el-button class="selection-summary">待提交 {{ selectedModels.length }} 个模型 · {{ totalSelected }} 个字段</el-button></template>
          <div class="selection-list"><strong>已选字段清单</strong><button v-for="pick in selectedModels" :key="pick.index + ':' + pick.id" :disabled="busy" @click="revisit(pick)">{{ pick.name }}<span>{{ pick.fields.length }} 项 · 返回核对</span></button></div>
        </el-popover>
        <span v-else class="selection-count">尚未选择字段</span>
        <small>保存与批准仅作用于当前模型</small>
      </div><div class="review-submit">
      <el-button v-if="editable && hasUpdates" :disabled="busy || choosing" @click="save('draft')">保存草稿</el-button>
      <el-button v-if="editable && hasUpdates" type="primary" :disabled="choosing || !!live?.id && !selected.length" :loading="busy" @click="save('approve')">批准当前模型{{ selected.length ? '（' + selected.length + ' 项）' : '' }}</el-button>
      </div></div>
    </template>
  </el-dialog>
</template>

<style scoped>
.range-summary{font-size:12px;color:#7b858b;margin:0 0 12px}.review-controls>select{font:inherit;min-width:180px;border:1px solid #dcdfe6;border-radius:5px;padding:8px 12px;color:#606266;background:#fff}.review-controls>.el-input{flex:1;min-width:160px}.model-option{display:flex;align-items:flex-start;gap:8px;border:1px solid #e3e9eb;border-radius:6px;margin-bottom:6px;padding:0 8px}.model-option.active-model{background:#edf3f3;border-color:#849fa5}.model-option>input{margin-top:14px;flex:none}.review-body aside .model-option button{margin:0;border:0;background:transparent;flex:1;min-width:0;padding:9px 4px}.review-body aside .model-option button[aria-pressed=true]{background:transparent}.empty-review{font-size:13px;line-height:1.7;color:#66767c}.review-controls{flex-wrap:wrap}
.model-heading{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px}.model-heading h3{margin:0}.field-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap;font-size:12px;color:#66767c}.picked-badge{display:inline-block;margin-top:8px;padding:3px 7px;border-radius:4px;background:#e8f1ee;color:#306353;font-size:12px}.review-footer{display:flex;gap:12px;align-items:center;justify-content:space-between;text-align:left}.selection-overview small{display:block;margin-top:5px;color:#7b858b;font-size:12px}.review-submit{display:flex;gap:8px}.selection-list{max-height:280px;overflow:auto}.selection-list button{display:block;text-align:left;width:100%;padding:10px 6px;background:white;border:0;border-bottom:1px solid #e9edef;cursor:pointer;overflow-wrap:anywhere}.selection-list button span{display:block;font-size:12px;color:#617b74;margin-top:4px}@media(max-width:680px){.review-footer{align-items:stretch;flex-direction:column}.review-submit{justify-content:flex-end}}
.comparison-help{font-size:13px;color:#778087;margin:12px 0}.field-state{font-size:12px;color:#7b858b;white-space:nowrap}.field-group table{table-layout:fixed}.field-group th:first-child{width:64px}.field-group th:nth-child(2){width:22%}.changed-field{background:#fafcfc}
.historical-diff th:first-child{width:28%}.historical-diff th:nth-child(2){width:36%}
.review-message{padding:12px 16px;background:#f5f8f8;border-radius:6px;color:#405a60;line-height:1.7}.technical-details{margin-top:18px;color:#778087;font-size:12px}.technical-details dd{margin:6px 0;overflow-wrap:anywhere}.technical-details dl{padding:10px;background:#f8fafb}
.review-body main{display:block;min-width:0;width:auto;margin:0;padding:0}.historical-diff td{white-space:pre-wrap}.comparison-empty{padding:12px;margin:0}
.review-controls{display:flex;gap:10px;margin-bottom:14px}.review-controls .el-select{width:180px}.review-body{display:grid;grid-template-columns:minmax(145px,24%) minmax(0,1fr);min-height:180px;max-height:64vh;overflow:auto;gap:18px;margin-top:12px}.review-body aside button{width:100%;border:1px solid #e3e9eb;background:white;padding:9px 12px;text-align:left;border-radius:6px;margin-bottom:5px;cursor:pointer;overflow-wrap:anywhere}.review-body aside button[aria-pressed=true]{background:#edf3f3;border-color:#849fa5}.review-body small,.review-body aside strong{display:block}.review-body fieldset{border:0;padding:0;margin:0;min-width:0}.field-group{margin:12px 0;border:1px solid #e3e9eb;border-radius:6px;overflow:hidden}.field-group h4{margin:0;background:#edf3f3;padding:10px 12px;color:#405a60}.field-group table{border-collapse:collapse;width:100%;text-align:left}.field-group td,.field-group th{padding:8px;vertical-align:top;border-bottom:1px solid #e9edef;overflow-wrap:anywhere}.field-group .el-form{padding:12px}.selection-count{margin-right:12px}summary{cursor:pointer}@media(max-width:680px){.review-body{grid-template-columns:1fr}.review-body aside{max-height:160px;overflow:auto}}
</style>
