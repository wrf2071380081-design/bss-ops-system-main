<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import { standardModelsApi } from "../../../api/standardModels";
import type { ResourceFilterOptions, ResourceGroup, ResourceQuery } from "../../../api/resources.types";
import type { StandardModelDTO } from "../../../api/standardModels.types";
import ResourceDetailDrawer from "./ResourceDetailDrawer.vue";

interface PlazaRow {
  id: string; name: string; official_id: string; company: string; model_type: string;
  supplier_count: number; resource_count: number; available_count: number;
  coefficient_min: string; coefficient_max: string; regions: string[]; state_counts: Record<string, number>;
}

const route = useRoute(), router = useRouter();
const directResourceID = computed(() => typeof route.params.resourceId === "string" ? route.params.resourceId : "");
const directRegion = computed(() => ["CN", "OVERSEAS"].includes(String(route.query.region)) ? String(route.query.region) : "");
const directPlanID = computed(() => typeof route.query.plan_id === "string" ? route.query.plan_id : "");
const directTab = computed<"profile" | "report" | "reports" | "operations" | "notices" | "plans" | "records">(() => {
  const tab = route.query.tab;
  return tab === "profile" || tab === "report" || tab === "reports" || tab === "records" || tab === "operations" || tab === "notices" ? tab : "plans";
});

