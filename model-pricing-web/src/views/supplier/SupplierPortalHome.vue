<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import type { SupplierSubmissionDTO, SupplierSubmissionFilterOptionsDTO, SupplierSubmissionListParams } from "../../api/supplierSubmissions.types";
import { useSessionStore } from "../../stores/session";
import SupplierSubmissionDetailDialog from "./SupplierSubmissionDetailDialog.vue";
import SupplierFilterSelect from "./SupplierFilterSelect.vue";

const router = useRouter();
const route = useRoute();
const session = useSessionStore();
const activeTab = ref(route.query.tab === "history" ? "history" : route.query.tab === "drafts" ? "drafts" : "start");
const submissions = ref<SupplierSubmissionDTO[]>([]);
const total = ref(0);
const loading = ref(false);
const error = ref("");
const supplierName = computed(() => session.user?.supplierName || "供应商");
const regionLabel = computed(() => session.user?.supplierRegion === "OVERSEAS" ? "海外主体" : "国内主体");
const drafts = computed(() => submissions.value.filter(item => item.status === "DRAFT"));
const history = computed(() => submissions.value.filter(item => item.status !== "DRAFT"));
let active = true;
let request: AbortController | undefined;

// ---- 申报记录筛选（原型 6 维多选 + 搜索，勾选即查询）----
type FilterKey = "company" | "model_type" | "region" | "kind" | "status" | "submitter";
const filters = reactive<Record<FilterKey, string[]>>({
  company: [], model_type: [], region: [], kind: [], status: [], submitter: [],
});
const keyword = ref("");
const options = ref<SupplierSubmissionFilterOptionsDTO | null>(null);
const optionsLoading = ref(false);
const filterParams = computed<SupplierSubmissionListParams>(() => {
  const params: SupplierSubmissionListParams = { page: 1, size: 100 };
  // 后端按逗号拆多值（也接受重复参数），空数组表示这一维不限。
  const join = (values: string[]) => values.filter(Boolean).join(",");
  if (filters.company.length) params.company = join(filters.company);
  if (filters.model_type.length) params.model_type = join(filters.model_type);
  if (filters.region.length) params.region = join(filters.region);
  if (filters.kind.length) params.kind = join(filters.kind) as SupplierSubmissionListParams["kind"];
  if (filters.status.length) params.status = join(filters.status);
  if (filters.submitter.length) params.submitter = join(filters.submitter);
  return params;
});
const kindOptions = computed(() => options.value?.kinds?.length ? options.value.kinds : ["MODEL_NEW", "PRICE_CHANGE"]);
const statusOptions = computed(() => options.value?.statuses?.length ? options.value.statuses : ["PENDING_TEST", "PENDING_APPROVAL", "PROCESSING", "APPROVED", "REJECTED", "WITHDRAWN"]);
// 部署节点是固定枚举，原型里就是「国内 / 海外」两项；后端存 CN / OVERSEAS。
const regionOptions = [
  { value: "CN", label: "国内" },
  { value: "OVERSEAS", label: "海外" },
];
// 其余维度优先用后端 filter-options 收窄到「本主体已有数据」，拿不到就退化成内置枚举。
const companyOptions = computed(() => (options.value?.companies || []).map(item => ({ value: item, label: item })));
const modelTypeOptions = computed(() => (options.value?.model_types || []).map(item => ({ value: item, label: item })));
const submitterOptions = computed(() => (options.value?.submitters || []).map(item => ({ value: item, label: item })));

function kindLabel(value: string) { return value === "PRICE_CHANGE" ? "价格变更" : "模型新增"; }
function regionLabelOf(value: string) {
  return regionOptions.find(item => item.value === value)?.label || value;
}
function resetFilters() {
  (Object.keys(filters) as FilterKey[]).forEach(key => { filters[key] = []; });
  keyword.value = "";
  void load();
}
function applyKeyword(list: SupplierSubmissionDTO[]) {
  // 搜索走前端：后端契约里没有 keyword 参数（原型是编号/模型名/接口模型 ID 混合搜）。
  const value = keyword.value.trim().toLowerCase();
  if (!value) return list;
  return list.filter(item => {
    const haystack = [item.submission_no, item.id, ...(item.model_names || []), ...item.rows.map(row => row.interface_model_id)].join(" ").toLowerCase();
    return haystack.includes(value);
  });
}

