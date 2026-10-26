<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierModelsApi } from "../../api/supplierModels";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import type { SupplierImportPreviewDTO, SupplierModelOptionDTO, SupplierSubmissionDTO, SupplierSubmissionRowDTO, SupplierSubmissionTestDTO, SupplierSubmissionWriteRow, SupplierTestCategory, SupplierTestJobDTO } from "../../api/supplierSubmissions.types";

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
// 折中口径：至少一行连通「通过」即可提交；其余行提交后标为待补。
const canSubmitBatch = computed(() => rows.value.some(row => row.connectivity_tested && row.connectivity_passed && !row.connectivity_job?.stale));
const locked = computed(() => !canEdit.value || saving.value || submitting.value || loadingDraft.value);
const testCategories: SupplierTestCategory[] = ["CONNECTIVITY", "STRESS"];
let active = true;
let optionRequest: AbortController | undefined;
let draftRequest: AbortController | undefined;
const testBusy = ref<Record<string, boolean>>({});
const batchBusy = ref<"" | "ALL">("");
// 门禁提示：把"哪一行还差什么"说清楚，而不是只让按钮变灰。
const missingConnectivity = computed(() => rows.value.map((row, index) => ({ row, index })).filter(item => !item.row.connectivity_tested).map(item => item.index + 1));
const passedRowCount = computed(() => rows.value.filter(row => row.connectivity_tested && row.connectivity_passed && !row.connectivity_job?.stale).length);
const pendingConnectivity = computed(() => rows.value.filter(row => !row.connectivity_job || row.connectivity_job.stale).length);
const selectedStressCount = computed(() => rows.value.filter(row => row.participate_test).length);
const pendingStress = computed(() => rows.value.filter(row => row.participate_test && !row.stress_job).length);
const gateHint = computed(() => {
  if (!rows.value.length) return "请至少添加一条模型。";
  if (passedRowCount.value === 0) return "至少需要一行连通测试通过（结果达标且未失效）。";
  const issues: string[] = [];
  if (selectedStressCount.value === 0) issues.push("建议至少勾选一个模型做压力测试。");
  const pending = rows.value.length - passedRowCount.value;
  if (pending > 0) issues.push(`其余 ${pending} 个模型提交后会标为「待补」。`);
  return issues.join("");
});
function blankRow(): EditableRow {
  return { id: "", interface_model_id: "", standard_model_id: "", standard_model_name: "", official_id: "", coefficient: "1", test_base_url: "", test_api_key_configured: false, test_api_key: "", row_status: "DRAFT", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false };
}
function hasSavedTestKey(row: EditableRow) {
  // Older drafts had a required key and no presence flag in their response.
  return row.test_api_key_configured ?? Boolean(row.id);
}
function snapshot() {
  return JSON.stringify({ reason: reason.value, rows: rows.value.map(row => ({ id: row.id, interface_model_id: row.interface_model_id, standard_model_id: row.standard_model_id, coefficient: row.coefficient, test_base_url: row.test_base_url, test_api_key: row.test_api_key, participate_test: !!row.participate_test })) });
}
function rememberSaved() { savedBaseline.value = snapshot(); lifecycle.value = crypto.randomUUID(); }
function normalizeSaved(dto: SupplierSubmissionDTO, previousRows = rows.value) {
  submissionID.value = dto.id;
  revision.value = dto.revision;
  canEdit.value = dto.permissions.can_edit;
  reason.value = dto.reason || reason.value;
  rows.value = dto.rows.map((item, index) => {
    const previouslyConfigured = previousRows[index]?.test_api_key_configured;
    const newlyEnteredKey = Boolean(previousRows[index]?.test_api_key.trim());
    return {
      ...item,
      test_api_key_configured: item.test_api_key_configured ?? (previouslyConfigured || newlyEnteredKey || Boolean(item.id)),
      test_api_key: "",
    };
  });
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
    if (!hasSavedTestKey(row) && !row.test_api_key.trim()) return `${line}请填写用于自测的 Key。`;
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
    participate_test: !!row.participate_test,
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
// 行状态中文文案（后端派生值 → 原型文案）
const ROW_STATUS_LABELS: Record<string, string> = {
  UNTESTED: "未测试", RUNNING: "测试中", STALE: "已失效",
  PASSED: "已通过", FAILED: "未通过", SUPPLEMENT: "待补", PENDING: "待测试", DRAFT: "草稿",
};
function rowStatusLabel(value: string) { return ROW_STATUS_LABELS[value] || value; }
function rowStatusType(value: string): "success" | "warning" | "info" | "danger" {
  switch (value) {
    case "PASSED": return "success";
    case "FAILED": return "danger";
    case "STALE": return "warning";
    case "RUNNING": return "warning";
    default: return "info";
  }
}
function testState(tested: boolean, passed: boolean) { return !tested ? "未测试" : passed ? "通过" : "已测试，未通过"; }
function testFor(row: EditableRow, category: SupplierTestCategory) { return category === "CONNECTIVITY" ? row.connectivity_job : row.stress_job; }
async function reloadKeepingKeys() {
  if (!submissionID.value) return;
  const result = await supplierSubmissionsApi.get(submissionID.value);
  if (active) normalizeSaved(result, rows.value);
}
// 单一入口「发起测试／异常重试」：连通测全部行，压力只测勾选行。
async function runBatch() {
  if (batchBusy.value || !rows.value.length) return;
  if (!submissionID.value || dirty.value) {
    if (!(await saveDraft())) return;
  }
  if (!submissionID.value) { error.value = "草稿尚未保存，请先保存后再测试。"; return; }
  batchBusy.value = "ALL";
  error.value = "";
  try {
    const result = await supplierSubmissionsApi.batchTest(submissionID.value, ["CONNECTIVITY", "STRESS"], lifecycle.value);
    if (!active) return;
    // 每次发起后轮换 lifecycle：否则幂等中间件会把上次响应原样回放，按钮像没反应。
    lifecycle.value = crypto.randomUUID();
    await reloadKeepingKeys();
    const parts = [`已发起 ${result.started} 个`];
    if (result.retried) parts.push(`重测 ${result.retried} 个`);
    if (result.skipped) parts.push(`跳过 ${result.skipped} 个（已有结果或正在跑）`);
    if (result.not_selected) parts.push(`${result.not_selected} 个未勾选压力测试`);
    if (result.failed) parts.push(`${result.failed} 个失败`);
    ElMessage.success(parts.join("，"));
  } catch (value) {
    if (active) showError(value, "测试未能启动，请重试。");
  } finally { if (active) batchBusy.value = ""; }
}

// ---- Excel 导入 ----
const importBusy = ref(false);
const importInput = ref<HTMLInputElement | null>(null);
const importPreview = ref<SupplierImportPreviewDTO | null>(null);
const importDialog = ref(false);

function triggerImport() { importInput.value?.click(); }
async function downloadTemplate() {
  try {
    const blob = await supplierSubmissionsApi.importTemplate();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "申报导入模板.xlsx";
    link.click();
    URL.revokeObjectURL(url);
  } catch (value) { showError(value, "模板下载失败。"); }
}
async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  importBusy.value = true;
  error.value = "";
  try {
    const preview = await supplierSubmissionsApi.parseImport(file);
    importPreview.value = preview;
    importDialog.value = true;
  } catch (value) {
    showError(value, "文件解析失败，请确认是模板格式的 xlsx。");
  } finally { importBusy.value = false; }
}
function applyImport() {
  const preview = importPreview.value;
  if (!preview) return;
  const accepted = preview.rows.filter(row => row.ok && row.standard_model_id);
  for (const item of accepted) {
    rows.value.push({
      ...blankRow(),
      interface_model_id: item.interface_model_id,
      standard_model_id: String(item.standard_model_id),
      standard_model_name: item.standard_model_name || "",
      official_id: item.official_id || "",
      coefficient: item.coefficient,
      participate_test: item.participate_test,
    });
  }
  importDialog.value = false;
  importPreview.value = null;
  ElMessage.success(`已导入 ${accepted.length} 行，请核对后再保存草稿。`);
}

