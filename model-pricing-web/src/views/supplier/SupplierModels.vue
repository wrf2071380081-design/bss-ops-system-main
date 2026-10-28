<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { supplierModelsApi } from "../../api/supplierModels";
import type { SupplierModelPageDTO, SupplierModelResourceDTO } from "../../api/supplierModels.types";
import { resourceBlockLabels } from "../../api/resources.contract";
import { modelTypeLabels } from "../../api/standardModels.contract";
import { ApiError } from "../../domain/common";
import SupplierFilterSelect from "./SupplierFilterSelect.vue";

/**
 * 供应商门户「模型管理」——按原型 `vPortalModels` 的最新定义对齐（2026-10-28 第二版）。
 *
 * 原型形态（不是旧版那套「统计时间 + 调用量/错误数」）：
 *   · 卡片标题「模型管理」，没有页面大标题；
 *   · 筛选：模型类型 / 模型公司 / 接入状态（多选勾选框）+ 搜索 + 重置，勾选即生效；
 *   · 一行系数口径说明；
 *   · 7 列：对应标准模型 / 接口模型 ID / 模型公司 / 模型类型 / 当前供货报价系数 /
 *     接入状态 / 操作，6 个数据列全部可排序（前端排序）；
 *   · 操作只有一个「价格变更」；`待测试` 显示成「待接入」；已下架整行置灰；
 *   · 无分页、无统计时间、无调用量/错误数。
 *
 * 可见行由后端按原型 sPortalResourceRows() 规则收敛（曾接入 / 已接入 / 已下架 /
 * 有过通过申报），所以这页只显示「进过流程」的模型；空态给出去申报页的指引。
 *
 * 唯一保留的偏差（D2 已确认）：点「对应标准模型」名字可以打开详情抽屉，
 * 看连通/压力结论与暂不可报价的原因——原型这一屏没有详情入口。
 */
const router = useRouter();
const data = ref<SupplierModelPageDTO>();
const selected = ref<SupplierModelResourceDTO>();
const loading = ref(false), error = ref("");
const detailOpen = ref(false);
const filters = ref<{ type: string[]; company: string[]; state: string[] }>({ type: [], company: [], state: [] });
const keyword = ref("");
let request: AbortController | undefined;
let active = true;
const message = (value: unknown) => value instanceof ApiError || value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。";

const rows = computed(() => data.value?.list || []);
const regionLabel = (region: string) => region === "CN" ? "国内" : region === "OVERSEAS" ? "海外" : region;
const testLabel = (tested: boolean, passed: boolean) => !tested ? "未测" : passed ? "通过" : "未通过";

/** 供应商视角的接入状态只有三档（原型固定三项）；待测试与待接入合并为「待接入」。 */
const accessStatusLabels: Record<string, string> = { PENDING_ACCESS: "待接入", ADMITTED: "已接入", OFFLINE: "已下架" };
const defaultAccessStatuses = ["PENDING_ACCESS", "ADMITTED", "OFFLINE"];
/** 模型类型固定四项（原型），不随该主体实际拥有的类型增减。 */
const defaultModelTypes = ["TEXT", "IMAGE", "VIDEO", "AUDIO"];

/** 原型话术：待测试 → 待接入（不向供应商暴露"测试中"这种内部状态）。 */
function stateLabel(state: string) { return accessStatusLabels[accessStatus(state)] || state; }
/** 内部资源状态折叠成供应商可见的三档（与后端 SupplierAccessStatus 同口径）。 */
function accessStatus(state: string) {
  if (state === "ADMITTED") return "ADMITTED";
  if (state === "OFFLINE") return "OFFLINE";
  // PENDING_TEST / PENDING_ACCESS 都算「待接入」。
  return "PENDING_ACCESS";
}
function stateType(state: string): "success" | "warning" | "info" {
  if (state === "ADMITTED") return "success";
  if (state === "OFFLINE") return "info";
  return "warning";
}
/** 系数按原型去尾零展示：0.6000 → 0.6，1.0000 → 1。 */
function coefficientText(value: string) {
  const amount = Number(value);
  return Number.isFinite(amount) ? String(amount) : value;
}

