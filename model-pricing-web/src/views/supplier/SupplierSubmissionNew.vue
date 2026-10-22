<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierModelsApi } from "../../api/supplierModels";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import type { SupplierModelOptionDTO, SupplierSubmissionDTO, SupplierSubmissionRowDTO, SupplierSubmissionTestDTO, SupplierSubmissionWriteRow, SupplierTestCategory, SupplierTestJobDTO } from "../../api/supplierSubmissions.types";

type EditableRow = SupplierSubmissionRowDTO & { test_api_key: string };
const route = useRoute();
const router = useRouter();
const kind = computed(() => route.query.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增");
const isPriceChange = computed(() => route.query.kind === "PRICE_CHANGE");
const resourceID = computed(() => typeof route.query.resource_id === "string" ? route.query.resource_id : "");
const expectedRevision = computed(() => typeof route.query.expected_revision === "string" ? route.query.expected_revision : "");
const modelName = computed(() => typeof route.query.model_name === "string" ? route.query.model_name : "当前模型");
const savedCoefficient = computed(() => typeof route.query.coefficient === "string" ? route.query.coefficient : "");
const coefficient = ref(savedCoefficient.value);
const reason = ref("");
const error = ref("");
const submitting = ref(false);
const saving = ref(false);
const lifecycle = ref(crypto.randomUUID());
const submissionID = ref("");
const revision = ref("");
const canEdit = ref(true);
const rows = ref<EditableRow[]>([]);
const modelOptions = ref<SupplierModelOptionDTO[]>([]);
const modelKeyword = ref("");
const modelLoading = ref(false);
const modelError = ref("");
const loadingDraft = ref(false);
const savedBaseline = ref("");
const dirty = computed(() => !!savedBaseline.value && savedBaseline.value !== snapshot());
const canSubmitBatch = computed(() => rows.value.length > 0 && rows.value.every(row => row.connectivity_tested) && rows.value.some(row => row.stress_tested));
const locked = computed(() => !canEdit.value || saving.value || submitting.value || loadingDraft.value);
const testCategories: SupplierTestCategory[] = ["CONNECTIVITY", "STRESS"];
let active = true;
let optionRequest: AbortController | undefined;
let draftRequest: AbortController | undefined;
const testBusy = ref<Record<string, boolean>>({});

function blankRow(): EditableRow {
  return { id: "", interface_model_id: "", standard_model_id: "", standard_model_name: "", official_id: "", coefficient: "1", test_base_url: "", test_api_key: "", row_status: "DRAFT", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false };
}
function snapshot() {
  return JSON.stringify({ reason: reason.value, rows: rows.value.map(row => ({ id: row.id, interface_model_id: row.interface_model_id, standard_model_id: row.standard_model_id, coefficient: row.coefficient, test_base_url: row.test_base_url, test_api_key: row.test_api_key })) });
}
function rememberSaved() { savedBaseline.value = snapshot(); lifecycle.value = crypto.randomUUID(); }
function normalizeSaved(dto: SupplierSubmissionDTO, previousRows = rows.value) {
  submissionID.value = dto.id;
  revision.value = dto.revision;
  canEdit.value = dto.permissions.can_edit;
  reason.value = dto.reason || reason.value;
  rows.value = dto.rows.map((item, index) => ({ ...item, test_api_key: previousRows[index]?.test_api_key || "" }));
  rememberSaved();
}
function showError(value: unknown, fallback: string) { error.value = value instanceof Error ? value.message : fallback; }
function coefficientIsValid(value: string) {
  const amount = Number(value.trim());
  return /^(?:0?\.\d+|1(?:\.0+)?)$/.test(value.trim()) && Number.isFinite(amount) && amount > 0 && amount <= 1;
}
function validateRows(): string {
  if (!rows.value.length) return "请至少添加一条模型。";
  for (const [index, row] of rows.value.entries()) {
    const line = `第 ${index + 1} 行`;
    if (!row.interface_model_id.trim()) return `${line}请填写接口模型 ID。`;
    if (!row.standard_model_id) return `${line}请选择对应标准模型。`;
    if (!coefficientIsValid(row.coefficient)) return `${line}采购系数须大于 0 且不超过 1。`;
    try {
      const url = new URL(row.test_base_url.trim());
      if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("scheme");
    } catch { return `${line}请填写有效的测试服务 URL。`; }
    if (!row.id && !row.test_api_key.trim()) return `${line}请填写用于自测的 Key。`;
  }
  return "";
}
async function loadModelOptions() {
  optionRequest?.abort();
  const request = optionRequest = new AbortController();
  modelLoading.value = true;
  modelError.value = "";
  try {
    const result = await supplierSubmissionsApi.modelOptions(modelKeyword.value, request.signal);
    if (active && !request.signal.aborted) modelOptions.value = result;
  } catch (value) {
    if (active && !request.signal.aborted) { modelOptions.value = []; modelError.value = value instanceof Error ? value.message : "标准模型目录暂时无法加载。"; }
  } finally { if (active && !request.signal.aborted) modelLoading.value = false; }
}
async function loadDraft(id: string) {
  draftRequest?.abort();
  const request = draftRequest = new AbortController();
  loadingDraft.value = true;
  error.value = "";
  try {
    const result = await supplierSubmissionsApi.get(id, request.signal);
    if (!active || request.signal.aborted) return;
    if (result.kind !== "MODEL_NEW") throw new Error("当前申报不是模型新增类型。请从申报记录重新打开。");
    normalizeSaved(result, []);
  } catch (value) {
    if (active && !request.signal.aborted) error.value = value instanceof Error ? value.message : "申报详情暂时无法加载。";
  } finally { if (active && !request.signal.aborted) loadingDraft.value = false; }
}
function addRow() { rows.value.push(blankRow()); }
function removeRow(index: number) { rows.value.splice(index, 1); }
function selectStandardModel(row: EditableRow, id: string | number) {
  const option = modelOptions.value.find(item => item.id === String(id));
  row.standard_model_id = String(id || "");
  row.standard_model_name = option?.name || "";
  row.official_id = option?.official_id || "";
}
function writePayload() {
  const payloadRows: SupplierSubmissionWriteRow[] = rows.value.map(row => ({
    ...(row.id ? { id: row.id } : {}),
    interface_model_id: row.interface_model_id.trim(),
    standard_model_id: row.standard_model_id,
    coefficient: row.coefficient.trim(),
    test_base_url: row.test_base_url.trim(),
    ...(row.test_api_key.trim() ? { test_api_key: row.test_api_key.trim() } : {}),
  }));
  return { kind: "MODEL_NEW" as const, ...(submissionID.value ? { expected_revision: revision.value } : {}), reason: reason.value.trim(), rows: payloadRows };
}
async function saveDraft(): Promise<boolean> {
  if (saving.value || submitting.value || !canEdit.value) return false;
  const issue = validateRows();
  if (issue) { error.value = issue; return false; }
  if (!reason.value.trim()) { error.value = "请填写申报说明。"; return false; }
  saving.value = true;
  error.value = "";
  const previous = rows.value;
  try {
    const payload = writePayload();
    const result = submissionID.value
      ? await supplierSubmissionsApi.update(submissionID.value, payload, lifecycle.value)
      : await supplierSubmissionsApi.create(payload, lifecycle.value);
    if (!active) return false;
    normalizeSaved(result, previous);
    ElMessage.success("草稿已保存");
    return true;
  } catch (value) {
    if (active) showError(value, "草稿保存失败，输入内容已保留。");
    return false;
  } finally { if (active) saving.value = false; }
}
function applyTest(row: EditableRow, result: SupplierTestJobDTO) {
  const test: SupplierSubmissionTestDTO = { category: result.category, status: result.status, tested: result.tested, passed: result.passed, job_id: result.job_id, report_id: result.report_id, connection_ms: result.connection_ms, rps: result.rps, tps: result.tps };
  if (result.category === "CONNECTIVITY") { row.connectivity_job = test; row.connectivity_tested = result.tested; row.connectivity_passed = result.passed; }
  else { row.stress_job = test; row.stress_tested = result.tested; row.stress_passed = result.passed; }
}
async function runTest(row: EditableRow, category: SupplierTestCategory) {
  if (!submissionID.value || !row.id || testBusy.value[row.id + category]) return;
  testBusy.value = { ...testBusy.value, [row.id + category]: true };
  error.value = "";
  try {
    applyTest(row, await supplierSubmissionsApi.startTest(submissionID.value, row.id, category, lifecycle.value));
  } catch (value) { showError(value, "测试任务未能启动。请保存草稿后重试。"); }
  finally { const next = { ...testBusy.value }; delete next[row.id + category]; testBusy.value = next; }
}
async function refreshTest(row: EditableRow, category: SupplierTestCategory) {
  if (!submissionID.value || !row.id || testBusy.value[row.id + category]) return;
  testBusy.value = { ...testBusy.value, [row.id + category]: true };
  try { applyTest(row, await supplierSubmissionsApi.testStatus(submissionID.value, row.id, category)); }
  catch (value) { showError(value, "测试进度暂时无法读取。可以稍后重试。"); }
  finally { const next = { ...testBusy.value }; delete next[row.id + category]; testBusy.value = next; }
}
function testState(tested: boolean, passed: boolean) { return !tested ? "未测试" : passed ? "通过" : "已测试，未通过"; }
function testFor(row: EditableRow, category: SupplierTestCategory) { return category === "CONNECTIVITY" ? row.connectivity_job : row.stress_job; }
async function submitDraft() {
  if (submitting.value || !canEdit.value) return;
  if (dirty.value || !submissionID.value) {
    if (!(await saveDraft())) return;
  }
  if (!submissionID.value || !revision.value) { error.value = "草稿尚未保存，请先保存后再提交。"; return; }
  if (!canSubmitBatch.value) { error.value = "提交要求：每个模型都完成连通性测试，且至少一个模型完成压力测试。测试是否通过由服务端按流程判断。"; return; }
  if (!reason.value.trim()) { error.value = "请填写申报说明。"; return; }
  submitting.value = true;
  error.value = "";
  try {
    await supplierSubmissionsApi.submit(submissionID.value, revision.value, reason.value, lifecycle.value);
    if (!active) return;
    ElMessage.success("申报已提交，后续状态以办理进度为准。");
    await router.replace({ path: "/supplier/home", query: { tab: "history" } });
  } catch (value) { if (active) showError(value, "申报未能提交，草稿和输入已保留。"); }
  finally { if (active) submitting.value = false; }
}
async function submitPriceChange() {
  if (submitting.value) return;
  if (!resourceID.value || !expectedRevision.value) { error.value = "缺少资源或版本信息，请从模型管理页面重新发起。"; return; }
  if (!reason.value.trim()) { error.value = "请填写价格变更原因。"; return; }
  submitting.value = true;
  error.value = "";
  try {
    await supplierModelsApi.priceChange(resourceID.value, expectedRevision.value, coefficient.value, reason.value, lifecycle.value);
    if (!active) return;
    ElMessage.success("价格变更申报已提交。");
    await router.replace({ path: "/supplier/home", query: { tab: "history" } });
  } catch (value) { if (active) showError(value, "价格变更申报未完成，请重试。"); }
  finally { if (active) submitting.value = false; }
}
async function canLeave() {
  if (saving.value || submitting.value) return false;
  if (!dirty.value) return true;
  try {
    await ElMessageBox.confirm("保存当前草稿后离开？", "内容尚未保存", { confirmButtonText: "保存草稿并离开", cancelButtonText: "放弃并离开", distinguishCancelAndClose: true, type: "warning" });
    return await saveDraft();
  } catch (action) { return action === "cancel"; }
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = ""; } }

