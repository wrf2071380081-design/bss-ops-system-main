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
import { modelTypeLabels } from "../../../api/standardModels.contract";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { PERMISSIONS } from "../../../domain/permissions";
import { usePermissionStore } from "../../../stores/permission";
import { useSessionStore } from "../../../stores/session";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";
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

// 原型「接入模型」的筛选：模型公司 / 模型类型。
const modelFilters = reactive({ company: "", modelType: "" });
const modelCompanies = computed(() => [...new Set(modelRows.value.map(row => row.company).filter(Boolean))].sort((a, b) => a.localeCompare(b, "zh-CN")));
const filteredModelRows = computed(() => modelRows.value.filter(row =>
  (!modelFilters.company || row.company === modelFilters.company)
  && (!modelFilters.modelType || row.model_type === modelFilters.modelType)));
function resetModelFilters() { Object.assign(modelFilters, { company: "", modelType: "" }); }
// 系数按有效位展示（0.7200 → 0.72），与模型广场一致。
const coefficientText = (raw: string) => raw && raw.includes(".") ? raw.replace(/0+$/, "").replace(/\.$/, "") : raw;
const modelEmptyText = computed(() =>
  justArchivedModel.value
    ? "该模型来源已停用并归档，可到「操作记录」查看本次操作"
    : "该客户还没有接入任何模型",
);

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

// ---------------- Tab3 该客户报价 ----------------
const quoteRows = ref<CustomerQuoteRow[]>([]);
const approvalRows = ref<CustomerApprovalRow[]>([]);
const quoteLoading = ref(false);
const quoteError = ref("");
const quoteLoaded = ref(false);

async function loadQuotes() {
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

// ---------------- Tab4 操作记录 ----------------
const logRows = ref<CustomerOperationLogDTO[]>([]);
const logLoading = ref(false);
const logError = ref("");
const logLoaded = ref(false);
const logTotal = ref(0);

// 原型「操作记录」筛选：操作人 / 操作类型 / 操作时间。
const logFilters = reactive({ operator: "", action: "", range: [] as string[] });
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
          {{ profile.settlement_currency }} · 税率 {{ profile.tax_rate || "未填写" }} · 合同 {{ profile.contract_status }}
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
        <el-button @click="resetModelFilters">重置</el-button>
        <span v-if="modelRows.length" class="hint">共 {{ filteredModelRows.length }} / {{ modelRows.length }} 条</span>
      </div>
      <el-table
        v-loading="modelLoading"
        :data="filteredModelRows"
        row-key="standard_model_id"
        class="grid"
        :row-class-name="({ row }: { row: CustomerModelServiceRow }) => row.service_state === '无可用来源' ? 'row-muted' : ''"
        :empty-text="modelEmptyText"
      >
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
                <el-table-column label="生效时间" width="170">
                  <template #default="{ row: link }: { row: CustomerLinkDetailDTO }">
                    <span class="mono">{{ link.activated_at ? formatDateTime(link.activated_at) : "—" }}</span>
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
        <el-table-column label="模型名称 / 官方 ID" min-width="210">
          <template #default="{ row }">
            <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
            <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
          </template>
        </el-table-column>
        <el-table-column label="模型公司" width="130">
          <template #default="{ row }">{{ row.company || "待补齐" }}</template>
        </el-table-column>
        <el-table-column label="模型类型" width="100">
          <template #default="{ row }">{{ modelTypeLabels[row.model_type] || "待补齐" }}</template>
        </el-table-column>
        <el-table-column label="部署区域" width="100">
          <template #default="{ row }">{{ row.region === "OVERSEAS" ? "海外" : row.region === "CN" ? "国内" : "—" }}</template>
        </el-table-column>
        <el-table-column label="客户报价系数" width="130">
          <template #default><span class="hint">待核对</span></template>
        </el-table-column>
        <el-table-column label="采购系数" width="110">
          <template #default="{ row }">{{ row.coefficient ? coefficientText(row.coefficient) : "—" }}</template>
        </el-table-column>
        <el-table-column label="模型服务状态" width="140">
          <template #default="{ row }">
            <el-tag size="small" :type="row.service_state === '服务中' ? 'success' : row.service_state === '已禁用' ? 'info' : 'warning'">
              {{ row.service_state }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="有效可用资源数" width="140" align="right">
          <template #default="{ row }"><span class="mono">{{ row.available_count }}</span></template>
        </el-table-column>
        <el-table-column label="已绑定资源数" width="130" align="right">
          <template #default="{ row }"><span class="mono">{{ row.bound_count }}</span></template>
        </el-table-column>
        <el-table-column label="风险异常" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="flag in row.risk_flags" :key="flag" size="small" type="danger" class="stack-tag">{{ flag }}</el-tag>
            <span v-if="!row.risk_flags?.length" class="hint">—</span>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <!-- ============ Tab 2 · 档案与办理（deeplink 落点） ============ -->
    <template v-if="activeTab === 'onboarding'">
      <el-alert v-if="flowError" :title="flowError" type="error" show-icon :closable="false" class="block-gap" />

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
        </el-table>
        <div class="hint foot">报价编辑器在「客户管理 → 客户报价单」，这里点「新建报价」会带着本客户跳过去。</div>

        <div class="card-head sub-block">
          <div class="card-head-left"><h3 class="card-title">报价审批</h3></div>
        </div>
        <el-table :data="approvalRows" size="small" class="grid" row-key="id" empty-text="暂无审批记录">
          <el-table-column label="特价变更单" width="130"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="关联报价" width="120"><template #default="{ row }">{{ row.quote_id ? `#${row.quote_id}` : "—" }}</template></el-table-column>
          <el-table-column label="审批结果" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'APPROVED' ? 'success' : row.status === 'REJECTED' ? 'danger' : 'warning'">
                {{ ({ PENDING: "待审批", APPROVED: "已通过", REJECTED: "已驳回" } as Record<string, string>)[row.status] || row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="申请原因" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.reason || "—" }}</template></el-table-column>
          <el-table-column label="提交人" width="120"><template #default="{ row }">{{ row.created_by || "—" }}</template></el-table-column>
          <el-table-column label="提交时间" width="180"><template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template></el-table-column>
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
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="{ row }">{{ row.action_label || row.action }}</template>
        </el-table-column>
        <el-table-column label="操作人" width="130">
          <template #default="{ row }">{{ row.operator_name || "—" }}</template>
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
.model-filterbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 12px}.model-filterbar label{display:flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}
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
.link-detail { padding: 8px 12px 8px 48px; background: var(--el-fill-color-lighter); border-radius: 8px; }
.link-detail :deep(.el-table) { background: transparent; }
:deep(.row-muted) { color: var(--app-text-muted); }
</style>
