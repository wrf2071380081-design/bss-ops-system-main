<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import type { ResourceGroup, ResourcePage, ResourceQuery, ResourceRow } from "../../../api/resources.types";
import ResourceDetailDrawer from "./ResourceDetailDrawer.vue";
const props = defineProps<{ group: ResourceGroup; filters: ResourceQuery }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const data = ref<ResourcePage<ResourceRow>>(), error = ref(""), loading = ref(false), selected = ref<ResourceRow>();
const query = reactive({ modelID: props.group.id, page: 1, size: 20, sort_by: "coefficient", order: "asc" });
let active = true, controller: AbortController | undefined;
async function load(reset = false) {
  if (reset) query.page = 1;
  controller?.abort(); const request = controller = new AbortController(); loading.value = true; error.value = "";
  try { const result = await resourcesApi.rows(buildResourceQuery(props.filters, query), request.signal); if (active && !request.signal.aborted) data.value = result; }
  catch (value) { if (active && !request.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "供应来源暂时无法加载，请重试。"; } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function sort(key: string) { query.order = query.sort_by === key && query.order === "asc" ? "desc" : "asc"; query.sort_by = key; void load(true); }
onMounted(() => { void load(); }); onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <el-dialog class="model-sources-dialog" :model-value="true" :title="group.name || '模型供应来源'" width="min(1450px, 96vw)" top="6vh" @close="emit('close')">
    <p class="sources-subtitle">{{ group.company }} · {{ group.official_id }} · 保留所选供应商、区域及资源状态范围</p>
    <div class="source-sort"><span>{{ data ? `${data.total} 条供应资源` : '供应来源' }} · 空值末尾</span><label>排序<el-select v-model="query.sort_by" aria-label="供应来源排序字段" @change="load(true)"><el-option label="供应商名称" value="supplier_name" /><el-option label="采购系数" value="coefficient" /><el-option label="连通耗时" value="connection_ms" /><el-option label="RPS" value="rps" /><el-option label="TPS" value="tps" /><el-option label="缓存命中率" value="cache_hit_rate" /></el-select></label><el-select v-model="query.order" aria-label="供应来源排序方向" @change="load(true)"><el-option label="升序" value="asc" /><el-option label="降序" value="desc" /></el-select></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load()">重新加载供应来源</el-button></el-alert>
    <div v-loading="loading" class="sources-scroll" role="region" aria-label="供应来源表，可横向滚动" tabindex="0"><table class="source-table"><thead><tr><th>供应商 / 接口模型 ID</th><th>部署节点</th><th><button @click="sort('coefficient')">采购系数 ↕</button></th><th><button @click="sort('connection_ms')">连通耗时 · ms ↕</button></th><th><button @click="sort('rps')">压力 RPS ↕</button></th><th><button @click="sort('tps')">压力 TPS ↕</button></th><th><button @click="sort('cache_hit_rate')">输入缓存命中率 ↕</button></th><th>资源状态</th><th>报价条件</th><th>详情</th></tr></thead><tbody><tr v-for="row in data?.list || []" :key="row.id"><td><strong>{{ row.supplier_name }}</strong><code>{{ row.supplier_model_id }}</code></td><td>{{ resourceRegionLabels[row.region] }}</td><td class="coefficient">{{ row.coefficient }}</td><td>{{ resourceMetricDisplay(row.test.metrics.connection_ms, row.test.status) }}</td><td>{{ resourceMetricDisplay(row.test.metrics.rps, row.test.status) }}</td><td>{{ resourceMetricDisplay(row.test.metrics.tps, row.test.status) }}</td><td>{{ resourceCacheDisplay(row.test.cache_stats, row.test.status) }}</td><td><el-tag :type="row.state === 'ADMITTED' ? 'success' : 'info'" size="small">{{ resourceStateLabels[row.state] }}</el-tag></td><td>{{ row.quote_readiness.eligible ? '可用于报价' : '暂不可用于报价' }}</td><td><el-button size="small" @click="selected = row">详情</el-button></td></tr><tr v-if="!loading && !error && !data?.list.length"><td colspan="10" class="empty">当前筛选暂无供应资源</td></tr></tbody></table></div>
    <footer v-if="data" class="source-pagination"><span>共 {{ data.total }} 条资源</span><el-pagination v-model:current-page="query.page" v-model:page-size="query.size" :total="data.total" :page-sizes="[10, 20, 50, 100]" layout="sizes, prev, pager, next" @current-change="load()" @size-change="load(true)" /></footer>
    <ResourceDetailDrawer v-if="selected" :key="selected.id" :id="selected.id" :region="selected.region" @close="selected = undefined" @changed="load(); emit('changed')" />
    <template #footer><el-button @click="emit('close')">关闭供应来源</el-button></template>
  </el-dialog>
</template>
<style scoped>
.sources-subtitle,.source-sort>span{color:#79868f;font-size:13px}.source-sort{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:16px 0}.source-sort>span{margin-right:auto}.source-sort label{display:flex;align-items:center;gap:8px}.source-sort .el-select{width:145px}.sources-scroll{overflow:auto;min-height:200px}.source-table{width:100%;border-collapse:collapse;font-size:13px}.source-table th,.source-table td{padding:15px 16px;border-bottom:1px solid #e6ebee;text-align:left;vertical-align:top;min-width:125px}.source-table th{background:#f5f7f8;white-space:nowrap;color:#526970}.source-table th button{border:0;padding:0;background:none;font:inherit;color:inherit;cursor:pointer}.source-table code{display:block;font-size:12px;color:#79868f;margin-top:8px;overflow-wrap:anywhere}.coefficient{color:#087c72;font-weight:600}.source-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding-top:18px}.source-pagination>span{font-size:13px;color:#79868f}.empty{text-align:center!important;color:#79868f;padding:40px!important}:global(.model-sources-dialog){--el-color-primary:#087c72}:global(.model-sources-dialog .el-dialog__body){max-height:75vh;overflow:auto}
</style>
