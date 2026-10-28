<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { apiRequest } from "../../../api/http";
import { standardModelsApi } from "../../../api/standardModels";
import { modelOperationLabel, normalizeModelOperationPage, type ModelOperation, type ModelOperationDetail } from "../../../api/standardModelPresentation";
import { formatDateTime } from "../../../domain/date";


const rows = ref<ModelOperation[]>([]), loading = ref(false), error = ref("");
const selected = ref<ModelOperation>(), sortKey = ref<"action" | "operator_name" | "created_at">("created_at"), order = ref<"asc" | "desc">("desc");
const detail = ref<ModelOperationDetail>(), detailLoading = ref(false), detailError = ref("");
let controller: AbortController | undefined, detailRequest: AbortController | undefined;
const objectName = (row: ModelOperation) => row.model_name || `标准模型 #${row.entity_id}`;
const sorted = computed(() => [...rows.value].sort((a, b) => {
  const x = sortKey.value === "action" ? modelOperationLabel(a.action) : a[sortKey.value], y = sortKey.value === "action" ? modelOperationLabel(b.action) : b[sortKey.value];
  const comparison = sortKey.value === "created_at" ? Date.parse(x) - Date.parse(y) : x.localeCompare(y, "zh-CN", { numeric: true });
  return comparison * (order.value === "asc" ? 1 : -1) || (BigInt(a.id) > BigInt(b.id) ? -1 : BigInt(a.id) < BigInt(b.id) ? 1 : 0);
}));
function sort(key: typeof sortKey.value) { order.value = sortKey.value === key && order.value === "asc" ? "desc" : "asc"; sortKey.value = key; }
const arrow = (key: typeof sortKey.value) => sortKey.value === key ? order.value === "asc" ? "↑" : "↓" : "↕";
async function openDetail(row: ModelOperation) {
  detailRequest?.abort(); const request = detailRequest = new AbortController(); selected.value = row; detail.value = undefined; detailError.value = ""; detailLoading.value = true;
  try { const result = await standardModelsApi.operation(row.entity_id, row.id, request.signal); if (!request.signal.aborted) detail.value = result; }
  catch (value) { if (!request.signal.aborted) detailError.value = value instanceof Error ? value.message : "日志详情加载失败。"; }
  finally { if (!request.signal.aborted) detailLoading.value = false; }
}
function closeDetail() { detailRequest?.abort(); selected.value = undefined; detail.value = undefined; }
async function load() {
  controller?.abort(); const request = controller = new AbortController(); loading.value = true; error.value = ""; selected.value = undefined;
  try {
    const list: ModelOperation[] = [], seen = new Set<string>(); let total: number | undefined;
    for (let page = 1; ; page++) {
      const result = normalizeModelOperationPage(await apiRequest<unknown>({ url: "/internal/standard-models/operations", params: { page, size: 100 }, signal: request.signal }), page);
      if (total !== undefined && total !== result.total) throw new Error("操作记录已变化，请重新加载。");
      total = result.total;
      for (const row of result.list) { if (seen.has(row.id)) throw new Error("操作记录已变化，请重新加载。"); seen.add(row.id); list.push(row); }
      if (list.length === total) break;
      if (!result.list.length || list.length > total) throw new Error("操作记录分页不完整，请重新加载。");
    }
    if (!request.signal.aborted) { rows.value = list; }
  } catch (value) { if (!request.signal.aborted) { rows.value = []; error.value = value instanceof Error ? value.message : "操作记录加载失败。"; } }
  finally { if (!request.signal.aborted) loading.value = false; }
}
onMounted(load);
onBeforeUnmount(() => { controller?.abort(); detailRequest?.abort(); });
</script>
<template>
  <section v-loading="loading" class="model-records">
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载操作记录</el-button></el-alert>
    <div v-else class="record-scroll" role="region" aria-label="标准模型库操作记录，可横向滚动" tabindex="0"><table class="record-table"><thead><tr>
      <th :aria-sort="sortKey === 'action' ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button @click="sort('action')">操作 {{ arrow('action') }}</button></th><th>对象</th>
      <th :aria-sort="sortKey === 'operator_name' ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button @click="sort('operator_name')">操作人 {{ arrow('operator_name') }}</button></th>
      <th :aria-sort="sortKey === 'created_at' ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button @click="sort('created_at')">时间 {{ arrow('created_at') }}</button></th><th>详情</th>
    </tr></thead><tbody><tr v-for="row in sorted" :key="row.id"><td>{{ modelOperationLabel(row.action) }}</td><td>{{ objectName(row) }}</td><td>{{ row.operator_name || '未记录' }}</td><td>{{ formatDateTime(row.created_at, "未记录", true) }}</td><td><el-button size="small" @click="openDetail(row)">查看</el-button></td></tr><tr v-if="!loading && !rows.length"><td colspan="5" class="empty">暂无操作记录</td></tr></tbody></table></div>
    <footer v-if="!error">共 {{ rows.length }} 条</footer>
    <el-dialog v-if="selected" :model-value="true" class="standard-model-record-dialog" :title="modelOperationLabel(selected.action)" width="min(1050px, 95vw)" align-center append-to-body @close="closeDetail">
      <p>{{ selected.operator_name || '未记录' }} · {{ formatDateTime(selected.created_at, "未记录", true) }}</p>
      <dl class="record-detail"><dt>对象</dt><dd>{{ objectName(selected) }}</dd><dt>操作说明</dt><dd>{{ selected.reason || '未记录' }}</dd><dt>记录编号</dt><dd>{{ selected.id }}</dd></dl>
      <div v-loading="detailLoading" class="snapshot-area"><el-alert v-if="detailError" :title="detailError" type="error" :closable="false"><el-button text @click="openDetail(selected!)">重新加载日志详情</el-button></el-alert><template v-if="detail"><pre v-if="detail.before || detail.after" class="snapshot">{{ JSON.stringify({ before: detail.before, after: detail.after }, null, 2) }}</pre><p v-else class="record-note">该历史记录未保存可用的资料快照。</p></template></div>
      <template #footer><el-button @click="closeDetail">关闭</el-button></template>
    </el-dialog>
  </section>
