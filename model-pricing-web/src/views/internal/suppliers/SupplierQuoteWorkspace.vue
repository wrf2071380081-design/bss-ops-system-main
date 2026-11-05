<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { supplyChainApi, type ApprovalHistoryRow, type ApprovalRow } from "../../../api/supplyChain";
import { formatDateTime } from "../../../domain/date";
import SubmissionFlowDialog from "../workbench/SubmissionFlowDialog.vue";
import SupplierQuoteList from "../supplierQuotes/SupplierQuoteList.vue";

type QuoteSubTab = "submission" | "price";
type ApprovalTab = "pending" | "history";

const route = useRoute();
const router = useRouter();

/** 主 Tab：接入申报（审批队列）/ 价格报价（SKU 报价）。 */
const sub = ref<QuoteSubTab>(route.query.sub === "price" ? "price" : "submission");
/** 接入申报内的两个子 Tab。 */
const approvalTab = ref<ApprovalTab>(route.query.approval === "history" ? "history" : "pending");

const pending = ref<ApprovalRow[]>([]), history = ref<ApprovalHistoryRow[]>([]);
const pendingLoading = ref(false), historyLoading = ref(false);
const pendingError = ref(""), historyError = ref("");
const flowID = ref("");
let pendingRequest: AbortController | undefined, historyRequest: AbortController | undefined;
let active = true;
let historyLoaded = false;

const regionLabel = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
const kindLabel = (kind: string) => kind === "PRICE_CHANGE" ? "价格变更" : "模型申报";
const statusLabels: Record<string, string> = {
  DRAFT: "草稿", PENDING_APPROVAL: "待审批", PENDING_TEST: "等待测试补齐", PROCESSING: "办理中",
  COMPLETED: "已完成", APPROVED: "已通过", REJECTED: "已驳回", WITHDRAWN: "已撤回",
  CANCELED: "已取消", CANCELLED: "已取消",
};
const resultLabels: Record<string, string> = { APPROVED: "通过", REJECTED: "驳回", COMPLETED: "已完成", PROCESSING: "办理中", CANCELED: "已取消", CANCELLED: "已取消" };
const statusLabel = (status: string) => statusLabels[status] || status || "未记录";
const statusType = (status: string) =>
  status === "REJECTED" ? "danger"
    : status === "PENDING_APPROVAL" || status === "PENDING_TEST" || status === "PROCESSING" ? "warning"
      : status === "COMPLETED" || status === "APPROVED" ? "success" : "info";
const historyResult = (row: ApprovalHistoryRow) => resultLabels[row.result] || statusLabels[row.result] || row.result || statusLabels[row.status] || row.status || "未记录";

/** 把 `sub` / `approval` 深链写回 URL，避免刷新丢失当前 Tab。 */
function currentStringQuery() {
  const query: Record<string, string> = {};
  for (const [key, value] of Object.entries(route.query)) if (typeof value === "string") query[key] = value;
  return query;
}
/** 把 `sub` 深链写回 URL，避免刷新丢失当前子 Tab。
 *  只在确实处于「供应商报价」Tab 时才写 query：父组件切回列表会删掉 tab，
 *  若无条件写回 tab=quote，会与父组件的 watch(route.query.tab) 形成回弹。 */
function replaceQuery(patch: Record<string, string>) {
  const current = currentStringQuery();
  if (current.tab !== "quote") return;
  const next: Record<string, string> = { ...current };
  for (const [key, value] of Object.entries(patch)) { if (value) next[key] = value; else delete next[key]; }
  const keys = new Set([...Object.keys(current), ...Object.keys(next)]);
  for (const key of keys) if ((current[key] ?? "") !== (next[key] ?? "")) { void router.replace({ path: route.path, query: next }); return; }
}

async function loadPending() {
  pendingRequest?.abort();
  const current = pendingRequest = new AbortController();
  pendingLoading.value = true; pendingError.value = "";
  try {
    const list = await supplyChainApi.approvals(current.signal);
    if (!active || current.signal.aborted) return;
    pending.value = list;
  } catch (value) {
    if (!active || current.signal.aborted) return;
    pending.value = [];
    pendingError.value = value instanceof Error ? value.message : "待审批申报加载失败。";
  } finally {
    if (active && !current.signal.aborted) pendingLoading.value = false;
  }
}

async function loadHistory() {
  historyRequest?.abort();
  const current = historyRequest = new AbortController();
  historyLoading.value = true; historyError.value = "";
  try {
    const list = await supplyChainApi.approvalHistory(current.signal);
    if (!active || current.signal.aborted) return;
    history.value = list;
    historyLoaded = true;
  } catch (value) {
    if (!active || current.signal.aborted) return;
    history.value = [];
    historyError.value = value instanceof Error ? value.message : "审批记录加载失败。";
  } finally {
    if (active && !current.signal.aborted) historyLoading.value = false;
  }
}

function reloadAfterChange() {
  void loadPending();
  if (historyLoaded) void loadHistory();
}
function openFlow(row: { id: number }) { flowID.value = String(row.id); }

