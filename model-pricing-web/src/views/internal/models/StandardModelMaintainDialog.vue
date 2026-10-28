<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { ApiError } from "../../../domain/common";
import { standardModelsApi } from "../../../api/standardModels";
import { abilityLabels, copyStandardModel, emptyStandardModel, formatStandardPrice, factLabels, modelTypeLabels, shanghaiToday, sourceLabels, standardFieldLabel, validateStandardModel } from "../../../api/standardModels.contract";
import type { StandardModelDTO, StandardModelPage } from "../../../api/standardModels.types";
import { applyModelNotes, modelNotes } from "../../../api/standardModelEditor";
import StandardPriceEditor from "./StandardPriceEditor.vue";
import { formatDateTime } from "../../../domain/date";

const props = defineProps<{ id?: string; initialPanel?: 'profile' | 'history' | 'records' | 'advanced' }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const current = ref<StandardModelDTO>();
const draft = ref(emptyStandardModel());
const notes = ref(modelNotes(draft.value));
const initialNotes = ref(JSON.stringify(notes.value));
const sourceURL = ref("");
const priceEditor = ref<InstanceType<typeof StandardPriceEditor>>();
const baseline = ref(JSON.stringify(draft.value));
const reason = ref("");
const loading = ref(false), saving = ref(false), error = ref(""), conflict = ref(false), tab = ref<string>(props.initialPanel || "profile");
const versions = ref<StandardModelPage>(), selectedHistory = ref<StandardModelDTO>();
const historyLoading = ref(false), historyError = ref("");
const records = ref<Awaited<ReturnType<typeof standardModelsApi.records>>>([]), recordsLoading = ref(false), recordsError = ref("");
const readonly = computed(() => current.value ? !current.value.permissions.can_edit : !!props.id);
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value || JSON.stringify(notes.value) !== initialNotes.value || sourceURL.value !== (draft.value.sources[0]?.url || "") || !!priceEditor.value?.dirty || !!reason.value.trim());
function formData() {
  const result = applyModelNotes(draft.value, notes.value);
  result.price_items = priceEditor.value?.getRows() || result.price_items;
  const primary = draft.value.sources[0];
  result.sources = [...(sourceURL.value.trim() ? [{ ...(primary || { label: "官方资料", source_kind: "OFFICIAL_DOCS" }), url: sourceURL.value.trim() }] : []), ...draft.value.sources.slice(1).map(source => ({ ...source }))];
  return result;
}
const issues = computed(() => { try { return validateStandardModel(formData()); } catch (value) { return [{ path: "", message: value instanceof Error ? value.message : "请核对资料内容。" }]; } });
const companies = computed(() => [...new Set([draft.value.company, "OpenAI", "Google", "DeepSeek", "阿里云", "Anthropic", "MiniMax", "智谱"].filter(Boolean))]);
const operationLabels: Record<string, string> = { CREATE: "创建", UPDATE: "更新", UPDATE_NOOP: "保存（无变化）", VERIFY: "人工核验", VERIFY_NOOP: "核验（无变化）", MANUAL_SAVE: "保存并留痕", MANUAL_SAVE_NOOP: "保存并留痕（无变化）" };
const verificationLabels: Record<string, string> = { INCOMPLETE: "资料待补齐", UNVERIFIED: "待人工核验", VERIFIED: "已人工核验" };
const blocked = computed(() => saving.value || loading.value || readonly.value || conflict.value);
let active = true;
let detailRequest: AbortController | undefined, historyRequest: AbortController | undefined, recordsRequest: AbortController | undefined;
let intent = crypto.randomUUID();
const message = (value: unknown) => value instanceof Error ? value.message : "请求未完成，请重试。";
function operationLabel(action: string) { return operationLabels[action] ?? action; }
function addSource() {
  if (!draft.value.sources.length) draft.value.sources.push({ label: "官方资料", url: sourceURL.value, source_kind: "OFFICIAL_DOCS" });
  draft.value.sources.push({ label: "", url: "", source_kind: "OFFICIAL_DOCS" });
}
function apply(model: StandardModelDTO) {
  current.value = model; draft.value = copyStandardModel(model); notes.value = modelNotes(draft.value); initialNotes.value = JSON.stringify(notes.value);
  sourceURL.value = draft.value.sources[0]?.url || ""; baseline.value = JSON.stringify(draft.value); reason.value = ""; intent = crypto.randomUUID();
}
async function load() {
  const id = current.value?.id ?? props.id;
  if (!id) return;
  detailRequest?.abort(); const request = detailRequest = new AbortController();
  loading.value = true; error.value = "";
  try { const result = await standardModelsApi.get(id, request.signal); if (active && !request.signal.aborted) { apply(result); conflict.value = false; } }
  catch (value) { if (active && !request.signal.aborted) error.value = message(value); }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
async function canLeave() {
  if (saving.value) return false;
  if (!dirty.value) return true;
  try { await ElMessageBox.confirm("尚有未保存的资料，是否放弃这些修改？", "离开维护", { confirmButtonText: "放弃修改", cancelButtonText: "继续编辑", type: "warning" }); return true; } catch { return false; }
}
async function close() { if (await canLeave()) emit("close"); }
async function reload() { if (await canLeave()) await load(); }
function failed(value: unknown) { error.value = message(value); if (value instanceof ApiError && value.httpStatus === 409) conflict.value = true; }
async function save() {
  if (blocked.value) return;
  if (issues.value.length) { error.value = `${issues.value[0]!.path ? standardFieldLabel(issues.value[0]!.path) + "：" : ""}${issues.value[0]!.message}`; return; }
  saving.value = true; error.value = "";
  try {
    const saved = await standardModelsApi.manualSave(formData(), reason.value.trim() || (current.value ? '手动补全官方资料' : '新增标准模型资料'), intent, current.value);
    if (active) { apply(saved); ElMessage.success(saved.missing_fields.length ? "已保存，仍有资料待补齐" : "已保存并记录人工核验"); emit("saved"); emit("close"); }
  } catch (value) { if (active) failed(value); }
  finally { if (active) saving.value = false; }
}
async function history(page = 1) {
  if (!current.value || historyLoading.value) return;
  historyRequest?.abort(); const request = historyRequest = new AbortController();
  historyLoading.value = true; historyError.value = "";
  try { const result = await standardModelsApi.versions(current.value.id, page, request.signal); if (active && !request.signal.aborted) { versions.value = result; selectedHistory.value = undefined; } }
  catch (value) { if (active && !request.signal.aborted) historyError.value = message(value); }
  finally { if (active && !request.signal.aborted) historyLoading.value = false; }
}
async function logs() {
  if (!current.value || recordsLoading.value) return;
  recordsRequest?.abort(); const request = recordsRequest = new AbortController();
  recordsLoading.value = true; recordsError.value = "";
  try { const result = await standardModelsApi.records(current.value.id, request.signal); if (active && !request.signal.aborted) records.value = result; }
  catch (value) { if (active && !request.signal.aborted) recordsError.value = message(value); }
  finally { if (active && !request.signal.aborted) recordsLoading.value = false; }
}
function changedTab() { if (tab.value === "history") void history(); if (tab.value === "records") void logs(); }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = ""; } }
onBeforeRouteLeave(canLeave);
onMounted(() => { void load().then(changedTab); window.addEventListener("beforeunload", beforeUnload); });
onBeforeUnmount(() => { active = false; detailRequest?.abort(); historyRequest?.abort(); recordsRequest?.abort(); window.removeEventListener("beforeunload", beforeUnload); });
</script>

