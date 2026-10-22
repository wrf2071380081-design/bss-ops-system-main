<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import type { ResourceGroup, ResourcePage, ResourceQuery, ResourceRow } from "../../../api/resources.types";
import ResourceDetailDrawer from "./ResourceDetailDrawer.vue";
import CreateStatePlanDialog from "./CreateStatePlanDialog.vue";
import { canCreateStatePlan } from "../../../api/planCreate.contract";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import { emptySourceFilters, filterSources, sourceRanges } from "../../../api/sourceFilters";
import ModelSourceOperationRecords from "./ModelSourceOperationRecords.vue";
const props = defineProps<{ group: ResourceGroup; filters: ResourceQuery }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const error = ref(""), loading = ref(false), selected = ref<ResourceRow>();
const fullRows = ref<ResourceRow[]>([]), filters = reactive(emptySourceFilters()), appliedFilters = ref(emptySourceFilters());
const activeTab = ref("sources");
const supplierOptions = computed(() => [...new Map(fullRows.value.map(row => [row.supplier_id, { id: row.supplier_id, name: row.supplier_name }])).values()]);
const filteredRows = computed(() => filterSources(fullRows.value, appliedFilters.value));
const data = computed<ResourcePage<ResourceRow>>(() => ({ view: "flat", list: filteredRows.value.slice((query.page - 1) * query.size, query.page * query.size), total: filteredRows.value.length, page: query.page, size: query.size }));
function applyFilters() {
  try { filterSources([], filters); appliedFilters.value = structuredClone(JSON.parse(JSON.stringify(filters))); query.page = 1; error.value = ""; }
  catch (value) { error.value = value instanceof Error ? value.message : "筛选条件不正确。"; }
}
function resetFilters() { Object.assign(filters, emptySourceFilters()); applyFilters(); }
const selectedTab = ref<"profile" | "report" | "reports" | "operations" | "plans">("profile");
const plannedResource = ref<ResourceRow>();
function showResource(row: ResourceRow, tab: typeof selectedTab.value = "profile") { selectedTab.value = tab; selected.value = row; }
function operate(row: ResourceRow) { if (canCreateStatePlan(row)) plannedResource.value = row; else showResource(row, "operations"); }
function planSaved() { plannedResource.value = undefined; void load(); emit("changed"); }
const query = reactive({ modelID: props.group.id, page: 1, size: 20, sort_by: "coefficient", order: "asc" });
let active = true, controller: AbortController | undefined;
async function load(reset = false) {
  if (reset) query.page = 1;
  controller?.abort(); const request = controller = new AbortController(); loading.value = true; error.value = "";
  try { const result = await resourcesApi.allRows(buildResourceQuery(props.filters, query), request.signal); if (active && !request.signal.aborted) fullRows.value = result; }
  catch (value) { if (active && !request.signal.aborted) { fullRows.value = []; error.value = value instanceof Error ? value.message : "供应来源暂时无法加载，请重试。"; } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function sort(key: string) { query.order = query.sort_by === key && query.order === "asc" ? "desc" : "asc"; query.sort_by = key; void load(true); }
onMounted(() => { void load(); }); onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <section class="model-sources-dialog">
    <el-button text @click="emit('close')">← 返回模型广场</el-button>
    <header class="sources-page-heading"><div class="eyebrow">TOKEN OPS / MODELS</div><h1>{{ group.name || '模型供应来源' }}</h1><p class="sources-subtitle">{{ group.company }} · {{ modelTypeLabels[group.model_type] || '类型待补齐' }} · {{ group.official_id }}</p></header>
    <el-tabs v-model="activeTab"><el-tab-pane label="供应来源" name="sources" /><el-tab-pane label="操作记录" name="records" /></el-tabs>
    <section v-if="activeTab === 'records'" class="sources-panel source-records"><header class="sources-panel-heading"><h2>操作记录</h2></header><ModelSourceOperationRecords :model-i-d="group.id" /></section>
    <section v-else class="sources-panel"><header class="sources-panel-heading"><h2>供应来源</h2></header>
    <form class="source-filters" @submit.prevent="applyFilters">
      <label>供应商名称<el-select v-model="filters.supplierIDs" multiple filterable collapse-tags clearable placeholder="全部供应商"><el-option v-for="supplier in supplierOptions" :key="supplier.id" :value="supplier.id" :label="supplier.name" /></el-select></label>
      <label v-for="range in sourceRanges" :key="range.key">{{ range.label }}<div class="range-inputs"><el-input v-model="filters.ranges[range.key].min" :aria-label="`${range.label}下限`" placeholder="下限" inputmode="decimal" /><span>—</span><el-input v-model="filters.ranges[range.key].max" :aria-label="`${range.label}上限`" placeholder="上限" inputmode="decimal" /></div></label>
      <label>状态<el-select v-model="filters.states" multiple collapse-tags clearable placeholder="全部状态"><el-option v-for="(label, key) in resourceStateLabels" :key="key" :label="label" :value="key" /></el-select></label>
      <el-button native-type="submit" :disabled="loading">筛选</el-button><el-button @click="resetFilters">重置</el-button>
    </form>
    <div class="source-sort"><span>{{ data ? `${data.total} 条供应资源` : '供应来源' }} · 空值末尾</span><label>排序<el-select v-model="query.sort_by" aria-label="供应来源排序字段" @change="load(true)"><el-option label="供应商名称" value="supplier_name" /><el-option label="采购系数" value="coefficient" /><el-option label="连通耗时" value="connection_ms" /><el-option label="RPS" value="rps" /><el-option label="TPS" value="tps" /><el-option label="缓存命中率" value="cache_hit_rate" /></el-select></label><el-select v-model="query.order" aria-label="供应来源排序方向" @change="load(true)"><el-option label="升序" value="asc" /><el-option label="降序" value="desc" /></el-select></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load()">重新加载供应来源</el-button></el-alert>
    <div v-loading="loading" class="sources-scroll" role="region" aria-label="供应来源表，可横向滚动" tabindex="0"><table class="source-table"><thead><tr><th>供应商 / 接口模型 ID</th><th>部署节点</th><th><button @click="sort('coefficient')">采购系数 ↕</button></th><th><button @click="sort('connection_ms')">连通耗时 · ms ↕</button></th><th><button @click="sort('rps')">压力 RPS ↕</button></th><th><button @click="sort('tps')">压力 TPS ↕</button></th><th><button @click="sort('cache_hit_rate')">输入缓存命中率 ↕</button></th><th>资源状态</th><th>报价条件</th><th>测试报告</th><th>资源操作</th><th>详情</th></tr></thead><tbody><tr v-for="row in data?.list || []" :key="row.id"><td><strong>{{ row.supplier_name }}</strong><code>{{ row.supplier_model_id }}</code></td><td>{{ resourceRegionLabels[row.region] }}</td><td class="coefficient">{{ row.coefficient }}</td><td>{{ resourceMetricDisplay(row.test.metrics.connection_ms, row.test.status) }}</td><td>{{ resourceMetricDisplay(row.test.metrics.rps, row.test.status) }}</td><td>{{ resourceMetricDisplay(row.test.metrics.tps, row.test.status) }}</td><td>{{ resourceCacheDisplay(row.test.cache_stats, row.test.status) }}</td><td><el-tag :type="row.state === 'ADMITTED' ? 'success' : 'info'" size="small">{{ resourceStateLabels[row.state] }}</el-tag></td><td>{{ row.quote_readiness.eligible ? '可用于报价' : '暂不可用于报价' }}</td><td><el-button v-if="row.test.report_id" link type="primary" @click="showResource(row, 'reports')">查看报告</el-button><span v-else class="muted">未测试</span></td><td><el-button v-if="canCreateStatePlan(row) || !row.ever_admitted" link type="primary" @click="operate(row)">{{ row.ever_admitted ? row.state === 'OFFLINE' ? '上架' : '下架' : '登记接入' }}</el-button><span v-else class="muted">—</span></td><td><el-button size="small" @click="showResource(row)">详情</el-button></td></tr><tr v-if="!loading && !error && !data?.list.length"><td colspan="12" class="empty">当前筛选暂无供应资源</td></tr></tbody></table></div>
    <footer v-if="data" class="source-pagination"><span>共 {{ data.total }} 条资源</span><el-pagination v-model:current-page="query.page" v-model:page-size="query.size" :total="data.total" :page-sizes="[10, 20, 50, 100]" layout="sizes, prev, pager, next" @size-change="query.page = 1" /></footer>
    </section>
    <CreateStatePlanDialog v-if="plannedResource" :resource="plannedResource" @close="plannedResource = undefined" @saved="planSaved" />
    <ResourceDetailDrawer v-if="selected" :key="`${selected.id}:${selectedTab}`" :id="selected.id" :region="selected.region" :initial-tab="selectedTab" @close="selected = undefined" @changed="load(); emit('changed')" />
  </section>
</template>
<style scoped>
.source-filters{display:flex;gap:12px;flex-wrap:wrap;align-items:end;padding:16px 20px;border-bottom:1px solid #e6e8ed}.source-filters label{display:grid;gap:8px;font-size:12px;width:190px}.range-inputs{display:flex;align-items:center;gap:5px}.source-records>.el-select{width:min(480px,90%);margin:18px}.source-records>.resource-records{padding:0 18px 18px}
.sources-page-heading{margin:12px 0 24px}.sources-page-heading h1{font-size:24px;margin:0 0 8px}.sources-panel{background:#fff;border:1px solid #e6e8ed;border-radius:8px;overflow:hidden}.sources-panel-heading{padding:16px 20px;border-bottom:1px solid #e6e8ed}.sources-panel-heading h2{font-size:15px;margin:0}.source-sort,.source-pagination{padding:16px 20px!important}

.sources-subtitle,.source-sort>span{color:#8a9099;font-size:13px}.source-sort{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:16px 0}.source-sort>span{margin-right:auto}.source-sort label{display:flex;align-items:center;gap:8px}.source-sort .el-select{width:145px}.sources-scroll{overflow:auto;min-height:200px}.source-table{width:100%;border-collapse:collapse;font-size:13px}.source-table th,.source-table td{padding:15px 16px;border-bottom:1px solid #e6e8ed;text-align:left;vertical-align:top;min-width:125px}.source-table th{background:#f5f7f8;white-space:nowrap;color:#526970}.source-table th button{border:0;padding:0;background:none;font:inherit;color:inherit;cursor:pointer}.source-table code{display:block;font-size:12px;color:#8a9099;margin-top:8px;overflow-wrap:anywhere}.coefficient{color:#315ee8;font-weight:600}.source-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding-top:18px}.source-pagination>span{font-size:13px;color:#8a9099}.empty{text-align:center!important;color:#8a9099;padding:40px!important}:global(.model-sources-dialog){--el-color-primary:#315ee8}:global(.model-sources-dialog .el-dialog__body){max-height:75vh;overflow:auto}

.source-table{font-size:12px}.source-table th,.source-table td{min-width:70px;padding:12px 10px}.source-table th{white-space:normal}.source-table th:first-child,.source-table td:first-child{min-width:135px}.model-sources-dialog{--el-color-primary:#315ee8}
</style>