// 两个 watch 会互相触发：父组件删掉 tab 回到列表时，这里必须直接返回，
// 否则「sub 重置为 submission」→「写回 tab=quote」→ 父组件切回报价 Tab，表现为点一次列表被弹回。
watch(sub, value => {
  if (route.query.tab !== "quote") return;
  replaceQuery({ sub: value === "price" ? "price" : "" });
});
watch(() => route.query.sub, value => {
  if (route.query.tab !== "quote") return;
  const next: QuoteSubTab = value === "price" ? "price" : "submission";
  if (next !== sub.value) sub.value = next;
});
// 从「供应商列表」切回报价 Tab 时，把当前子 Tab 重新写回 URL，
// 保证「看到的是价格报价 ⟺ URL 带 sub=price」，刷新后不会掉回接入申报。
watch(() => route.query.tab, value => {
  if (value !== "quote") return;
  replaceQuery({ sub: sub.value === "price" ? "price" : "" });
});
watch(approvalTab, value => { if (value === "history" && !historyLoaded && !historyLoading.value) void loadHistory(); });

onMounted(() => {
  void loadPending();
  if (approvalTab.value === "history") void loadHistory();
});
onBeforeUnmount(() => { active = false; pendingRequest?.abort(); historyRequest?.abort(); });
</script>

<template>
  <section class="quote-workspace">
    <el-tabs v-model="sub" class="quote-workspace__tabs">
      <el-tab-pane label="接入申报" name="submission">
        <p class="muted quote-workspace__hint">供应商新增模型与价格变更的整批申报；审批通过后由办理人完成测试与资源配置、合同与授信。</p>
        <el-tabs v-model="approvalTab" class="quote-workspace__inner">
          <el-tab-pane label="待审批" name="pending">
            <el-alert v-if="pendingError" :title="pendingError" type="error" show-icon :closable="false"><el-button text @click="loadPending">重新加载</el-button></el-alert>
            <template v-else>
              <div class="quote-workspace__toolbar"><span class="muted">按提交时间倒序；点「详情」进入审批与办理分派。</span><el-button :loading="pendingLoading" @click="loadPending">刷新</el-button></div>
              <el-table v-loading="pendingLoading" :data="pending" style="width:100%">
                <el-table-column prop="id" label="申报 ID" min-width="130" />
                <el-table-column prop="supplier_name" label="供应商" min-width="150" />
                <el-table-column label="申报类型" width="110"><template #default="{ row }">{{ kindLabel(row.kind) }}</template></el-table-column>
                <el-table-column label="部署节点" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
                <el-table-column prop="row_count" label="模型数" width="90" />
                <el-table-column label="提交人" min-width="120"><template #default="{ row }">{{ row.submitter || "未记录" }}</template></el-table-column>
                <el-table-column label="提交时间" width="170"><template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template></el-table-column>
                <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
                <el-table-column label="操作" width="100"><template #default="{ row }"><el-button size="small" @click="openFlow(row)">详情</el-button></template></el-table-column>
                <template #empty><el-empty description="暂无待审批申报" :image-size="64" /></template>
              </el-table>
            </template>
          </el-tab-pane>
          <el-tab-pane label="审批记录" name="history">
            <el-alert v-if="historyError" :title="historyError" type="error" show-icon :closable="false"><el-button text @click="loadHistory">重新加载</el-button></el-alert>
            <template v-else>
              <div class="quote-workspace__toolbar"><span class="muted">已完成的审批结论；点「详情」查看整批申报与办理记录。</span><el-button :loading="historyLoading" @click="loadHistory">刷新</el-button></div>
              <el-table v-loading="historyLoading" :data="history" style="width:100%">
                <el-table-column prop="id" label="申报单" width="100" />
                <el-table-column prop="supplier_name" label="供应商" min-width="150" />
                <el-table-column label="申报类型" width="110"><template #default="{ row }">{{ kindLabel(row.kind) }}</template></el-table-column>
                <el-table-column label="结果" width="110"><template #default="{ row }"><el-tag :type="statusType(row.result || row.status)" size="small">{{ historyResult(row) }}</el-tag></template></el-table-column>
                <el-table-column label="审批人" min-width="120"><template #default="{ row }">{{ row.approver || "未记录" }}</template></el-table-column>
                <el-table-column label="审批时间" width="170"><template #default="{ row }">{{ formatDateTime(row.approved_at) }}</template></el-table-column>
                <el-table-column label="意见" min-width="180"><template #default="{ row }">{{ row.note || "未记录" }}</template></el-table-column>
                <el-table-column label="操作" width="100"><template #default="{ row }"><el-button size="small" @click="openFlow(row)">详情</el-button></template></el-table-column>
                <template #empty><el-empty description="暂无审批记录" :image-size="64" /></template>
              </el-table>
            </template>
          </el-tab-pane>
        </el-tabs>
      </el-tab-pane>
      <el-tab-pane label="价格报价" name="price">
        <SupplierQuoteList v-if="sub === 'price'" embedded />
      </el-tab-pane>
    </el-tabs>
    <SubmissionFlowDialog v-if="flowID" :id="flowID" @close="flowID = ''" @changed="reloadAfterChange" />
  </section>
</template>

<style scoped>
.quote-workspace { padding: 4px 0 0; }
.quote-workspace__hint { margin: 0 0 8px; }
.quote-workspace__inner { margin-top: 4px; }
.quote-workspace__toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 4px 0 12px; }
.quote-workspace .muted { color: var(--app-text-muted); font-size: 12px; }
</style>
