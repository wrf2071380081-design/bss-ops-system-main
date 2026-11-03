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
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import {
  customerDetailApi,
  type CustomerModelServiceRow,
  type CustomerOperationLogDTO,
  type CustomerProfileDTO,
  type OnboardingFlowDTO,
  type OnboardingNodeDTO,
} from "../../../api/customerDetail";
import { customerWorkspaceApi, type CustomerQuoteRow } from "../../../api/customerWorkspace";
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

async function loadModels() {
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

async function loadLogs() {
  if (logLoaded.value || logLoading.value) return;
  logLoading.value = true;
  logError.value = "";
  try {
    const result = await customerDetailApi.operationLogs(customerId.value, 1, 100);
    logRows.value = result.list;
    logTotal.value = result.total;
    logLoaded.value = true;
  } catch (value) {
    logError.value = message(value);
  } finally {
    logLoading.value = false;
  }
}

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
void loadModels();
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
      <el-table
        v-loading="modelLoading"
        :data="modelRows"
        row-key="standard_model_id"
        class="grid"
        :row-class-name="({ row }: { row: CustomerModelServiceRow }) => row.service_state === '无可用来源' ? 'row-muted' : ''"
        empty-text="该客户还没有接入任何模型"
      >
        <el-table-column label="模型" min-width="200">
          <template #default="{ row }">
            <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
            <div class="cell-sub">部署节点 {{ row.region }}</div>
          </template>
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
      </div>
    </template>

    <!-- ============ Tab 4 · 操作记录 ============ -->
    <template v-if="activeTab === 'records'">
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
:deep(.row-muted) { color: var(--app-text-muted); }
</style>
