<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { resourcesApi } from "../../../api/resources";
import { resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels } from "../../../api/resources.contract";
import { standardModelsApi } from "../../../api/standardModels";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import type { ResourceRow } from "../../../api/resources.types";
import type { StandardModelDTO } from "../../../api/standardModels.types";
import { formatDateTime } from "../../../domain/date";
import { decodeSourceFilters, emptySourceFilters, encodeSourceFilters, validateSourceFilters, sourceResourceQuery } from "../../../domain/modelPlaza";
import ResourceDetailDrawer from "./ResourceDetailDrawer.vue";
import ResourceStateRegisterDialog from "./ResourceStateRegisterDialog.vue";
import ModelSourceOperationRecords from "./ModelSourceOperationRecords.vue";
import ResourceReportsPanel from "./ResourceReportsPanel.vue";
import SourceFilterBar from "./SourceFilterBar.vue";
import { useResourceOptions } from "./useResourceOptions";

const route = useRoute(), router = useRouter();
const modelID = computed(() => String(route.params.id || ""));
const model = ref<StandardModelDTO>(), rows = ref<ResourceRow[]>([]);
const loading = ref(false), error = ref(""), filterError = ref("");
const activeTab = ref("sources"), detailVisible = ref(false), reportsVisible = ref(false);
const selected = ref<ResourceRow>();
const selectedTab = ref<"profile" | "reports" | "operations" | "plans" | "records">("profile");
const filters = ref(emptySourceFilters()), applied = ref(emptySourceFilters());
const { choices, optionError, loadOptions } = useResourceOptions(filters);
// 原型在 Tab 下直接写明查询范围：让"这页只看了一部分供应商"这件事可感知。
const scope = computed(() => ({
  supplier: applied.value.supplier_ids.length
    ? applied.value.supplier_ids.map(id => choices.value.suppliers.find(supplier => supplier.id === id)?.name || `供应商 ${id}`).join("、")
    : "全部",
  region: applied.value.regions.length ? applied.value.regions.map(key => regionLabel(key)).join("、") : "全部",
}));
const page = ref(1), size = ref(20), total = ref(0);
const sortKey = ref("supplier_name"), sortOrder = ref<"asc" | "desc">("asc");
const sourceSortKeys = ["supplier_name", "region", "coefficient", "connection", "rps", "tps", "admitted_at", "state"];
const plazaReturnKeys = ["filters", "keyword", "company", "model_type", "supplier_id", "supplier_name", "supplier_region", "region", "state", "page", "size", "sort_by", "order"];
let active = true, controller: AbortController | undefined;
function routeText(value: unknown) { return typeof value === "string" ? value : ""; }
function returnContext() {
  return Object.fromEntries(plazaReturnKeys.flatMap(key => { const value = routeText(route.query[`return_${key}`]); return value ? [[`return_${key}`, value]] : []; }));
}
function sourceContext() {
  return { source_filters: encodeSourceFilters(applied.value), source_sort_by: sortKey.value, source_order: sortOrder.value, source_page: String(page.value), source_size: String(size.value) };
}
const regionLabel = (region: string) => resourceRegionLabels[region] || region;
const stateLabel = (state: string) => state === "ADMITTED" ? "已接入" : state === "PENDING_ACCESS" ? "待接入" : state === "OFFLINE" ? "已下架" : state === "PENDING_TEST" ? "待测试" : state;
const stateType = (state: string) => state === "ADMITTED" ? "success" : state === "PENDING_ACCESS" ? "warning" : "info";
// 指标空态按原型口径展示：没有记录=未记录，只跑出一部分=部分结果。
const metric = (row: ResourceRow, key: "connection_ms" | "rps" | "tps") => {
  const text = resourceMetricDisplay(row.test.metrics[key], row.test.status);
  return text === "未测试" ? "未记录" : text === "未提供" ? "部分结果" : text;
};
const cacheText = (row: ResourceRow) => {
  const text = resourceCacheDisplay(row.test.cache_stats, row.test.status);
  return text === "未测试" ? "未记录" : text === "未提供" ? "部分结果" : text;
};
// 系数按原型保留有效位（0.7200 → 0.72），不丢精度。
const coefficientText = (raw: string) => raw.includes(".") ? raw.replace(/0+$/, "").replace(/\.$/, "") : raw;
const formatTime = (value?: string | null) => value ? formatDateTime(value, "未记录", false) : "未记录";
function applyFilters() {
  try { validateSourceFilters(filters.value); applied.value = JSON.parse(encodeSourceFilters(filters.value)); filterError.value = ""; page.value = 1; void load(); }
  catch (value) { filterError.value = value instanceof Error ? value.message : "筛选参数无效。"; }
}
function resetFilters() { filters.value = emptySourceFilters(); applyFilters(); }
function sortBy(key: string) { sortOrder.value = sortKey.value === key && sortOrder.value === "asc" ? "desc" : "asc"; sortKey.value = key; page.value = 1; void load(); }
function arrow(key: string) { return sortKey.value === key ? sortOrder.value === "asc" ? "↑" : "↓" : "↕"; }
const sortedRows = computed(() => rows.value);
async function load() {
  if (!modelID.value) return;
  controller?.abort(); const request = controller = new AbortController();
  const id = modelID.value; loading.value = true; error.value = ""; model.value = undefined; rows.value = []; selectedRows.value = [];
  try {
    const [detail, list] = await Promise.all([
      standardModelsApi.get(id, request.signal),
      resourcesApi.rows(sourceResourceQuery(applied.value, page.value, size.value, sortKey.value === "connection" ? "connection_ms" : sortKey.value, sortOrder.value, id), request.signal),
    ]);
    if (active && !request.signal.aborted) {
      model.value = detail; rows.value = list.list; total.value = list.total;
      const last = Math.max(1, Math.ceil(list.total / size.value)); if (page.value > last) { page.value = last; void load(); }
    }
  } catch (value) { if (active && !request.signal.aborted) { total.value = 0; error.value = value instanceof Error ? value.message : "模型详情加载失败，请重试。"; } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function openDetail(row: ResourceRow, tab: typeof selectedTab.value = "profile") { selected.value = row; selectedTab.value = tab; detailVisible.value = true; }
function openReports(row: ResourceRow) { selected.value = row; reportsVisible.value = true; }
// 行内与批量「下架 / 上架」都走同一张登记单：与原型一致，一次点击即登记完成。
const selectedRows = ref<ResourceRow[]>([]);
const registerVisible = ref(false), registerKind = ref<"down" | "up">("down"), registerRows = ref<ResourceRow[]>([]);
const canRegister = (row: ResourceRow) => row.ever_admitted && ["ADMITTED", "OFFLINE"].includes(row.state);
function openRegister(row: ResourceRow) {
  registerKind.value = row.state === "OFFLINE" ? "up" : "down";
  registerRows.value = [row];
  registerVisible.value = true;
}
function batchRegister(kind: "down" | "up") {
  // 原型的三种反馈原样保留：未选、跨供应商、正常进登记。
  if (!selectedRows.value.length) { ElMessage.warning("请选择不重复的精确供应资源"); return; }
  if (new Set(selectedRows.value.map(row => row.supplier_id)).size > 1) { ElMessage.warning("请按同一供应商分别登记，不能跨供应商批量操作"); return; }
  const targets = selectedRows.value.filter(row => kind === "down" ? row.state !== "OFFLINE" : row.state === "OFFLINE");
  if (!targets.length) { ElMessage.warning(kind === "down" ? "所选资源均不在架，无需下架" : "所选资源均不是已下架状态，无需上架"); return; }
  registerKind.value = kind; registerRows.value = targets; registerVisible.value = true;
}
function closeRegister() { registerVisible.value = false; registerRows.value = []; }
function registerDone() { registerVisible.value = false; registerRows.value = []; selectedRows.value = []; void load(); }
function openSupplierModel(row: ResourceRow) {
  void router.push({ path: `/internal/suppliers/${row.supplier_id}/models`, query: { ...returnContext(), ...sourceContext(), resource_id: row.id, model_id: row.standard_model_id, region: row.region, from_model_id: modelID.value } });
}
function closeResourceDetail() { detailVisible.value = false; selected.value = undefined; }
function back() { void router.push({ path: "/internal/model-plaza", query: Object.fromEntries(plazaReturnKeys.flatMap(key => { const value = routeText(route.query[`return_${key}`]); return value ? [[key, value]] : []; })) }); }
function restore() {
  try {
    filters.value = decodeSourceFilters(route.query.source_filters);
    if (!route.query.source_filters) {
      filters.value.keyword = routeText(route.query.source_keyword);
      const state = routeText(route.query.source_state); if (state) filters.value.states = [state];
      for (const key of ["coefficient_min", "coefficient_max", "connection_max", "rps_min", "tps_min"] as const) filters.value[key] = routeText(route.query[`source_${key}`]);
    }
    validateSourceFilters(filters.value); applied.value = JSON.parse(encodeSourceFilters(filters.value));
  } catch { filters.value = emptySourceFilters(); applied.value = emptySourceFilters(); filterError.value = "筛选参数无效，已恢复默认筛选。"; }
  sortKey.value = sourceSortKeys.includes(routeText(route.query.source_sort_by)) ? routeText(route.query.source_sort_by) : "coefficient";
  sortOrder.value = route.query.source_order === "desc" ? "desc" : "asc";
  const restoredPage = Number(routeText(route.query.source_page)), restoredSize = Number(routeText(route.query.source_size));
  page.value = Number.isSafeInteger(restoredPage) && restoredPage > 0 ? restoredPage : 1;
  size.value = Number.isSafeInteger(restoredSize) && restoredSize > 0 && restoredSize <= 100 ? restoredSize : 20;
}
onMounted(() => { restore(); void load(); void loadOptions(); });
watch(modelID, () => { detailVisible.value = false; reportsVisible.value = false; restore(); void load(); void loadOptions(); });
onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>

<template>
  <section class="model-detail">
    <header class="detail-heading">
      <el-button text @click="back">← 返回模型广场</el-button>
      <div class="eyebrow">TOKEN OPS / MODELS</div>
      <h1>{{ model?.name || "模型详情" }}</h1>
      <p v-if="model">{{ model.company }} · {{ modelTypeLabels[model.model_type] || model.model_type }} · <code>{{ model.official_id }}</code></p>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载模型详情</el-button></el-alert>

    <section class="panel detail-panel" v-loading="loading">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="供应来源" name="sources">
          <p class="query-scope">当前查询范围：供应商 {{ scope.supplier }}；部署节点 {{ scope.region }}</p>
          <SourceFilterBar v-model="filters" class="source-filters" :rows="rows" :options="choices" @search="loadOptions" metrics @apply="applyFilters" @reset="resetFilters" />
          <el-alert v-if="optionError" :title="optionError" type="warning" :closable="false" />
          <p v-if="choices.has_more" class="muted">还有更多选项，请输入名称缩小搜索范围。</p>
          <el-alert v-if="filterError" :title="filterError" type="warning" :closable="false" />
          <div v-if="rows.length" class="batch-actions">
            <el-button @click="batchRegister('up')">上架</el-button>
            <el-button @click="batchRegister('down')">下架</el-button>
            <span v-if="selectedRows.length" class="muted">已选 {{ selectedRows.length }} 条</span>
          </div>
          <el-table :data="sortedRows" row-key="id" style="width: 100%" @selection-change="(rows: ResourceRow[]) => selectedRows = rows">
            <el-table-column type="selection" width="46" />
            <el-table-column label="供应商 / 接口模型" min-width="170">
              <template #header><button type="button" class="sort-head" @click="sortBy('supplier_name')">供应商 {{ arrow('supplier_name') }}</button></template>
              <template #default="{ row }">
                <strong>{{ row.supplier_name }}</strong>
                <div class="muted">{{ row.supplier_model_id }}</div>
              </template>
            </el-table-column>
            <el-table-column label="部署节点" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('region')">部署节点 {{ arrow('region') }}</button></template>
              <template #default="{ row }">{{ regionLabel(row.region) }}</template>
            </el-table-column>
            <el-table-column label="采购系数" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('coefficient')">采购系数 {{ arrow('coefficient') }}</button></template>
              <template #default="{ row }">{{ coefficientText(row.coefficient) }}</template>
            </el-table-column>
            <el-table-column label="连通耗时（ms）" width="132">
              <template #header><button type="button" class="sort-head" @click="sortBy('connection')">连通耗时（ms） {{ arrow('connection') }}</button></template>
              <template #default="{ row }">{{ metric(row, 'connection_ms') }}<span v-if="row.test.status === 'COMPLETED'" class="muted metric-result">{{ row.test.connectivity_passed ? '通过' : '不通过' }}</span></template>
            </el-table-column>
            <el-table-column label="RPS" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('rps')">RPS {{ arrow('rps') }}</button></template>
              <template #default="{ row }">{{ metric(row, 'rps') }}</template>
            </el-table-column>
            <el-table-column label="TPS" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('tps')">TPS {{ arrow('tps') }}</button></template>
              <template #default="{ row }">{{ metric(row, 'tps') }}</template>
            </el-table-column>
            <el-table-column label="输入缓存命中率" width="130">
              <template #default="{ row }">{{ cacheText(row) }}</template>
            </el-table-column>
            <el-table-column label="上架时间" width="170">
              <template #header><button type="button" class="sort-head" @click="sortBy('admitted_at')">上架时间 {{ arrow('admitted_at') }}</button></template>
              <template #default="{ row }">{{ formatTime(row.admitted_at) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('state')">状态 {{ arrow('state') }}</button></template>
              <template #default="{ row }"><el-tag :type="stateType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag></template>
            </el-table-column>
            <el-table-column label="测试与报告" width="110">
              <template #default="{ row }"><el-button size="small" text type="primary" @click="openReports(row)">测试与报告</el-button></template>
            </el-table-column>
            <el-table-column label="资源操作" width="140">
              <template #default="{ row }">
                <el-button size="small" text type="primary" @click="openDetail(row)">详情</el-button>
                <el-button v-if="canRegister(row)" size="small" text type="primary" @click="openRegister(row)">{{ row.state === "OFFLINE" ? "申请上架" : "下架" }}</el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="暂无供应来源" :image-size="64" /></template>
          </el-table>
          <el-pagination v-if="total > size" v-model:current-page="page" :page-size="size" :total="total" layout="prev, pager, next" @current-change="load" />
          <p class="source-count">共 {{ total }} 条供应来源</p>
        </el-tab-pane>

        <el-tab-pane label="操作记录" name="records">
          <ModelSourceOperationRecords :model-i-d="modelID" />
        </el-tab-pane>
      </el-tabs>
    </section>

    <el-dialog v-if="reportsVisible && selected" v-model="reportsVisible" :title="`${model?.name || '模型'} · 资源 #${selected?.id || ''}`" width="min(1120px, 96vw)" append-to-body><p class="muted">{{ selected.supplier_name }} · 每次测试单独留档</p><ResourceReportsPanel :resource-i-d="selected.id" :current-report-i-d="selected.test.report_id" /><template #footer><el-button @click="reportsVisible = false">关闭测试列表</el-button></template></el-dialog>
    <ResourceStateRegisterDialog v-if="registerVisible && registerRows.length" :key="`${registerKind}:${registerRows.map(row => row.id).join(',')}`" :rows="registerRows" :kind="registerKind" @close="closeRegister" @done="registerDone" />
    <ResourceDetailDrawer v-if="detailVisible && selected" :key="`${selected.id}:${selectedTab}`" :id="selected.id" :region="selected.region" :expected-supplier-i-d="selected.supplier_id" :expected-model-i-d="selected.standard_model_id" :initial-tab="selectedTab" @close="closeResourceDetail" @changed="load()" />
  </section>
</template>

<style scoped>
.model-detail { color: var(--app-text-primary); }
.detail-heading { margin-bottom: 18px; }
.detail-heading .eyebrow { margin-top: 8px; color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.detail-heading h1 { margin: 8px 0 6px; font-size: 25px; letter-spacing: -.02em; }
.detail-heading p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.detail-heading code { color: var(--app-text-secondary); }
.detail-panel { padding: 18px 20px; }
.source-filters { margin-bottom: 12px; }
.batch-actions{display:flex;align-items:center;gap:10px;margin:0 0 12px}
.sources-title{font-size:17px;margin:10px 0 0}.query-scope{margin:14px 0 0;font-size:13px;color:var(--app-text-secondary)}.source-count{font-size:12px;color:var(--app-text-muted)}.metric-result{display:block;margin-top:4px}
.muted { color: var(--app-text-muted); font-size: 12px; }
.sort-head { border: 0; background: none; padding: 0; color: inherit; font: inherit; cursor: pointer; }
</style>
