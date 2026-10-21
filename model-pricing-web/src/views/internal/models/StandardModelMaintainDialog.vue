<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { ApiError } from "../../../domain/common";
import { standardModelsApi } from "../../../api/standardModels";
import { abilityLabels, copyStandardModel, emptyStandardModel, formatStandardPrice, factLabels, modelTypeLabels, shanghaiToday, sourceLabels, standardFieldLabel, validateStandardModel } from "../../../api/standardModels.contract";
import type { StandardModelDTO, StandardModelPage } from "../../../api/standardModels.types";
import StandardFactEditor from "./StandardFactEditor.vue";
import StandardPriceEditor from "./StandardPriceEditor.vue";

const props = defineProps<{ id?: string }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const current = ref<StandardModelDTO>();
const draft = ref(emptyStandardModel());
const baseline = ref(JSON.stringify(draft.value));
const reason = ref("");
const loading = ref(false), saving = ref(false), error = ref(""), conflict = ref(false), tab = ref("profile");
const versions = ref<StandardModelPage>(), selectedHistory = ref<StandardModelDTO>();
const historyLoading = ref(false), historyError = ref("");
const records = ref<Awaited<ReturnType<typeof standardModelsApi.records>>>([]), recordsLoading = ref(false), recordsError = ref("");
const readonly = computed(() => current.value ? !current.value.permissions.can_edit : !!props.id);
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value);
const issues = computed(() => validateStandardModel(draft.value));
const blocked = computed(() => saving.value || loading.value || readonly.value || conflict.value);
let active = true;
let detailRequest: AbortController | undefined, historyRequest: AbortController | undefined, recordsRequest: AbortController | undefined;
let intent = crypto.randomUUID();
const message = (value: unknown) => value instanceof Error ? value.message : "请求未完成，请重试。";
function apply(model: StandardModelDTO) { current.value = model; draft.value = copyStandardModel(model); baseline.value = JSON.stringify(draft.value); reason.value = ""; intent = crypto.randomUUID(); }
async function load() {
  if (!props.id) return;
  detailRequest?.abort(); const request = detailRequest = new AbortController();
  loading.value = true; error.value = "";
  try { const result = await standardModelsApi.get(props.id, request.signal); if (active && !request.signal.aborted) { apply(result); conflict.value = false; } }
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
  if (issues.value.length) { error.value = `${standardFieldLabel(issues.value[0]!.path)}：${issues.value[0]!.message}`; return; }
  saving.value = true; error.value = "";
  try {
    const saved = await standardModelsApi.save(draft.value, reason.value, intent, current.value);
    if (active) { apply(saved); ElMessage.success(saved.missing_fields.length ? "已保存，仍有资料待补齐" : "已保存资料"); emit("saved"); }
  } catch (value) { if (active) failed(value); }
  finally { if (active) saving.value = false; }
}
async function verify() {
  if (blocked.value || !current.value?.permissions.can_verify || dirty.value) return;
  saving.value = true; error.value = "";
  try { const saved = await standardModelsApi.verify(current.value, reason.value, intent); if (active) { apply(saved); ElMessage.success("人工核验已记录"); emit("saved"); } }
  catch (value) { if (active) failed(value); }
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
function isFuture(date: Date) { return date.toLocaleDateString("sv-SE", { timeZone: "Asia/Shanghai" }) > shanghaiToday(); }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = ""; } }
onBeforeRouteLeave(canLeave);
onMounted(() => { void load(); window.addEventListener("beforeunload", beforeUnload); });
onBeforeUnmount(() => { active = false; detailRequest?.abort(); historyRequest?.abort(); recordsRequest?.abort(); window.removeEventListener("beforeunload", beforeUnload); });
</script>