// 三个筛选项的候选值来自后端 filter-options：
//   · 模型类型 / 接入状态是**固定枚举**（原型就是写死的四项 / 三项）；
//   · 模型公司来自该主体名下的实际资源（开放集合，枚举不出来）。
// 都不从当前列表推导——否则列表一空，下拉就没有选项、变成点不动的灰控件。
const options = ref<{ companies: string[]; model_types: string[]; states: string[] }>({ companies: [], model_types: [], states: [] });
const typeOptions = computed(() => (options.value.model_types.length ? options.value.model_types : defaultModelTypes)
  .map(value => ({ value, label: modelTypeLabels[value] || value })));
const companyOptions = computed(() => options.value.companies.map(value => ({ value, label: value })));
const stateOptions = computed(() => (options.value.states.length ? options.value.states : defaultAccessStatuses)
  .map(value => ({ value, label: accessStatusLabels[value] || value })));

const filtered = computed(() => {
  const text = keyword.value.trim().toLowerCase();
  return rows.value.filter(row => {
    if (filters.value.type.length && !filters.value.type.includes(row.model_type)) return false;
    if (filters.value.company.length && !filters.value.company.includes(row.company)) return false;
    if (filters.value.state.length && !filters.value.state.includes(accessStatus(row.state))) return false;
    if (!text) return true;
    return `${row.supplier_model_id} ${row.model_name}`.toLowerCase().includes(text);
  });
});
const hasFilter = computed(() => Boolean(filters.value.type.length || filters.value.company.length || filters.value.state.length || keyword.value.trim()));

function resetFilters() { filters.value = { type: [], company: [], state: [] }; keyword.value = ""; }