<template>
  <el-dialog class="standard-model-dialog" :model-value="true" :title="current || id ? '手动补全' : '新增标准模型'" width="min(880px, 95vw)" align-center :before-close="close" :close-on-click-modal="false" :close-on-press-escape="!saving" destroy-on-close append-to-body>
    <div v-loading="loading" class="model-maintenance">
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-alert v-if="conflict" title="记录已更新。你的输入已保留，请核对最新资料后重新编辑。" type="warning" :closable="false"><el-button text @click="reload">重新读取当前资料</el-button></el-alert>
      <el-alert v-if="readonly && !loading" title="当前为只读查看；仅绑定维护人可以补全资料并保存核验记录。" type="info" :closable="false" />
      <div class="maintenance-note">资料完整后直接保存，所有修改留痕。官方未披露参数明确填写“未披露”。价格及计费条件必须可核验；自动获取失败时手动补全。</div>
      <el-alert v-if="current?.missing_fields.length" class="missing-fields" type="warning" :closable="false" title="资料不完整时可先保存；补齐并由维护人核对后才能报价。"><p>{{ current.missing_fields.map(standardFieldLabel).join('、') }}</p></el-alert>
      <el-form label-position="top" :disabled="blocked" class="model-form">
        <el-form-item label="厂商"><el-select v-model="draft.company" filterable allow-create default-first-option aria-label="厂商"><el-option v-for="company in companies" :key="company" :label="company" :value="company" /></el-select></el-form-item>
        <el-form-item label="模型类型"><el-select v-model="draft.model_type" clearable aria-label="模型类型"><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></el-form-item>
        <el-form-item label="官方标准名称"><el-input v-model="draft.name" /></el-form-item><el-form-item label="官方 Model ID／版本"><el-input v-model="draft.official_id" /></el-form-item>
        <el-form-item label="输入／输出模态" class="full"><el-input v-model="notes.modalities" type="textarea" :rows="3" placeholder="输入：文本、图片&#10;输出：文本" /></el-form-item>
        <el-form-item label="容量与规格" class="full"><el-input v-model="notes.capacity" type="textarea" :rows="3" placeholder="上下文：128000 tokens&#10;最大输出：未披露" /></el-form-item>
        <el-form-item label="核心能力" class="full"><el-input v-model="notes.abilities" type="textarea" :rows="3" placeholder="Thinking ×&#10;Function Calling ✓&#10;Streaming：未核实" /></el-form-item>
        <el-form-item label="API、限流与访问条件" class="full"><el-input v-model="notes.api" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="发布日期、版本与生命周期" class="full"><el-input v-model="notes.lifecycle" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="官方价格与计费条件" class="full"><StandardPriceEditor ref="priceEditor" v-model="draft.price_items" :disabled="blocked" :model-type="draft.model_type" :show-extras="false" /></el-form-item>
        <el-form-item label="原厂官方资料 URL" class="full"><el-input v-model="sourceURL" placeholder="https:// 原厂官网或原厂模型卡" /></el-form-item>
        <el-form-item label="核验日期"><input v-model="draft.checked_at" class="verification-date" type="date" :max="shanghaiToday()" :disabled="blocked" aria-label="核验日期" /></el-form-item>
      </el-form>
      <p v-if="issues.length && !readonly" class="validation-hint">{{ issues.length }} 处已填写内容需修正：{{ issues[0]!.path ? standardFieldLabel(issues[0]!.path) + '，' : '' }}{{ issues[0]!.message }}</p>
    </div>
    <template #footer><div class="dialog-actions"><el-button :disabled="saving" @click="close">{{ readonly ? '关闭' : '取消' }}</el-button><el-button v-if="!readonly" type="primary" :disabled="blocked" :loading="saving" @click="save">保存并留痕</el-button></div></template>
  </el-dialog>
  <el-dialog v-if="tab === 'advanced'" :model-value="true" title="补充模型资料" width="min(880px, 95vw)" align-center append-to-body :close-on-click-modal="false" @close="tab = 'profile'">
    <p class="hint">补充来源、计费维度和自定义操作说明；返回手动补全后统一保存并留痕。</p>
    <el-form label-position="top" :disabled="blocked">
      <el-form-item label="补充官方来源"><div class="supplementary-sources"><div v-for="(source, index) in draft.sources.slice(1)" :key="index" class="source-row"><el-input v-model="source.label" :aria-label="`补充来源${index + 1}名称`" placeholder="来源名称" /><el-select v-model="source.source_kind" :aria-label="`补充来源${index + 1}类别`"><el-option v-for="(text, key) in sourceLabels" :key="key" :label="text" :value="key" /></el-select><el-input v-model="source.url" :aria-label="`补充来源${index + 1}网址`" placeholder="https://…" /><el-button v-if="!readonly" @click="draft.sources.splice(index + 1, 1)">移除</el-button></div><el-button v-if="!readonly" size="small" @click="addSource">添加来源</el-button></div></el-form-item>
      <StandardPriceEditor v-model="draft.price_items" :disabled="blocked" :model-type="draft.model_type" details-only />
      <el-form-item v-if="!readonly" label="操作说明"><el-input v-model="reason" type="textarea" :rows="2" placeholder="选填，不填写时记录本次手动补全操作" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="tab = 'profile'">返回手动补全</el-button></template>
  </el-dialog>
  <el-dialog v-if="tab === 'history' || tab === 'records'" :model-value="true" title="模型资料追溯" width="min(1000px, 95vw)" align-center append-to-body @close="tab = 'profile'">
    <el-tabs v-model="tab" @tab-change="changedTab">
        <el-tab-pane label="历史版本" name="history" :disabled="!current">
          <el-alert v-if="historyError" :title="historyError" type="error" :closable="false"><el-button text @click="history()">重试</el-button></el-alert>
          <el-table v-loading="historyLoading" :data="versions?.list || []" empty-text="暂无历史版本"><el-table-column prop="version_no" label="版本" /><el-table-column label="记录时间"><template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template></el-table-column><el-table-column label="核验状态"><template #default="{ row }">{{ verificationLabels[row.verify_status] || row.verify_status }}</template></el-table-column><el-table-column label="详情"><template #default="{ row }"><el-button text @click="selectedHistory = row">查看</el-button></template></el-table-column></el-table>
          <el-pagination v-if="versions" :current-page="versions.page" :page-size="versions.size" :total="versions.total" layout="prev, pager, next" @current-change="history" />
          <section v-if="selectedHistory" class="history-snapshot"><h3>版本 {{ selectedHistory.version_no }} · 只读快照</h3><p>历史资料仅用于追溯，不能作为新报价依据。</p><dl><dt>名称 / ID / 公司</dt><dd>{{ selectedHistory.name }} / {{ selectedHistory.official_id }} / {{ selectedHistory.company }}</dd><dt>模型类型 / 输入输出</dt><dd>{{ modelTypeLabels[selectedHistory.model_type] }} / {{ selectedHistory.input_modalities.map(value => modelTypeLabels[value]).join('、') }} → {{ selectedHistory.output_modalities.map(value => modelTypeLabels[value]).join('、') }}</dd><dt>容量规格</dt><dd v-for="fact in selectedHistory.capacity" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>核心能力</dt><dd v-for="ability in selectedHistory.abilities" :key="ability.key">{{ ability.key }}：{{ abilityLabels[ability.state] }} {{ ability.description }}</dd><dt>官方价格</dt><dd v-for="price in selectedHistory.price_items" :key="price.item_key">{{ formatStandardPrice(price) }} · {{ price.condition }}</dd><dt>API / 访问条件</dt><dd v-for="fact in selectedHistory.api_access" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>发布 / 生命周期</dt><dd v-for="fact in selectedHistory.lifecycle" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>来源 / 核对日期</dt><dd v-for="source in selectedHistory.sources" :key="source.url">{{ source.label }}：{{ source.url }}</dd><dd>{{ selectedHistory.checked_at || '未核对' }}</dd></dl></section>
        </el-tab-pane>
        <el-tab-pane label="操作记录" name="records" :disabled="!current"><el-alert v-if="recordsError" :title="recordsError" type="error" :closable="false"><el-button text @click="logs">重试</el-button></el-alert><el-table v-loading="recordsLoading" :data="records" empty-text="暂无操作记录"><el-table-column label="操作"><template #default="{ row }">{{ operationLabel(row.action) }}</template></el-table-column><el-table-column prop="operator_name" label="操作人" /><el-table-column prop="reason" label="说明" /><el-table-column label="时间"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column></el-table><p class="hint">显示最近100条记录。</p></el-tab-pane>

    </el-tabs><template #footer><el-button @click="tab = 'profile'">返回维护</el-button></template>
  </el-dialog>