<template>
  <el-dialog class="standard-model-dialog" :model-value="true" :title="current || id ? '标准模型资料维护' : '新增标准模型'" width="min(1100px, 96vw)" top="6vh" :before-close="close" :close-on-click-modal="false" :close-on-press-escape="!saving" destroy-on-close>
    <div v-loading="loading" class="model-maintenance">
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-alert v-if="conflict" title="记录已更新。你的输入已保留，请核对最新资料后重新编辑。" type="warning" :closable="false"><el-button text @click="reload">重新读取当前资料</el-button></el-alert>
      <el-alert v-if="readonly && !loading" title="当前为只读查看；仅绑定维护人可以修改和核验。" type="info" :closable="false" />
      <div v-if="current" class="model-state"><span>{{ current.name || '待补充名称' }} · 版本 {{ current.version_no }}</span><el-tag :type="current.verify_status === 'VERIFIED' ? 'success' : 'warning'">{{ current.verify_status === 'VERIFIED' ? '已人工核验' : current.missing_fields.length ? '资料待补齐' : '待人工核验' }}</el-tag><span>{{ current.quote_readiness.ready ? '可用于报价' : '暂不可用于报价' }}</span></div>
      <el-tabs v-model="tab" @tab-change="changedTab">
        <el-tab-pane label="官方资料" name="profile">
          <el-alert v-if="current?.missing_fields.length" type="warning" :closable="false" title="可继续保存资料；补齐以下项目并人工核验后才能报价。"><p>{{ current.missing_fields.map(standardFieldLabel).join('、') }}</p></el-alert>
          <el-form label-position="top" :disabled="blocked" class="model-form">
            <section class="identity-grid">
              <el-form-item label="模型公司"><el-input v-model="draft.company" /></el-form-item><el-form-item label="官方模型名称"><el-input v-model="draft.name" /></el-form-item>
              <el-form-item label="官方 ID / 版本标识"><el-input v-model="draft.official_id" /></el-form-item><el-form-item label="模型类型"><el-select v-model="draft.model_type" clearable><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></el-form-item>
              <el-form-item label="输入模态"><el-select v-model="draft.input_modalities" multiple><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></el-form-item><el-form-item label="输出模态"><el-select v-model="draft.output_modalities" multiple><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></el-form-item>
            </section>
            <StandardFactEditor v-model="draft.capacity" title="容量规格" :disabled="blocked" />
            <section><header class="section-heading"><h3>核心能力</h3><el-button v-if="!readonly" size="small" @click="draft.abilities.push({ key: '', state: 'UNVERIFIED', description: '' })">添加能力</el-button></header><p class="hint">未核实、未披露与不支持分别记录。</p><div v-for="(ability, index) in draft.abilities" :key="index" class="ability-row"><el-input v-model="ability.key" :aria-label="`第${index + 1}项能力标识`" placeholder="能力标识，如 tool_call" /><el-select v-model="ability.state" :aria-label="`第${index + 1}项能力状态`"><el-option v-for="(text, key) in abilityLabels" :key="key" :label="text" :value="key" /></el-select><el-input v-model="ability.description" :aria-label="`第${index + 1}项能力说明`" placeholder="官方说明" /><el-button v-if="!readonly" @click="draft.abilities.splice(index, 1)">移除</el-button></div></section>
            <StandardPriceEditor v-model="draft.price_items" :disabled="blocked" />
            <StandardFactEditor v-model="draft.api_access" title="API / 限流 / 访问条件" :disabled="blocked" />
            <StandardFactEditor v-model="draft.lifecycle" title="发布 / 版本 / 生命周期" :disabled="blocked" />
            <section><header class="section-heading"><h3>官方来源 / 核对日期</h3><el-button v-if="!readonly" size="small" @click="draft.sources.push({ label: '', url: '', source_kind: 'OFFICIAL_DOCS' })">添加来源</el-button></header><div v-for="(source, index) in draft.sources" :key="index" class="source-row"><el-input v-model="source.label" :aria-label="`第${index + 1}项来源名称`" placeholder="来源名称" /><el-select v-model="source.source_kind" :aria-label="`第${index + 1}项来源类别`"><el-option v-for="(text, key) in sourceLabels" :key="key" :label="text" :value="key" /></el-select><el-input v-model="source.url" :aria-label="`第${index + 1}项来源网址`" placeholder="https://…" /><el-button v-if="!readonly" @click="draft.sources.splice(index, 1)">移除</el-button></div><el-form-item label="核对日期"><el-date-picker v-model="draft.checked_at" type="date" value-format="YYYY-MM-DD" :disabled-date="isFuture" placeholder="请选择实际核对日期" /></el-form-item></section>
          </el-form>
          <div v-if="!readonly" class="save-area"><label for="standard-model-reason">操作说明</label><el-input id="standard-model-reason" v-model="reason" type="textarea" :rows="2" placeholder="说明补录、变更或人工核验的依据" :disabled="saving || loading" /><p v-if="dirty" class="hint">有未保存的资料；请先保存，再进行人工核验。</p><p v-if="issues.length" class="validation-hint">{{ issues.length }} 处已填写内容需修正：{{ standardFieldLabel(issues[0]!.path) }}，{{ issues[0]!.message }}</p></div>
        </el-tab-pane>
        <el-tab-pane label="历史版本" name="history" :disabled="!current">
          <el-alert v-if="historyError" :title="historyError" type="error" :closable="false"><el-button text @click="history()">重试</el-button></el-alert>
          <el-table v-loading="historyLoading" :data="versions?.list || []" empty-text="暂无历史版本"><el-table-column prop="version_no" label="版本" /><el-table-column prop="updated_at" label="记录时间" /><el-table-column prop="verify_status" label="核验状态" /><el-table-column label="详情"><template #default="{ row }"><el-button text @click="selectedHistory = row">查看</el-button></template></el-table-column></el-table>
          <el-pagination v-if="versions" :current-page="versions.page" :page-size="versions.size" :total="versions.total" layout="prev, pager, next" @current-change="history" />
          <section v-if="selectedHistory" class="history-snapshot"><h3>版本 {{ selectedHistory.version_no }} · 只读快照</h3><p>历史资料仅用于追溯，不能作为新报价依据。</p><dl><dt>名称 / ID / 公司</dt><dd>{{ selectedHistory.name }} / {{ selectedHistory.official_id }} / {{ selectedHistory.company }}</dd><dt>模型类型 / 输入输出</dt><dd>{{ modelTypeLabels[selectedHistory.model_type] }} / {{ selectedHistory.input_modalities.map(value => modelTypeLabels[value]).join('、') }} → {{ selectedHistory.output_modalities.map(value => modelTypeLabels[value]).join('、') }}</dd><dt>容量规格</dt><dd v-for="fact in selectedHistory.capacity" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>核心能力</dt><dd v-for="ability in selectedHistory.abilities" :key="ability.key">{{ ability.key }}：{{ abilityLabels[ability.state] }} {{ ability.description }}</dd><dt>官方价格</dt><dd v-for="price in selectedHistory.price_items" :key="price.item_key">{{ formatStandardPrice(price) }} · {{ price.condition }}</dd><dt>API / 访问条件</dt><dd v-for="fact in selectedHistory.api_access" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>发布 / 生命周期</dt><dd v-for="fact in selectedHistory.lifecycle" :key="fact.key">{{ fact.key }}：{{ fact.value || factLabels[fact.disclosure] }}</dd><dt>来源 / 核对日期</dt><dd v-for="source in selectedHistory.sources" :key="source.url">{{ source.label }}：{{ source.url }}</dd><dd>{{ selectedHistory.checked_at || '未核对' }}</dd></dl></section>
        </el-tab-pane>
        <el-tab-pane label="操作记录" name="records" :disabled="!current"><el-alert v-if="recordsError" :title="recordsError" type="error" :closable="false"><el-button text @click="logs">重试</el-button></el-alert><el-table v-loading="recordsLoading" :data="records" empty-text="暂无操作记录"><el-table-column prop="action" label="操作" /><el-table-column prop="operator_id" label="操作人" /><el-table-column prop="reason" label="说明" /><el-table-column prop="occurred_at" label="时间" /><el-table-column prop="request_id" label="追踪编号" /></el-table><p class="hint">显示最近100条记录。</p></el-tab-pane>
      </el-tabs>
    </div>
    <template #footer><div class="dialog-actions"><el-button :disabled="saving" @click="close">{{ readonly ? '关闭' : '取消' }}</el-button><template v-if="tab === 'profile' && !readonly"><el-button :disabled="blocked || dirty || !current?.permissions.can_verify" :loading="saving" @click="verify">人工核验</el-button><el-button type="primary" :disabled="blocked" :loading="saving" @click="save">保存资料</el-button></template></div></template>
  </el-dialog>