async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const [result, filterOptions] = await Promise.all([
      supplierModelsApi.list({ page: 1, size: 100 }, current.signal),
      // 候选值失败不影响列表：退化成"没有候选项"，但控件仍然可点。
      supplierModelsApi.filterOptions(current.signal).catch(() => ({ companies: [], model_types: [], states: [] })),
    ]);
    if (!active || current.signal.aborted) return;
    data.value = result;
    options.value = filterOptions;
  } catch (value) {
    if (active && !current.signal.aborted) { data.value = undefined; error.value = message(value); }
  } finally { if (active && !current.signal.aborted) loading.value = false; }
}
function priceChange(row: SupplierModelResourceDTO) {
  void router.push({
    path: "/supplier/submissions/new",
    query: {
      kind: "PRICE_CHANGE", resource_id: row.id, expected_revision: row.revision,
      model_name: row.model_name, coefficient: row.coefficient,
      standard_model_id: row.standard_model_id, supplier_model_id: row.supplier_model_id,
    },
  });
}
function openDetail(row: SupplierModelResourceDTO) { selected.value = row; detailOpen.value = true; }
function rowClass({ row }: { row: SupplierModelResourceDTO }) { return row.state === "OFFLINE" ? "row-muted" : ""; }
onMounted(() => { void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="panel supplier-model-panel" v-loading="loading">
    <h2 class="card-title">模型管理</h2>

    <div class="filters">
      <SupplierFilterSelect v-model="filters.type" label="模型类型" :options="typeOptions" />
      <SupplierFilterSelect v-model="filters.company" label="模型公司" :options="companyOptions" />
      <SupplierFilterSelect v-model="filters.state" label="接入状态" :options="stateOptions" />
      <el-input v-model="keyword" class="search" placeholder="搜索模型名称 / 接口模型 ID" clearable />
      <el-button @click="resetFilters">重置</el-button>
    </div>

    <p class="sub note">当前供货报价系数沿用平台既有系数及计价口径；待审核或待执行的申报价不替代当前生效价格。</p>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button text @click="load">重新加载</el-button>
    </el-alert>

    <el-table :data="filtered" row-key="id" :row-class-name="rowClass" class="model-table">
      <el-table-column label="对应标准模型" min-width="180" sortable prop="model_name">
        <template #default="{ row }">
          <el-button link type="primary" @click="openDetail(row)">{{ row.model_name }}</el-button>
        </template>
      </el-table-column>
      <el-table-column label="接口模型 ID" min-width="190" sortable prop="supplier_model_id" />
      <el-table-column label="模型公司" min-width="140" sortable prop="company" />
      <el-table-column label="模型类型" width="110" sortable prop="model_type" :formatter="(row: SupplierModelResourceDTO) => modelTypeLabels[row.model_type] || row.model_type" />
      <el-table-column label="当前供货报价系数" width="170" align="right" sortable :sort-method="(a: SupplierModelResourceDTO, b: SupplierModelResourceDTO) => Number(a.coefficient) - Number(b.coefficient)">
        <template #default="{ row }">{{ coefficientText(row.coefficient) }}</template>
      </el-table-column>
      <el-table-column label="接入状态" width="130" sortable :sort-method="(a: SupplierModelResourceDTO, b: SupplierModelResourceDTO) => stateLabel(a.state).localeCompare(stateLabel(b.state), 'zh-CN')">
        <template #default="{ row }"><el-tag :type="stateType(row.state)">{{ stateLabel(row.state) }}</el-tag></template>
      </el-table-column>
      <el-table-column label="操作" width="130" align="center">
        <template #default="{ row }"><el-button size="small" @click="priceChange(row)">价格变更</el-button></template>
      </el-table-column>

      <template #empty>
        <div v-if="hasFilter" class="empty-state">当前筛选暂无数据</div>
        <div v-else-if="data?.hidden_total" class="empty-state">
          <p>暂无已接入的模型。另有 {{ data.hidden_total }} 个模型正在接入流程中，可在申报页查看办理进度。</p>
          <el-button type="primary" plain size="small" @click="router.push('/supplier/home')">去申报页</el-button>
        </div>
        <div v-else class="empty-state">
          <p>还没有申报过模型，先发起一次模型申报吧。</p>
          <el-button type="primary" plain size="small" @click="router.push('/supplier/home')">去申报页</el-button>
        </div>
      </template>
    </el-table>
  </section>

  <el-drawer v-model="detailOpen" title="模型详情" size="min(720px, 94vw)">
    <template v-if="selected">
      <div class="detail-title">
        <div><h3>{{ selected.model_name }}</h3><span class="muted">{{ selected.official_id }} · {{ selected.supplier_model_id }} · {{ regionLabel(selected.region) }}</span></div>
        <el-tag :type="stateType(selected.state)">{{ stateLabel(selected.state) }}</el-tag>
      </div>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="模型公司">{{ selected.company || '—' }}</el-descriptions-item>
        <el-descriptions-item label="模型类型">{{ modelTypeLabels[selected.model_type] || selected.model_type }}</el-descriptions-item>
        <el-descriptions-item label="当前供货报价系数">{{ coefficientText(selected.coefficient) }}</el-descriptions-item>
        <el-descriptions-item label="报价状态">{{ selected.quote_readiness.eligible ? '可供报价' : '暂不可报价' }}</el-descriptions-item>
        <el-descriptions-item label="连通性测试">{{ testLabel(selected.test.connectivity_tested, selected.test.connectivity_passed) }}</el-descriptions-item>
        <el-descriptions-item label="压力测试">{{ testLabel(selected.test.stress_tested, selected.test.stress_passed) }}</el-descriptions-item>
        <el-descriptions-item label="连接耗时">{{ selected.test.connection_ms || '—' }} ms</el-descriptions-item>
        <el-descriptions-item label="RPS / TPS">{{ selected.test.rps || '—' }} / {{ selected.test.tps || '—' }}</el-descriptions-item>
      </el-descriptions>
      <el-alert v-if="selected.quote_readiness.blocked_reasons.length" class="detail-alert" title="暂不可供报价" type="warning" :closable="false">
        <ul><li v-for="reason in selected.quote_readiness.blocked_reasons" :key="reason">{{ resourceBlockLabels[reason] || reason }}</li></ul>
      </el-alert>
      <p class="muted safety-note">仅显示当前供应商自身模型资料；测试凭据不会在页面返回或展示。</p>
    </template>
  </el-drawer>
</template>

<style scoped>
.supplier-model-panel { padding: 20px; }
.card-title { margin: 0 0 16px; font-size: 17px; font-weight: 500; }
.filters { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 12px; }
.search { width: 300px; }
.sub { color: var(--app-text-muted); font-size: 12px; }
.note { margin: 0 0 16px; }
.model-table :deep(.row-muted) { color: var(--app-text-muted); }
.model-table :deep(.row-muted .el-button) { color: var(--app-text-muted); }
.model-table :deep(.row-muted .el-tag) { opacity: .7; }
.empty-state { padding: 24px 0; color: var(--app-text-muted); font-size: 13px; }
.empty-state p { margin: 0 0 10px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.detail-title { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
.detail-title h3 { margin: 0 0 6px; font-size: 19px; font-weight: 500; }
.detail-alert { margin-top: 18px; }
.detail-alert ul { margin: 8px 0 0; padding-left: 18px; }
.safety-note { margin-top: 18px; }
</style>
