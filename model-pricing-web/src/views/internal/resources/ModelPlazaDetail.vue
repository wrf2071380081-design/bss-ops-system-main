<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceRegionLabels } from "../../../api/resources.contract";
import { standardModelsApi } from "../../../api/standardModels";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import type { ResourceRow } from "../../../api/resources.types";
import type { StandardModelDTO } from "../../../api/standardModels.types";
import { formatDateTime } from "../../../domain/date";

const route = useRoute();
const router = useRouter();
const modelID = computed(() => String(route.params.id || ""));
const model = ref<StandardModelDTO>();
const rows = ref<ResourceRow[]>([]);
const loading = ref(false);
const error = ref("");
const activeTab = ref("sources");
const detailVisible = ref(false);
const selected = ref<ResourceRow>();
const filters = reactive({ keyword: "", state: "", coefficientMin: "", coefficientMax: "", connectionMax: "", rpsMin: "", tpsMin: "" });
const sortKey = ref("supplier_name");
const sortOrder = ref<"asc" | "desc">("asc");

const regionLabel = (region: string) => resourceRegionLabels[region] || region;
const stateLabel = (state: string) => state === "ADMITTED" ? "已接入" : state === "PENDING_ACCESS" ? "待接入" : state === "OFFLINE" ? "已下架" : state === "PENDING_TEST" ? "待测试" : state;
const stateType = (state: string) => state === "ADMITTED" ? "success" : state === "PENDING_ACCESS" ? "warning" : "info";
const numberOr = (value?: string | null) => value == null || value === "" ? null : Number(value);
const metric = (value?: string | null) => value == null || value === "" ? "—" : value;
const cacheText = (row: ResourceRow) => row.test.cache_stats.status === "PROVIDED" ? `${row.test.cache_stats.hit_percent || "—"}%` : "未提供";
const formatTime = (value?: string | null) => value ? formatDateTime(value, "未记录", false) : "未记录";

const filteredRows = computed(() => rows.value.filter(row => {
  const keyword = filters.keyword.trim().toLowerCase();
  if (keyword && !`${row.supplier_name} ${row.supplier_model_id}`.toLowerCase().includes(keyword)) return false;
  if (filters.state && row.state !== filters.state) return false;
  const coefficient = Number(row.coefficient);
  if (filters.coefficientMin && coefficient < Number(filters.coefficientMin)) return false;
  if (filters.coefficientMax && coefficient > Number(filters.coefficientMax)) return false;
  const connection = numberOr(row.test.metrics.connection_ms);
  if (filters.connectionMax && connection != null && connection > Number(filters.connectionMax)) return false;
  const rps = numberOr(row.test.metrics.rps);
  if (filters.rpsMin && rps != null && rps < Number(filters.rpsMin)) return false;
  const tps = numberOr(row.test.metrics.tps);
  if (filters.tpsMin && tps != null && tps < Number(filters.tpsMin)) return false;
  return true;
}));

function sortBy(key: string) {
  if (sortKey.value === key) sortOrder.value = sortOrder.value === "asc" ? "desc" : "asc";
  else { sortKey.value = key; sortOrder.value = "asc"; }
}
function arrow(key: string) { return sortKey.value === key ? (sortOrder.value === "asc" ? "↑" : "↓") : "↕"; }
const sortValue = (row: ResourceRow, key: string): string | number => {
  switch (key) {
    case "region": return row.region;
    case "coefficient": return Number(row.coefficient);
    case "connection": return numberOr(row.test.metrics.connection_ms) ?? Number.POSITIVE_INFINITY;
    case "rps": return numberOr(row.test.metrics.rps) ?? Number.POSITIVE_INFINITY;
    case "tps": return numberOr(row.test.metrics.tps) ?? Number.POSITIVE_INFINITY;
    case "created_at": return row.created_at ? Date.parse(row.created_at) : 0;
    case "state": return row.state;
    default: return row.supplier_name;
  }
};
const sortedRows = computed(() => {
  const list = [...filteredRows.value];
  const direction = sortOrder.value === "asc" ? 1 : -1;
  list.sort((a, b) => {
    const x = sortValue(a, sortKey.value), y = sortValue(b, sortKey.value);
    if (typeof x === "number" && typeof y === "number") {
      if (x === Number.POSITIVE_INFINITY && y !== Number.POSITIVE_INFINITY) return 1;
      if (y === Number.POSITIVE_INFINITY && x !== Number.POSITIVE_INFINITY) return -1;
      return (x - y) * direction;
    }
    const xs = String(x), ys = String(y);
    if (!xs && ys) return 1;
    if (!ys && xs) return -1;
    return xs.localeCompare(ys) * direction;
  });
  return list;
});