</template>
<style scoped>
:global(.standard-model-dialog){--el-color-primary:#315ee8;--el-color-primary-dark-2:#284fc4;padding:0;border-radius:10px;overflow:hidden}:global(.standard-model-dialog .el-dialog__header){padding:20px 22px;border-bottom:1px solid #e3e6eb;margin:0}:global(.standard-model-dialog .el-dialog__title){font-size:15px;font-weight:600;color:#30343b}:global(.standard-model-dialog .el-dialog__headerbtn){top:10px;right:12px}:global(.standard-model-dialog .el-dialog__body){max-height:calc(90vh - 142px);overflow-y:auto;padding:20px}:global(.standard-model-dialog .el-dialog__footer){padding:12px 20px;border-top:1px solid #e3e6eb;background:#fafbfc}
.model-maintenance{min-height:250px;color:#30343b}.maintenance-note{background:#f8f9fb;border:1px solid #e8ebef;color:#6e747e;padding:12px;border-radius:6px;font-size:12px;line-height:1.6;margin-bottom:0}.missing-fields{margin-bottom:16px}.model-form{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:14px 16px}.model-form>.full{grid-column:1/-1}.model-form:deep(.el-form-item){margin-bottom:0;min-width:0}.model-form:deep(.el-form-item__label){padding:0;margin-bottom:6px;line-height:18px;height:auto;font-size:12px;color:#69707c}.model-form:deep(.el-select),.model-form:deep(.el-date-editor){width:100%}.model-form:deep(.el-textarea__inner){font-family:inherit;font-size:12px;line-height:19px;min-height:75px;padding:8px 10px;resize:vertical}.model-form:deep(.el-input__inner){font-size:12px}.trace-links{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-top:14px;font-size:12px;color:#8b9098}.trace-links .el-button{margin:0}.source-extra{font-size:12px;color:#8b9098}.source-extra summary{cursor:pointer}.source-row{display:grid;grid-template-columns:1fr 110px 2fr auto;gap:8px;margin:10px 0}.validation-hint{font-size:12px;color:#b45309}.dialog-actions{display:flex;justify-content:flex-end;gap:8px}.dialog-actions .el-button{margin:0}.history-snapshot{padding:18px;background:#f8f9fb;margin-top:18px;font-size:13px}.history-snapshot dt{font-weight:600;margin-top:14px}.history-snapshot dd{margin:7px 0;overflow-wrap:anywhere}.hint{font-size:12px;color:#8b9098}@media(max-width:600px){.model-form{grid-template-columns:minmax(0,1fr)}.model-form>div:empty{display:none}.source-row{grid-template-columns:1fr}.model-form>.full{grid-column:auto}:global(.standard-model-dialog .el-dialog__body){padding:14px}}
:global(.standard-model-dialog .el-dialog__header){padding:20px;border-color:#e2e6ed}:global(.standard-model-dialog .el-dialog__title){font-size:16px}:global(.standard-model-dialog .el-dialog__headerbtn){top:14px;right:20px;width:36px;height:34px;border:1px solid #e2e6ed;border-radius:6px}:global(.standard-model-dialog .el-dialog__headerbtn .el-dialog__close){font-size:12px}:global(.standard-model-dialog .el-dialog__body){padding:20px;max-height:calc(90vh - 128px)}:global(.standard-model-dialog .el-dialog__footer){padding:12px 20px;background:#fafbfc}.maintenance-note{margin-bottom:20px}.model-form{gap:20px 16px}.model-form:deep(.el-form-item__label){font-size:12px;margin-bottom:8px;color:#778391}.model-form:deep(.el-input__wrapper),.model-form:deep(.el-select__wrapper){min-height:34px}.model-form:deep(.el-textarea__inner){min-height:76px}.verification-date{box-sizing:border-box;display:block;width:100%;min-width:0;height:34px;padding:0 10px;border:1px solid #dfe4ed;border-radius:5px;background:#fff;color:#263b57;font:inherit;font-size:12px;color-scheme:light}.verification-date:focus{outline:2px solid #315ee8;outline-offset:-1px}.verification-date:disabled{background:#f5f7fa;color:#a8abb2}.supplementary-sources{width:100%}.dialog-actions .el-button{height:30px;padding:0 12px;font-size:12px;border-radius:5px}.dialog-actions .el-button--primary{font-weight:600}@media(max-width:600px){:global(.standard-model-dialog .el-dialog__header){padding:18px 16px}:global(.standard-model-dialog .el-dialog__body){padding:16px}:global(.standard-model-dialog .el-dialog__footer){padding:12px 16px}}
</style>
