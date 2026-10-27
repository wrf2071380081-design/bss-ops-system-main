<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { resourcesApi } from "../../../api/resources";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import type { ResourceGroup } from "../../../api/resources.types";
import { decodeSourceFilters, emptySourceFilters, encodeSourceFilters, validateSourceFilters, sourceResourceQuery, type PlazaGroup } from "../../../domain/modelPlaza";
import ResourceDetailDrawer from "./ResourceDetailDrawer.vue";
import SourceFilterBar from "./SourceFilterBar.vue";
import { useResourceOptions } from "./useResourceOptions";

const route = useRoute(), router = useRouter();
const directResourceID = computed(() => typeof route.params.resourceId === "string" ? route.params.resourceId : "");
const directRegion = computed(() => ["CN", "OVERSEAS"].includes(String(route.query.region)) ? String(route.query.region) : "");
const directPlanID = computed(() => typeof route.query.plan_id === "string" ? route.query.plan_id : "");
const directTab = computed<"profile" | "report" | "reports" | "operations" | "notices" | "plans" | "records">(() => {
  const tab = route.query.tab;
  return tab === "profile" || tab === "report" || tab === "reports" || tab === "records" || tab === "operations" || tab === "notices" ? tab : "plans";
});
const filters = ref(emptySourceFilters()), applied = ref(emptySourceFilters());
const groups = ref<ResourceGroup[]>([]), total = ref(0), loading = ref(false), error = ref(""), filterError = ref("");
const { choices, optionError, loadOptions } = useResourceOptions(filters);
const page = ref(1), size = ref(20), sortBy = ref("name"), order = ref<"asc" | "desc">("asc");
const sortFields = ["name", "company", "model_type", "supplier_count", "resource_count", "available_count", "coefficient_min"];
let active = true, request: AbortController | undefined;
const text = (value: unknown) => typeof value === "string" ? value : "";
function positive(value: unknown, fallback: number, max = Number.MAX_SAFE_INTEGER) { const n = Number(text(value)); return Number.isSafeInteger(n) && n > 0 && n <= max ? n : fallback; }
function restore() {
  try {
    filters.value = decodeSourceFilters(route.query.filters);
    if (!route.query.filters) {
      filters.value.keyword = text(route.query.keyword);
      for (const [key, legacy] of [["companies", "company"], ["model_types", "model_type"], ["supplier_ids", "supplier_id"], ["regions", "region"], ["states", "state"]] as const) {
        const value = text(route.query[legacy]); if (value) filters.value[key] = [value];
      }
    }
    applied.value = JSON.parse(encodeSourceFilters(filters.value));
    page.value = positive(route.query.page, 1); size.value = positive(route.query.size, 20, 100);
    sortBy.value = sortFields.includes(text(route.query.sort_by)) ? text(route.query.sort_by) : "name";
    order.value = route.query.order === "desc" ? "desc" : "asc";
  } catch { filterError.value = "筛选参数无效，已恢复默认筛选。"; filters.value = emptySourceFilters(); applied.value = emptySourceFilters(); }
}
const viewPage = computed(() => ({ list: groups.value.map(group => ({ ...group, state_label: group.state_counts.ADMITTED ? "有可用资源" : group.state_counts.PENDING_TEST || group.state_counts.PENDING_ACCESS ? "资源待接入" : "无可用资源" })), total: total.value }));
async function load() {
  request?.abort(); const current = request = new AbortController(); loading.value = true; error.value = "";
  try {
    const result = await resourcesApi.groups(sourceResourceQuery(applied.value, page.value, size.value, sortBy.value, order.value), current.signal);
    if (active && !current.signal.aborted) {
      groups.value = result.list; total.value = result.total;
      const last = Math.max(1, Math.ceil(result.total / size.value)); if (page.value > last) { page.value = last; void load(); }
    }
  } catch (value) { if (active && !current.signal.aborted) { groups.value = []; total.value = 0; error.value = value instanceof Error ? value.message : "模型广场暂时无法加载。"; } }
  finally { if (active && !current.signal.aborted) loading.value = false; }
}
function apply() {
  try { validateSourceFilters(filters.value); applied.value = JSON.parse(encodeSourceFilters(filters.value)); page.value = 1; filterError.value = ""; void load(); }
  catch (value) { filterError.value = value instanceof Error ? value.message : "筛选参数无效。"; }
}
function reset() { filters.value = emptySourceFilters(); sortBy.value = "name"; order.value = "asc"; apply(); }
function sort(key: string) { order.value = sortBy.value === key && order.value === "asc" ? "desc" : "asc"; sortBy.value = key; page.value = 1; void load(); }
function arrow(key: string) { return sortBy.value === key ? order.value === "asc" ? "↑" : "↓" : "↕"; }
function openDetail(row: PlazaGroup) {
  const query: Record<string, string> = { return_filters: encodeSourceFilters(applied.value), return_page: String(page.value), return_size: String(size.value), return_sort_by: sortBy.value, return_order: order.value };
  for (const key of ["keyword", "company", "model_type", "supplier_id", "supplier_name", "supplier_region", "region", "state"]) {
    const value = text(route.query[key]); if (value) query[`return_${key}`] = value;
  }
  void router.push({ path: `/internal/model-detail/${row.id}`, query });
}
function closeDirectResource() { void router.replace("/internal/model-plaza"); }
onMounted(() => { restore(); void load(); void loadOptions(); });
watch(() => route.fullPath, () => { if (!directResourceID.value) { restore(); void load(); } });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>
<template>
  <section class="model-plaza">
    <template v-if="!directResourceID">
      <header class="plaza-heading"><div class="eyebrow">TOKEN OPS / MODELS</div><h1>模型广场</h1><p>按标准模型汇总供应资源，采购系数展示当前筛选范围。</p></header>
      <section class="plaza-panel">
        <header class="plaza-panel-heading"><h2>模型资源目录</h2></header>
        <SourceFilterBar v-model="filters" class="plaza-filters" :rows="[]" :options="choices" :loading="loading" @search="loadOptions" @apply="apply" @reset="reset" />
        <el-alert v-if="optionError" :title="optionError" type="warning" :closable="false" /><p v-if="choices.has_more" class="plaza-note">还有更多选项，请输入名称缩小搜索范围。</p>
        <div class="plaza-note"><span>共 {{ viewPage.total }} 个模型 · 统计基于当前筛选范围</span><el-button text :loading="loading" @click="load">刷新资源</el-button></div>
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载模型广场</el-button></el-alert>
        <el-alert v-if="filterError" :title="filterError" type="warning" :closable="false" />
        <div v-loading="loading" class="plaza-scroll" role="region" aria-label="模型资源目录，可横向滚动" tabindex="0">
          <table class="plaza-table"><thead><tr>
            <th v-for="column in [{key:'name',label:'模型 / 官方 ID'},{key:'company',label:'模型公司'},{key:'model_type',label:'模型类型'}]" :key="column.key" :aria-sort="sortBy === column.key ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button type="button" @click="sort(column.key)">{{ column.label }} {{ arrow(column.key) }}</button></th>
            <th>部署节点</th><th :aria-sort="sortBy === 'supplier_count' ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button type="button" @click="sort('supplier_count')">供应来源 {{ arrow('supplier_count') }}</button></th><th :aria-sort="sortBy === 'coefficient_min' ? order === 'asc' ? 'ascending' : 'descending' : 'none'"><button type="button" @click="sort('coefficient_min')">采购系数 {{ arrow('coefficient_min') }}</button></th><th>资源状态</th><th>详情</th>
          </tr></thead><tbody>
            <tr v-for="row in viewPage.list" :key="row.id"><td><strong>{{ row.name || '待补充名称' }}</strong><code>{{ row.official_id || '官方 ID 待补齐' }}</code></td><td>{{ row.company || '待补齐' }}</td><td>{{ modelTypeLabels[row.model_type] || '待补齐' }}</td><td>{{ row.regions.map(region => resourceRegionLabels[region]).join(' / ') }}</td><td>{{ row.supplier_count }} 家<span class="meta">{{ row.resource_count }} 条资源</span></td><td class="coefficient">{{ row.coefficient_min === row.coefficient_max ? row.coefficient_min : `${row.coefficient_min} – ${row.coefficient_max}` }}</td><td><el-tag :type="row.state_counts.ADMITTED ? 'success' : row.state_label === '资源待接入' ? 'warning' : 'info'" size="small">{{ row.state_label }}</el-tag><span class="meta">{{ row.available_count }} 条可报价</span></td><td><el-button size="small" @click="openDetail(row)">详情</el-button></td></tr>
            <tr v-if="!loading && !error && !filterError && !viewPage.list.length"><td colspan="8" class="empty">当前筛选暂无供应资源</td></tr>
          </tbody></table>
        </div>
        <footer v-if="viewPage.total" class="plaza-pagination"><span>共 {{ viewPage.total }} 个模型</span><el-pagination v-model:current-page="page" v-model:page-size="size" :total="viewPage.total" :page-sizes="[10,20,50,100]" layout="sizes, prev, pager, next" @current-change="load" @size-change="page = 1; load()" /></footer>
      </section>
    </template>
    <ResourceDetailDrawer v-if="directResourceID" :key="`${directResourceID}:${directRegion}:${directTab}:${directPlanID}`" :id="directResourceID" :region="directRegion" :initial-tab="directTab" :initial-plan-i-d="directPlanID" @close="closeDirectResource" @changed="load" />
  </section>
