<script setup lang="ts">
/**
 * 客户详情整页（B3b 第 1 项）。
 *
 * 4 个 Tab：接入模型 / 档案与办理 / 该客户报价 / 操作记录。
 * 工作台客户待办的 deeplink 是 `/internal/customers/{id}?tab=onboarding`，落点是 Tab 2。
 *
 * 设计取舍：
 *  - Tab 切换用 `router.replace`（不是 push）：否则浏览器后退会在 Tab 之间回退，回不到列表；
 *  - 每个 Tab **首次进入才请求**（`loaded` 标记），来回切换不重复拉；也不做 v-show 全量预渲染；
 *  - 报价编辑器**不搬**（380 行，见工单坑 3）：「该客户报价」Tab 只读，新建报价跳回列表页复用编辑器。
 */
import { computed, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  CUSTOMER_LINK_ACTION_LABELS,
  customerDetailApi,
  type CustomerLinkDetailDTO,
  type CustomerModelServiceRow,
  type CustomerOperationLogDTO,
  type CustomerProfileDTO,
  type OnboardingFlowDTO,
  type OnboardingNodeDTO,
} from "../../../api/customerDetail";
import { customerWorkspaceApi, type CustomerApprovalRow, type CustomerQuoteRow } from "../../../api/customerWorkspace";
import { changeRequestsApi } from "../../../api/changeRequests";
import type { ChangeRequestDetailDTO } from "../../../api/changeRequests.types";
import {
  quoteApprovalApi,
  type ApprovalDecisionInput,
  type QuoteApprovalDTO,
  type QuoteApprovalSummaryDTO,
} from "../../../api/customerQuoteApproval";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { formatSupplierTaxRate } from "../../../domain/supplierTax";
import { PERMISSIONS } from "../../../domain/permissions";
import { usePermissionStore } from "../../../stores/permission";
import { useSessionStore } from "../../../stores/session";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";
import ResourceReportsPanel from "../resources/ResourceReportsPanel.vue";
import CustomerOnboardingStartForm from "./CustomerOnboardingStartForm.vue";
import CustomerTransferDialog from "./CustomerTransferDialog.vue";

const TABS = ["models", "onboarding", "quotes", "records"] as const;
type TabKey = (typeof TABS)[number];

const TAB_LABELS: Record<TabKey, string> = {
  models: "接入模型",
  onboarding: "档案与办理",
  quotes: "该客户报价",
  records: "操作记录",
};

const route = useRoute();
const router = useRouter();
const permissions = usePermissionStore();
const session = useSessionStore();

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const customerId = computed(() => String(route.params.id ?? ""));
const canEdit = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_EDIT));
const canQuoteView = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_VIEW));
const canQuoteEdit = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_EDIT));
const canQuoteApprove = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_APPROVE));

function resolveTab(value: unknown): TabKey {
  const text = typeof value === "string" ? value : "";
  const tab = (TABS as readonly string[]).includes(text) ? (text as TabKey) : "models";
  // 纯 M8 角色看不到「该客户报价」Tab：URL 里硬写 ?tab=quotes 也回落到首个 Tab，避免空白页。
  if (tab === "quotes" && !canQuoteView.value) return "models";
  return tab;
}

const activeTab = ref<TabKey>(resolveTab(route.query.tab));

watch(() => route.query.tab, value => {
  const next = resolveTab(value);
  if (next !== activeTab.value) activeTab.value = next;
});

/** 切 Tab 写回 URL，用 replace（push 会让后退困在 Tab 之间）。 */
function selectTab(tab: TabKey) {
  if (tab === activeTab.value) return;
  activeTab.value = tab;
  void router.replace({ path: route.path, query: { ...route.query, tab } });
}

// ---------------- 档案（页头 + Tab2 共用） ----------------
const profile = ref<CustomerProfileDTO>();
const profileLoading = ref(false);
const profileError = ref("");
const canManageF12 = computed(() =>
  Boolean(profile.value && session.user?.staffId && String(profile.value.owner_sales_operator_id) === String(session.user.staffId)));

async function loadProfile() {
  if (!customerId.value) return;
  profileLoading.value = true;
  profileError.value = "";
  try {
    profile.value = await customerDetailApi.profile(customerId.value);
  } catch (value) {
    profileError.value = message(value);
  } finally {
    profileLoading.value = false;
  }
}

// ---------------- Tab1 接入模型 ----------------
const modelRows = ref<CustomerModelServiceRow[]>([]);
const modelLoading = ref(false);
const modelError = ref("");
const modelLoaded = ref(false);

async function loadModels(force = false) {
  if (force) modelLoaded.value = false;
  if (modelLoaded.value || modelLoading.value) return;
  modelLoading.value = true;
  modelError.value = "";
  try {
    const result = await customerDetailApi.resourceLinks(customerId.value);
    modelRows.value = result.list;
    modelLoaded.value = true;
  } catch (value) {
    modelError.value = message(value);
  } finally {
    modelLoading.value = false;
  }
}

/**
 * 绑定动作（A2）。动作清单来自后端的 `available_actions`（状态机推导），
 * 这里只做三件事：收原因 → 带上 expected_revision 提交 → 成功后强制重拉投影。
 *
 * 为什么成功后必须强制重拉：`modelRows` 是**聚合投影**（服务状态/已绑定数），
 * 只把展开区那一行改掉会让外层行显示旧值，用户会以为动作没生效。
 */
const linkActionSubmitting = ref<string>("");

/**
 * 本会话内是否刚做过「归档类」动作（确认停用 / 确认禁用）。
 *
 * 为什么需要它：投影是按「未归档绑定」驱动的，确认停用会把绑定归档、该模型不再产生行，
 * 于是整行从「接入模型」里消失、表格变空。用户第一反应是「我把模型删了」，
 * 而实际上他只是停用了一个来源。
 *
 * 这里只改**空表文案**（前端表达层），不去改 `ListCustomerModels` 的投影：
 * 让「有归档绑定但无未归档」的模型也出一行，会把所有客户的历史归档模型
 * （373 就有 3 条归档）全部翻出来，回归面太大，属新需求。
 */
const justArchivedModel = ref(false);

/**
 * 会真正**归档**绑定的动作。
 *
 * 与后端 `customer.ArchivedAfter` 保持一致：只有 `OFFLINE_CONFIRM`（确认停用）会归档。
 * `DISABLE_CONFIRM`（确认禁用）只把状态改成 DISABLED、**不归档**，列表里那行仍在，
 * 所以不置位——否则一旦以后有别的路径让表格变空，会给出「已归档」的错误解释。
 */
const ARCHIVING_ACTIONS = new Set(["OFFLINE_CONFIRM"]);

// 原型「接入模型」的筛选：模型公司 / 模型类型 / 搜索模型名称·官方 ID。
const modelFilters = reactive({ company: "", modelType: "", keyword: "" });
const modelCompanies = computed(() => [...new Set(modelRows.value.map(row => row.company).filter(Boolean))].sort((a, b) => a.localeCompare(b, "zh-CN")));
/**
 * 关键字搜索口径与原型一致：模型名称 / 官方 ID（原型 `c4VisibleModelRows` 只搜这两个）。
 * 额外带上标准模型 ID：运维口头沟通时说的是「3 号模型」，加一列不影响原口径。
 */
const filteredModelRows = computed(() => {
  const keyword = modelFilters.keyword.trim().toLowerCase();
  return modelRows.value.filter(row => {
    if (modelFilters.company && row.company !== modelFilters.company) return false;
    if (modelFilters.modelType && row.model_type !== modelFilters.modelType) return false;
    if (!keyword) return true;
    return [row.model_name, row.official_id, row.standard_model_id]
      .some(value => Boolean(value) && String(value).toLowerCase().includes(keyword));
  });
});
function resetModelFilters() { Object.assign(modelFilters, { company: "", modelType: "", keyword: "" }); }

/**
 * 筛选/搜索变化时清空已勾选。
 *
 * `reserve-selection` 会把「被筛掉的行」留在选择集里，批量禁用它就会作用到看不见的模型上。
 * 与其让用户在弹窗里才发现多出了几条，不如在筛选那一刻就清空——同屏所见即所选。
 */
watch([() => modelFilters.company, () => modelFilters.modelType, () => modelFilters.keyword], () => {
  if (!batchSelected.value.length) return;
  modelTable.value?.clearSelection();
  batchSelected.value = [];
});

// 列排序：模板里的列都用了自定义 cell，没有 prop，所以显式给 sort-by。
// 文案类统一转小写后比较；系数/计数按数值排，避免「0.9 < 0.52」这种字典序错。
const byText = (pick: (row: CustomerModelServiceRow) => string | undefined) =>
  (row: CustomerModelServiceRow) => (pick(row) || "").toLowerCase();
const sortByName = byText(row => row.model_name);
const sortByOfficial = byText(row => row.official_id);
const sortByCompany = byText(row => row.company);
const sortByType = byText(row => row.model_type);
const sortByRegion = byText(row => row.region);
const sortByCoefficient = (row: CustomerModelServiceRow) => Number(row.coefficient) || 0;
const sortByAvailable = (row: CustomerModelServiceRow) => row.available_count ?? 0;
const sortByBound = (row: CustomerModelServiceRow) => row.bound_count ?? 0;
const sortByRisk = (row: CustomerModelServiceRow) => (row.risk_flags?.length ? 1 : 0);
// 系数按有效位展示（0.7200 → 0.72），与模型广场一致。
const coefficientText = (raw: string) => raw && raw.includes(".") ? raw.replace(/0+$/, "").replace(/\.$/, "") : raw;
const modelEmptyText = computed(() => {
  // 有数据但被筛没了 ≠ 客户没有模型：两种空态分开说，否则用户以为接入记录丢了。
  if (modelRows.value.length && !filteredModelRows.value.length) return "没有匹配的模型：可调整模型公司 / 类型筛选，或清空搜索关键字";
  return justArchivedModel.value
    ? "该模型来源已停用并归档，可到「操作记录」查看本次操作"
    : "该客户还没有接入任何模型";
});

function linkActionLabel(action: string) {
  return CUSTOMER_LINK_ACTION_LABELS[action] ?? action;
}

function linkActionTagType(action: string): "primary" | "danger" | "warning" | "success" {
  if (action === "DISABLE_REQUEST" || action === "OFFLINE_CONFIRM" || action === "DISABLE_CONFIRM") return "danger";
  if (action === "RESTORE_CONFIRM") return "success";
  return "warning";
}