async function load() {
  if (!modelID.value) return;
  loading.value = true;
  error.value = "";
  try {
    const base = buildResourceQuery({ keyword: "", company: "", model_type: "", supplier_id: "", region: "", state: "" });
    const [modelPage, list] = await Promise.all([
      standardModelsApi.list({ page: 1, size: 100 }),
      resourcesApi.allRows({ ...base, standard_model_id: modelID.value, sort_by: "supplier_name", order: "asc" }),
    ]);
    model.value = modelPage.list.find(item => item.id === modelID.value);
    if (!model.value) throw new Error("模型不存在或当前不可见。");
    rows.value = list;
  } catch (value) {
    error.value = value instanceof Error ? value.message : "模型详情加载失败，请重试。";
  } finally {
    loading.value = false;
  }
}

function resetFilters() {
  Object.assign(filters, { keyword: "", state: "", coefficientMin: "", coefficientMax: "", connectionMax: "", rpsMin: "", tpsMin: "" });
}
function openDetail(row: ResourceRow) { selected.value = row; detailVisible.value = true; }
function report() { ElMessage.info("测试报告列表接口待接入，当前展示最新指标。"); }
function operation() { ElMessage.info("下架/登记接入由资源状态接口接入后开放。"); }
function back() { void router.push("/internal/model-plaza"); }

onMounted(load);
</script>