</template>
<style scoped>
.model-plaza{color:#1c1d1f;--el-color-primary:#315ee8}.plaza-heading{margin-bottom:22px}.plaza-heading h1{font-size:25px;margin:0 0 9px}.plaza-heading p{font-size:14px;color:#8a9099;margin:0}.plaza-panel{border:1px solid #e6e8ed;border-radius:10px;background:#fff;overflow:hidden}.plaza-panel-heading{padding:18px 22px;border-bottom:1px solid #e6e8ed}.plaza-panel-heading h2{font-size:17px;margin:0}.plaza-filters{padding:14px 18px;border-bottom:1px solid #e6e8ed}.plaza-note{display:flex;align-items:center;justify-content:space-between;padding:4px 18px;font-size:12px;color:#8a9099}.plaza-scroll{overflow:auto;min-height:200px}.plaza-table{width:100%;border-collapse:collapse;font-size:13px}.plaza-table th,.plaza-table td{padding:14px 16px;border-bottom:1px solid #e6e8ed;text-align:left;vertical-align:top;min-width:100px}.plaza-table th{background:#f8f9fb;white-space:nowrap;color:#526970;font-weight:500}.plaza-table th button{border:0;background:none;padding:0;font:inherit;color:inherit;cursor:pointer}.plaza-table td:first-child{min-width:190px}.plaza-table td:last-child{min-width:75px}.plaza-table code{display:block;margin-top:7px;color:#8a9099;font-size:12px;overflow-wrap:anywhere}.coefficient{color:#315ee8;font-weight:600;font-variant-numeric:tabular-nums;white-space:nowrap}.meta{display:block;color:#8a9099;font-size:12px;margin-top:7px}.plaza-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:16px 22px;font-size:13px;color:#8a9099}.empty{text-align:center!important;color:#8a9099;padding:45px!important}
</style>