async function submitDraft() {
  if (submitting.value || !canEdit.value) return;
  if (dirty.value || !submissionID.value) {
    if (!(await saveDraft())) return;
  }
  if (!submissionID.value || !revision.value) { error.value = "草稿尚未保存，请先保存后再提交。"; return; }
  if (!canSubmitBatch.value) { error.value = `还不能提交：${gateHint.value}`; return; }
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

    <div class="batch-actions">
      <el-button size="small" type="primary" :loading="batchBusy === 'ALL'" :disabled="locked || !rows.length" @click="runBatch">发起测试／异常重试</el-button>
      <el-button size="small" :loading="importBusy" :disabled="locked" @click="downloadTemplate">下载导入模板</el-button>
      <el-button size="small" :disabled="locked" @click="triggerImport">导入 Excel</el-button>
      <input ref="importInput" type="file" accept=".xlsx" hidden @change="onImportFile" />
      <span class="batch-hint">所有模型都会做连通测试；压力测试只做勾选的模型（已勾 {{ selectedStressCount }} 个，待测 {{ pendingStress }} 个）。</span>
    </div>

    <article v-for="(row, index) in rows" :key="row.id || `new-${index}`" class="submission-row">
      <header class="row-heading">
        <h2>模型 {{ index + 1 }}</h2>
        <el-tag v-if="row.id" :type="rowStatusType(row.row_status)">{{ rowStatusLabel(row.row_status) }}</el-tag>
        <el-tag v-if="row.supplement_required" type="warning" effect="plain">待补</el-tag>
        <el-button text type="danger" :disabled="locked || rows.length === 1" @click="removeRow(index)">移除</el-button>
      </header>
      <div class="row-grid">
        <el-form-item label="接口模型 ID *"><el-input v-model="row.interface_model_id" placeholder="供应商侧接口模型 ID" :disabled="locked" /></el-form-item>
        <el-form-item label="对应标准模型 *"><el-select v-model="row.standard_model_id" filterable clearable :loading="modelLoading" placeholder="选择标准模型" :disabled="locked" @change="selectStandardModel(row, $event)"><el-option v-for="item in modelOptions" :key="item.id" :label="`${item.name} · ${item.official_id}`" :value="item.id" /></el-select><small v-if="row.standard_model_name" class="muted">{{ row.standard_model_name }} · {{ row.official_id }}</small></el-form-item>
        <el-form-item label="采购系数 *"><el-input v-model="row.coefficient" inputmode="decimal" placeholder="0.6800" :disabled="locked" /></el-form-item>
        <el-form-item label="压力测试"><el-checkbox v-model="row.participate_test" :disabled="locked">该模型参与压力测试</el-checkbox></el-form-item>
        <el-form-item label="测试服务 URL *"><el-input v-model="row.test_base_url" placeholder="https://api.example.com" :disabled="locked" /></el-form-item>
        <el-form-item :label="hasSavedTestKey(row) ? '测试 Key（留空沿用）' : '测试 Key *'"><el-input v-model="row.test_api_key" type="password" show-password autocomplete="new-password" :placeholder="hasSavedTestKey(row) ? '已保存；填写新 Key 可替换' : '供应商自测使用的 Key'" :disabled="locked" /></el-form-item>
      </div>
      <div class="test-area">
        <div><strong>连通性：</strong>{{ testState(row.connectivity_tested, row.connectivity_passed) }}<span v-if="row.connectivity_job"> · {{ row.connectivity_job.status }}</span></div>
        <el-button v-if="row.connectivity_job" size="small" :loading="testBusy[row.id + 'CONNECTIVITY']" :disabled="locked || dirty" @click="refreshTest(row, 'CONNECTIVITY')">刷新结果</el-button>
        <div><strong>压力测试：</strong>{{ testState(row.stress_tested, row.stress_passed) }}<span v-if="row.stress_job"> · {{ row.stress_job.status }}</span></div>
        <el-button v-if="row.stress_job" size="small" :loading="testBusy[row.id + 'STRESS']" :disabled="locked || dirty" @click="refreshTest(row, 'STRESS')">刷新结果</el-button>
        <p v-for="category in testCategories" :key="category" v-show="testFor(row, category)" class="test-metrics">{{ category === 'CONNECTIVITY' ? '连通性' : '压力测试' }}：{{ testFor(row, category)?.connection_ms ? `${testFor(row, category)?.connection_ms} ms` : '' }} {{ testFor(row, category)?.rps ? `RPS ${testFor(row, category)?.rps}` : '' }} {{ testFor(row, category)?.tps ? `TPS ${testFor(row, category)?.tps}` : '' }}</p>
      </div>
    </article>
    <el-button :disabled="locked" @click="addRow">添加模型</el-button>

    <el-form-item label="申报说明 *"><el-input v-model="reason" type="textarea" :rows="3" maxlength="500" show-word-limit :disabled="locked" /></el-form-item>
    <el-alert v-if="!canSubmitBatch" :title="`尚不能提交：${gateHint}`" type="warning" :closable="false" show-icon />
    <el-alert v-else :title="`可以提交。${gateHint}`" type="success" :closable="false" show-icon />
    <p class="muted">规则：所有模型均执行连通性测试；同一受测配置一次有效测试，配置变化后可重测；仅改报价不增加测试机会；工具异常的未完成部分可重试；结果不达标也属于有效结果。</p>

    <el-dialog v-model="importDialog" title="导入预览" width="760px">
      <template v-if="importPreview">
        <p class="muted">共 {{ importPreview.total }} 行，可导入 {{ importPreview.ok_count }} 行，错误 {{ importPreview.error_count }} 行。</p>
        <el-table :data="importPreview.rows" size="small" max-height="380" style="width: 100%">
          <el-table-column prop="line" label="行" width="60" />
          <el-table-column prop="standard_model_name" label="标准模型" min-width="140" />
          <el-table-column prop="interface_model_id" label="接口模型 ID" min-width="140" />
          <el-table-column prop="coefficient" label="系数" width="80" />
          <el-table-column label="压力" width="70"><template #default="{ row }">{{ row.participate_test ? "是" : "否" }}</template></el-table-column>
          <el-table-column label="校验" min-width="220">
            <template #default="{ row }">
              <span v-if="row.ok" class="muted">通过</span>
              <span v-else class="inline-error">{{ row.errors.join("；") }}</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
      <template #footer>
        <el-button @click="importDialog = false">取消</el-button>
        <el-button type="primary" :disabled="!importPreview?.ok_count" @click="applyImport">导入 {{ importPreview?.ok_count || 0 }} 行</el-button>
      </template>
    </el-dialog>
    <div class="form-actions">
      <el-button @click="router.push('/supplier/home')">返回</el-button>
      <el-button type="primary" :loading="saving" :disabled="locked || !dirty" @click="saveDraft">保存草稿</el-button>
      <el-button type="success" :loading="submitting" :disabled="locked || !canSubmitBatch" @click="submitDraft">提交申报</el-button>
    </div>
    <p class="muted security-note">供应商自测使用本行测试 URL / Key 和供应商额度；保存后发起测试可复用配置，Key 不回显。内部人员发起测试使用平台自己的 URL / Key 和平台额度，与供应商凭据分开。正式运行配置仍单独维护。</p>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-panel,.price-change-panel { display: grid; gap: 18px; padding: 22px; }
.options-search { display: flex; gap: 10px; align-items: center; }
.batch-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.batch-hint { color: var(--app-text-muted); font-size: 12px; }
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