async function runLinkAction(link: CustomerLinkDetailDTO, action: string) {
  if (!canEdit.value || linkActionSubmitting.value) return;
  const label = linkActionLabel(action);
  let reason = "";
  try {
    const { value } = await ElMessageBox.prompt(
      `来源：${link.supplier_name || "—"} / 资源 #${link.resource_id}｜当前状态：${link.state_label}`,
      `${label}（必填原因）`,
      {
        inputPlaceholder: "请填写本次操作原因，会写入审计",
        inputValidator: (value: string) => Boolean(value?.trim()) || "请填写原因",
        confirmButtonText: label,
        cancelButtonText: "取消",
        type: action === "RESTORE_REQUEST" || action === "RESTORE_CONFIRM" ? "info" : "warning",
      },
    );
    reason = String(value ?? "").trim();
  } catch {
    return; // 用户取消
  }
  const key = `${link.id}:${action}`;
  linkActionSubmitting.value = key;
  try {
    await customerDetailApi.actLink(link.id, { action, reason, expected_revision: link.revision });
    ElMessage.success(`${label}成功`);
    // 归档类动作做完，该模型的行会从投影里消失（绑定已归档）；
    // 用会话内标记把空表文案换成「已停用并归档」，避免用户以为模型被删了。
    justArchivedModel.value = ARCHIVING_ACTIONS.has(action);
    await loadModels(true);
    void loadProfile();
  } catch (value) {
    // 409 = 乐观锁冲突（别人刚改过）。后端文案已是人话，直接展示并重拉最新版本。
    if (value instanceof ApiError && value.httpStatus === 409) {
      ElMessage.warning(message(value));
      await loadModels(true);
    } else {
      ElMessage.error(message(value));
    }
  } finally {
    linkActionSubmitting.value = "";
  }
}

/**
 * 原型「接入模型」展开区的「测试与报告」：弹窗看该来源资源的历次测试。
 *
 * 这里直接复用模型广场那份 `ResourceReportsPanel`，不新写一套：
 * 测试报告只有一条证据链，两个入口各读一份必然漂移。
 * `/internal/resources/:id/test-reports` 的权限是「内部员工」，与客户管理同域，不需要额外权限点。
 */
const reportsVisible = ref(false);
const reportsTarget = ref<{ row: CustomerModelServiceRow; link: CustomerLinkDetailDTO }>();
function openResourceReports(row: CustomerModelServiceRow, link: CustomerLinkDetailDTO) {
  reportsTarget.value = { row, link };
  reportsVisible.value = true;
}

// ---------------- Tab1 批量操作（原型「配置资源 / 禁用 / 启用」） ----------------
//
// 原型这一排按钮的含义分两类，落到我们的模型上是两件事：
//   1. 配置资源 —— 给「已申请但还没配来源」的模型登记技术配置。我们的配置是**流程级节点**
//      （CONFIG 一次确认整单），没有行级配置接口，所以这里不另造一套只改界面的表单，
//      而是校验完所选模型确实在本次申请内后，直接落到「开通流程 → 完成节点 · 资源配置」。
//   2. 禁用 / 启用 —— 对已绑定的来源做状态动作。后端是两步（REQUEST → CONFIRM），
//      这里按状态机自动连做两步；每个模型独立提交，中途失败不吞掉已成功项。

const batchSelected = ref<CustomerModelServiceRow[]>([]);
const modelTable = ref<{ clearSelection: () => void }>();

/** 可批量操作的行：有可提交动作的绑定，或属于进行中的申请（待配置）。 */
function canBatchRow(row: CustomerModelServiceRow) {
  if (!canEdit.value) return false;
  if (row.pending_config) return true;
  return row.links.some(link => link.available_actions.length > 0);
}

type BatchServiceKind = "disable" | "restore";

/**
 * 两步动作的规格：from 里的状态才允许该批量动作。
 *
 * 顺序不能省：ACTIVE --DISABLE_REQUEST--> DISABLE_PENDING --DISABLE_CONFIRM--> DISABLED，
 * 恢复同理。已经停在「待确认」的绑定直接做第二步就行，所以第二段的判据仍用 from。
 */
const BATCH_SERVICE_SPEC: Record<BatchServiceKind, { first: string; second: string; from: string[]; label: string }> = {
  disable: { first: "DISABLE_REQUEST", second: "DISABLE_CONFIRM", from: ["ACTIVE", "DISABLE_PENDING"], label: "禁用" },
  restore: { first: "RESTORE_REQUEST", second: "RESTORE_CONFIRM", from: ["DISABLED", "RESTORE_PENDING"], label: "启用" },
};

interface BatchServiceItem { model: string; state: string; link: CustomerLinkDetailDTO; }
interface BatchResult { model: string; result: string; detail: string; }

const batchServiceVisible = ref(false);
const batchServiceKind = ref<BatchServiceKind>("disable");
const batchServiceItems = ref<BatchServiceItem[]>([]);
const batchServiceSkipped = ref<string[]>([]);
const batchServiceReason = ref("");
const batchServiceConfirmed = ref(false);
const batchServiceBusy = ref(false);
const batchServiceError = ref("");
const batchResults = ref<BatchResult[]>([]);
const batchResultVisible = ref(false);

function openBatchService(kind: BatchServiceKind) {
  if (!canEdit.value) return;
  const spec = BATCH_SERVICE_SPEC[kind];
  if (!batchSelected.value.length) {
    ElMessage.warning("请显式勾选要批量操作的客户模型");
    return;
  }
  const items: BatchServiceItem[] = [];
  const skipped: string[] = [];
  for (const row of batchSelected.value) {
    const name = row.model_name || `标准模型 #${row.standard_model_id}`;
    // 待配置行没有来源，禁用/启用对它没有意义；跳过并在弹窗里说明。
    const links = row.links.filter(link => spec.from.includes(link.state)
      && (link.available_actions.includes(spec.first) || link.available_actions.includes(spec.second)));
    if (!links.length) { skipped.push(name); continue; }
    for (const link of links) items.push({ model: name, state: link.state_label, link });
  }
  if (!items.length) {
    ElMessage.warning(`所选模型当前没有可${spec.label}的来源（只有「服务中」可禁用、「已禁用」可启用）`);
    return;
  }
  batchServiceKind.value = kind;
  batchServiceItems.value = items;
  batchServiceSkipped.value = skipped;
  batchServiceReason.value = "";
  batchServiceConfirmed.value = false;
  batchServiceError.value = "";
  batchServiceVisible.value = true;
}

/** 一个绑定走完两步；第二步必须带第一步返回的 revision（乐观锁）。 */
async function runServiceActions(link: CustomerLinkDetailDTO, kind: BatchServiceKind, reason: string) {
  const spec = BATCH_SERVICE_SPEC[kind];
  let revision = link.revision;
  let state = link.state;
  if (state === spec.from[0] && link.available_actions.includes(spec.first)) {
    const next = await customerDetailApi.actLink(link.id, { action: spec.first, reason, expected_revision: revision });
    revision = next.revision;
    state = next.state;
  }
  if (spec.from.includes(state)) {
    await customerDetailApi.actLink(link.id, { action: spec.second, reason, expected_revision: revision });
  }
}

async function submitBatchService() {
  if (batchServiceBusy.value) return;
  if (!batchServiceReason.value.trim()) { batchServiceError.value = "请填写原因 / 依据（会写入审计）"; return; }
  if (!batchServiceConfirmed.value) { batchServiceError.value = "请勾选「已确认生产平台操作已完成」"; return; }
  const kind = batchServiceKind.value;
  const spec = BATCH_SERVICE_SPEC[kind];
  const reason = batchServiceReason.value.trim();
  batchServiceBusy.value = true;
  batchServiceError.value = "";
  const results: BatchResult[] = [];
  for (const item of batchServiceItems.value) {
    try {
      await runServiceActions(item.link, kind, reason);
      results.push({ model: item.model, result: `已${spec.label}`, detail: `绑定 #${item.link.id}` });
    } catch (value) {
      // 每个模型独立登记：失败只记录这一条，继续处理后面的（原型也是逐项报告）。
      results.push({ model: item.model, result: "失败", detail: message(value) });
    }
  }
  batchServiceBusy.value = false;
  batchServiceVisible.value = false;
  batchResults.value = results;
  batchResultVisible.value = true;
  justArchivedModel.value = false;
  modelTable.value?.clearSelection();
  batchSelected.value = [];
  await loadModels(true);
  if (logLoaded.value) { logLoaded.value = false; void loadLogs(true); }
  void loadProfile();
}

/**
 * 原型「配置资源」：勾选同一申请里的待配置模型，登记技术配置完成。
 *
 * 我们的 CONFIG 是**流程级节点**，一次确认覆盖整单申请的全部模型；接口没有行级配置能力。
 * 所以这里先校验「所选模型都在当前申请内」，再打开真实的节点完成弹窗（同源、可审计），
 * 并把所选模型写进操作说明。不要在这里造一个只改状态的前端假动作。
 */
async function prepareConfigAccess() {
  if (!canEdit.value) return;
  if (!batchSelected.value.length) {
    ElMessage.warning("请先勾选要配置资源的模型");
    return;
  }
  await loadFlow();
  const current = flow.value;
  if (!current) {
    ElMessage.warning("该客户还没有开通申请，请先在「开通流程」发起开通");
    return;
  }
  const config = current.nodes.find(node => node.key === "CONFIG");
  if (!config) {
    ElMessage.warning("当前申请没有「资源配置」节点");
    return;
  }
  if (config.status !== "IN_PROGRESS") {
    ElMessage.warning(`「资源配置」节点当前为${nodeStatusText(config.status)}，不能重复登记`);
    return;
  }
  const covered = new Set(current.models.map(item => item.standard_model_id));
  const outside = batchSelected.value.filter(row => !covered.has(row.standard_model_id));
  if (outside.length) {
    ElMessage.warning(`以下模型不在本次申请内：${outside.map(row => row.model_name).join("、")}`);
    return;
  }
  const names = batchSelected.value.map(row => row.model_name || `标准模型 #${row.standard_model_id}`);
  selectTab("onboarding");
  openComplete(config);
  completeReason.value = `配置资源：${names.join("、")}`;
  ElMessage.info("「资源配置」是整单节点：本次确认会一并登记本申请内的全部模型。");
}

/** 某行是否有可执行的服务动作（禁用/启用），判据与批量完全一致。 */
function rowHasServiceState(row: CustomerModelServiceRow, kind: BatchServiceKind) {
  const spec = BATCH_SERVICE_SPEC[kind];
  return row.links.some(link => spec.from.includes(link.state)
    && (link.available_actions.includes(spec.first) || link.available_actions.includes(spec.second)));
}
const canDisableRow = (row: CustomerModelServiceRow) => canEdit.value && rowHasServiceState(row, "disable");
const canRestoreRow = (row: CustomerModelServiceRow) => canEdit.value && rowHasServiceState(row, "restore");
/** 行内「禁用模型 / 启用模型」：复用批量弹窗，保证与勾选批量是同一条路径。 */
function openRowService(row: CustomerModelServiceRow, kind: BatchServiceKind) {
  batchSelected.value = [row];
  openBatchService(kind);
}

/** 行内「去配置」：等价于只勾这一行再点批量「配置资源」。 */
function prepareConfigAccessFromRow(row: CustomerModelServiceRow) {
  batchSelected.value = [row];
  void prepareConfigAccess();
}