</template>

<style scoped>
:global(.standard-model-dialog){--el-color-primary:#087c72;--el-color-primary-dark-2:#076e65}:global(.standard-model-dialog .el-dialog__body){max-height:calc(85vh - 130px);overflow-y:auto}
.model-maintenance{min-height:250px}.model-state,.section-heading,.dialog-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.model-state{padding:15px 0;color:#526970}.section-heading{justify-content:space-between}h3{font-size:15px}.identity-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 22px}.model-form section{border-bottom:1px solid #e6ebee;padding:8px 0 18px}.ability-row,.source-row{display:grid;grid-template-columns:1fr 140px 2fr auto;gap:10px;margin:12px 0}.hint{font-size:13px;color:#79868f}.validation-hint{color:#b45309;font-size:13px}.save-area{display:grid;gap:10px;padding-top:18px}.history-snapshot{padding:18px;background:#f4f8f8;margin-top:18px}.history-snapshot dt{font-weight:600;margin-top:14px}.history-snapshot dd{margin:7px 0;overflow-wrap:anywhere}.dialog-actions{justify-content:flex-end}@media(max-width:700px){.identity-grid{grid-template-columns:1fr}.ability-row,.source-row{grid-template-columns:1fr 1fr}.ability-row>:nth-child(3),.source-row>:nth-child(3){grid-column:1/-1}}
</style>