function newModel() { void router.push({ path: "/supplier/submissions/new", query: { kind: "MODEL_NEW" } }); }
function editDraft(item: SupplierSubmissionDTO) { void router.push({ path: "/supplier/submissions/new", query: { kind: item.kind, id: item.id } }); }
function onTabChange(tabName: string | number) { if (tabName !== "start") void load(); }
function statusLabel(value: string) {
  const labels: Record<string, string> = {
    DRAFT: "草稿",
    PENDING_TEST: "等待测试补齐",
    PENDING_APPROVAL: "待审批",
    PROCESSING: "办理中",
    APPROVED: "通过",
    REJECTED: "不通过",
    WITHDRAWN: "已撤回",
  };
  return labels[value] || value;
}
function statusType(value: string): "success" | "warning" | "danger" | "info" {
  if (value === "APPROVED") return "success";
  if (value === "REJECTED") return "danger";
  if (value === "PENDING_TEST" || value === "PENDING_APPROVAL") return "warning";
  if (value === "WITHDRAWN") return "info";
  return "info";
}
// 操作栏固定三个按钮，按状态决定能不能点（原型就是「都在，但灰掉」）：
//   查看申报 —— 任何状态都能看，只读；
//   撤回     —— 只有待审批（含等待测试补齐）且后端给了 can_withdraw 才能撤回；
//   修改重提 —— 只有已撤回 / 不通过，且后端给了 can_edit 才能重新编辑。
function canWithdraw(item: SupplierSubmissionDTO) {
  return Boolean(item.permissions?.can_withdraw) && (item.status === "PENDING_APPROVAL" || item.status === "PENDING_TEST");
}
function canEdit(item: SupplierSubmissionDTO) {
  return Boolean(item.permissions?.can_edit) && (item.status === "WITHDRAWN" || item.status === "REJECTED");
}
/** 草稿 tab 的「继续编辑」只看后端给没给编辑权，不受申报记录的状态门禁影响。 */
function canContinueDraft(item: SupplierSubmissionDTO) { return Boolean(item.permissions?.can_edit); }

async function loadOptions() {
  if (options.value || optionsLoading.value) return;
  optionsLoading.value = true;
  try { options.value = await supplierSubmissionsApi.filterOptions(); }
  catch { /* 选项加载失败不阻断列表：下拉退化成内置枚举。 */ }
  finally { optionsLoading.value = false; }
}
async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const result = await supplierSubmissionsApi.list(filterParams.value, current.signal);
    if (active && !current.signal.aborted) { submissions.value = result.list; total.value = result.total; }
  } catch (value) {
    if (active && !current.signal.aborted) { submissions.value = []; total.value = 0; error.value = value instanceof Error ? value.message : "申报记录暂时无法加载。"; }
  } finally { if (active && !current.signal.aborted) loading.value = false; }
}
const filteredHistory = computed(() => applyKeyword(history.value));
// 计数用过滤后的实际条数：后端 total 把 DRAFT 也算进去了，跟这一屏看到的行数对不上。
const historyCount = computed(() => filteredHistory.value.length);

// 勾选/取消勾选即刻查询（原型没有「查询」按钮）；关键字是前端过滤，不需要重查。
let filterTimer: ReturnType<typeof setTimeout> | undefined;
watch(() => (Object.keys(filters) as FilterKey[]).map(key => filters[key].join(",")).join("|"), () => {
  if (activeTab.value !== "history") return;
  if (filterTimer) clearTimeout(filterTimer);
  filterTimer = setTimeout(() => { void load(); }, 180);
});

/** 原型的时间写法：2026/10/27 16:32:31（后端给的是 RFC3339）。 */
function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

// ---- 查看申报 ----
const detailVisible = ref(false);
const detailId = ref("");
function openDetail(item: SupplierSubmissionDTO) { detailId.value = item.id; detailVisible.value = true; }