onMounted(() => {
  void loadModelOptions();
  const id = typeof route.query.id === "string" ? route.query.id : "";
  if (id && !isPriceChange.value) void loadDraft(id);
  else if (!isPriceChange.value) { rows.value = [blankRow()]; savedBaseline.value = snapshot(); }
  window.addEventListener("beforeunload", beforeUnload);
});
onBeforeRouteLeave(canLeave);
onBeforeUnmount(() => { active = false; optionRequest?.abort(); draftRequest?.abort(); window.removeEventListener("beforeunload", beforeUnload); });
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / SUBMISSION</div>
    <h1>发起申报 · {{ kind }}</h1>
    <p>供应商只提交申请；办理状态、测试结论和接入结果以服务端返回为准。</p>
  </section>

  <section v-if="isPriceChange" class="panel price-change-panel">
    <el-alert title="价格变更会创建独立申报，并按当前主体和资源版本进入办理流程。" type="info" :closable="false" />
    <el-form label-position="top" :disabled="submitting" @submit.prevent="submitPriceChange">
      <el-form-item label="模型 / 资源"><el-input :model-value="`${modelName} · 资源 ${resourceID || '缺失'}`" disabled /></el-form-item>
      <el-form-item label="当前采购系数"><el-input :model-value="savedCoefficient || '—'" disabled /></el-form-item>
      <el-form-item label="新采购系数 *"><el-input v-model="coefficient" inputmode="decimal" placeholder="例如 0.6800" /></el-form-item>
      <el-form-item label="变更原因 *"><el-input v-model="reason" type="textarea" :rows="3" maxlength="500" show-word-limit /></el-form-item>
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <div class="form-actions"><el-button :disabled="submitting" @click="router.back()">返回</el-button><el-button type="primary" :loading="submitting" @click="submitPriceChange">提交价格变更</el-button></div>
    </el-form>
  </section>

  <section v-else class="panel submission-panel" v-loading="loadingDraft">
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-alert title="一份申报可包含多个模型。草稿阶段只保存申报行和测试记录；提交后由办理流程创建正式资源。" type="info" :closable="false" />
    <div class="options-search">
      <el-input v-model="modelKeyword" clearable placeholder="搜索标准模型名称 / 官方 ID" @keyup.enter="loadModelOptions" />
      <el-button :loading="modelLoading" @click="loadModelOptions">查询标准模型</el-button>
      <span v-if="modelError" class="inline-error">{{ modelError }}</span>
    </div>

    <article v-for="(row, index) in rows" :key="row.id || `new-${index}`" class="submission-row">
      <header class="row-heading"><h2>模型 {{ index + 1 }}</h2><el-tag v-if="row.id">{{ row.row_status }}</el-tag><el-button text type="danger" :disabled="locked || rows.length === 1" @click="removeRow(index)">移除</el-button></header>
      <div class="row-grid">
        <el-form-item label="接口模型 ID *"><el-input v-model="row.interface_model_id" placeholder="供应商侧接口模型 ID" :disabled="locked" /></el-form-item>
        <el-form-item label="对应标准模型 *"><el-select v-model="row.standard_model_id" filterable clearable :loading="modelLoading" placeholder="选择标准模型" :disabled="locked" @change="selectStandardModel(row, $event)"><el-option v-for="item in modelOptions" :key="item.id" :label="`${item.name} · ${item.official_id}`" :value="item.id" /></el-select><small v-if="row.standard_model_name" class="muted">{{ row.standard_model_name }} · {{ row.official_id }}</small></el-form-item>
        <el-form-item label="采购系数 *"><el-input v-model="row.coefficient" inputmode="decimal" placeholder="0.6800" :disabled="locked" /></el-form-item>
        <el-form-item label="测试服务 URL *"><el-input v-model="row.test_base_url" placeholder="https://api.example.com" :disabled="locked" /></el-form-item>
        <el-form-item label="测试 Key *"><el-input v-model="row.test_api_key" type="password" show-password autocomplete="new-password" placeholder="仅用于申报自测" :disabled="locked" /></el-form-item>
      </div>
      <div class="test-area">
        <div><strong>连通性：</strong>{{ testState(row.connectivity_tested, row.connectivity_passed) }}<span v-if="row.connectivity_job"> · {{ row.connectivity_job.status }}</span></div>
        <el-button size="small" :loading="testBusy[row.id + 'CONNECTIVITY']" :disabled="locked || dirty || !row.id" @click="runTest(row, 'CONNECTIVITY')">发起测试</el-button>
        <el-button v-if="row.connectivity_job" size="small" :loading="testBusy[row.id + 'CONNECTIVITY']" :disabled="locked || dirty" @click="refreshTest(row, 'CONNECTIVITY')">刷新结果</el-button>
        <div><strong>压力测试：</strong>{{ testState(row.stress_tested, row.stress_passed) }}<span v-if="row.stress_job"> · {{ row.stress_job.status }}</span></div>
        <el-button size="small" :loading="testBusy[row.id + 'STRESS']" :disabled="locked || dirty || !row.id" @click="runTest(row, 'STRESS')">发起测试</el-button>
        <el-button v-if="row.stress_job" size="small" :loading="testBusy[row.id + 'STRESS']" :disabled="locked || dirty" @click="refreshTest(row, 'STRESS')">刷新结果</el-button>
        <p v-for="category in testCategories" :key="category" v-show="testFor(row, category)" class="test-metrics">{{ category === 'CONNECTIVITY' ? '连通性' : '压力测试' }}：{{ testFor(row, category)?.connection_ms ? `${testFor(row, category)?.connection_ms} ms` : '' }} {{ testFor(row, category)?.rps ? `RPS ${testFor(row, category)?.rps}` : '' }} {{ testFor(row, category)?.tps ? `TPS ${testFor(row, category)?.tps}` : '' }}</p>
      </div>
    </article>
    <el-button :disabled="locked" @click="addRow">添加模型</el-button>

    <el-form-item label="申报说明 *"><el-input v-model="reason" type="textarea" :rows="3" maxlength="500" show-word-limit :disabled="locked" /></el-form-item>
    <el-alert title="批次提交要求每个模型都做过连通性测试，并且至少一个模型做过压力测试。提交条件表示已测试，不代表测试通过；每个资源仍须自身两项测试均通过后才能接入和报价。" type="warning" :closable="false" />
    <div class="form-actions">
      <el-button @click="router.push('/supplier/home')">返回</el-button>
      <el-button type="primary" :loading="saving" :disabled="locked || !dirty" @click="saveDraft">保存草稿</el-button>
      <el-button type="success" :loading="submitting" :disabled="locked || !canSubmitBatch" @click="submitDraft">提交申报</el-button>
    </div>
    <p class="muted security-note">测试 Key 仅用于申报自测，不会显示在详情或测试结果中；正式配置需在内部流程单独维护。</p>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-panel,.price-change-panel { display: grid; gap: 18px; padding: 22px; }
.options-search { display: flex; gap: 10px; align-items: center; }
.submission-row { border: 1px solid var(--app-border); border-radius: 8px; padding: 18px; }
.row-heading { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.row-heading h2 { margin: 0; font-size: 17px; flex: 1; }
.row-grid { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 0 18px; }
.row-grid .el-select { width: 100%; }
.test-area { display: grid; grid-template-columns: 1fr auto auto; gap: 10px; align-items: center; border-top: 1px solid var(--app-border); padding-top: 14px; }
.test-metrics { grid-column: 1/-1; margin: 0; color: var(--app-text-muted); font-size: 12px; }
.muted,.inline-error { color: var(--app-text-muted); font-size: 12px; }
.inline-error { color: #b42318; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; }
.security-note { margin: 0; }
@media(max-width: 760px) { .row-grid { grid-template-columns: 1fr; } .test-area { grid-template-columns: 1fr auto; } .test-area :deep(.el-button:nth-of-type(2n)) { grid-column: 2; } }
</style>