// ---------------- Tab2 开通流程 ----------------
const flow = ref<OnboardingFlowDTO>();
const flowLoading = ref(false);
const flowError = ref("");
const flowLoaded = ref(false);
/** 没有流程是正常状态（后端返回 404），单独记一个标记，避免把 404 当错误展示。 */
const flowMissing = ref(false);

async function loadFlow() {
  if (flowLoaded.value || flowLoading.value) return;
  flowLoading.value = true;
  flowError.value = "";
  try {
    flow.value = await customerDetailApi.onboarding(customerId.value);
    flowMissing.value = false;
    flowLoaded.value = true;
  } catch (value) {
    if (value instanceof ApiError && value.httpStatus === 404) {
      flow.value = undefined;
      flowMissing.value = true;
      flowLoaded.value = true;
    } else {
      flowError.value = message(value);
    }
  } finally {
    flowLoading.value = false;
  }
}

const startVisible = ref(false);
function openStart() { startVisible.value = true; }
function onStarted(next: OnboardingFlowDTO) {
  flow.value = next;
  flowMissing.value = false;
  flowLoaded.value = true;
  startVisible.value = false;
  // 开通发起后「接入模型」可能还没变化，但流程节点变了；这里只刷新流程与页头。
  void loadProfile();
}

// 完成节点：CONTRACT 必须填说明（作为「合同与授信已归档」的确认依据），可选填 F12 凭据。
const completeVisible = ref(false);
const completeNode = ref<OnboardingNodeDTO>();
const completeReason = ref("");
const completeReference = ref("");
const completeSubmitting = ref(false);
const completeError = ref("");

function openComplete(node: OnboardingNodeDTO) {
  completeNode.value = node;
  completeReason.value = "";
  completeReference.value = "";
  completeError.value = "";
  completeVisible.value = true;
}

async function submitComplete() {
  if (!completeNode.value || completeSubmitting.value) return;
  if (!completeReason.value.trim()) {
    completeError.value = "请填写本次操作说明（CONTRACT 节点即「合同与授信已归档」的确认依据）。";
    return;
  }
  completeSubmitting.value = true;
  completeError.value = "";
  try {
    const next = await customerDetailApi.completeNode(completeNode.value.id, {
      reason: completeReason.value.trim(),
      f12_reference: completeReference.value.trim() || undefined,
    });
    flow.value = next;
    completeVisible.value = false;
    ElMessage.success("节点已完成。");
    // CONFIG 完成后会写客户来源绑定 → 接入模型 Tab 需要重新拉一次。
    modelLoaded.value = false;
    void loadModels();
  } catch (value) {
    completeError.value = message(value);
  } finally {
    completeSubmitting.value = false;
  }
}

const nodeStatusText = (status: string) =>
  ({ IN_PROGRESS: "办理中", WAITING_PREDECESSOR: "等待前序", COMPLETED: "已完成", CANCELED: "已取消", DISABLED: "已停用" } as Record<string, string>)[status] || status;
const nodeStatusType = (status: string) =>
  status === "COMPLETED" ? "success" : status === "IN_PROGRESS" ? "warning" : "info";
const nodeKeyText = (key: string) => ({ CONTRACT: "合同与授信", CONFIG: "资源配置" } as Record<string, string>)[key] || key;

// 原型「档案与办理 → 主体资料」的维护表单。
const profileFormOpen = ref(false), profileSaving = ref(false), profileFormError = ref("");
const profileForm = reactive({ contact: "", taxRate: "", gateway: "", remark: "", reason: "" });
function openProfileForm() {
  profileFormOpen.value = true;
  profileFormError.value = "";
  profileForm.contact = profile.value?.contact_name || "";
  profileForm.taxRate = profile.value?.tax_rate ? String(Number(profile.value.tax_rate) * 100) : "";
  profileForm.gateway = profile.value?.gateway_username || "";
  profileForm.remark = profile.value?.remark || "";
  profileForm.reason = "";
}
async function saveProfile() {
  if (!profileForm.reason.trim()) { profileFormError.value = "请填写操作说明。"; return; }
  if (profileSaving.value) return;
  profileSaving.value = true; profileFormError.value = "";
  try {
    profile.value = await customerDetailApi.updateProfile(customerId.value, {
      contact_name: profileForm.contact.trim(),
      tax_rate: profileForm.taxRate.trim(),
      gateway_username: profileForm.gateway.trim(),
      remark: profileForm.remark.trim(),
      reason: profileForm.reason.trim(),
    }, crypto.randomUUID());
    profileFormOpen.value = false;
    ElMessage.success("主体资料已更新");
  } catch (value) {
    profileFormError.value = message(value);
  } finally {
    profileSaving.value = false;
  }
}

// ---------------- Tab3 该客户报价 ----------------
const quoteRows = ref<CustomerQuoteRow[]>([]);
const approvalRows = ref<CustomerApprovalRow[]>([]);
const quoteLoading = ref(false);
const quoteError = ref("");
const quoteLoaded = ref(false);

async function loadQuotes(force = false) {
  if (force) quoteLoaded.value = false;
  if (quoteLoaded.value || quoteLoading.value) return;
  quoteLoading.value = true;
  quoteError.value = "";
  try {
    const result = await customerWorkspaceApi.quotes({ customer_id: Number(customerId.value), page: 1, size: 200 });
    quoteRows.value = result.list;
    // 原型「该客户报价」有「报价记录 + 报价审批」两张表；审批接口不带客户维度，
    // 用本客户的报价单号反查，避免把别人的审批显示出来。
    try {
      const all = await customerWorkspaceApi.approvals();
      const ids = new Set(result.list.map(row => row.id));
      approvalRows.value = all.filter(row => ids.has(row.quote_id));
    } catch { approvalRows.value = []; }
    // 报价单审批（000057）与上面的特价审批是两套，列表分别拉。
    await loadQuoteApprovals();
    quoteLoaded.value = true;
  } catch (value) {
    quoteError.value = message(value);
  } finally {
    quoteLoading.value = false;
  }
}

const quoteStatusText = (status: string) =>
  ({ EFFECTIVE: "生效", DRAFT: "草稿", PENDING: "待审批", APPROVED: "已批准", EXPIRED: "已过期", REJECTED: "已驳回", FORMAL: "正式" } as Record<string, string>)[status] || status;
const quoteTypeText = (type: string) =>
  ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", RESOURCE: "按资源报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;

/** 新建报价：跳回列表页的报价 Tab，复用那边的编辑器（本页不搬 380 行编辑器）。 */
function newQuote() {
  void router.push({ path: "/internal/customers", query: { tab: "quote", customer_id: customerId.value } });
}

// ---------------- Tab3 报价单审批（原型：销售提交 → 超管逐模型批准/驳回） ----------------
//
// 与下面「特价审批」的区别：特价批的是「允许破线」（change_request[SPECIAL_PRICE]，两步角色链），
// 这里批的是「这张报价单这批模型可以对外」（迁移 000057，一步、M9:A、逐模型）。
// 两者并存，所以这个页签里是两块表，标题分开写，别混成一张。
const quoteApprovalRows = ref<QuoteApprovalSummaryDTO[]>([]);
const quoteApprovalError = ref("");
const quoteApprovalLoading = ref(false);
const quoteApprovalVisible = ref(false);
const quoteApprovalDetail = ref<QuoteApprovalDTO>();
const quoteApprovalDetailLoading = ref(false);
const quoteApprovalDetailError = ref("");
const quoteApprovalBusy = ref(false);
/** 提交中/撤回中的报价单 ID，避免连点。 */
const quoteApprovalActing = ref<number>(0);
/** 逐模型草稿：row_id → {result, reason}。只放待决行。 */
const decisionDraft = ref<Record<number, { result: "APPROVED" | "REJECTED"; reason: string }>>({});

const QUOTE_APPROVAL_STATUS: Record<string, { label: string; type: "success" | "danger" | "info" | "warning" }> = {
  PENDING: { label: "待审批", type: "warning" },
  APPROVED: { label: "已批准", type: "success" },
  REJECTED: { label: "已驳回", type: "danger" },
  WITHDRAWN: { label: "已撤回", type: "info" },
};
const approvalStatusLabel = (status: string) => QUOTE_APPROVAL_STATUS[status]?.label ?? status;
const approvalStatusType = (status: string) => QUOTE_APPROVAL_STATUS[status]?.type ?? "info";

async function loadQuoteApprovals(signal?: AbortSignal) {
  quoteApprovalLoading.value = true;
  quoteApprovalError.value = "";
  try {
    quoteApprovalRows.value = await quoteApprovalApi.list(customerId.value, "", signal);
  } catch (value) {
    quoteApprovalError.value = message(value);
  } finally {
    quoteApprovalLoading.value = false;
  }
}

/** 某张报价单当前的待审批单（同一报价单同时只会有一个）。 */
function pendingApprovalOf(quoteID: number) {
  return quoteApprovalRows.value.find(row => row.quote_id === quoteID && row.status === "PENDING");
}

const quotePendingRows = computed(() => (quoteApprovalDetail.value?.rows ?? []).filter(row => row.result === "PENDING"));

/** 能不能批：M9:A + 不是提交人 + 审批单还在待处理。前端只做提示，服务端仍会挡。 */
const quoteApprovalBlockReason = computed(() => {
  const detail = quoteApprovalDetail.value;
  if (!detail) return "";
  if (detail.status !== "PENDING") return `该审批单已结束（${approvalStatusLabel(detail.status)}），不能重复处理。`;
  if (staffId() && detail.submitted_by === staffId()) return "不能审批自己提交的报价单，请交给其他超管。";
  if (!canQuoteApprove.value) return "当前账号没有报价审批权限（M9:A）。";
  return "";
});

function staffId(): number {
  return session.user?.staffId ? Number(session.user.staffId) : 0;
}

async function openQuoteApproval(row: QuoteApprovalSummaryDTO) {
  quoteApprovalVisible.value = true;
  quoteApprovalDetail.value = undefined;
  quoteApprovalDetailError.value = "";
  decisionDraft.value = {};
  quoteApprovalDetailLoading.value = true;
  try {
    const detail = await quoteApprovalApi.get(row.id);
    quoteApprovalDetail.value = detail;
    decisionDraft.value = Object.fromEntries(
      detail.rows.filter(item => item.result === "PENDING")
        .map(item => [item.id, { result: "APPROVED" as const, reason: "" }]),
    );
  } catch (value) {
    quoteApprovalDetailError.value = message(value);
  } finally {
    quoteApprovalDetailLoading.value = false;
  }
}

function setRowDecision(rowID: number, result: "APPROVED" | "REJECTED") {
  const current = decisionDraft.value[rowID];
  if (!current) return;
  decisionDraft.value = { ...decisionDraft.value, [rowID]: { ...current, result } };
}

function setRowReason(rowID: number, reason: string) {
  const current = decisionDraft.value[rowID];
  if (!current) return;
  decisionDraft.value = { ...decisionDraft.value, [rowID]: { ...current, reason } };
}