async function withdrawSubmission(item: SupplierSubmissionDTO) {
  if (!canWithdraw(item)) return;
  try {
    // 原型：撤回不强制填原因（2026-10-27 确认选填）。
    const { value } = await ElMessageBox.prompt("撤回后该申报回到可编辑状态，之前提交的版本保留。可填写撤回原因（选填）。", "撤回申报", {
      inputPlaceholder: "撤回原因（选填）",
      confirmButtonText: "确认撤回",
      cancelButtonText: "返回",
      type: "warning",
    });
    await supplierSubmissionsApi.withdraw(item.id, item.revision, value ?? "", crypto.randomUUID());
    ElMessage.success("申报已撤回");
    await load();
  } catch (value) {
    if (value === "cancel" || value === "close") return;
    error.value = value instanceof Error ? value.message : "撤回未完成，请刷新申报记录后重试。";
  }
}
watch(() => route.query.tab, value => { activeTab.value = value === "history" ? "history" : value === "drafts" ? "drafts" : "start"; if (activeTab.value !== "start") void load(); });
onMounted(() => { void loadOptions(); if (activeTab.value !== "start") void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">TOKEN OPS / PORTAL</div>
    <h1>供应商报价系统</h1>
    <p>{{ supplierName }} · {{ regionLabel }}</p>
  </section>

  <el-tabs v-model="activeTab" class="supplier-tabs" @tab-change="onTabChange">
    <el-tab-pane label="发起申报" name="start">
      <section class="panel supplier-start">
        <div class="supplier-start__head"><h2>发起申报</h2></div>
        <div class="supplier-start__body">
          <p>填写模型申报，测试和提交使用同一份申报记录。</p>
          <div class="supplier-start__actions"><el-button type="primary" @click="newModel">新建模型申报</el-button><el-button @click="activeTab = 'drafts'">查看草稿</el-button></div>
        </div>
      </section>
    </el-tab-pane>

    <el-tab-pane label="草稿" name="drafts">
      <section class="panel supplier-list-panel">
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
        <el-table v-loading="loading" :data="drafts" row-key="id" empty-text="暂无草稿">
          <el-table-column label="申报内容" min-width="240"><template #default="{ row }"><strong>{{ row.rows.length }} 个模型</strong><div class="muted">{{ row.reason || '尚未填写说明' }}</div></template></el-table-column>
          <el-table-column label="最近保存" min-width="170"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
          <el-table-column label="操作" width="120"><template #default="{ row }"><el-button link type="primary" :disabled="!canContinueDraft(row)" @click="editDraft(row)">继续编辑</el-button></template></el-table-column>
        </el-table>
      </section>
    </el-tab-pane>

    <el-tab-pane label="申报记录" name="history">
      <section class="panel supplier-list-panel">
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>

        <div class="record-filters">
          <SupplierFilterSelect v-model="filters.company" label="模型公司" :options="companyOptions" :disabled="!companyOptions.length" />
          <SupplierFilterSelect v-model="filters.model_type" label="模型类型" :options="modelTypeOptions" :disabled="!modelTypeOptions.length" />
          <SupplierFilterSelect v-model="filters.region" label="部署节点" :options="regionOptions" />
          <SupplierFilterSelect v-model="filters.kind" label="申报类型" :options="kindOptions.map(item => ({ value: item, label: kindLabel(item) }))" />
          <SupplierFilterSelect v-model="filters.status" label="当前状态" :options="statusOptions.map(item => ({ value: item, label: statusLabel(item) }))" />
          <SupplierFilterSelect v-model="filters.submitter" label="供应商提交人" :options="submitterOptions" :disabled="!submitterOptions.length" />
          <el-input v-model="keyword" size="small" class="record-search" placeholder="搜索申报编号 / 模型名称 / 接口模型 ID" clearable />
          <el-button size="small" @click="resetFilters">重置</el-button>
        </div>

        <el-table v-loading="loading" :data="filteredHistory" row-key="id" empty-text="暂无申报记录">
          <el-table-column label="申报内容" min-width="280">
            <template #default="{ row }">
              <strong>{{ (row.model_names?.length ? row.model_names.join('、') : `${row.rows.length} 个模型`) }}</strong>
              <div class="muted">共 {{ row.row_count || row.rows.length }} 个模型 · {{ kindLabel(row.kind) }} · {{ row.submission_no || row.id }}</div>
            </template>
          </el-table-column>
          <el-table-column label="提交人" width="120" sortable :sort-method="(a: SupplierSubmissionDTO, b: SupplierSubmissionDTO) => (a.submitter_name || '').localeCompare(b.submitter_name || '')">
            <template #default="{ row }">{{ row.submitter_name || '—' }}</template>
          </el-table-column>
          <el-table-column label="提交时间" prop="submitted_at" min-width="170" sortable>
            <template #default="{ row }">{{ formatTime(row.submitted_at || row.updated_at) }}</template>
          </el-table-column>
          <el-table-column label="当前状态" width="150" sortable :sort-method="(a: SupplierSubmissionDTO, b: SupplierSubmissionDTO) => statusLabel(a.status).localeCompare(statusLabel(b.status))">
            <template #default="{ row }">
              <el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
              <div v-if="row.pending_row_count" class="muted">待补 {{ row.pending_row_count }} 个</div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="230">
            <template #default="{ row }">
              <!-- 三个按钮常驻，按当前状态置灰（原型如此）：可看、待审批可撤、驳回/撤回可重提 -->
              <el-button link type="primary" @click="openDetail(row)">查看申报</el-button>
              <el-button link type="danger" :disabled="!canWithdraw(row)" @click="withdrawSubmission(row)">撤回</el-button>
              <el-button link type="primary" :disabled="!canEdit(row)" @click="editDraft(row)">修改重提</el-button>
            </template>
          </el-table-column>
        </el-table>

        <p class="record-count">共 {{ historyCount }} 条</p>
      </section>
    </el-tab-pane>
  </el-tabs>

  <SupplierSubmissionDetailDialog v-model="detailVisible" :submission-id="detailId" />

  <p class="supplier-footnote">Token Ops · 供应商申报门户</p>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-tabs :deep(.el-tabs__header) { margin-bottom: 16px; }
.supplier-start { padding: 0; overflow: hidden; }
.supplier-start__head { padding: 18px 22px; border-bottom: 1px solid var(--app-border); }
.supplier-start__head h2 { margin: 0; font-size: 17px; }
.supplier-start__body { padding: 20px 22px 22px; }
.supplier-start__body p { margin: 0 0 16px; color: var(--app-text-muted); font-size: 13px; }
.supplier-start__actions { display: flex; gap: 10px; }
.supplier-list-panel { padding: 18px; }
.record-filters { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 14px; }
.record-search { width: 240px; flex: 0 1 240px; }
.record-count { margin: 12px 0 0; color: var(--app-text-muted); font-size: 12px; }
.muted { margin-top: 4px; color: var(--app-text-muted); font-size: 12px; }
.supplier-footnote { margin: 26px 0 0; color: var(--app-text-muted); font-size: 11px; }
</style>