</template>
<style scoped>
:global(.standard-model-record-dialog .el-dialog__header){padding:4px 0 16px;border-bottom:1px solid #e3e6eb}:global(.standard-model-record-dialog .el-dialog__body){padding:20px 4px}:global(.standard-model-record-dialog .el-dialog__footer){border-top:1px solid #e3e6eb;padding-top:12px}
.snapshot-area{min-height:80px}.snapshot{white-space:pre-wrap;overflow-wrap:anywhere;max-height:360px;overflow:auto;background:#f8f9fb;border:1px solid #e3e6eb;border-radius:6px;padding:12px;font-size:12px}.model-records{min-height:180px}.record-scroll{overflow:auto}.record-table{width:100%;min-width:800px;border-collapse:collapse;font-size:13px}.record-table th,.record-table td{padding:10px 12px;text-align:left;border-bottom:1px solid #eceef1;line-height:19px;overflow-wrap:anywhere}.record-table th{background:#f8f9fb;color:#727781;font-size:12px;font-weight:500}.record-table th:first-child{width:25%}.record-table th:nth-child(2){width:27%}.record-table th:nth-child(4){width:190px}.record-table th:last-child{width:90px}.record-table th button{border:0;background:none;padding:0;color:inherit;font:inherit;cursor:pointer}.record-table tbody tr:hover{background:#f8f9fb}.record-table td.empty{text-align:center;padding:40px;color:#8b9098}footer{padding:10px 16px;color:#91969f;font-size:12px}.record-detail{display:grid;grid-template-columns:90px 1fr;gap:14px;padding:16px;background:#f8f9fb;border-radius:6px}.record-detail dt,.record-note{color:#8b9098}.record-detail dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}.record-note{font-size:12px}button:focus-visible{outline:2px solid #315ee8;outline-offset:3px}
</style>
