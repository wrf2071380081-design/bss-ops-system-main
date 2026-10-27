<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import type { SupplierSubmissionDTO, SupplierSubmissionFilterOptionsDTO, SupplierSubmissionListParams } from "../../api/supplierSubmissions.types";
import { useSessionStore } from "../../stores/session";
import SupplierSubmissionDetailDialog from "./SupplierSubmissionDetailDialog.vue";

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

// ---- 申报记录筛选（原型 6 维 + 搜索）----
const filters = reactive<{ company: string; model_type: string; region: string; kind: string; status: string; submitter: string }>({
  company: "", model_type: "", region: "", kind: "", status: "", submitter: "",
});
const keyword = ref("");
const options = ref<SupplierSubmissionFilterOptionsDTO | null>(null);
const optionsLoading = ref(false);
const filterParams = computed<SupplierSubmissionListParams>(() => {
  const params: SupplierSubmissionListParams = { page: 1, size: 100 };
  if (filters.company) params.company = filters.company;
  if (filters.model_type) params.model_type = filters.model_type;
  if (filters.region) params.region = filters.region;
  if (filters.kind) params.kind = filters.kind as SupplierSubmissionListParams["kind"];
  if (filters.status) params.status = filters.status;
  if (filters.submitter) params.submitter = filters.submitter;
  return params;
});
const hasFilter = computed(() => Boolean(filters.company || filters.model_type || filters.region || filters.kind || filters.status || filters.submitter));
const kindOptions = computed(() => options.value?.kinds?.length ? options.value.kinds : ["MODEL_NEW", "PRICE_CHANGE"]);
const statusOptions = computed(() => options.value?.statuses?.length ? options.value.statuses : ["PENDING_TEST", "PENDING_APPROVAL", "PROCESSING", "APPROVED", "REJECTED", "WITHDRAWN"]);

function kindLabel(value: string) { return value === "PRICE_CHANGE" ? "价格变更" : "模型新增"; }
function resetFilters() {
  filters.company = ""; filters.model_type = ""; filters.region = ""; filters.kind = ""; filters.status = ""; filters.submitter = "";
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
function canWithdraw(item: SupplierSubmissionDTO) { return item.permissions?.can_withdraw && (item.status === "PENDING_APPROVAL" || item.status === "PENDING_TEST"); }
function canEdit(item: SupplierSubmissionDTO) { return Boolean(item.permissions?.can_edit); }

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
          <el-table-column label="操作" width="120"><template #default="{ row }"><el-button link type="primary" :disabled="!canEdit(row)" @click="editDraft(row)">继续编辑</el-button></template></el-table-column>
        </el-table>
      </section>
    </el-tab-pane>

    <el-tab-pane label="申报记录" name="history">
      <section class="panel supplier-list-panel">
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>

        <div class="record-filters">
          <el-select v-model="filters.company" placeholder="模型公司" clearable size="small" :loading="optionsLoading" :disabled="!options?.companies?.length">
            <el-option v-for="item in options?.companies || []" :key="item" :label="item" :value="item" />
          </el-select>
          <el-select v-model="filters.model_type" placeholder="模型类型" clearable size="small" :loading="optionsLoading" :disabled="!options?.model_types?.length">
            <el-option v-for="item in options?.model_types || []" :key="item" :label="item" :value="item" />
          </el-select>
          <el-select v-model="filters.region" placeholder="部署节点" clearable size="small" :loading="optionsLoading" :disabled="!options?.regions?.length">
            <el-option v-for="item in options?.regions || []" :key="item" :label="item" :value="item" />
          </el-select>
          <el-select v-model="filters.kind" placeholder="申报类型" clearable size="small">
            <el-option v-for="item in kindOptions" :key="item" :label="kindLabel(item)" :value="item" />
          </el-select>
          <el-select v-model="filters.status" placeholder="当前状态" clearable size="small">
            <el-option v-for="item in statusOptions" :key="item" :label="statusLabel(item)" :value="item" />
          </el-select>
          <el-select v-model="filters.submitter" placeholder="供应商提交人" clearable size="small" :loading="optionsLoading" :disabled="!options?.submitters?.length">
            <el-option v-for="item in options?.submitters || []" :key="item" :label="item" :value="item" />
          </el-select>
          <el-input v-model="keyword" size="small" class="record-search" placeholder="搜索申报编号 / 模型名称 / 接口模型 ID" clearable />
          <el-button size="small" @click="resetFilters">重置</el-button>
          <el-button size="small" type="primary" @click="load">查询</el-button>
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
          <el-table-column label="操作" width="170">
            <template #default="{ row }">
              <el-button link type="primary" @click="openDetail(row)">查看申报</el-button>
              <el-button v-if="canWithdraw(row)" link type="danger" @click="withdrawSubmission(row)">撤回</el-button>
              <el-button v-if="canEdit(row)" link type="primary" @click="editDraft(row)">修改重提</el-button>
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
.record-filters :deep(.el-select) { width: 130px; }
.record-search { width: 240px; }
.record-count { margin: 12px 0 0; color: var(--app-text-muted); font-size: 12px; }
.muted { margin-top: 4px; color: var(--app-text-muted); font-size: 12px; }
.supplier-footnote { margin: 26px 0 0; color: var(--app-text-muted); font-size: 11px; }
</style>
