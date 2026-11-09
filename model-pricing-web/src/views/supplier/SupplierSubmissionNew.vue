<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import { supplierTestConfigGroupsApi, type SupplierTestConfigGroupDTO } from "../../api/supplierTestConfigGroups";
import { ApiError } from "../../domain/common";
import type { SupplierImportPreviewDTO, SupplierModelOptionDTO, SupplierSubmissionDTO, SupplierSubmissionRowDTO, SupplierSubmissionTestDTO, SupplierSubmissionWriteRow, SupplierTestCategory, SupplierTestJobDTO, SupplierTestProgressRowDTO } from "../../api/supplierSubmissions.types";

type EditableRow = SupplierSubmissionRowDTO;
const route = useRoute();
const router = useRouter();
const kind = computed(() => route.query.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增");
const isPriceChange = computed(() => route.query.kind === "PRICE_CHANGE");
const resourceID = computed(() => typeof route.query.resource_id === "string" ? route.query.resource_id : "");
const expectedRevision = computed(() => typeof route.query.expected_revision === "string" ? route.query.expected_revision : "");
const modelName = computed(() => typeof route.query.model_name === "string" ? route.query.model_name : "当前模型");
const savedCoefficient = computed(() => typeof route.query.coefficient === "string" ? route.query.coefficient : "");
// 价格变更申报要带标准模型与供方模型名，否则后端建行时缺字段。
const standardModelID = computed(() => typeof route.query.standard_model_id === "string" ? route.query.standard_model_id : "");
const supplierModelID = computed(() => typeof route.query.supplier_model_id === "string" ? route.query.supplier_model_id : "");
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

// ---- 准入测试配置组（000052 / 原型顶部卡片区）----
//
// 一个主体可以有多组 URL/Key（如「原单组配置」「华东通道」），模型行按行挂组。
// Key 只写不读：编辑卡片时不回显，提交时不传 api_key 表示保持不变。
const groups = ref<SupplierTestConfigGroupDTO[]>([]);
const groupsLoading = ref(false);
const groupSaving = ref(false);
const groupDialog = ref(false);
const groupEditingID = ref("");
const groupForm = ref({ name: "", base_url: "", api_key: "" });
// 弹窗内错误。**不能复用页面顶部的 error**：那片区域在弹窗打开时被完全遮挡，
// 结果是「点保存没反应、窗口关不掉」，用户看不到真正原因（最常见是组名重复 409）。
const groupError = ref("");
const enabledGroups = computed(() => groups.value.filter(group => group.enabled));
const hasGroups = computed(() => groups.value.length > 0);
const defaultGroupID = computed(() => enabledGroups.value[0]?.id || "");
function groupByID(id?: string) { return groups.value.find(group => group.id === id); }
function groupLabel(id?: string) {
  const group = groupByID(id);
  if (!group) return "未选择";
  return `${group.name}${group.enabled ? "" : "（已停用）"}`;
}

async function loadGroups() {
  groupsLoading.value = true;
  try {
    const list = await supplierTestConfigGroupsApi.list();
    if (!active) return;
    groups.value = list;
    // 新行/未挂组的行默认选第一个启用组，避免"加一行就红"。
    for (const row of rows.value) {
      if (!row.test_config_group_id) row.test_config_group_id = defaultGroupID.value;
    }
  } catch (value) {
    if (active) showError(value, "准入配置组暂时无法读取，可稍后重试。");
  } finally {
    if (active) groupsLoading.value = false;
  }
}

function openCreateGroup() {
  groupEditingID.value = "";
  groupForm.value = { name: "", base_url: "", api_key: "" };
  groupError.value = "";
  groupDialog.value = true;
}

function openEditGroup(group: SupplierTestConfigGroupDTO) {
  groupEditingID.value = group.id;
  // Key 不回显：留空＝保持已保存的 Key。
  groupForm.value = { name: group.name, base_url: group.base_url, api_key: "" };
  groupError.value = "";
  groupDialog.value = true;
}

async function saveGroup() {
  if (groupSaving.value) return;
  const name = groupForm.value.name.trim();
  const baseURL = groupForm.value.base_url.trim();
  if (!name) { groupError.value = "请填写配置组名称。"; return; }
  if (!baseURL) { groupError.value = "请填写测试接口 URL。"; return; }
  groupSaving.value = true;
  groupError.value = "";
  error.value = "";
  let saved = false;
  try {
    const apiKey = groupForm.value.api_key.trim();
    if (groupEditingID.value) {
      // 不传 api_key = 保留原 Key；传值 = 覆盖。
      const patch: { name: string; base_url: string; api_key?: string } = { name, base_url: baseURL };
      if (apiKey) patch.api_key = apiKey;
      await supplierTestConfigGroupsApi.update(groupEditingID.value, patch, crypto.randomUUID());
      ElMessage.success("配置组已保存");
    } else {
      const created = await supplierTestConfigGroupsApi.create(
        apiKey ? { name, base_url: baseURL, api_key: apiKey } : { name, base_url: baseURL },
        crypto.randomUUID(),
      );
      ElMessage.success("配置组已新增");
      // 先把新组并入本地列表，再挂到当前表单里还没选组的行上。
      // 否则行立刻挂上 created.id，而 groups 还没刷新 → groupByID 查不到 →
      // 表格里会闪过（甚至一直停着）「该组尚未配置 Key」的误报。
      const merged = [...groups.value, created].sort(
        (a, b) => a.sort_order - b.sort_order || Number(a.id) - Number(b.id),
      );
      groups.value = merged;
      for (const row of rows.value) {
        if (!row.test_config_group_id) row.test_config_group_id = created.id;
      }
    }
    saved = true;
    groupDialog.value = false;
  } catch (value) {
    // 落在弹窗内，用户才看得到「该配置组名称已存在，请换一个名字」这类原因。
    if (active) groupError.value = value instanceof Error && value.message
      ? value.message
      : "配置组保存失败，请重试。";
  } finally {
    if (active) groupSaving.value = false;
  }
  if (!saved) return;
  // 关闭之后的刷新失败不等于「保存失败」，不能再用保存失败的文案去误导用户。
  try {
    await loadGroups();
    // 改组会刷新挂在它上面的行快照（换 URL/Key → 旧结果失效），重拉一次才看得到。
    if (submissionID.value) await reloadKeepingKeys();
  } catch (value) {
    if (active) showError(value, "配置组已保存，但列表刷新失败，请手动刷新页面。");
  }
}

async function disableGroup(group: SupplierTestConfigGroupDTO) {
  if (groupSaving.value) return;
  const used = rows.value.filter(row => row.test_config_group_id === group.id).length;
  try {
    await ElMessageBox.confirm(
      used > 0
        ? `停用后不影响已挂在该组上的 ${used} 个模型（仍可测试与提交），但建议改挂到其它组。确认停用？`
        : "停用后该组不再出现在「准入配置组」下拉里，确认停用？",
      `停用配置组「${group.name}」`, { confirmButtonText: "停用", cancelButtonText: "取消", type: "warning" },
    );
  } catch { return; }
  groupSaving.value = true;
  try {
    await supplierTestConfigGroupsApi.disable(group.id, crypto.randomUUID());
    ElMessage.success("配置组已停用");
    await loadGroups();
  } catch (value) {
    if (active) showError(value, "停用失败，请重试。");
  } finally {
    if (active) groupSaving.value = false;
  }
}

async function enableGroup(group: SupplierTestConfigGroupDTO) {
  if (groupSaving.value) return;
  groupSaving.value = true;
  try {
    await supplierTestConfigGroupsApi.update(group.id, { enabled: true }, crypto.randomUUID());
    ElMessage.success("配置组已启用");
    await loadGroups();
  } catch (value) {
    if (active) showError(value, "启用失败，请重试。");
  } finally {
    if (active) groupSaving.value = false;
  }
}

// ---- 门禁（000052 原型口径：**每组**至少一个连通成功 + 整批至少一次有效压测）----
//
// 与后端 checkSubmissionCoverage 同口径，两端必须一起改：
//   ① 只校验「本表单里实际用到的组」——建了但没用的组不拦人；
//   ② 连通要看 verdict 通过（不是"有结果就行"）；
//   ③ 每行必须挂组。
const selectedStressCount = computed(() => rows.value.filter(row => row.participate_test).length);
const pendingStress = computed(() => rows.value.filter(row => row.participate_test && !row.stress_tested).length);
// 未挂组的行：提交前要拦（对应后端 400）。
const rowsWithoutGroup = computed(() => rows.value.filter(row => !row.test_config_group_id));
// 本次表单用到的组，按卡片顺序编号（「第 N 组」与卡片顺序一致）。
const usedGroups = computed(() => {
  const used = new Set(rows.value.map(row => row.test_config_group_id).filter(Boolean) as string[]);
  return groups.value.filter(group => used.has(group.id));
});
// 某组是否已经有「连通且通过」的模型。
function groupHasPassedConnectivity(groupID: string) {
  return rows.value.some(row => row.test_config_group_id === groupID && row.connectivity_tested && row.connectivity_passed);
}
const gateError = computed(() => {
  if (!rows.value.length) return "请至少添加一条模型。";
  if (rowsWithoutGroup.value.length > 0) return `还有 ${rowsWithoutGroup.value.length} 个模型没有选择准入配置组。`;
  const failed = usedGroups.value.find(group => !groupHasPassedConnectivity(group.id));
  if (failed) {
    const index = groups.value.findIndex(group => group.id === failed.id) + 1;
    const note = failed.enabled ? "" : "（该组已停用，可改挂其它组）";
    return `第 ${index} 组「${failed.name}」还没有连通成功的模型，请先在该组发起连通性测试${note}。`;
  }
  if (selectedStressCount.value === 0) return "请至少勾选一个压力测试模型。";
  if (pendingStress.value > 0) return `勾选的模型里还有 ${pendingStress.value} 个没有压力测试结果，请先发起测试。`;
  return "";
});
const canSubmitBatch = computed(() => !gateError.value);
// 放行时的提示：门禁只保证每组有一个能通，其余模型可能一个都没测。
const untestedCount = computed(() => rows.value.filter(row => !row.connectivity_tested).length);

function blankRow(): EditableRow {
  // 新行默认挂第一个启用组：与原型一致（新建行不用每次手选）。
  return { id: "", interface_model_id: "", standard_model_id: "", standard_model_name: "", official_id: "", coefficient: "1", test_base_url: "", test_api_key_configured: false, test_config_group_id: defaultGroupID.value, row_status: "DRAFT", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false };
}
function snapshot() {
  // 凭据不参与脏检查：它是主体资源，改了也不影响「这份申报还没保存」的判断。
  return JSON.stringify({ reason: reason.value, rows: rows.value.map(row => ({ id: row.id, interface_model_id: row.interface_model_id, standard_model_id: row.standard_model_id, coefficient: row.coefficient, participate_test: !!row.participate_test, test_config_group_id: row.test_config_group_id })) });
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
    if (!row.test_config_group_id) return `${line}请选择准入配置组。`;
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
    // 重新打开一张还有任务在跑的草稿时接着盯，别让用户回来只看到「测试中」干等。
    if (hasRunningTest.value) watchTestProgress();
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
  // 000052：行级 URL/Key 已废弃，改为「每行挂一个准入配置组」，URL/Key 由服务端按组回填。
  const payloadRows: SupplierSubmissionWriteRow[] = rows.value.map(row => ({
    ...(row.id ? { id: row.id } : {}),
    interface_model_id: row.interface_model_id.trim(),
    standard_model_id: row.standard_model_id,
    coefficient: row.coefficient.trim(),
    participate_test: !!row.participate_test,
    test_config_group_id: row.test_config_group_id || "",
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
      // 只有版本冲突才进「取最新版本」流程；测试占用（409 测试进行中）是拒绝本次改动，
      // 提示成「草稿已变化」会把人引去刷新，刷新也解决不了。
      // 「申报单已变化」这句是后端 writeSubmissionError 的固定文案，改文案时两边一起改。
      if (value instanceof ApiError && value.httpStatus === 409 && value.message.includes("申报单已变化")) {
        draftConflict.value = true; draftRefreshed.value = false;
      }
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
  // verdict / stale 要一起带回来，否则「未通过」会被显示成「已完成」。
  const test: SupplierSubmissionTestDTO = { category: result.category, status: result.status, tested: result.tested, passed: result.passed, verdict: result.verdict, stale: result.stale, job_id: result.job_id, report_id: result.report_id, connection_ms: result.connection_ms, rps: result.rps, tps: result.tps, started_at: result.started_at, eta_seconds: result.eta_seconds };
  if (result.category === "CONNECTIVITY") { row.connectivity_job = test; row.connectivity_tested = result.tested; row.connectivity_passed = result.passed; }
  else { row.stress_job = test; row.stress_tested = result.tested; row.stress_passed = result.passed; }
}
// 正在刷新的行：用 null 而不是 ""——未保存的行 id 就是空串，
// 用空串当初值会让「刷新结果」按钮在这些行上一直显示 loading（2026-10-28 报障）。
// 正在刷新的行：用 null 而不是 ""——未保存的行 id 就是空串，
// 用空串当初值会让「刷新结果」按钮在这些行上一直显示 loading（2026-10-28 报障）。
const refreshingRow = ref<string | null>(null);
async function refreshRow(row: EditableRow) {
  if (!submissionID.value || !row.id || refreshingRow.value) return;
  const tasks: Promise<void>[] = [];
  if (row.connectivity_job) tasks.push(refreshTest(row, "CONNECTIVITY"));
  if (row.stress_job) tasks.push(refreshTest(row, "STRESS"));
  if (!tasks.length) { ElMessage.info("这一行还没有测试任务，请先发起测试。"); return; }
  refreshingRow.value = row.id;
  try {
    await Promise.all(tasks);
    if (!active) return;
    // 以前点完什么都没有：现在至少告诉用户刷新到了什么。
    const running = [row.connectivity_job, row.stress_job].some(job => job && ["QUEUED", "RUNNING"].includes(job.status));
    if (running) ElMessage.info("已刷新：测试仍在进行中，请稍后再看。");
    else ElMessage.success("已刷新测试结果。");
  } finally { if (active) refreshingRow.value = null; }
}
async function refreshTest(row: EditableRow, category: SupplierTestCategory) {
  if (!submissionID.value || !row.id || testBusy.value[row.id + category]) return;
  testBusy.value = { ...testBusy.value, [row.id + category]: true };
  try { applyTest(row, await supplierSubmissionsApi.testStatus(submissionID.value, row.id, category)); }
  catch (value) { showError(value, "测试进度暂时无法读取。可以稍后重试。"); }
  finally { const next = { ...testBusy.value }; delete next[row.id + category]; testBusy.value = next; }
}

// ---- 自动盯结果（A）：发起后每几秒拉一次进度，用户不用再手点「刷新结果」 ----
// 前提：查进度接口是**只读**的（后端 TestProgress 不会发单），否则轮询等于反复发起测试。
const nowMs = ref(Date.now());
const pollDelayMs = ref(3000);
const finishedRowIDs = ref<string[]>([]);
let pollTimer: number | undefined;
let clockTimer: number | undefined;
let lastRunning = new Set<string>();

const isJobRunning = (job?: SupplierSubmissionTestDTO) => Boolean(job && ["QUEUED", "RUNNING"].includes(job.status));
const hasRunningTest = computed(() => rows.value.some(row => isJobRunning(row.connectivity_job) || isJobRunning(row.stress_job)));
// 压测耗时来自后端（和发单参数同源），有就拿来当等待预期。
const testEtaSeconds = computed(() => rows.value.map(row => row.stress_job?.eta_seconds).find(value => typeof value === "number" && value > 0) || 0);

function runningKeySet() {
  const keys = new Set<string>();
  for (const row of rows.value) {
    if (isJobRunning(row.connectivity_job)) keys.add(`${row.id}:CONNECTIVITY`);
    if (isJobRunning(row.stress_job)) keys.add(`${row.id}:STRESS`);
  }
  return keys;
}
function clearPollTimer() { if (pollTimer !== undefined) { window.clearTimeout(pollTimer); pollTimer = undefined; } }
function clearClockTimer() { if (clockTimer !== undefined) { window.clearInterval(clockTimer); clockTimer = undefined; } }
function stopPolling() { clearPollTimer(); clearClockTimer(); pollDelayMs.value = 3000; }
function schedulePoll(delay = pollDelayMs.value) {
  clearPollTimer();
  if (!active) return;
  pollTimer = window.setTimeout(() => { void pollProgress(); }, delay);
}
function ensureClock() {
  if (clockTimer !== undefined) return;
  // 只为了让「已用 N 秒」自己往前走，避免只能等下一次轮询才跳数字。
  clockTimer = window.setInterval(() => { nowMs.value = Date.now(); }, 1000);
}

async function pollProgress() {
  const id = submissionID.value;
  if (!active || !id) { stopPolling(); return; }
  // 页面在后台就别 3 秒一次了，切回来会立刻补一次。
  if (document.visibilityState === "hidden") { schedulePoll(10000); return; }
  try {
    const result = await supplierSubmissionsApi.testProgress(id);
    if (!active || submissionID.value !== id) return;
    applyProgress(result.rows);
    if (hasRunningTest.value) {
      pollDelayMs.value = Math.min(6000, pollDelayMs.value + 500);
      schedulePoll();
    } else { finishPolling(); }
  } catch {
    // 拉进度失败不该打断用户，退避后重试；真正的错误留给主动操作去暴露。
    pollDelayMs.value = Math.min(15000, pollDelayMs.value * 2);
    schedulePoll();
  }
}

function applyProgress(items: SupplierTestProgressRowDTO[]) {
  for (const item of items) {
    const row = rows.value.find(entry => entry.id === item.row_id);
    if (!row) continue;
    // NOT_STARTED 表示这一行根本没有这类任务，别平白给它挂一个「未测试」标签。
    if (item.connectivity.status !== "NOT_STARTED") applyTest(row, item.connectivity);
    if (item.stress.status !== "NOT_STARTED") applyTest(row, item.stress);
  }
  const after = runningKeySet();
  const done = [...lastRunning].filter(key => !after.has(key));
  lastRunning = after;
  if (!done.length) return;
  // 有任务刚落地：闪一下这几行，并告诉用户可以去核对了。
  const ids = [...new Set(done.map(key => key.split(":")[0]))];
  finishedRowIDs.value = ids;
  window.setTimeout(() => { finishedRowIDs.value = []; }, 5000);
  ElMessage.success(ids.length === 1 ? "测试结果已出，可以核对后提交了。" : `${ids.length} 个模型的测试结果已出，可以核对后提交了。`);
}

function finishPolling() { stopPolling(); lastRunning = new Set(); }

// 发起成功 / 重新打开页面时启动；已无任务在跑就什么都不做。
function watchTestProgress() {
  if (!active || !submissionID.value) return;
  lastRunning = runningKeySet();
  if (!lastRunning.size) { stopPolling(); return; }
  ensureClock();
  schedulePoll(1200);
}

function rowClassName({ row }: { row: EditableRow }) { return finishedRowIDs.value.includes(row.id) ? "row-finished" : ""; }

// 运行中的标签不说「未测试」，并带上已用时间；超过预估两倍就明说「比预期慢」。
function testStateLabel(job: SupplierSubmissionTestDTO | undefined, tested: boolean, passed: boolean) {
  if (isJobRunning(job)) return job?.status === "QUEUED" ? "排队中" : "测试中";
  return testState(tested, passed, job?.verdict);
}
function testTagType(job: SupplierSubmissionTestDTO | undefined, tested: boolean, passed: boolean): "success" | "danger" | "info" | "warning" {
  if (isJobRunning(job)) return "warning";
  return testType(tested, passed, job?.verdict);
}
function elapsedLabel(job?: SupplierSubmissionTestDTO) {
  if (!isJobRunning(job) || !job?.started_at) return "";
  const startedAt = Date.parse(job.started_at);
  if (!Number.isFinite(startedAt)) return "";
  const seconds = Math.max(0, Math.floor((nowMs.value - startedAt) / 1000));
  if (job.status === "QUEUED") return `已排队 ${seconds}s`;
  const eta = job.eta_seconds || 0;
  if (eta && seconds > eta * 2) return `已用 ${seconds}s · 比预期慢`;
  return `已用 ${seconds}s${eta ? ` / 约 ${eta}s` : ""}`;
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
  // 压测是提交门禁的一部分，所以它也是「发起测试」的前置条件：
  // 一个都没勾时不许先跑连通性。否则连通性白测一轮、压测又补不上，
  // 用户到提交时才被挡住（2026-10-28 报障）。
  if (selectedStressCount.value === 0) {
    error.value = "请至少勾选一个压力测试";
    ElMessage.warning("请至少勾选一个压力测试");
    return;
  }
  // 每个用到的组都得先配好 Key：否则发出去也只会被平台拒，白占任务。
  // 这里在**前端就拦掉**，不发起任何请求。
  const missingKey = usedGroups.value.find(group => !group.has_api_key);
  if (missingKey) {
    error.value = `配置组「${missingKey.name}」还没有配置 Key，请先到上方「准入测试配置」里补上。`;
    ElMessage.warning(error.value);
    return;
  }
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
    // 发起成功就开始盯结果：压测要跑几十秒，别让用户守着点「刷新结果」。
    watchTestProgress();
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
  // 有错误行就整批不导入：以前是"只导 ok 的"，用户以为全导进去了，
  // 实际错的行被静默丢掉（000052 的「组名匹不上」尤其容易这样消失）。
  if (preview.error_count > 0) {
    error.value = `还有 ${preview.error_count} 行有问题，请修正后重新导入（错误见预览列表）。`;
    ElMessage.error(error.value);
    return;
  }
  const accepted = preview.rows.filter(row => row.ok && row.standard_model_id);
  // 「准入配置组」按**组名**匹配：模板里写的是人名友好的组名，不是 ID。
  // 匹不上的行整批拦下来提示，不能静默丢掉——那是用户填了却"消失"的数据。
  const unmatched: string[] = [];
  const mapped: Array<{ item: typeof accepted[number]; groupID: string }> = [];
  for (const item of accepted) {
    const groupName = (item.test_config_group_name || "").trim();
    // 模板没写组名时落到第一个启用组（与新建行默认值一致）。
    if (!groupName) {
      if (!defaultGroupID.value) { unmatched.push(`第 ${item.line || "?"} 行：没有启用中的配置组`); continue; }
      mapped.push({ item, groupID: defaultGroupID.value });
      continue;
    }
    const group = groups.value.find(candidate => candidate.name === groupName);
    if (!group) { unmatched.push(`第 ${item.line || "?"} 行：找不到配置组「${groupName}」`); continue; }
    mapped.push({ item, groupID: group.id });
  }
  if (unmatched.length) {
    error.value = `导入未生效：${unmatched.slice(0, 3).join("；")}${unmatched.length > 3 ? ` 等 ${unmatched.length} 处` : ""}。请先在模板里改成已存在的配置组名。`;
    ElMessage.error(error.value);
    return;
  }
  for (const { item, groupID } of mapped) {
    rows.value.push({
      ...blankRow(),
      interface_model_id: item.interface_model_id,
      standard_model_id: String(item.standard_model_id),
      standard_model_name: item.standard_model_name || "",
      official_id: item.official_id || "",
      coefficient: item.coefficient,
      participate_test: item.participate_test,
      test_config_group_id: groupID,
    });
  }
  importDialog.value = false;
  importPreview.value = null;
  ElMessage.success(`已导入 ${mapped.length} 行，请核对后再保存草稿。`);
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
    stopPolling();
    ElMessage.success("申报已提交，后续状态以办理进度为准。");
    await router.replace({ path: "/supplier/home", query: { tab: "history" } });
  } catch (value) { if (active) showError(value, "申报未能提交，草稿和输入已保留。"); }
  finally { if (active) submitting.value = false; }
}
async function submitPriceChange() {
  if (submitting.value) return;
  if (!resourceID.value || !expectedRevision.value) { error.value = "缺少资源或版本信息，请从模型管理页面重新发起。"; return; }
  if (!standardModelID.value || !supplierModelID.value) { error.value = "缺少模型信息，请从模型管理页面重新发起。"; return; }
  if (!reason.value.trim()) { error.value = "请填写价格变更原因。"; return; }
  submitting.value = true;
  error.value = "";
  try {
    // 价格变更走申报主链路（kind=PRICE_CHANGE），不再依赖不存在的
    // /supplier/models/:id/price-change：创建 + 提交，后端对这类单子不做测试门禁。
    const created = await supplierSubmissionsApi.create({
      kind: "PRICE_CHANGE",
      reason: reason.value.trim(),
      // 000052：行必须有准入配置组。价格变更不走准入测试，这里取第一个启用组作为留痕归属。
      rows: [{ interface_model_id: supplierModelID.value, standard_model_id: standardModelID.value, coefficient: coefficient.value.trim(), test_config_group_id: defaultGroupID.value }],
    }, lifecycle.value);
    await supplierSubmissionsApi.submit(created.id, created.revision, reason.value.trim(), lifecycle.value);
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
  void loadGroups();
  const id = typeof route.query.id === "string" ? route.query.id : "";
  if (id && !isPriceChange.value) void loadDraft(id);
  else if (!isPriceChange.value) { rows.value = [blankRow()]; savedBaseline.value = snapshot(); }
  window.addEventListener("beforeunload", beforeUnload);
});
onBeforeRouteLeave(canLeave);
onBeforeUnmount(() => { active = false; stopPolling(); optionRequest?.abort(); draftRequest?.abort(); window.removeEventListener("beforeunload", beforeUnload); });
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

    <!-- 准入测试配置组（原型表单顶部：卡片式多组） -->
    <div class="group-block" v-loading="groupsLoading">
      <div class="group-head">
        <div>
          <h3 class="group-title">准入测试配置</h3>
          <p class="group-note">每个配置组是一套独立 URL／Key；下面的模型要选择用哪一组做准入测试。仅用于本主体准入测试，不用于平台运营测试。</p>
        </div>
        <el-button size="small" type="primary" plain :disabled="locked || groupSaving" @click="openCreateGroup">＋ 新增配置组</el-button>
      </div>
      <el-empty v-if="!groups.length && !groupsLoading" description="还没有配置组，先新增一组再添加模型" :image-size="60" />
      <div v-else class="group-cards">
        <div v-for="(group, index) in groups" :key="group.id" class="group-card" :class="{ 'is-disabled': !group.enabled }">
          <div class="group-card-head">
            <span class="group-name">第 {{ index + 1 }} 组 · {{ group.name }}</span>
            <el-tag v-if="!group.enabled" size="small" type="info">已停用</el-tag>
          </div>
          <div class="group-url" :title="group.base_url">{{ group.base_url }}</div>
          <div class="group-card-foot">
            <el-tag size="small" :type="group.has_api_key ? 'success' : 'warning'">
              {{ group.has_api_key ? "Key 已保存" : "待配置 Key" }}
            </el-tag>
            <span class="group-actions">
              <el-button text size="small" :disabled="locked || groupSaving" @click="openEditGroup(group)">编辑</el-button>
              <el-button v-if="group.enabled" text size="small" type="danger" :disabled="locked || groupSaving" @click="disableGroup(group)">停用</el-button>
              <el-button v-else text size="small" :disabled="locked || groupSaving" @click="enableGroup(group)">启用</el-button>
            </span>
          </div>
        </div>
      </div>
    </div>

    <div class="batch-actions">
      <el-button size="small" :loading="importBusy" :disabled="locked" @click="downloadTemplate">下载导入模板</el-button>
      <el-button size="small" :disabled="locked" @click="triggerImport">导入 Excel</el-button>
      <input ref="importInput" type="file" accept=".xlsx" hidden @change="onImportFile" />
      <span class="batch-hint">
        连通性测试按「每组至少一个通过」校验；压力测试只做勾选的模型（已勾 {{ selectedStressCount }} 个，待测 {{ pendingStress }} 个）<template v-if="untestedCount">；当前共 {{ untestedCount }} 个模型未测</template>。
      </span>
    </div>

    <el-table :data="rows" row-key="id" class="submission-table" :row-class-name="rowClassName" empty-text="还没有模型，点「添加模型」开始">
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
      <el-table-column label="准入配置组" min-width="190">
        <template #default="{ row }">
          <!-- 只列启用中的组；行已挂在停用组上时，把当前组也补进选项，否则下拉会显示空。 -->
          <el-select v-model="row.test_config_group_id" placeholder="选择准入配置组" :disabled="locked">
            <el-option
              v-for="group in (enabledGroups.some(g => g.id === row.test_config_group_id) || !row.test_config_group_id
                ? enabledGroups
                : [groupByID(row.test_config_group_id)!, ...enabledGroups])"
              :key="group.id" :value="group.id" :label="group.name + (group.enabled ? '' : '（已停用）')"
            />
          </el-select>
          <small v-if="groupByID(row.test_config_group_id) && !groupByID(row.test_config_group_id)!.enabled" class="warn-text">该组已停用，建议改挂其它组</small>
          <small v-else-if="!groupByID(row.test_config_group_id)?.has_api_key" class="warn-text">该组尚未配置 Key</small>
        </template>
      </el-table-column>
      <el-table-column label="采购系数" width="130">
        <template #default="{ row }"><el-input v-model="row.coefficient" inputmode="decimal" placeholder="0.6800" :disabled="locked" /></template>
      </el-table-column>
      <el-table-column label="压力测试" width="100" align="center">
        <template #default="{ row }"><el-checkbox v-model="row.participate_test" :disabled="locked">参与</el-checkbox></template>
      </el-table-column>
      <el-table-column label="测试" min-width="250">
        <template #default="{ row }">
          <div class="test-cell">
            <span class="test-pill"><el-tag size="small" :type="testTagType(row.connectivity_job, row.connectivity_tested, row.connectivity_passed)">连通 {{ testStateLabel(row.connectivity_job, row.connectivity_tested, row.connectivity_passed) }}</el-tag></span>
            <span v-if="row.participate_test || row.stress_job" class="test-pill"><el-tag size="small" :type="testTagType(row.stress_job, row.stress_tested, row.stress_passed)">压力 {{ testStateLabel(row.stress_job, row.stress_tested, row.stress_passed) }}</el-tag></span>
            <small v-if="elapsedLabel(row.connectivity_job)" class="muted">{{ elapsedLabel(row.connectivity_job) }}</small>
            <small v-if="row.participate_test && elapsedLabel(row.stress_job)" class="muted">{{ elapsedLabel(row.stress_job) }}</small>
            <small v-if="rowMetrics(row)" class="muted">{{ rowMetrics(row) }}</small>
            <el-button link type="primary" size="small" :loading="!!row.id && refreshingRow === row.id" :disabled="locked || (!row.connectivity_job && !row.stress_job)" @click="refreshRow(row)">刷新结果</el-button>
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

    <el-alert
      v-if="hasRunningTest"
      type="info"
      show-icon
      :closable="false"
      :title="`测试正在服务端运行${testEtaSeconds ? `（压测约 ${testEtaSeconds} 秒）` : ''}：页面会自动更新进度和结果，可以先补充其他模型，或者直接离开——回来在「申报记录」里也能看到结果。`"
    />

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
        <el-button type="primary" :disabled="!importPreview?.ok_count || importPreview.error_count > 0" @click="applyImport">
          导入 {{ importPreview?.ok_count || 0 }} 行{{ importPreview?.error_count ? `（有 ${importPreview.error_count} 行待修正）` : "" }}
        </el-button>
      </template>
    </el-dialog>
    <!-- 新增/编辑配置组 -->
    <el-dialog v-model="groupDialog" :title="groupEditingID ? '编辑配置组' : '新增配置组'" width="520px">
      <!-- 错误必须落在弹窗里：页面顶部的 error 区域会被遮罩完全挡住。 -->
      <el-alert v-if="groupError" :title="groupError" type="error" show-icon :closable="false" class="group-dialog-error" />
      <el-form label-width="110px">
        <el-form-item label="组名" required>
          <el-input v-model="groupForm.name" maxlength="64" placeholder="如：原单组配置 / 华东通道" :disabled="groupSaving" />
        </el-form-item>
        <el-form-item label="测试接口 URL" required>
          <el-input v-model="groupForm.base_url" placeholder="https://api.example.com/v1（填 demo 使用平台演示模型）" :disabled="groupSaving" />
        </el-form-item>
        <el-form-item :label="groupEditingID ? '测试 API Key（留空保留）' : '测试 API Key'">
          <el-input v-model="groupForm.api_key" type="password" show-password autocomplete="new-password"
                    :placeholder="groupEditingID ? '留空表示不修改已保存的 Key' : '供应商自测使用的 Key（可稍后再填）'" :disabled="groupSaving" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="groupDialog = false">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="saveGroup">保存</el-button>
      </template>
    </el-dialog>

    <div class="form-actions">
      <el-button @click="router.push('/supplier/home')">返回</el-button>
    </div>
    <p class="muted security-note">每个模型按所选「准入配置组」的 URL／Key 发起测试并计算指纹；改某一组的 URL 或 Key 只会让**挂在该组上**的旧结果失效（其它组不受影响）。内部人员发起测试使用平台自己的凭据与额度，与供应商凭据分开。</p>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-panel,.price-change-panel { display: grid; gap: 18px; padding: 22px; }
.group-block { border: 1px solid var(--app-border); border-radius: 8px; padding: 16px 18px; display: grid; gap: 12px; }
.group-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.group-title { margin: 0; font-size: 14px; font-weight: 600; }
.group-note { margin: 4px 0 0; color: var(--app-text-muted); font-size: 12px; line-height: 1.6; max-width: 720px; }
.group-cards { display: grid; grid-template-columns: repeat(auto-fill,minmax(240px,1fr)); gap: 12px; }
.group-card { border: 1px solid var(--app-border); border-radius: 8px; padding: 12px 14px; display: grid; gap: 8px; background: var(--app-bg-subtle, #fafafa); }
.group-card.is-disabled { opacity: .72; border-style: dashed; }
.group-card-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.group-name { font-weight: 600; font-size: 13px; }
/* URL 脱敏：长地址横向截断，完整值放 title 里 */
.group-url { font-size: 12px; color: var(--app-text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.group-card-foot { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.group-actions { display: flex; gap: 2px; }
.warn-text { color: #b42318; font-size: 12px; display: block; margin-top: 2px; }
.group-dialog-error { margin-bottom: 12px; }
.batch-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.batch-hint { color: var(--app-text-muted); font-size: 12px; }
.submission-table :deep(.el-select) { width: 100%; }
.test-cell { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
/* 结果刚下来时闪一下，避免用户没注意到自动刷新。 */
:deep(.el-table__row.row-finished > td) { background: #f0f9eb !important; transition: background-color .6s ease; }
.test-pill { display: inline-flex; }
.rule-note { margin: 0; color: var(--app-text-muted); font-size: 12px; line-height: 1.7; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; }
.form-actions--inline { justify-content: flex-start; flex-wrap: wrap; }
.muted,.inline-error { color: var(--app-text-muted); font-size: 12px; }
.inline-error { color: #b42318; }
.security-note { margin: 0; }
@media(max-width: 900px) { .group-cards { grid-template-columns: 1fr; } }
</style>