async function submitQuoteApprovalDecision() {
  const detail = quoteApprovalDetail.value;
  if (!detail || quoteApprovalBusy.value) return;
  const pending = detail.rows.filter(row => row.result === "PENDING");
  if (!pending.length) return;
  const missing = pending.filter(row => {
    const draft = decisionDraft.value[row.id];
    return !draft || (draft.result === "REJECTED" && !draft.reason.trim());
  });
  if (missing.length) {
    quoteApprovalDetailError.value = `以下模型还没填驳回原因：${missing.map(row => row.model_name).join("、")}`;
    return;
  }
  const decisions: ApprovalDecisionInput[] = pending.map(row => {
    const draft = decisionDraft.value[row.id];
    return { row_id: row.id, result: draft.result, reason: draft.result === "REJECTED" ? draft.reason.trim() : undefined };
  });
  const approved = decisions.filter(item => item.result === "APPROVED").length;
  quoteApprovalBusy.value = true;
  quoteApprovalDetailError.value = "";
  try {
    await ElMessageBox.confirm(
      `报价单 #${detail.quote_id} V${detail.quote_version}：批准 ${approved} 个模型，驳回 ${decisions.length - approved} 个。`
      + (approved === decisions.length ? "" : " 有驳回时报价单会回到草稿，销售可改价后重新提交。"),
      "提交审批结论",
      { confirmButtonText: "确认提交", cancelButtonText: "取消", type: approved === decisions.length ? "info" : "warning" },
    );
    quoteApprovalDetail.value = await quoteApprovalApi.decide(detail.id, decisions, crypto.randomUUID());
    decisionDraft.value = {};
    ElMessage.success(approved === decisions.length ? "已全部批准，报价单状态为已批准。" : "已提交结论，报价单回到草稿。");
    await loadQuoteApprovals();
    await loadQuotes(true);
    if (logLoaded.value) { logLoaded.value = false; void loadLogs(true); }
  } catch (value) {
    if (value !== "cancel" && value !== "close") quoteApprovalDetailError.value = message(value);
  } finally {
    quoteApprovalBusy.value = false;
  }
}

/** 提交审批：DRAFT → PENDING。销售在报价单行上直接提，不必回报价编辑器。 */
async function submitQuoteForApproval(row: CustomerQuoteRow) {
  if (quoteApprovalActing.value) return;
  quoteApprovalActing.value = row.id;
  try {
    await ElMessageBox.confirm(
      `把报价单 #${row.id} V${row.version_no}（${row.item_count} 个价项）提交审批？提交后销售不能再改，需超管逐模型批准。`,
      "提交报价审批",
      { confirmButtonText: "提交", cancelButtonText: "取消" },
    );
    await quoteApprovalApi.submit(row.id, crypto.randomUUID());
    ElMessage.success("已提交审批");
    await loadQuoteApprovals();
    await loadQuotes(true);
  } catch (value) {
    if (value !== "cancel" && value !== "close") ElMessage.error(message(value));
  } finally {
    quoteApprovalActing.value = 0;
  }
}

async function withdrawQuoteApproval(target: { id: number; quote_id: number }) {
  if (quoteApprovalActing.value) return;
  const approvalID = target.id;
  quoteApprovalActing.value = approvalID;
  try {
    await ElMessageBox.confirm(`撤回报价单 #${target.quote_id} 的审批？撤回后报价单回到草稿。`, "撤回审批", {
      confirmButtonText: "撤回", cancelButtonText: "取消", type: "warning",
    });
    const next = await quoteApprovalApi.withdraw(approvalID, crypto.randomUUID());
    if (quoteApprovalVisible.value && quoteApprovalDetail.value?.id === approvalID) quoteApprovalDetail.value = next;
    ElMessage.success("已撤回");
    await loadQuoteApprovals();
    await loadQuotes(true);
  } catch (value) {
    if (value !== "cancel" && value !== "close") {
      const text = message(value);
      if (quoteApprovalVisible.value) quoteApprovalDetailError.value = text;
      else ElMessage.error(text);
    }
  } finally {
    quoteApprovalActing.value = 0;
  }
}

// ---------------- Tab3 报价审批（特价审批链，可操作） ----------------
//
// 后端这张表是 `change_request[change_type=SPECIAL_PRICE]`：报价破线时建单，
// 固定两步（PRICING_OP → FINANCE），全步通过才把 `customer_quote.special_price_status` 置 APPROVED。
// 之前这里只读，等于这条链路在界面上走不完——决策接口一直是通用的
// `POST /internal/approvals/:id/decision`，只是没人调。
//
// 前端只做「能不能点」的提示，**不要**复制一份后端规则当真相：
// 角色匹配、同单两步不能同一人、步骤已决策，最终都由服务层裁决（403/400/409 都有文案）。
const APPROVAL_ROLE_LABELS: Record<string, string> = {
  PLATFORM_ADMIN: "平台管理员", MODEL_OPS: "模型运营", PROCUREMENT: "采购",
  PRICING_OP: "定价运营", SALES: "销售", FINANCE: "财务", OPS_ADMIN: "运营管理员",
  RETRO_OP: "补录专员", AUDIT_READONLY: "审计只读", LEADER: "领导", STAFF: "员工",
};
const roleText = (code?: string | null) => (code ? APPROVAL_ROLE_LABELS[code] ?? code : "—");
const approvalStatusText = (status?: string) =>
  ({ PENDING: "待审批", CONFIRMING: "待确认", APPROVING: "审批中", APPROVED: "已批准", EFFECTIVE: "已生效", REJECTED: "已驳回" } as Record<string, string>)[status ?? ""] || status || "—";

/**
 * 后端这两列存的是 `staff:<id>`（审批人存纯 id）。
 * 前端没有员工目录接口，硬翻名字只能靠猜——所以只把它翻成能读的「员工 #id」，
 * 不假装知道是谁。真要显示姓名，得由后端 JOIN internal_staff 下发。
 */
function staffText(raw?: string | null) {
  const text = (raw ?? "").trim();
  if (!text) return "—";
  const matched = /^staff:(\d+)$/.exec(text);
  if (matched) return `员工 #${matched[1]}`;
  return /^\d+$/.test(text) ? `员工 #${text}` : text;
}

const approvalVisible = ref(false);
const approvalTarget = ref<CustomerApprovalRow>();
const approvalDetail = ref<ChangeRequestDetailDTO>();
const approvalLoading = ref(false);
const approvalError = ref("");
const approvalComment = ref("");
const approvalBusy = ref(false);

/** payload / margin_preview 在 DTO 里是 unknown：取不到就当没填，不要编数字。 */
function objectOf(raw: unknown): Record<string, unknown> {
  return raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
}
const approvalPayload = computed(() => objectOf(approvalDetail.value?.payload));
const approvalMargin = computed(() => objectOf(approvalDetail.value?.marginPreview));
const payloadText = (record: Record<string, unknown>, key: string) => {
  const value = record[key];
  return value === null || value === undefined || value === "" ? "—" : String(value);
};

const approvalPending = computed(() => approvalDetail.value?.pendingStepNo ?? null);

/** 当前账号在本单上是否还可能有权点：只排除「明显没戏」的情况，剩下交给后端。 */
const approvalBlockReason = computed(() => {
  const detail = approvalDetail.value;
  if (!detail) return "";
  if (approvalPending.value === null) return "该单没有待处理步骤（已批准或已驳回）。";
  const me = session.user?.staffId ? String(session.user.staffId) : "";
  // 同一单的两个步骤不得由同一人完成（后端 ErrSelfApproval），提前告知避免白点。
  if (me && detail.steps.some(step => step.decision && String(step.approverId ?? "") === me))
    return "同一单的两个步骤不能由同一人完成，请交给另一位审批人。";
  const roles = session.user?.roleCodes ?? [];
  if (roles.length && detail.pendingRole && !roles.includes(detail.pendingRole))
    return `本步需要「${roleText(detail.pendingRole)}」角色，当前账号无权审批。`;
  return "";
});
const approvalCanDecide = computed(() => Boolean(approvalDetail.value) && approvalPending.value !== null && !approvalBlockReason.value);

async function openApproval(row: CustomerApprovalRow) {
  approvalTarget.value = row;
  approvalVisible.value = true;
  approvalDetail.value = undefined;
  approvalError.value = "";
  approvalComment.value = "";
  approvalLoading.value = true;
  try {
    // 列表只给报价单维度，审批链与待办步骤要读变更单详情（同源，别在前端猜 step_no）。
    approvalDetail.value = await changeRequestsApi.get(String(row.id));
  } catch (value) {
    approvalError.value = message(value);
  } finally {
    approvalLoading.value = false;
  }
}

async function decideApproval(decision: "APPROVED" | "REJECTED") {
  const detail = approvalDetail.value;
  if (!detail || approvalPending.value === null || approvalBusy.value) return;
  if (decision === "REJECTED" && !approvalComment.value.trim()) {
    approvalError.value = "驳回必须写明理由（会写进审批记录）。";
    return;
  }
  const step = approvalPending.value;
  approvalBusy.value = true;
  approvalError.value = "";
  try {
    await ElMessageBox.confirm(
      decision === "APPROVED"
        ? `确认通过报价单 #${detail.id} 的第 ${step} 步（${roleText(detail.pendingRole)}）？`
        : `确认驳回报价单 #${detail.id}？驳回后本单审批立即终止。`,
      decision === "APPROVED" ? "审批通过" : "审批驳回",
      { confirmButtonText: "确认", cancelButtonText: "取消", type: decision === "APPROVED" ? "info" : "warning" },
    );
    const result = await changeRequestsApi.decide(detail.id, {
      stepNo: step, decision, comment: approvalComment.value.trim() || undefined,
    });
    ElMessage.success(`第 ${result.stepNo} 步已${decision === "APPROVED" ? "通过" : "驳回"}，单据状态：${approvalStatusText(result.finalStatus)}`);
    approvalComment.value = "";
    approvalDetail.value = await changeRequestsApi.get(detail.id);
    // 审批结果会改报价单的 special_price_status，列表必须重拉，不能只改弹窗。
    await loadQuotes(true);
    if (logLoaded.value) { logLoaded.value = false; void loadLogs(true); }
  } catch (value) {
    if (value !== "cancel" && value !== "close") approvalError.value = message(value);
  } finally {
    approvalBusy.value = false;
  }
}

// ---------------- Tab4 操作记录 ----------------
const logRows = ref<CustomerOperationLogDTO[]>([]);
const logLoading = ref(false);
const logError = ref("");
const logLoaded = ref(false);
const logTotal = ref(0);

// 原型「操作记录」筛选：操作人 / 操作类型 / 操作时间。
const logFilters = reactive({ operator: "", action: "", range: [] as string[] });
/**
 * 「对象」列：原型用一列说明这次操作落在谁身上。
 * 库里是 entity_type + entity_id，这里翻成人话（不认识的类型原样输出，不猜语义）。
 */