const filters = reactive({ keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" });
const applied = ref<ResourceQuery>(buildResourceQuery(filters));
const page = ref(1), size = ref(10), sortBy = ref("name"), order = ref("asc");
const models = ref<StandardModelDTO[]>([]), groups = ref<ResourceGroup[]>([]);
const loading = ref(false), error = ref("");
const optionError = ref(""), supplierLoading = ref(false), suppliers = ref<ResourceFilterOptions["suppliers"]>([]), moreOptions = ref(false);
const supplierNames = new Map<string, ResourceFilterOptions["suppliers"][number]>();
const supplierChoices = computed(() => filters.supplier_id && !suppliers.value.some(supplier => supplier.id === filters.supplier_id) && supplierNames.has(filters.supplier_id) ? [supplierNames.get(filters.supplier_id)!, ...suppliers.value] : suppliers.value);
let active = true, listRequest: AbortController | undefined, supplierRequest: AbortController | undefined, companyRequest: AbortController | undefined;
let restoringRouteState = false;
const message = (value: unknown) => value instanceof Error ? value.message : "数据暂时无法加载，请重试。";
const groupedSortFields = ["name", "company", "supplier_count", "available_count", "coefficient_min"];
function routeText(value: unknown) { return typeof value === "string" ? value : ""; }
function positiveRouteInt(value: unknown, fallback: number, max = Number.MAX_SAFE_INTEGER) {
  const parsed = Number(routeText(value));
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= max ? parsed : fallback;
}
function restoreRouteState() {
  restoringRouteState = true;
  try {
    Object.assign(filters, {
      keyword: routeText(route.query.keyword), company: routeText(route.query.company),
      model_type: routeText(route.query.model_type), supplier_id: routeText(route.query.supplier_id),
      region: routeText(route.query.region), state: routeText(route.query.state),
    });
    page.value = positiveRouteInt(route.query.page, 1);
    size.value = positiveRouteInt(route.query.size, 10, 100);
    sortBy.value = groupedSortFields.includes(routeText(route.query.sort_by)) ? routeText(route.query.sort_by) : "name";
    order.value = route.query.order === "desc" ? "desc" : "asc";
    const supplierName = routeText(route.query.supplier_name), supplierRegion = routeText(route.query.supplier_region);
    if (filters.supplier_id && supplierName && ["CN", "OVERSEAS"].includes(supplierRegion)) {
      supplierNames.set(filters.supplier_id, { id: filters.supplier_id, name: supplierName, region: supplierRegion });
    }
    applied.value = buildResourceQuery(filters);
  } catch {
    Object.assign(filters, { keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" });
    page.value = 1;
    size.value = 10;
    sortBy.value = "name";
    order.value = "asc";
    applied.value = buildResourceQuery(filters);
  } finally {
    restoringRouteState = false;
  }
}
function modelPlazaReturnState() {
  const selectedSupplier = supplierChoices.value.find(supplier => supplier.id === filters.supplier_id) || supplierNames.get(filters.supplier_id);
  const values: Record<string, string> = {
    keyword: filters.keyword, company: filters.company, model_type: filters.model_type, supplier_id: filters.supplier_id,
    region: filters.region, state: filters.state, page: String(page.value), size: String(size.value),
    sort_by: sortBy.value, order: order.value,
  };
  if (selectedSupplier) {
    values.supplier_name = selectedSupplier.name;
    values.supplier_region = selectedSupplier.region;
  }
  return Object.fromEntries(Object.entries(values).filter(([, value]) => value !== "").map(([key, value]) => [`return_${key}`, value]));
}

const viewPage = computed(() => {
  const groupByID = new Map(groups.value.map(group => [group.id, group]));
  const rows: PlazaRow[] = models.value.map(model => {
    const group = groupByID.get(model.id);
    return {
      id: model.id,
      name: model.name,
      official_id: model.official_id,
      company: model.company,
      model_type: model.model_type,
      supplier_count: group?.supplier_count ?? 0,
      resource_count: group?.resource_count ?? 0,
      available_count: group?.available_count ?? 0,
      coefficient_min: group?.coefficient_min ?? "",
      coefficient_max: group?.coefficient_max ?? "",
      regions: group?.regions ?? [],
      state_counts: group?.state_counts ?? {},
    };
  });
  const direction = order.value === "asc" ? 1 : -1;
  rows.sort((a, b) => {
    let n = 0;
    switch (sortBy.value) {
      case "company": n = a.company.localeCompare(b.company); break;
      case "supplier_count": n = a.supplier_count - b.supplier_count; break;
      case "available_count": n = a.available_count - b.available_count; break;
      case "coefficient_min": n = (Number(a.coefficient_min) || Number.POSITIVE_INFINITY) - (Number(b.coefficient_min) || Number.POSITIVE_INFINITY); break;
      default: n = a.name.localeCompare(b.name);
    }
    return n * direction || a.id.localeCompare(b.id);
  });
  const start = (page.value - 1) * size.value;
  return { list: rows.slice(start, start + size.value), total: rows.length };
});

async function load() {
  listRequest?.abort();
  const request = listRequest = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const [modelPage, groupPage] = await Promise.all([
      standardModelsApi.list({
        page: 1,
        size: 100,
        keyword: filters.keyword,
        companies: filters.company ? [filters.company] : [],
        model_types: filters.model_type ? [filters.model_type] : [],
      }, request.signal),
      resourcesApi.groups({ ...applied.value, page: 1, size: 100, sort_by: "name", order: "asc" }, request.signal),
    ]);
    if (!active || request.signal.aborted) return;
    models.value = modelPage.list;
    groups.value = groupPage.list;
  } catch (value) {
    if (active && !request.signal.aborted) { models.value = []; groups.value = []; error.value = message(value); }
  } finally {
    if (active && !request.signal.aborted) loading.value = false;
  }
}

function apply() {
  try { applied.value = buildResourceQuery(filters); page.value = 1; void load(); }
  catch (value) { error.value = message(value); }
}
function reset() {
  Object.assign(filters, { keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" });
  sortBy.value = "name"; order.value = "asc"; apply();
}
function sort(key: string) { order.value = sortBy.value === key && order.value === "asc" ? "desc" : "asc"; sortBy.value = key; page.value = 1; }
function openDetail(row: PlazaRow) { void router.push({ path: `/internal/model-detail/${row.id}`, query: modelPlazaReturnState() }); }

async function searchSuppliers(keyword = "") {
  supplierRequest?.abort();
  const request = supplierRequest = new AbortController();
  supplierLoading.value = true; optionError.value = "";
  try {
    const result = await resourcesApi.options(keyword, filters.region, request.signal);
    if (active && !request.signal.aborted) {
      suppliers.value = result.suppliers; moreOptions.value = result.has_more;
      for (const supplier of result.suppliers) supplierNames.set(supplier.id, supplier);
    }
  } catch (value) {
    if (active && !request.signal.aborted) { suppliers.value = []; optionError.value = message(value); }
  } finally {
    if (active && !request.signal.aborted) supplierLoading.value = false;
  }
}
async function companySuggestions(keyword: string, callback: (values: { value: string }[]) => void) {
  companyRequest?.abort();
  const request = companyRequest = new AbortController();
  try {
    const result = await resourcesApi.options(keyword, filters.region, request.signal);
    if (active && !request.signal.aborted) { callback(result.companies.map(value => ({ value }))); moreOptions.value = result.has_more; }
  } catch (value) {
    if (active && !request.signal.aborted) { optionError.value = message(value); callback([]); }
  }
}
function supplierOpened(visible: boolean) { if (visible) void searchSuppliers(); }
watch(() => filters.region, () => { if (restoringRouteState) return; filters.supplier_id = ""; suppliers.value = []; supplierRequest?.abort(); }, { flush: "sync" });
function closeDirectResource() { void router.replace("/internal/model-plaza"); }
onMounted(() => { restoreRouteState(); void load(); });
watch(directResourceID, (id) => { if (!id) void load(); });
onBeforeUnmount(() => { active = false; listRequest?.abort(); supplierRequest?.abort(); companyRequest?.abort(); });
</script>
<template>
  <section class="model-plaza">
    <template v-if="!directResourceID">
      <header class="plaza-heading"><div class="eyebrow">TOKEN OPS / MODELS</div><h1>模型广场</h1><p>按标准模型汇总供应资源，采购系数展示当前筛选范围。</p></header>
      <section class="plaza-panel">
        <header class="plaza-panel-heading"><h2>模型资源目录</h2><span>按模型分组查看供应来源与测试表现</span></header>
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
        <div class="plaza-note">
          <span>共 {{ viewPage.total }} 个官方模型 · 所有统计基于已应用的筛选</span>
          <label>排序
            <el-select v-model="sortBy" aria-label="广场排序字段" @change="page = 1"><el-option label="模型名称" value="name" /><el-option label="模型公司" value="company" /><el-option label="供应商数" value="supplier_count" /><el-option label="可用资源数" value="available_count" /><el-option label="采购系数下限" value="coefficient_min" /></el-select>
            <el-select v-model="order" aria-label="广场排序方向" @change="page = 1"><el-option label="升序" value="asc" /><el-option label="降序" value="desc" /></el-select>
          </label>
        </div>
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载模型广场</el-button></el-alert>
        <div v-loading="loading" class="plaza-scroll" role="region" aria-label="模型资源目录，可横向滚动" tabindex="0">
          <table class="plaza-table">
            <thead><tr><th><button @click="sort('name')">模型 / 官方 ID ↕</button></th><th><button @click="sort('company')">模型公司 ↕</button></th><th>模型类型</th><th>部署节点</th><th><button @click="sort('supplier_count')">供应来源 ↕</button></th><th><button @click="sort('coefficient_min')">采购系数 ↕</button></th><th>资源状态</th><th>详情</th></tr></thead>
            <tbody>
              <tr v-for="row in viewPage.list" :key="row.id">
                <td><strong>{{ row.name || '待补充名称' }}</strong><code>{{ row.official_id || '官方 ID 待补齐' }}</code></td>
                <td>{{ row.company || '待补齐' }}</td>
                <td>{{ modelTypeLabels[row.model_type] || '待补齐' }}</td>
                <td>{{ row.regions.length ? row.regions.map(region => resourceRegionLabels[region]).join(' / ') : '—' }}</td>
                <td><strong>{{ row.supplier_count }} 家</strong><span class="meta">{{ row.resource_count }} 条资源 · {{ row.available_count }} 条可用</span></td>
                <td class="coefficient">{{ row.coefficient_min ? (row.coefficient_min === row.coefficient_max ? row.coefficient_min : `${row.coefficient_min}～${row.coefficient_max}`) : '—' }}</td>
                <td><el-tag v-if="row.available_count > 0" type="success" size="small">有可用资源</el-tag><el-tag v-else type="info" size="small">暂无可用</el-tag></td>
                <td><el-button size="small" @click="openDetail(row)">详情</el-button></td>
              </tr>
              <tr v-if="!loading && !error && !viewPage.list.length"><td colspan="8" class="empty">当前筛选暂无官方模型</td></tr>
            </tbody>
          </table>
        </div>
        <footer v-if="viewPage.total" class="plaza-pagination"><span>共 {{ viewPage.total }} 个模型</span><el-pagination v-model:current-page="page" v-model:page-size="size" :total="viewPage.total" :page-sizes="[10, 20, 50, 100]" layout="sizes, prev, pager, next" @size-change="page = 1" /></footer>
      </section>
    </template>
    <ResourceDetailDrawer v-if="directResourceID" :key="`${directResourceID}:${directRegion}:${directTab}:${directPlanID}`" :id="directResourceID" :region="directRegion" :initial-tab="directTab" :initial-plan-i-d="directPlanID" @close="closeDirectResource" @changed="load()" />
  </section>
</template>
<style scoped>
.model-plaza{color:#1c1d1f;--el-color-primary:#315ee8}.plaza-heading{margin-bottom:22px}.plaza-heading h1{font-size:25px;margin:0 0 9px}.plaza-heading p{font-size:14px;color:#8a9099;margin:0}.plaza-panel{border:1px solid #e6e8ed;border-radius:10px;background:#fff;overflow:hidden}.plaza-panel-heading{display:flex;align-items:center;gap:18px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6e8ed}.plaza-panel-heading h2{font-size:17px;margin:0}.plaza-panel-heading>span,.plaza-note,.options-hint{font-size:12px;color:#8a9099}.plaza-filters{display:flex;align-items:end;gap:12px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6e8ed}.plaza-filters label{display:grid;gap:7px;width:175px;font-size:13px}.plaza-note{display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap;padding:16px 22px}.plaza-note label{display:flex;align-items:center;gap:8px}.plaza-note .el-select{width:140px}.options-hint{padding:0 22px}.plaza-scroll{overflow:auto;min-height:200px}.plaza-table{width:100%;border-collapse:collapse;font-size:13px}.plaza-table th,.plaza-table td{padding:16px;border-bottom:1px solid #e6e8ed;text-align:left;vertical-align:top;min-width:125px}.plaza-table th{background:#f5f7f8;white-space:nowrap;color:#526970}.plaza-table th button{border:0;background:none;padding:0;font:inherit;color:inherit;cursor:pointer}.plaza-table td:first-child{min-width:200px}.plaza-table .model-name-link{height:auto;padding:0;font-size:13px;font-weight:600}.plaza-table code{display:block;margin-top:8px;color:#8a9099;font-size:12px;overflow-wrap:anywhere}.coefficient{color:#315ee8;font-weight:600}.meta{display:block;color:#8a9099;font-size:12px;margin-top:7px}.state-count{display:block;margin-bottom:7px}.plaza-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:16px 22px}.plaza-pagination>span{font-size:13px;color:#8a9099}.empty{text-align:center!important;color:#8a9099;padding:45px!important}@media(max-width:700px){.plaza-filters label{width:calc(50% - 6px)}}
</style>
