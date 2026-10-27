<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierModelsApi } from "../../api/supplierModels";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import { supplierTestCredentialApi, type SupplierTestCredentialDTO } from "../../api/supplierTestCredential";
import { ApiError } from "../../domain/common";
import type { SupplierImportPreviewDTO, SupplierModelOptionDTO, SupplierSubmissionDTO, SupplierSubmissionRowDTO, SupplierSubmissionTestDTO, SupplierSubmissionWriteRow, SupplierTestCategory, SupplierTestJobDTO } from "../../api/supplierSubmissions.types";

type EditableRow = SupplierSubmissionRowDTO;
const route = useRoute();
const router = useRouter();
const kind = computed(() => route.query.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增");
const isPriceChange = computed(() => route.query.kind === "PRICE_CHANGE");
const resourceID = computed(() => typeof route.query.resource_id === "string" ? route.query.resource_id : "");
const expectedRevision = computed(() => typeof route.query.expected_revision === "string" ? route.query.expected_revision : "");
const modelName = computed(() => typeof route.query.model_name === "string" ? route.query.model_name : "当前模型");
const savedCoefficient = computed(() => typeof route.query.coefficient === "string" ? route.query.coefficient : "");
const coefficient = ref(savedCoefficient.value);
// 申报说明是选填（2026-10-27 确认）：草稿和提交都不拦，详情里空值显示「无补充说明」。
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
// 草稿并发编辑（409）：提示用户先取最新版本再重试，输入一律保留。
const draftConflict = ref(false), draftRefreshed = ref(false), latestDraftReason = ref("");
let savedRowIDs: string[] = [];
const dirty = computed(() => !!savedBaseline.value && savedBaseline.value !== snapshot());
const locked = computed(() => !canEdit.value || saving.value || submitting.value || loadingDraft.value);
const testCategories: SupplierTestCategory[] = ["CONNECTIVITY", "STRESS"];
let active = true;
let optionRequest: AbortController | undefined;
let draftRequest: AbortController | undefined;
const testBusy = ref<Record<string, boolean>>({});
const batchBusy = ref<"" | "ALL">("");

// ---- 主体级测试凭据（000044 / 原型顶部凭据区）----
const credential = ref<SupplierTestCredentialDTO>({ base_url: "", has_api_key: false, configured: false });
const credentialURL = ref("");
const credentialKey = ref("");
const credentialLoading = ref(false);
const credentialSaving = ref(false);
const credentialNote = computed(() => {
  if (!credential.value.configured) return "尚未登记测试凭据：发起测试前需先保存一次。";
  return credential.value.has_api_key ? "已登记测试凭据，Key 只写不读。" : "已登记 URL，还没有保存 Key。";
});

async function loadCredential() {
  credentialLoading.value = true;
  try {
    const dto = await supplierTestCredentialApi.get();
    if (!active) return;
    credential.value = dto;
    credentialURL.value = dto.base_url || "";
  } catch (value) {
    if (active) showError(value, "测试凭据暂时无法读取，可稍后重试。");
  } finally {
    if (active) credentialLoading.value = false;
  }
}

async function saveCredential() {
  if (credentialSaving.value) return;
  const url = credentialURL.value.trim();
  if (!url) { error.value = "请填写测试接口 URL。"; return; }
  credentialSaving.value = true;
  error.value = "";
  try {
    const dto = await supplierTestCredentialApi.save(
      credentialKey.value.trim() ? { base_url: url, api_key: credentialKey.value.trim() } : { base_url: url },
      crypto.randomUUID(),
    );
    if (!active) return;
    credential.value = dto;
    credentialURL.value = dto.base_url;
    credentialKey.value = "";
    ElMessage.success("测试凭据已保存");
    // 凭据参与指纹：刷新一次行状态，否则「已失效」要等下次进入页面才更新。
    if (submissionID.value) await reloadKeepingKeys();
  } catch (value) {
    if (active) showError(value, "测试凭据保存失败，请重试。");
  } finally {
    if (active) credentialSaving.value = false;
  }
}

// ---- 门禁（原型口径：覆盖全部连通 + 至少一个压力模型，不看是否通过）----
const connectivityMissing = computed(() => rows.value.filter(row => !row.connectivity_tested).length);
const selectedStressCount = computed(() => rows.value.filter(row => row.participate_test).length);
const pendingStress = computed(() => rows.value.filter(row => row.participate_test && !row.stress_tested).length);
const gateError = computed(() => {
  if (!rows.value.length) return "请至少添加一条模型。";
  if (connectivityMissing.value > 0) return `还有 ${connectivityMissing.value} 个模型没有连通性测试结果，请先发起测试。`;
  if (selectedStressCount.value === 0) return "请至少勾选一个压力测试模型。";
  if (pendingStress.value > 0) return `勾选的模型里还有 ${pendingStress.value} 个没有压力测试结果，请先发起测试。`;
  return "";
});
const canSubmitBatch = computed(() => !gateError.value);

function blankRow(): EditableRow {
  return { id: "", interface_model_id: "", standard_model_id: "", standard_model_name: "", official_id: "", coefficient: "1", test_base_url: "", test_api_key_configured: false, row_status: "DRAFT", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false };
}
function snapshot() {
  // 凭据不参与脏检查：它是主体资源，改了也不影响「这份申报还没保存」的判断。
  return JSON.stringify({ reason: reason.value, rows: rows.value.map(row => ({ id: row.id, interface_model_id: row.interface_model_id, standard_model_id: row.standard_model_id, coefficient: row.coefficient, participate_test: !!row.participate_test })) });
}
function rememberSaved() { savedBaseline.value = snapshot(); lifecycle.value = crypto.randomUUID(); }
function normalizeSaved(dto: SupplierSubmissionDTO) {
  draftConflict.value = false; draftRefreshed.value = false; latestDraftReason.value = "";
  savedRowIDs = dto.rows.map(row => row.id).sort();
  submissionID.value = dto.id;
  revision.value = dto.revision;
  canEdit.value = dto.permissions.can_edit;
  reason.value = dto.reason || reason.value;
  rows.value = dto.rows;
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
  }
  return "";
}
// 打开下拉时才加载：避免页面初次加载遇到后端未就绪就永久空白。
function ensureModelOptions(visible: boolean) {
  if (visible && !modelOptions.value.length && !modelLoading.value) void loadModelOptions();
}
function searchModelOptions(keyword: string) { modelKeyword.value = keyword; void loadModelOptions(); }
async function loadModelOptions() {
  optionRequest?.abort();
  const request = optionRequest = new AbortController();
  modelLoading.value = true;
  modelError.value = "";
  try {
    const result = await supplierSubmissionsApi.modelOptions(modelKeyword.value, request.signal);
    if (active && !request.signal.aborted) modelOptions.value = result;
  } catch (value) {
    if (active && !request.signal.aborted) {
      modelOptions.value = [];
      modelError.value = `${value instanceof Error ? value.message : "标准模型目录加载失败"}（请确认后端已启动，展开下拉可重试）`;
    }
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
    normalizeSaved(result);
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
  // 000044：行级 URL/Key 已废弃，凭据走 /supplier/test-credential。
  const payloadRows: SupplierSubmissionWriteRow[] = rows.value.map(row => ({
    ...(row.id ? { id: row.id } : {}),
    interface_model_id: row.interface_model_id.trim(),
    standard_model_id: row.standard_model_id,
    coefficient: row.coefficient.trim(),
    participate_test: !!row.participate_test,
  }));
  return { kind: "MODEL_NEW" as const, ...(submissionID.value ? { expected_revision: revision.value } : {}), reason: reason.value.trim(), rows: payloadRows };
}
async function saveDraft(): Promise<boolean> {
  if (saving.value || submitting.value || loadingDraft.value || !canEdit.value || draftConflict.value) return false;
  const issue = validateRows();
  if (issue) { error.value = issue; return false; }
  saving.value = true;
  error.value = "";
  try {
    const payload = writePayload();
    const result = submissionID.value
      ? await supplierSubmissionsApi.update(submissionID.value, payload, lifecycle.value)
      : await supplierSubmissionsApi.create(payload, lifecycle.value);
    if (!active) return false;
    normalizeSaved(result);
    ElMessage.success("草稿已保存");
    return true;
  } catch (value) {
    if (active) {
      if (value instanceof ApiError && value.httpStatus === 409) { draftConflict.value = true; draftRefreshed.value = false; }
      showError(value, "草稿保存失败，输入内容已保留。");
    }
    return false;
  } finally { if (active) saving.value = false; }
}
/**
 * 草稿被别人改过（409）时，只允许「取最新版本」而不是直接覆盖：
 * 先比对行 ID 集合，行结构变了就要求用户重新打开草稿核对，避免静默丢掉别人的改动。
 */
async function refreshDraftConflict() {
  if (!draftConflict.value || !submissionID.value || locked.value) return;
  loadingDraft.value = true;
  try {
    const latest = await supplierSubmissionsApi.get(submissionID.value);
    if (!active) return;
    if (latest.id !== submissionID.value || latest.kind !== "MODEL_NEW" || !latest.permissions.can_edit) throw new Error("当前申报已不能编辑，请从申报记录重新打开；本次输入仍保留。");
    if (JSON.stringify(latest.rows.map(row => row.id).sort()) !== JSON.stringify(savedRowIDs)) throw new Error("服务端模型行已变化，请复制本次输入并重新打开草稿核对，不能直接覆盖。当前输入已保留。");
    revision.value = latest.revision;
    latestDraftReason.value = latest.reason || "未填写";
    lifecycle.value = crypto.randomUUID();
    draftConflict.value = false; draftRefreshed.value = true; error.value = "";
  } catch (value) { if (active) showError(value, "刷新失败，草稿已保留。"); }
  finally { if (active) loadingDraft.value = false; }
}
function applyTest(row: EditableRow, result: SupplierTestJobDTO) {
  const test: SupplierSubmissionTestDTO = { category: result.category, status: result.status, tested: result.tested, passed: result.passed, job_id: result.job_id, report_id: result.report_id, connection_ms: result.connection_ms, rps: result.rps, tps: result.tps };
  if (result.category === "CONNECTIVITY") { row.connectivity_job = test; row.connectivity_tested = result.tested; row.connectivity_passed = result.passed; }
  else { row.stress_job = test; row.stress_tested = result.tested; row.stress_passed = result.passed; }
}
async function refreshRow(row: EditableRow) {
  if (!submissionID.value || !row.id) return;
  const tasks: Promise<void>[] = [];
  if (row.connectivity_job) tasks.push(refreshTest(row, "CONNECTIVITY"));
  if (row.stress_job) tasks.push(refreshTest(row, "STRESS"));
  await Promise.all(tasks);
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
// 压测没有 verdict：测完只能算「已完成」，不能显示成「未通过」。
function testState(tested: boolean, passed: boolean, verdict?: string) {
  if (!tested) return "未测试";
  if (passed) return "通过";
  if (!verdict) return "已完成";
  return "未通过";
}
// 压测没有 verdict：测完只能算「已完成」，不能显示成「未通过」——颜色也得中性。
function testType(tested: boolean, passed: boolean, verdict?: string): "success" | "danger" | "info" {
  if (!tested) return "info";
  if (passed) return "success";
  if (!verdict) return "info";
  return "danger";
}
function rowMetrics(row: EditableRow) {
  const parts: string[] = [];
  if (row.connectivity_job?.connection_ms) parts.push(`${row.connectivity_job.connection_ms} ms`);
  if (row.stress_job?.rps) parts.push(`RPS ${row.stress_job.rps}`);
  if (row.stress_job?.tps) parts.push(`TPS ${row.stress_job.tps}`);
  return parts.join(" · ");
}
async function reloadKeepingKeys() {
  if (!submissionID.value) return;
  const result = await supplierSubmissionsApi.get(submissionID.value);
  if (active) normalizeSaved(result);
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
  if (!canSubmitBatch.value) { error.value = `还不能提交：${gateError.value}`; return; }
  submitting.value = true;
  error.value = "";
  try {
    await supplierSubmissionsApi.submit(submissionID.value, revision.value, reason.value, lifecycle.value);
    if (!active) return;
    // 提交成功后这份草稿就不该再被改：先落基线，再锁编辑态，避免返回前又点一次保存。
    rememberSaved();
    canEdit.value = false;
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
  void loadCredential();
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
    <el-alert v-if="draftConflict" title="草稿版本已变化，当前输入已保留。请先刷新最新版本，再核对后重试。" type="warning" :closable="false"><el-button text :disabled="locked" @click="refreshDraftConflict">刷新最新版本并保留草稿</el-button></el-alert>
    <el-alert v-if="draftRefreshed" :title="`已取得最新版本 R${revision}；服务端说明：${latestDraftReason}。当前表单仍为本次草稿，请核对后重试。`" type="warning" :closable="false" />
    <el-alert v-if="modelError" :title="modelError" type="error" :closable="false" show-icon />

    <!-- 主体级测试凭据（原型表单顶部） -->
    <div class="credential-block" v-loading="credentialLoading">
      <p class="credential-note">此 URL／Key 仅由供应商本人维护，用于本主体准入测试、供申报模型共用；不用于平台运营测试。</p>
      <div class="credential-grid">
        <el-form-item label="测试接口 URL">
          <el-input v-model="credentialURL" placeholder="https://api.example.com/v1（填 demo 使用平台演示模型）" :disabled="credentialSaving" />
        </el-form-item>
        <el-form-item label="测试 API Key（留空保留已保存值）">
          <el-input v-model="credentialKey" type="password" show-password autocomplete="new-password" :placeholder="credential.has_api_key ? '已保存；填写新 Key 可替换' : '供应商自测使用的 Key'" :disabled="credentialSaving" />
        </el-form-item>
      </div>
      <div class="credential-actions">
        <el-button size="small" :loading="credentialSaving" @click="saveCredential">保存测试凭据</el-button>
        <span class="credential-status" :class="{ 'is-missing': !credential.configured }">{{ credentialNote }}</span>
      </div>
    </div>

    <div class="batch-actions">
      <el-button size="small" :loading="importBusy" :disabled="locked" @click="downloadTemplate">下载导入模板</el-button>
      <el-button size="small" :disabled="locked" @click="triggerImport">导入 Excel</el-button>
      <input ref="importInput" type="file" accept=".xlsx" hidden @change="onImportFile" />
      <span class="batch-hint">所有模型都会做连通性测试；压力测试只做勾选的模型（已勾 {{ selectedStressCount }} 个，待测 {{ pendingStress }} 个）。</span>
    </div>

    <el-table :data="rows" row-key="id" class="submission-table" empty-text="还没有模型，点「添加模型」开始">
      <el-table-column label="对应标准模型" min-width="260">
        <template #default="{ row }">
          <el-select v-model="row.standard_model_id" filterable clearable remote :remote-method="searchModelOptions" :loading="modelLoading" placeholder="选择对应标准模型" :disabled="locked" @change="selectStandardModel(row, $event)" @visible-change="ensureModelOptions">
            <el-option v-for="item in modelOptions" :key="item.id" :label="`${item.name} · ${item.official_id}`" :value="item.id" />
          </el-select>
          <small v-if="row.standard_model_name" class="muted">{{ row.standard_model_name }} · {{ row.official_id }}</small>
        </template>
      </el-table-column>
      <el-table-column label="接口模型 ID" min-width="200">
        <template #default="{ row }"><el-input v-model="row.interface_model_id" placeholder="供应商侧接口模型 ID" :disabled="locked" /></template>
      </el-table-column>
      <el-table-column label="采购系数" width="130">
        <template #default="{ row }"><el-input v-model="row.coefficient" inputmode="decimal" placeholder="0.6800" :disabled="locked" /></template>
      </el-table-column>
      <el-table-column label="压力测试" width="100" align="center">
        <template #default="{ row }"><el-checkbox v-model="row.participate_test" :disabled="locked">参与</el-checkbox></template>
      </el-table-column>
      <el-table-column label="测试" min-width="230">
        <template #default="{ row }">
          <div class="test-cell">
            <span class="test-pill"><el-tag size="small" :type="testType(row.connectivity_tested, row.connectivity_passed, row.connectivity_job?.verdict)">连通 {{ testState(row.connectivity_tested, row.connectivity_passed, row.connectivity_job?.verdict) }}</el-tag></span>
            <span v-if="row.participate_test || row.stress_job" class="test-pill"><el-tag size="small" :type="testType(row.stress_tested, row.stress_passed, row.stress_job?.verdict)">压力 {{ testState(row.stress_tested, row.stress_passed, row.stress_job?.verdict) }}</el-tag></span>
            <small v-if="rowMetrics(row)" class="muted">{{ rowMetrics(row) }}</small>
            <el-button link type="primary" size="small" :disabled="locked || dirty || (!row.connectivity_job && !row.stress_job)" @click="refreshRow(row)">刷新结果</el-button>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.id" size="small" :type="rowStatusType(row.row_status)">{{ rowStatusLabel(row.row_status) }}</el-tag>
          <span v-else class="muted">未保存</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="80" align="center">
        <template #default="{ $index }"><el-button link type="danger" :disabled="locked" @click="removeRow($index)">删除</el-button></template>
      </el-table-column>
    </el-table>

    <p class="rule-note">所有模型均执行连通性测试；至少勾选一个压力测试模型。同一受测配置一次有效测试；配置变化后可重测，仅改报价不增加机会。工具异常的未完成部分可重试。结果不达标也属于有效结果。</p>

    <div class="form-actions form-actions--inline">
      <el-button :disabled="locked" @click="addRow">添加模型</el-button>
      <el-button type="primary" :loading="batchBusy === 'ALL'" :disabled="locked || !rows.length" @click="runBatch">发起测试／异常重试</el-button>
      <el-button :loading="saving" :disabled="locked || !dirty || draftConflict" @click="saveDraft">{{ draftRefreshed ? '按最新版本重试' : '保存草稿' }}</el-button>
      <el-button type="success" :loading="submitting" :disabled="locked || !canSubmitBatch || draftConflict" @click="submitDraft">提交申报</el-button>
    </div>

    <el-form-item label="申报说明">
      <el-input v-model="reason" type="textarea" :rows="3" maxlength="500" show-word-limit :disabled="locked" placeholder="选填：补充给办理方的信息" />
    </el-form-item>
    <el-alert v-if="!canSubmitBatch" :title="`尚不能提交：${gateError}`" type="warning" :closable="false" show-icon />
    <el-alert v-else title="可以提交：覆盖了全部连通性测试并至少有一个压力测试模型。" type="success" :closable="false" show-icon />

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
    </div>
    <p class="muted security-note">测试凭据由主体统一维护，发起测试时按当前凭据计算指纹；换了 URL 或 Key 之后，旧结果会标记为「已失效」需要重测。内部人员发起测试使用平台自己的凭据与额度，与供应商凭据分开。</p>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-panel,.price-change-panel { display: grid; gap: 18px; padding: 22px; }
.credential-block { border: 1px solid var(--app-border); border-radius: 8px; padding: 16px 18px; display: grid; gap: 10px; }
.credential-note { margin: 0; color: var(--app-text-muted); font-size: 12px; line-height: 1.6; }
.credential-grid { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 0 18px; }
.credential-actions { display: flex; align-items: center; gap: 12px; }
.credential-status { color: var(--app-text-muted); font-size: 12px; }
.credential-status.is-missing { color: #b42318; }
.batch-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.batch-hint { color: var(--app-text-muted); font-size: 12px; }
.submission-table :deep(.el-select) { width: 100%; }
.test-cell { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.test-pill { display: inline-flex; }
.rule-note { margin: 0; color: var(--app-text-muted); font-size: 12px; line-height: 1.7; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; }
.form-actions--inline { justify-content: flex-start; flex-wrap: wrap; }
.muted,.inline-error { color: var(--app-text-muted); font-size: 12px; }
.inline-error { color: #b42318; }
.security-note { margin: 0; }
@media(max-width: 900px) { .credential-grid { grid-template-columns: 1fr; } }
</style>