<template>
  <section class="model-detail">
    <header class="detail-heading">
      <el-button text @click="back">← 返回模型广场</el-button>
      <div class="eyebrow">TOKEN OPS / MODELS</div>
      <h1>{{ model?.name || "模型详情" }}</h1>
      <p v-if="model">{{ model.company }} · {{ modelTypeLabels[model.model_type] || model.model_type }} · <code>{{ model.official_id }}</code></p>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

    <section class="panel detail-panel" v-loading="loading">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="供应来源" name="sources">
          <div class="source-filters">
            <el-input v-model="filters.keyword" placeholder="供应商名称 / 接口模型 ID" clearable style="width: 220px" />
            <el-select v-model="filters.state" placeholder="资源状态" clearable style="width: 130px">
              <el-option value="ADMITTED" label="已接入" />
              <el-option value="PENDING_ACCESS" label="待接入" />
              <el-option value="PENDING_TEST" label="待测试" />
              <el-option value="OFFLINE" label="已下架" />
            </el-select>
            <el-input v-model="filters.coefficientMin" placeholder="系数下限" style="width: 100px" />
            <el-input v-model="filters.coefficientMax" placeholder="系数上限" style="width: 100px" />
            <el-input v-model="filters.connectionMax" placeholder="连通 ≤ ms" style="width: 110px" />
            <el-input v-model="filters.rpsMin" placeholder="RPS ≥" style="width: 90px" />
            <el-input v-model="filters.tpsMin" placeholder="TPS ≥" style="width: 90px" />
            <el-button @click="resetFilters">重置</el-button>
          </div>

          <el-table :data="sortedRows" style="width: 100%">
            <el-table-column label="供应商 / 接口模型" min-width="170">
              <template #header><button type="button" class="sort-head" @click="sortBy('supplier_name')">供应商名称 {{ arrow('supplier_name') }}</button></template>
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
              <template #default="{ row }">{{ row.coefficient }}</template>
            </el-table-column>
            <el-table-column label="连通耗时" width="100">
              <template #header><button type="button" class="sort-head" @click="sortBy('connection')">连通耗时 {{ arrow('connection') }}</button></template>
              <template #default="{ row }">{{ metric(row.test.metrics.connection_ms) }} ms</template>
            </el-table-column>
            <el-table-column label="压力 RPS" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('rps')">压力 RPS {{ arrow('rps') }}</button></template>
              <template #default="{ row }">{{ metric(row.test.metrics.rps) }}</template>
            </el-table-column>
            <el-table-column label="压力 TPS" width="100">
              <template #header><button type="button" class="sort-head" @click="sortBy('tps')">压力 TPS {{ arrow('tps') }}</button></template>
              <template #default="{ row }">{{ metric(row.test.metrics.tps) }}</template>
            </el-table-column>
            <el-table-column label="输入缓存命中率" width="130">
              <template #default="{ row }">{{ cacheText(row) }}</template>
            </el-table-column>
            <el-table-column label="上架时间" width="170">
              <template #header><button type="button" class="sort-head" @click="sortBy('created_at')">上架时间 {{ arrow('created_at') }}</button></template>
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #header><button type="button" class="sort-head" @click="sortBy('state')">状态 {{ arrow('state') }}</button></template>
              <template #default="{ row }"><el-tag :type="stateType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag></template>
            </el-table-column>
            <el-table-column label="测试报告" width="100">
              <template #default><el-button size="small" text type="primary" @click="report">查看报告</el-button></template>
            </el-table-column>
            <el-table-column label="资源操作" width="110">
              <template #default="{ row }">
                <el-button size="small" text type="primary" @click="operation">{{ row.state === "PENDING_ACCESS" ? "登记接入" : "下架" }}</el-button>
              </template>
            </el-table-column>
            <el-table-column label="详情" width="80">
              <template #default="{ row }"><el-button size="small" @click="openDetail(row)">详情</el-button></template>
            </el-table-column>
            <template #empty><el-empty description="暂无供应来源" :image-size="64" /></template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="操作记录" name="records">
          <el-empty description="操作记录将在资源操作接口接入后展示" :image-size="72" />
        </el-tab-pane>
      </el-tabs>
    </section>

    <el-dialog v-model="detailVisible" title="供应来源详情" width="560px">
      <el-descriptions v-if="selected" :column="1" border>
        <el-descriptions-item label="供应商">{{ selected.supplier_name }} · {{ regionLabel(selected.region) }}</el-descriptions-item>
        <el-descriptions-item label="接口模型 ID">{{ selected.supplier_model_id }}</el-descriptions-item>
        <el-descriptions-item label="采购系数">{{ selected.coefficient }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ stateLabel(selected.state) }}</el-descriptions-item>
        <el-descriptions-item label="连通 / RPS / TPS">{{ metric(selected.test.metrics.connection_ms) }} ms / {{ metric(selected.test.metrics.rps) }} / {{ metric(selected.test.metrics.tps) }}</el-descriptions-item>
        <el-descriptions-item label="缓存命中率">{{ cacheText(selected) }}</el-descriptions-item>
        <el-descriptions-item label="测试报告">{{
          selected.test.report_id || "—"
        }}</el-descriptions-item>
        <el-descriptions-item label="创建 / 更新">{{ formatTime(selected.created_at) }} / {{ formatTime(selected.updated_at) }}</el-descriptions-item>
      </el-descriptions>
      <template #footer><el-button @click="detailVisible = false">关闭</el-button></template>
    </el-dialog>
  </section>
</template>

<style scoped>
.model-detail { color: var(--app-text-primary); }
.detail-heading { margin-bottom: 18px; }
.detail-heading .eyebrow { margin-top: 8px; color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.detail-heading h1 { margin: 8px 0 6px; font-size: 30px; letter-spacing: -.02em; }
.detail-heading p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.detail-heading code { color: var(--app-text-secondary); }
.detail-panel { padding: 18px 20px; }
.source-filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.sort-head { border: 0; background: none; padding: 0; color: inherit; font: inherit; cursor: pointer; }
</style>
