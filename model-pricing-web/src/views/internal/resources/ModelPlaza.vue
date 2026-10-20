<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import type { ResourceFilterOptions, ResourceGroup, ResourcePage, ResourceQuery } from "../../../api/resources.types";
import ModelSourcesDialog from "./ModelSourcesDialog.vue";
const filters = reactive({ keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" });
const applied = ref(buildResourceQuery(filters)), page = ref(1), size = ref(20), sortBy = ref("name"), order = ref("asc");
const data = ref<ResourcePage<ResourceGroup>>(), loading = ref(false), error = ref(""), selected = ref<ResourceGroup>();
const optionError = ref(""), supplierLoading = ref(false), suppliers = ref<ResourceFilterOptions["suppliers"]>([]), moreOptions = ref(false);
const supplierNames = new Map<string, ResourceFilterOptions["suppliers"][number]>();
const supplierChoices = computed(() => filters.supplier_id && !suppliers.value.some(supplier => supplier.id === filters.supplier_id) && supplierNames.has(filters.supplier_id) ? [supplierNames.get(filters.supplier_id)!, ...suppliers.value] : suppliers.value);
let active = true, listRequest: AbortController | undefined, supplierRequest: AbortController | undefined, companyRequest: AbortController | undefined;
const message = (value: unknown) => value instanceof Error ? value.message : "数据暂时无法加载，请重试。";
async function load() {
  listRequest?.abort(); const request = listRequest = new AbortController(); loading.value = true; error.value = "";
  try { const result = await resourcesApi.groups({ ...applied.value, page: page.value, size: size.value, sort_by: sortBy.value, order: order.value }, request.signal); if (active && !request.signal.aborted) data.value = result; }
  catch (value) { if (active && !request.signal.aborted) { data.value = undefined; error.value = message(value); } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function apply() { try { applied.value = buildResourceQuery(filters); selected.value = undefined; page.value = 1; void load(); } catch (value) { error.value = message(value); } }
function reset() { Object.assign(filters, { keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" }); sortBy.value = "name"; order.value = "asc"; apply(); }
function sort(key: string) { order.value = sortBy.value === key && order.value === "asc" ? "desc" : "asc"; sortBy.value = key; page.value = 1; void load(); }
async function searchSuppliers(keyword = "") {
  supplierRequest?.abort(); const request = supplierRequest = new AbortController(); supplierLoading.value = true; optionError.value = "";
  try { const result = await resourcesApi.options(keyword, filters.region, request.signal); if (active && !request.signal.aborted) { suppliers.value = result.suppliers; moreOptions.value = result.has_more; for (const supplier of result.suppliers) supplierNames.set(supplier.id, supplier); } }
  catch (value) { if (active && !request.signal.aborted) { suppliers.value = []; optionError.value = message(value); } }
  finally { if (active && !request.signal.aborted) supplierLoading.value = false; }
}
async function companySuggestions(keyword: string, callback: (values: { value: string }[]) => void) {
  companyRequest?.abort(); const request = companyRequest = new AbortController();
  try { const result = await resourcesApi.options(keyword, filters.region, request.signal); if (active && !request.signal.aborted) { callback(result.companies.map(value => ({ value }))); moreOptions.value = result.has_more; } }
  catch (value) { if (active && !request.signal.aborted) { optionError.value = message(value); callback([]); } }
}
const scopeForSources = computed<ResourceQuery>(() => ({ ...applied.value }));
function supplierOpened(visible: boolean) { if (visible) void searchSuppliers(); }
watch(() => filters.region, () => { filters.supplier_id = ""; suppliers.value = []; supplierRequest?.abort(); });
onMounted(() => { void load(); }); onBeforeUnmount(() => { active = false; listRequest?.abort(); supplierRequest?.abort(); companyRequest?.abort(); });
</script>
<template>
  <section class="model-plaza">
    <header class="plaza-heading"><h1>模型广场</h1><p>按标准模型汇总供应资源，采购系数展示当前筛选范围。</p></header>
    <section class="plaza-panel"><header class="plaza-panel-heading"><h2>模型资源目录</h2><span>按模型分组查看供应来源与测试表现</span></header>
      <form class="plaza-filters" @submit.prevent="apply">
        <label>搜索<el-input v-model="filters.keyword" aria-label="搜索模型" clearable placeholder="模型 / 官方 ID / 供应商" /></label>
        <label>模型公司<el-autocomplete v-model="filters.company" aria-label="模型公司" :fetch-suggestions="companySuggestions" clearable placeholder="输入公司名称" /></label>
        <label>模型类型<el-select v-model="filters.model_type" aria-label="模型类型" clearable placeholder="全部类型"><el-option v-for="(name, key) in modelTypeLabels" :key="key" :label="name" :value="key" /></el-select></label>
        <label>部署节点<el-select v-model="filters.region" aria-label="部署节点" clearable placeholder="全部节点"><el-option v-for="(name, key) in resourceRegionLabels" :key="key" :label="name" :value="key" /></el-select></label>
        <label>供应商<el-select v-model="filters.supplier_id" aria-label="供应商" clearable filterable remote reserve-keyword :remote-method="searchSuppliers" :loading="supplierLoading" placeholder="输入匹配供应商名称" @visible-change="supplierOpened"><el-option v-for="supplier in supplierChoices" :key="supplier.id" :label="`${supplier.name} · ${resourceRegionLabels[supplier.region]}`" :value="supplier.id" /></el-select></label>
        <label>资源状态<el-select v-model="filters.state" aria-label="资源状态" clearable placeholder="全部状态"><el-option v-for="(name, key) in resourceStateLabels" :key="key" :label="name" :value="key" /></el-select></label>
        <el-button native-type="submit" type="primary" :loading="loading">查询</el-button><el-button @click="reset">重置</el-button>
      </form>
      <el-alert v-if="optionError" :title="optionError" type="warning" :closable="false"><el-button text @click="searchSuppliers()">重新加载筛选选项</el-button></el-alert><p v-if="moreOptions" class="options-hint">匹配项较多，可继续输入完整名称查找。</p>
      <div class="plaza-note"><span>{{ data ? `${data.total} 个模型` : '模型资源' }} · 所有统计基于已应用的筛选</span><label>排序<el-select v-model="sortBy" aria-label="广场排序字段" @change="page = 1; load()"><el-option label="模型名称" value="name" /><el-option label="模型公司" value="company" /><el-option label="供应商数" value="supplier_count" /><el-option label="可用资源数" value="available_count" /><el-option label="采购系数下限" value="coefficient_min" /></el-select><el-select v-model="order" aria-label="广场排序方向" @change="page = 1; load()"><el-option label="升序" value="asc" /><el-option label="降序" value="desc" /></el-select></label></div>
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载模型广场</el-button></el-alert>
      <div v-loading="loading" class="plaza-scroll" role="region" aria-label="模型资源目录，可横向滚动" tabindex="0"><table class="plaza-table"><thead><tr><th><button @click="sort('name')">模型 / 官方 ID ↕</button></th><th><button @click="sort('company')">模型公司 ↕</button></th><th>模型类型</th><th>部署节点</th><th><button @click="sort('supplier_count')">供应来源 ↕</button></th><th><button @click="sort('coefficient_min')">采购系数 ↕</button></th><th>资源状态</th><th>详情</th></tr></thead><tbody><tr v-for="group in data?.list || []" :key="group.id"><td><strong>{{ group.name || '待补充名称' }}</strong><code>{{ group.official_id || '官方 ID 待补齐' }}</code></td><td>{{ group.company || '待补齐' }}</td><td>{{ modelTypeLabels[group.model_type] || '待补齐' }}</td><td>{{ group.regions.map(region => resourceRegionLabels[region]).join(' / ') }}</td><td><strong>{{ group.supplier_count }} 家</strong><span class="meta">{{ group.resource_count }} 条资源 · {{ group.available_count }} 条可用</span></td><td class="coefficient">{{ group.coefficient_min === group.coefficient_max ? group.coefficient_min : `${group.coefficient_min}～${group.coefficient_max}` }}</td><td><span v-for="(count, state) in group.state_counts" :key="state" class="state-count">{{ resourceStateLabels[state] }} {{ count }}</span></td><td><el-button size="small" @click="selected = group">详情</el-button></td></tr><tr v-if="!loading && !error && !data?.list.length"><td colspan="8" class="empty">当前筛选暂无模型资源</td></tr></tbody></table></div>
      <footer v-if="data" class="plaza-pagination"><span>共 {{ data.total }} 个模型</span><el-pagination v-model:current-page="page" v-model:page-size="size" :total="data.total" :page-sizes="[10, 20, 50, 100]" layout="sizes, prev, pager, next" @current-change="load()" @size-change="page = 1; load()" /></footer>
    </section>
    <ModelSourcesDialog v-if="selected" :key="selected.id" :group="selected" :filters="scopeForSources" @close="selected = undefined" @changed="load()" />
  </section>
</template>
<style scoped>
.model-plaza{color:#20313c;--el-color-primary:#087c72}.plaza-heading{margin-bottom:22px}.plaza-heading h1{font-size:25px;margin:0 0 9px}.plaza-heading p{font-size:14px;color:#79868f;margin:0}.plaza-panel{border:1px solid #e6ebee;border-radius:10px;background:#fff;overflow:hidden}.plaza-panel-heading{display:flex;align-items:center;gap:18px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6ebee}.plaza-panel-heading h2{font-size:17px;margin:0}.plaza-panel-heading>span,.plaza-note,.options-hint{font-size:12px;color:#79868f}.plaza-filters{display:flex;align-items:end;gap:12px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6ebee}.plaza-filters label{display:grid;gap:7px;width:175px;font-size:13px}.plaza-note{display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap;padding:16px 22px}.plaza-note label{display:flex;align-items:center;gap:8px}.plaza-note .el-select{width:140px}.options-hint{padding:0 22px}.plaza-scroll{overflow:auto;min-height:200px}.plaza-table{width:100%;border-collapse:collapse;font-size:13px}.plaza-table th,.plaza-table td{padding:16px;border-bottom:1px solid #e6ebee;text-align:left;vertical-align:top;min-width:125px}.plaza-table th{background:#f5f7f8;white-space:nowrap;color:#526970}.plaza-table th button{border:0;background:none;padding:0;font:inherit;color:inherit;cursor:pointer}.plaza-table td:first-child{min-width:200px}.plaza-table code{display:block;margin-top:8px;color:#79868f;font-size:12px;overflow-wrap:anywhere}.coefficient{color:#087c72;font-weight:600}.meta{display:block;color:#79868f;font-size:12px;margin-top:7px}.state-count{display:block;margin-bottom:7px}.plaza-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:16px 22px}.plaza-pagination>span{font-size:13px;color:#79868f}.empty{text-align:center!important;color:#79868f;padding:45px!important}@media(max-width:700px){.plaza-filters label{width:calc(50% - 6px)}}
</style>