const LOG_OBJECT_LABELS: Record<string, string> = {
  customer_profile: "客户档案",
  onboarding_flow: "开通流程",
  onboarding_node: "开通节点",
  customer_resource_link: "客户接入绑定",
  customer_quote: "客户报价",
  quote: "报价单",
};
function logObjectText(row: CustomerOperationLogDTO) {
  const label = LOG_OBJECT_LABELS[row.entity_type] || row.entity_type || "未记录";
  return row.entity_id ? `${label} #${row.entity_id}` : label;
}

const logActionOptions = computed(() => [...new Map(logRows.value.map(row => [row.action, row.action_label || row.action])).entries()]);
const logOperatorOptions = computed(() => [...new Set(logRows.value.map(row => row.operator_name).filter(Boolean))]);
function logRangeParams() {
  const params: { from?: string; to?: string } = {};
  if (logFilters.range[0]) params.from = new Date(`${logFilters.range[0]}T00:00:00`).toISOString();
  if (logFilters.range[1]) { const end = new Date(`${logFilters.range[1]}T00:00:00`); end.setDate(end.getDate() + 1); params.to = end.toISOString(); }
  return params;
}
async function loadLogs(force = false) {
  if (!force && (logLoaded.value || logLoading.value)) return;
  logLoading.value = true;
  logError.value = "";
  try {
    const result = await customerDetailApi.operationLogs(customerId.value, 1, 100, undefined, {
      operator: logFilters.operator.trim() || undefined, action: logFilters.action || undefined, ...logRangeParams(),
    });
    logRows.value = result.list;
    logTotal.value = result.total;
    logLoaded.value = true;
  } catch (value) {
    logError.value = message(value);
  } finally {
    logLoading.value = false;
  }
}
function resetLogFilters() { logFilters.operator = ""; logFilters.action = ""; logFilters.range = []; void loadLogs(true); }

const logDetail = ref<CustomerOperationLogDTO>();
/** before/after 是 JSON 字符串：能解析就格式化，解析不了原样展示（不要让详情弹窗白屏）。 */
function prettyJson(raw: string) {
  const text = (raw ?? "").trim();
  if (!text) return "（空）";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

// ---------------- 移交（共用弹窗） ----------------
const transferVisible = ref(false);

// ---------------- 首屏与 Tab 切换加载 ----------------
void loadProfile();
// 不在这里无条件 loadModels()：直接落在 `?tab=onboarding` 时不该打 resource-links。
// 懒加载统一由下面的 watch(immediate) 驱动 —— 初始 Tab 是 models 时它会在首帧就触发。
watch(activeTab, tab => {
  if (tab === "models") void loadModels();
  if (tab === "onboarding") void loadFlow();
  if (tab === "quotes") void loadQuotes();
  if (tab === "records") void loadLogs();
}, { immediate: true });

function backToList() {
  void router.push({ path: "/internal/customers" });
}
</script>

<template>
  <section class="detail-page">
    <div class="page-head">
      <el-button link @click="backToList">← 返回客户列表</el-button>
      <div class="head-main">
        <div class="head-title">
          {{ profile?.legal_name || `客户 #${customerId}` }}
          <el-tag v-if="profile" size="small" class="head-tag">{{ profile.status }}</el-tag>
        </div>
        <div class="head-sub" v-if="profile">
          销售 {{ profile.owner_sales_name }} · {{ profile.region === "OVERSEAS" ? "海外" : "国内" }} ·
          {{ profile.settlement_currency }} · 税率 {{ formatSupplierTaxRate(profile.tax_rate) }} · 合同 {{ profile.contract_status }}
        </div>
        <div class="head-sub" v-else-if="profileLoading">加载中…</div>
      </div>
      <div class="head-actions">
        <el-button v-if="canEdit && profile" @click="transferVisible = true">移交客户</el-button>
      </div>
    </div>

    <el-alert v-if="profileError" :title="profileError" type="error" show-icon :closable="false" class="block-gap" />

    <div class="tabbar">
      <!--
        注意：这里用 v-show（不是 v-if）——Tab 按钮始终在 DOM 里，只是不可见。
        写自动化用例时必须断言**可见性**（isVisible / offsetParent），不能数元素个数，
        否则纯 M8 角色下也会数到 4 个 Tab 而误判"有权限"。
      -->
      <button
        v-for="tab in TABS"
        :key="tab"
        class="tab"
        :class="{ active: activeTab === tab }"
        v-show="tab !== 'quotes' || canQuoteView"
        @click="selectTab(tab)"
      >
        {{ TAB_LABELS[tab] }}
      </button>
    </div>

    <!-- ============ Tab 1 · 接入模型 ============ -->
    <template v-if="activeTab === 'models'">
      <el-alert v-if="modelError" :title="modelError" type="error" show-icon :closable="false" class="block-gap" />
      <div class="model-filterbar">
        <label>模型公司<el-select v-model="modelFilters.company" clearable placeholder="全部" aria-label="模型公司" style="width:150px"><el-option v-for="company in modelCompanies" :key="company" :label="company" :value="company" /></el-select></label>
        <label>模型类型<el-select v-model="modelFilters.modelType" clearable placeholder="全部" aria-label="模型类型" style="width:130px"><el-option v-for="(name, key) in modelTypeLabels" :key="key" :label="name" :value="key" /></el-select></label>
        <el-input
          v-model="modelFilters.keyword"
          clearable
          aria-label="搜索模型名称 / 官方 ID"
          placeholder="搜索模型名称 / 官方 ID"
          style="width:230px"
        />
        <el-button @click="resetModelFilters">重置</el-button>
        <span v-if="modelRows.length" class="hint">共 {{ filteredModelRows.length }} / {{ modelRows.length }} 条</span>
      </div>
      <div class="model-batchbar" aria-label="客户模型批量操作">
        <el-button :disabled="!canEdit || !batchSelected.length" @click="prepareConfigAccess">配置资源</el-button>
        <el-button :disabled="!canEdit || !batchSelected.length" @click="openBatchService('disable')">禁用</el-button>
        <el-button :disabled="!canEdit || !batchSelected.length" @click="openBatchService('restore')">启用</el-button>
        <span class="hint">{{ batchSelected.length ? `已选 ${batchSelected.length} 个模型` : "勾选后可批量配置资源 / 禁用 / 启用；待配置行需先配来源" }}</span>
      </div>
      <el-table
        ref="modelTable"
        v-loading="modelLoading"
        :data="filteredModelRows"
        row-key="standard_model_id"
        class="grid"
        :row-class-name="({ row }: { row: CustomerModelServiceRow }) => row.service_state === '无可用来源' ? 'row-muted' : ''"
        :empty-text="modelEmptyText"
        @selection-change="(rows: CustomerModelServiceRow[]) => batchSelected = rows"
      >
        <el-table-column type="selection" width="44" :selectable="canBatchRow" reserve-selection />
        <!-- 展开区：该模型下的绑定明细 + 按状态机给出的动作（A2） -->
        <el-table-column type="expand">
          <template #default="{ row }: { row: CustomerModelServiceRow }">
            <div class="link-detail">
              <el-table :data="row.links" size="small" class="grid" empty-text="该模型暂无未归档的绑定">
                <el-table-column label="供应商" min-width="160">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <div class="cell-main">{{ link.supplier_name || "—" }}</div>
                    <div class="cell-sub">部署节点 {{ link.region }}</div>
                  </template>
                </el-table-column>
                <el-table-column label="供应资源" width="150">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <div class="mono">#{{ link.resource_id }}</div>
                    <div class="cell-sub">资源状态 {{ link.resource_state || "—" }}</div>
                  </template>
                </el-table-column>
                <el-table-column label="绑定状态" width="130">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <el-tag
                      size="small"
                      :type="link.state === 'ACTIVE' ? 'success' : link.state === 'SUPPLY_OFFLINE_PENDING' ? 'danger' : 'info'"
                    >
                      {{ link.state_label }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="连通 · ms" width="100" align="right">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.test_connection_ms || "未记录" }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="RPS" width="90" align="right">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.test_rps || "未记录" }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="TPS" width="90" align="right">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.test_tps || "未记录" }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="输入缓存命中率" width="130" align="right">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.test_cache_hit_rate ? link.test_cache_hit_rate + "%" : "未记录" }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="生效时间" width="170">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.activated_at ? formatDateTime(link.activated_at) : "—" }}</span>
                  </template>
                </el-table-column>
                <!-- 原型展开区有独立的「测试与报告」入口：报告是准入证据，不该只能从模型广场找。 -->
                <el-table-column label="测试与报告" width="110">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <el-button size="small" link type="primary" @click="openResourceReports(row, link)">测试与报告</el-button>
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="150">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <template v-if="canEdit && link.available_actions.length">
                      <el-button
                        v-for="action in link.available_actions"
                        :key="action"
                        size="small"
                        :type="linkActionTagType(action)"
                        :loading="linkActionSubmitting === `${link.id}:${action}`"
                        :disabled="Boolean(linkActionSubmitting) && linkActionSubmitting !== `${link.id}:${action}`"
                        @click="runLinkAction(link, action)"
                      >
                        {{ linkActionLabel(action) }}
                      </el-button>
                    </template>
                    <span v-else class="hint">{{ link.available_actions.length ? "无编辑权限" : "无可用动作" }}</span>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="模型名称 / 官方 ID" min-width="210" sortable :sort-by="sortByName">
          <template #default="{ row }">
            <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
            <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
          </template>
        </el-table-column>
        <el-table-column label="模型公司" width="130" sortable :sort-by="sortByCompany">
          <template #default="{ row }">{{ row.company || "待补齐" }}</template>
        </el-table-column>
        <el-table-column label="模型类型" width="100" sortable :sort-by="sortByType">
          <template #default="{ row }">{{ modelTypeLabels[row.model_type] || "待补齐" }}</template>
        </el-table-column>
        <el-table-column label="部署区域" width="100" sortable :sort-by="sortByRegion">
          <template #default="{ row }">{{ row.region === "OVERSEAS" ? "海外" : row.region === "CN" ? "国内" : "—" }}</template>
        </el-table-column>
        <el-table-column label="客户报价系数" width="130">
          <template #default><span class="hint">待核对</span></template>
        </el-table-column>
        <el-table-column label="采购系数" width="110" sortable :sort-by="sortByCoefficient">
          <template #default="{ row }">{{ row.coefficient ? coefficientText(row.coefficient) : "—" }}</template>
        </el-table-column>
        <el-table-column label="模型服务状态" width="140">
          <template #default="{ row }">
            <el-tag size="small" :type="row.service_state === '服务中' ? 'success' : row.service_state === '已禁用' ? 'info' : 'warning'">
              {{ row.service_state }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="有效可用资源数" width="140" align="right" sortable :sort-by="sortByAvailable">
          <template #default="{ row }"><span class="mono">{{ row.available_count }}</span></template>
        </el-table-column>
        <el-table-column label="已绑定资源数" width="130" align="right" sortable :sort-by="sortByBound">
          <template #default="{ row }"><span class="mono">{{ row.bound_count }}</span></template>
        </el-table-column>
        <el-table-column label="风险异常" min-width="200" sortable :sort-by="sortByRisk">
          <template #default="{ row }">
            <el-tag v-for="flag in row.risk_flags" :key="flag" size="small" type="danger" class="stack-tag">{{ flag }}</el-tag>
            <span v-if="!row.risk_flags?.length" class="hint">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="170" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.pending_config" link type="primary" @click="prepareConfigAccessFromRow(row)">配置资源</el-button>
            <template v-else>
              <el-button v-if="canDisableRow(row)" link type="danger" @click="openRowService(row, 'disable')">禁用模型</el-button>
              <el-button v-if="canRestoreRow(row)" link type="primary" @click="openRowService(row, 'restore')">启用模型</el-button>
              <span v-if="!canDisableRow(row) && !canRestoreRow(row)" class="hint">展开查看来源</span>
            </template>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <!-- ============ Tab 2 · 档案与办理（deeplink 落点） ============ -->
    <template v-if="activeTab === 'onboarding'">
      <el-alert v-if="flowError" :title="flowError" type="error" show-icon :closable="false" class="block-gap" />

      <div class="card">
        <div class="card-head">
          <div class="card-head-left"><h3 class="card-title">主体资料</h3></div>
          <div class="card-head-right">
            <el-button v-if="canEdit && profile" type="primary" plain size="small" @click="openProfileForm">维护主体资料</el-button>
          </div>
        </div>
        <el-descriptions v-if="profile" :column="3" border size="small">
          <el-descriptions-item label="客户名称">{{ profile.legal_name }}</el-descriptions-item>
          <el-descriptions-item label="联系人">{{ profile.contact_name || "未填写" }}</el-descriptions-item>
          <el-descriptions-item label="国内／海外">{{ profile.region === "OVERSEAS" ? "海外" : "国内" }}</el-descriptions-item>
          <el-descriptions-item label="税率">{{ formatSupplierTaxRate(profile.tax_rate) }}</el-descriptions-item>
          <el-descriptions-item label="网关用户名">{{ profile.gateway_username || "未填写" }}</el-descriptions-item>
          <el-descriptions-item label="备注">{{ profile.remark || "未填写" }}</el-descriptions-item>
        </el-descriptions>
        <div v-else class="hint">加载客户档案后显示主体资料。</div>
      </div>

      <div class="card">
        <div class="card-head">
          <div class="card-head-left"><h3 class="card-title">合同与授信</h3></div>
        </div>
        <F12SubjectPanel
          v-if="profile"
          subject-type="CUSTOMER"
          :subject-id="customerId"
          :can-manage="canManageF12"
          @changed="loadProfile"
        />
        <div v-else class="hint">加载客户档案后显示合同与授信记录。</div>
      </div>

      <div class="card">
        <div class="card-head">
          <div class="card-head-left">
            <h3 class="card-title">开通流程</h3>
            <el-tag v-if="flow" size="small" :type="flow.status === 'COMPLETED' ? 'success' : 'warning'">
              {{ flow.status === "COMPLETED" ? "已完成" : "办理中" }}
            </el-tag>
          </div>
          <div class="card-head-right">
            <el-button v-if="canEdit && flowMissing" type="primary" @click="openStart">发起开通</el-button>
          </div>
        </div>

        <div v-loading="flowLoading">
          <el-empty v-if="flowMissing" description="尚未发起开通" :image-size="64" />
          <template v-else-if="flow">
            <el-table :data="flow.nodes" size="small" class="grid" row-key="id">
              <el-table-column label="节点" width="150">
                <template #default="{ row }">{{ nodeKeyText(row.key) }}</template>
              </el-table-column>
              <el-table-column label="状态" width="120">
                <template #default="{ row }">
                  <el-tag size="small" :type="nodeStatusType(row.status)">{{ nodeStatusText(row.status) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="办理人" width="140" prop="assignee_name" />
              <el-table-column label="完成时间" width="180">
                <template #default="{ row }">{{ row.completed_at ? formatDateTime(row.completed_at) : "—" }}</template>
              </el-table-column>
              <el-table-column label="说明" min-width="260">
                <template #default="{ row }">
                  <div class="note">{{ row.note || "—" }}</div>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="100" fixed="right">
                <template #default="{ row }">
                  <el-button
                    v-if="canEdit && row.status === 'IN_PROGRESS'"
                    link
                    type="primary"
                    @click="openComplete(row)"
                  >完成节点</el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="sub-block">
              <div class="selected-title">本次开通的模型（{{ flow.models.length }}）</div>
              <el-table :data="flow.models" size="small" class="grid" empty-text="没有模型明细">
                <el-table-column label="模型" min-width="180" prop="model_name" />
                <el-table-column label="供应商" min-width="150" prop="supplier_name" />
                <el-table-column label="节点" width="90" prop="region" />
                <el-table-column label="绑定状态" width="120">
                  <template #default="{ row }">
                    <el-tag size="small" type="success">已开通</el-tag>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </div>
      </div>
    </template>

    <!-- ============ Tab 3 · 该客户报价（只读） ============ -->
    <template v-if="activeTab === 'quotes'">
      <div class="card">
        <div class="card-head">
          <div class="card-head-left"><h3 class="card-title">该客户报价</h3></div>
          <div class="card-head-right">
            <el-button v-if="canQuoteEdit" type="primary" @click="newQuote">新建报价</el-button>
          </div>
        </div>
        <el-alert v-if="quoteError" :title="quoteError" type="error" show-icon :closable="false" class="block-gap" />
        <el-table v-loading="quoteLoading" :data="quoteRows" size="small" class="grid" row-key="id" empty-text="该客户还没有报价">
          <el-table-column label="报价编号" width="110">
            <template #default="{ row }">#{{ row.id }}</template>
          </el-table-column>
          <el-table-column label="版本" width="80">
            <template #default="{ row }">V{{ row.version_no }}</template>
          </el-table-column>
          <el-table-column label="类型" width="130">
            <template #default="{ row }">{{ quoteTypeText(row.quote_type) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'EFFECTIVE' ? 'success' : row.status === 'REJECTED' ? 'danger' : 'info'">
                {{ quoteStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="价目表版本" width="110" align="right">
            <template #default="{ row }"><span class="mono">{{ row.price_book_version }}</span></template>
          </el-table-column>
          <el-table-column label="有效期至" width="180">
            <template #default="{ row }">{{ row.valid_until ? formatDateTime(row.valid_until) : "—" }}</template>
          </el-table-column>
          <el-table-column label="项数" width="90" align="right">
            <template #default="{ row }"><span class="mono">{{ row.item_count }}</span></template>
          </el-table-column>
          <el-table-column label="操作人" width="130" prop="owner_name" />
          <el-table-column label="创建时间" min-width="180">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="150" fixed="right">
            <template #default="{ row }">
              <template v-if="pendingApprovalOf(row.id)">
                <el-button
                  v-if="canQuoteEdit && pendingApprovalOf(row.id)?.submitted_by === staffId()"
                  link type="warning"
                  :loading="quoteApprovalActing === pendingApprovalOf(row.id)?.id"
                  @click="withdrawQuoteApproval(pendingApprovalOf(row.id)!)"
                >撤回审批</el-button>
                <span v-else class="hint">审批中</span>
              </template>
              <el-button
                v-else-if="canQuoteEdit && row.status === 'DRAFT'"
                link type="primary"
                :loading="quoteApprovalActing === row.id"
                @click="submitQuoteForApproval(row)"
              >提交审批</el-button>
              <span v-else class="hint">—</span>
            </template>
          </el-table-column>
        </el-table>
        <div class="hint foot">报价编辑器在「客户管理 → 客户报价单」，这里点「新建报价」会带着本客户跳过去。</div>

        <!-- 报价审批（原型）：销售提交 → 超管逐模型批准/驳回 -->
        <div class="card-head sub-block">
          <div class="card-head-left">
            <h3 class="card-title">报价审批</h3>
            <span class="hint">销售提交报价单后，超管逐模型批准/驳回；驳回需填原因</span>
          </div>
        </div>
        <el-alert v-if="quoteApprovalError" :title="quoteApprovalError" type="error" show-icon :closable="false" class="block-gap" />
        <el-table
          v-loading="quoteApprovalLoading"
          :data="quoteApprovalRows"
          size="small"
          class="grid"
          row-key="id"
          empty-text="暂无报价审批（在报价单行点「提交审批」后产生）"
        >
          <el-table-column label="报价单" width="150">
            <template #default="{ row }">#{{ row.quote_id }}</template>
          </el-table-column>
          <el-table-column label="模型数" width="90" align="right">
            <template #default="{ row }"><span class="mono">{{ row.model_count }}</span></template>
          </el-table-column>
          <el-table-column label="提交人" width="120">
            <template #default="{ row }">{{ staffText(`staff:${row.submitted_by}`) }}</template>
          </el-table-column>
          <el-table-column label="提交时间" width="180">
            <template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="approvalStatusType(row.status)">{{ approvalStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="处理时间" min-width="180">
            <template #default="{ row }">{{ row.decided_at ? formatDateTime(row.decided_at) : "—" }}</template>
          </el-table-column>
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openQuoteApproval(row)">{{ row.status === "PENDING" ? "审批" : "查看" }}</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- 特价审批（破线）：与上面的报价审批不是同一件事，单独一块 -->
        <div class="card-head sub-block">
          <div class="card-head-left">
            <h3 class="card-title">特价审批（破线）</h3>
            <span class="hint">破线报价走 change_request[SPECIAL_PRICE]，两步：定价运营 → 财务</span>
          </div>
        </div>
        <!-- 列按原型：报价单 ID / 销售与提交时间 / 状态；申请原因是我们多出来的判断依据，保留。 -->
        <el-table :data="approvalRows" size="small" class="grid" row-key="id" empty-text="暂无特价审批（报价破线申请特价后才会产生）">
          <el-table-column label="报价单 ID" width="130">
            <template #default="{ row }">{{ row.quote_id ? `#${row.quote_id}` : `#${row.id}` }}</template>
          </el-table-column>
          <el-table-column label="销售与提交时间" min-width="240">
            <template #default="{ row }">{{ staffText(row.created_by) }} · {{ formatDateTime(row.created_at, "—", false) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'APPROVED' ? 'success' : row.status === 'REJECTED' ? 'danger' : 'warning'">
                {{ ({ PENDING: "待审批", APPROVED: "已通过", REJECTED: "已驳回" } as Record<string, string>)[row.status] || row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="申请原因" min-width="240" show-overflow-tooltip><template #default="{ row }">{{ row.reason || "—" }}</template></el-table-column>
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openApproval(row)">{{ row.status === "PENDING" ? "审批" : "查看" }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>

    <!-- ============ Tab 4 · 操作记录 ============ -->
    <template v-if="activeTab === 'records'">
      <div class="model-filterbar">
        <label>操作人<el-select v-model="logFilters.operator" clearable filterable allow-create default-first-option placeholder="全部" aria-label="操作人" style="width:150px" @change="loadLogs(true)"><el-option v-for="name in logOperatorOptions" :key="name" :label="name" :value="name" /></el-select></label>
        <label>操作类型<el-select v-model="logFilters.action" clearable placeholder="全部" aria-label="操作类型" style="width:170px" @change="loadLogs(true)"><el-option v-for="[action, label] in logActionOptions" :key="action" :label="label" :value="action" /></el-select></label>
        <label>操作时间<el-date-picker v-model="logFilters.range" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始" end-placeholder="结束" aria-label="操作时间" style="width:250px" @change="loadLogs(true)" /></label>
        <el-button @click="resetLogFilters">重置</el-button>
      </div>
      <el-alert v-if="logError" :title="logError" type="error" show-icon :closable="false" class="block-gap" />
      <el-table v-loading="logLoading" :data="logRows" size="small" class="grid" row-key="id" empty-text="暂无操作记录">
        <el-table-column label="操作" width="180">
          <template #default="{ row }">{{ row.action_label || row.action }}</template>
        </el-table-column>
        <el-table-column label="对象" width="200">
          <template #default="{ row }">{{ logObjectText(row) }}</template>
        </el-table-column>
        <el-table-column label="操作人" width="130">
          <template #default="{ row }">{{ row.operator_name || "—" }}</template>
        </el-table-column>
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="说明" min-width="240">
          <template #default="{ row }">{{ row.reason || "—" }}</template>
        </el-table-column>
        <el-table-column label="详情" width="90" fixed="right">
          <template #default="{ row }"><el-button link type="primary" @click="logDetail = row">查看</el-button></template>
        </el-table-column>
      </el-table>
      <div class="hint foot" v-if="logTotal > logRows.length">仅显示最近 {{ logRows.length }} 条（共 {{ logTotal }} 条）。</div>
    </template>

    <!-- 批量禁用 / 启用（原型「禁用 / 启用」） -->
    <el-dialog v-model="batchServiceVisible" :title="`批量${BATCH_SERVICE_SPEC[batchServiceKind].label}客户模型`" width="min(680px, 94vw)" :close-on-click-modal="false">
      <p class="hint">
        客户：{{ profile?.legal_name || "—" }} · 本次 {{ batchServiceItems.length }} 个来源。
        每个来源独立登记，中途失败只影响该条，已成功项保留。
      </p>
      <el-alert v-if="batchServiceSkipped.length" type="warning" :closable="false" show-icon class="block-gap"
        :title="`以下模型已跳过（当前状态不可${BATCH_SERVICE_SPEC[batchServiceKind].label}）：${batchServiceSkipped.join('、')}`" />
      <el-table :data="batchServiceItems" size="small" class="grid">
        <el-table-column prop="model" label="模型" min-width="180" />
        <el-table-column prop="state" label="当前状态" width="120" />
        <el-table-column label="登记动作" width="110">
          <template #default>{{ BATCH_SERVICE_SPEC[batchServiceKind].label }}</template>
        </el-table-column>
      </el-table>
      <el-form label-position="top" class="batch-form">
        <el-form-item label="原因 / 依据（必填）">
          <el-input v-model="batchServiceReason" type="textarea" :rows="2" maxlength="200" aria-label="批量操作原因" placeholder="请先在外部平台完成对应操作，再在此登记；原因会写入审计" />
        </el-form-item>
      </el-form>
      <el-checkbox v-model="batchServiceConfirmed">确认上述 {{ batchServiceItems.length }} 个来源的生产平台操作均已实际完成</el-checkbox>
      <el-alert v-if="batchServiceError" :title="batchServiceError" type="error" :closable="false" class="block-gap" />
      <template #footer>
        <el-button :disabled="batchServiceBusy" @click="batchServiceVisible = false">关闭</el-button>
        <el-button type="primary" :loading="batchServiceBusy" @click="submitBatchService">逐项提交登记</el-button>
      </template>
    </el-dialog>

    <!-- 批量登记结果 -->
    <el-dialog v-model="batchResultVisible" title="批量登记结果" width="min(680px, 94vw)">
      <el-table :data="batchResults" size="small" class="grid">
        <el-table-column prop="model" label="模型" min-width="180" />
        <el-table-column label="结果" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.result === '失败' ? 'danger' : 'success'">{{ row.result }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="detail" label="说明" min-width="240" />
      </el-table>
      <template #footer><el-button type="primary" @click="batchResultVisible = false">知道了</el-button></template>
    </el-dialog>

    <!-- 主体资料维护 -->
    <el-dialog v-model="profileFormOpen" title="维护主体资料" width="min(620px, 94vw)" :close-on-click-modal="false">
      <el-form label-position="top" :disabled="profileSaving">
        <el-form-item label="联系人"><el-input v-model="profileForm.contact" maxlength="64" aria-label="联系人" placeholder="客户侧对接人" /></el-form-item>
        <el-form-item label="税率 %"><el-input v-model="profileForm.taxRate" inputmode="decimal" aria-label="税率" placeholder="例如 6 或 6.25；留空表示未填写" /></el-form-item>
        <el-form-item label="网关用户名"><el-input v-model="profileForm.gateway" maxlength="128" aria-label="网关用户名" placeholder="客户调用侧账号" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="profileForm.remark" type="textarea" :rows="3" maxlength="500" show-word-limit aria-label="备注" /></el-form-item>
        <el-form-item label="操作说明 *"><el-input v-model="profileForm.reason" maxlength="200" aria-label="操作说明" placeholder="说明本次资料调整原因（落审计）" /></el-form-item>
      </el-form>
      <el-alert v-if="profileFormError" :title="profileFormError" type="error" :closable="false" />
      <template #footer>
        <el-button :disabled="profileSaving" @click="profileFormOpen = false">取消</el-button>
        <el-button type="primary" :loading="profileSaving" @click="saveProfile">保存主体资料</el-button>
      </template>
    </el-dialog>

    <!-- 发起开通弹窗（表单与向导 Step2 共用） -->
    <el-dialog v-model="startVisible" title="发起客户开通" width="min(980px, 96vw)" :close-on-click-modal="false">
      <CustomerOnboardingStartForm
        :customer-id="customerId"
        :region="profile?.region || 'CN'"
        @started="onStarted"
      />
    </el-dialog>

    <!-- 完成节点弹窗 -->
    <el-dialog v-model="completeVisible" :title="`完成节点 · ${completeNode ? nodeKeyText(completeNode.key) : ''}`" width="min(620px, 96vw)">
      <el-form label-position="top" :disabled="completeSubmitting">
        <el-form-item label="操作说明 *">
          <el-input
            v-model="completeReason"
            type="textarea"
            :rows="3"
            maxlength="200"
            show-word-limit
            placeholder="CONTRACT：合同与授信已归档的依据；CONFIG：资源配置完成说明"
          />
        </el-form-item>
        <el-form-item label="F12 合同号 / 记录号（可选）">
          <el-input v-model="completeReference" maxlength="64" placeholder="落审计，便于日后追溯" />
        </el-form-item>
      </el-form>
      <el-alert v-if="completeError" :title="completeError" type="error" show-icon :closable="false" />
      <template #footer>
        <el-button :disabled="completeSubmitting" @click="completeVisible = false">取消</el-button>
        <el-button type="primary" :loading="completeSubmitting" @click="submitComplete">确认完成</el-button>
      </template>
    </el-dialog>

    <!-- 操作记录详情 -->
    <el-dialog v-model="logDetail" :title="logDetail ? (logDetail.action_label || logDetail.action) : '操作详情'" width="min(760px, 96vw)">
      <template v-if="logDetail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="时间">{{ formatDateTime(logDetail.created_at) }}</el-descriptions-item>
          <el-descriptions-item label="操作人">{{ logDetail.operator_name || "—" }}</el-descriptions-item>
          <el-descriptions-item label="动作码">{{ logDetail.action }}</el-descriptions-item>
          <el-descriptions-item label="对象">{{ logDetail.entity_type }} #{{ logDetail.entity_id }}</el-descriptions-item>
          <el-descriptions-item label="请求号">{{ logDetail.request_id || "—" }}</el-descriptions-item>
          <el-descriptions-item label="说明">{{ logDetail.reason || "—" }}</el-descriptions-item>
        </el-descriptions>
        <div class="json-block">
          <div class="json-title">变更前</div>
          <pre class="json">{{ prettyJson(logDetail.before) }}</pre>
        </div>
        <div class="json-block">
          <div class="json-title">变更后</div>
          <pre class="json">{{ prettyJson(logDetail.after) }}</pre>
        </div>
      </template>
    </el-dialog>

    <!-- 报价审批（原型）：逐模型批准 / 驳回，驳回必填原因 -->
    <el-dialog v-model="quoteApprovalVisible" title="报价审批" width="min(880px, 96vw)" :close-on-click-modal="false">
      <div v-loading="quoteApprovalDetailLoading">
        <el-alert v-if="quoteApprovalDetailError" :title="quoteApprovalDetailError" type="error" show-icon :closable="false" class="block-gap" />
        <template v-if="quoteApprovalDetail">
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="报价单">#{{ quoteApprovalDetail.quote_id }} · V{{ quoteApprovalDetail.quote_version }}</el-descriptions-item>
            <el-descriptions-item label="审批状态">
              <el-tag size="small" :type="approvalStatusType(quoteApprovalDetail.status)">{{ approvalStatusLabel(quoteApprovalDetail.status) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="提交人">{{ staffText(`staff:${quoteApprovalDetail.submitted_by}`) }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ formatDateTime(quoteApprovalDetail.submitted_at, "—", false) }}</el-descriptions-item>
            <el-descriptions-item label="处理人">{{ quoteApprovalDetail.decided_by ? staffText(`staff:${quoteApprovalDetail.decided_by}`) : "—" }}</el-descriptions-item>
            <el-descriptions-item label="处理时间">{{ quoteApprovalDetail.decided_at ? formatDateTime(quoteApprovalDetail.decided_at, "—", false) : "—" }}</el-descriptions-item>
          </el-descriptions>

          <div class="sub-block">
            <div class="selected-title">逐模型结论（{{ quoteApprovalDetail.rows.length }} 个模型）</div>
            <el-table :data="quoteApprovalDetail.rows" size="small" class="grid" row-key="id" empty-text="没有模型明细">
              <el-table-column label="模型" min-width="170">
                <template #default="{ row }">
                  <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
                  <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
                </template>
              </el-table-column>
              <el-table-column label="价项数" width="80" align="right">
                <template #default="{ row }"><span class="mono">{{ row.item_count }}</span></template>
              </el-table-column>
              <el-table-column label="报价区间" width="150" align="right">
                <template #default="{ row }">
                  <span class="mono">{{ row.price_min ? (row.price_min === row.price_max ? row.price_min : `${row.price_min} ~ ${row.price_max}`) : "—" }}</span>
                </template>
              </el-table-column>
              <el-table-column label="结论" width="150">
                <template #default="{ row }">
                  <el-radio-group
                    v-if="row.result === 'PENDING' && !quoteApprovalBlockReason"
                    :model-value="decisionDraft[row.id]?.result || 'APPROVED'"
                    size="small"
                    @change="(value: string | number | boolean | undefined) => setRowDecision(row.id, value === 'REJECTED' ? 'REJECTED' : 'APPROVED')"
                  >
                    <el-radio-button value="APPROVED">批准</el-radio-button>
                    <el-radio-button value="REJECTED">驳回</el-radio-button>
                  </el-radio-group>
                  <el-tag v-else-if="row.result !== 'PENDING'" size="small" :type="row.result === 'APPROVED' ? 'success' : 'danger'">
                    {{ row.result === "APPROVED" ? "已批准" : "已驳回" }}
                  </el-tag>
                  <span v-else class="hint">待处理</span>
                </template>
              </el-table-column>
              <el-table-column label="驳回原因 / 意见" min-width="200">
                <template #default="{ row }">
                  <el-input
                    v-if="row.result === 'PENDING' && !quoteApprovalBlockReason && decisionDraft[row.id]?.result === 'REJECTED'"
                    :model-value="decisionDraft[row.id]?.reason || ''"
                    size="small"
                    maxlength="200"
                    aria-label="驳回原因"
                    placeholder="驳回必须说明原因"
                    @update:model-value="(value: string) => setRowReason(row.id, value)"
                  />
                  <span v-else>{{ row.reason || "—" }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <el-alert v-if="quoteApprovalBlockReason" :title="quoteApprovalBlockReason" type="info" show-icon :closable="false" class="block-gap" />
        </template>
      </div>
      <template #footer>
        <el-button @click="quoteApprovalVisible = false">关闭</el-button>
        <el-button
          v-if="quoteApprovalDetail && quoteApprovalDetail.status === 'PENDING' && quoteApprovalDetail.submitted_by === staffId()"
          type="warning" plain :disabled="quoteApprovalBusy"
          @click="withdrawQuoteApproval({ id: quoteApprovalDetail.id, quote_id: quoteApprovalDetail.quote_id })"
        >撤回</el-button>
        <el-button
          v-if="quoteApprovalDetail && quoteApprovalDetail.status === 'PENDING' && !quoteApprovalBlockReason && quotePendingRows.length"
          type="primary" :loading="quoteApprovalBusy"
          @click="submitQuoteApprovalDecision"
        >提交审批结论</el-button>
      </template>
    </el-dialog>

    <!-- 特价审批：两步角色链（PRICING_OP → FINANCE），与上面的报价审批无关 -->
    <el-dialog v-model="approvalVisible" title="特价审批（破线）" width="min(780px, 96vw)" :close-on-click-modal="false">
      <div v-loading="approvalLoading">
        <el-alert v-if="approvalError" :title="approvalError" type="error" show-icon :closable="false" class="block-gap" />
        <template v-if="approvalDetail">
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="报价单">{{ approvalTarget?.quote_id ? `#${approvalTarget.quote_id}` : `#${approvalDetail.id}` }}</el-descriptions-item>
            <el-descriptions-item label="单据状态">{{ approvalStatusText(approvalDetail.status) }}</el-descriptions-item>
            <el-descriptions-item label="提交人">{{ staffText(approvalTarget?.created_by || approvalDetail.createdBy) }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ formatDateTime(approvalDetail.createdAt, "—", false) }}</el-descriptions-item>
            <el-descriptions-item label="审批进度">
              {{ approvalDetail.stepsApproved }} / {{ approvalDetail.stepsTotal }}
              <span v-if="approvalPending !== null" class="hint">（当前第 {{ approvalPending }} 步 · {{ roleText(approvalDetail.pendingRole) }}）</span>
            </el-descriptions-item>
            <el-descriptions-item label="风险等级">{{ approvalDetail.riskLevel || "—" }}</el-descriptions-item>
          </el-descriptions>

          <div class="sub-block">
            <div class="selected-title">申请依据</div>
            <el-descriptions :column="2" border size="small">
              <el-descriptions-item label="申请原因" :span="2">{{ payloadText(approvalPayload, "reason") }}</el-descriptions-item>
              <el-descriptions-item label="当前价">{{ payloadText(approvalPayload, "current_price") }}</el-descriptions-item>
              <el-descriptions-item label="目标价">{{ payloadText(approvalPayload, "target_price") }}</el-descriptions-item>
              <el-descriptions-item label="期望毛利">{{ payloadText(approvalPayload, "expected_margin") }}</el-descriptions-item>
              <el-descriptions-item label="与底价距离">{{ payloadText(approvalMargin, "delta_gap_distance") }}</el-descriptions-item>
            </el-descriptions>
          </div>

          <div class="sub-block">
            <div class="selected-title">审批链</div>
            <el-table :data="approvalDetail.steps" size="small" class="grid" row-key="stepNo" empty-text="没有审批步骤记录">
              <el-table-column label="步骤" width="70">
                <template #default="{ row }">第 {{ row.stepNo }} 步</template>
              </el-table-column>
              <el-table-column label="需要角色" width="120">
                <template #default="{ row }">{{ roleText(row.requiredRole) }}</template>
              </el-table-column>
              <el-table-column label="结果" width="100">
                <template #default="{ row }">
                  <el-tag v-if="row.decision" size="small" :type="row.decision === 'APPROVED' ? 'success' : 'danger'">
                    {{ row.decision === "APPROVED" ? "已通过" : "已驳回" }}
                  </el-tag>
                  <el-tag v-else size="small" type="info">待处理</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="审批人" width="100">
                <template #default="{ row }">{{ row.approverId ? staffText(row.approverId) : "—" }}</template>
              </el-table-column>
              <el-table-column label="意见" min-width="160" show-overflow-tooltip>
                <template #default="{ row }">{{ row.comment || "—" }}</template>
              </el-table-column>
              <el-table-column label="时间" width="170">
                <template #default="{ row }">{{ row.decidedAt ? formatDateTime(row.decidedAt, "—", false) : "—" }}</template>
              </el-table-column>
            </el-table>
          </div>

          <div v-if="approvalPending !== null" class="sub-block">
            <el-alert v-if="approvalBlockReason" :title="approvalBlockReason" type="info" show-icon :closable="false" class="block-gap" />
            <el-input
              v-model="approvalComment"
              type="textarea"
              :rows="2"
              maxlength="200"
              show-word-limit
              aria-label="审批意见"
              placeholder="审批意见（驳回必填，会写入审批记录）"
            />
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="approvalVisible = false">关闭</el-button>
        <template v-if="approvalDetail && approvalPending !== null">
          <el-button type="danger" plain :disabled="!approvalCanDecide || approvalBusy" @click="decideApproval('REJECTED')">驳回</el-button>
          <el-button type="primary" :loading="approvalBusy" :disabled="!approvalCanDecide || approvalBusy" @click="decideApproval('APPROVED')">通过本步</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 测试与报告：复用模型广场那份面板，同一份证据链 -->
    <el-dialog
      v-model="reportsVisible"
      :title="`${reportsTarget?.row.model_name || '模型'} · 测试与报告`"
      width="min(1120px, 96vw)"
      append-to-body
    >
      <p v-if="reportsTarget" class="hint">
        {{ reportsTarget.link.supplier_name || "来源待核对" }} · 资源 #{{ reportsTarget.link.resource_id }} · 每次测试单独留档
      </p>
      <ResourceReportsPanel
        v-if="reportsVisible && reportsTarget"
        :key="reportsTarget.link.resource_id"
        :resource-i-d="String(reportsTarget.link.resource_id)"
      />
      <template #footer><el-button @click="reportsVisible = false">关闭</el-button></template>
    </el-dialog>

    <!-- 移交客户（共用弹窗） -->
    <CustomerTransferDialog
      v-model:visible="transferVisible"
      :customer-id="customerId"
      :customer-name="profile?.legal_name || ''"
      :owner-name="profile?.owner_sales_name || ''"
      @transferred="loadProfile"
    />
  </section>
</template>

<style scoped>
.detail-page { padding: 18px 22px 32px; }
.page-head { display: flex; align-items: flex-start; gap: 16px; margin-bottom: 14px; }
.head-main { flex: 1; min-width: 0; }
.head-title { font-size: 18px; font-weight: 600; display: flex; align-items: center; gap: 8px; }
.head-tag { margin-left: 4px; }
.head-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 6px; }
.head-actions { display: flex; gap: 8px; }
.tabbar { display: flex; gap: 24px; border-bottom: 1px solid var(--el-border-color-light); margin-bottom: 16px; }
.tab { background: none; border: none; padding: 10px 2px; cursor: pointer; font-size: 14px; color: var(--app-text-secondary); border-bottom: 2px solid transparent; }
.tab.active { color: var(--el-color-primary); border-bottom-color: var(--el-color-primary); font-weight: 600; }
.grid { width: 100%; }
.sub-block{margin-top:22px}
.model-batchbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 10px}
.batch-form{margin-top:14px}
.model-filterbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 12px}.model-filterbar label{display:inline-flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}
.cell-main { font-weight: 600; }
.cell-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 2px; }
.stack-tag { margin: 0 6px 4px 0; }
.card { border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 18px 20px; margin-bottom: 18px; background: var(--el-bg-color); }
.card-title { margin: 0; font-size: 15px; font-weight: 600; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; flex-wrap: wrap; }
.card-head-left, .card-head-right { display: flex; align-items: center; gap: 8px; }
.sub-block { margin-top: 18px; }
.selected-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.note { white-space: pre-wrap; word-break: break-word; }
.hint { color: var(--app-text-muted); font-size: 12px; }
.foot { margin-top: 10px; }
.block-gap { margin: 8px 0; }
.json-block { margin-top: 14px; }
.json-title { font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.json { margin: 0; max-height: 240px; overflow: auto; background: var(--el-fill-color-light); border-radius: 8px; padding: 10px 12px; font-size: 12px; white-space: pre-wrap; word-break: break-word; }
.mono { font-variant-numeric: tabular-nums; }
/* 展开区：与外层表格拉开层级，避免两条表线糊在一起 */
.link-detail { padding: 8px 12px 8px 48px; background: var(--el-fill-color-lighter); border-radius: 8px; overflow-x: auto; }
/* 展开区列变多（加了 连通/RPS/TPS/缓存），内层表格给个最小宽度，宁可横向滚动也不压字。 */
.link-detail :deep(.el-table) { min-width: 1040px; }
.link-detail :deep(.el-table) { background: transparent; }
:deep(.row-muted) { color: var(--app-text-muted); }
</style>
