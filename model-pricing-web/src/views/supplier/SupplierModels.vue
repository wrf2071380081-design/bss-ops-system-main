<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { supplierModelsApi } from "../../api/supplierModels";
import type { SupplierModelPageDTO, SupplierModelResourceDTO } from "../../api/supplierModels.types";
import { resourceBlockLabels, resourceStateLabels } from "../../api/resources.contract";
import { ApiError } from "../../domain/common";

const router = useRouter();
const query = reactive({ page: 1, size: 20, keyword: "", region: "" as "" | "CN" | "OVERSEAS", state: "" });
const data = ref<SupplierModelPageDTO>();
const selected = ref<SupplierModelResourceDTO>();
const loading = ref(false), error = ref("");
const detailOpen = ref(false);
let request: AbortController | undefined;
let active = true;
const message = (value: unknown) => value instanceof ApiError || value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。";
const regionLabel = (region: string) => region === "CN" ? "国内" : region === "OVERSEAS" ? "海外" : region;
const testLabel = (tested: boolean, passed: boolean) => !tested ? "未测" : passed ? "通过" : "未通过";

async function load(reset = false) {
  if (reset) query.page = 1;
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const result = await supplierModelsApi.list({ ...query }, current.signal);
    if (active && !current.signal.aborted) data.value = result;
  } catch (value) {
    if (active && !current.signal.aborted) { data.value = undefined; error.value = message(value); }
  } finally {
    if (active && !current.signal.aborted) loading.value = false;
  }
}
function reset() { Object.assign(query, { page: 1, size: 20, keyword: "", region: "", state: "" }); void load(); }
function priceChange(row: SupplierModelResourceDTO) {
  void router.push({ path: "/supplier/submissions/new", query: { kind: "PRICE_CHANGE", resource_id: row.id, expected_revision: row.revision, model_name: row.model_name, coefficient: row.coefficient } });
}
onMounted(() => { void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / MODELS</div>
    <h1>模型管理</h1>
    <p>查看本企业模型、测试状态和接入情况；价格变更将创建独立申报。</p>
  </section>

  <section class="panel supplier-model-panel">
    <form class="filters" @submit.prevent="load(true)">
      <el-input v-model="query.keyword" clearable placeholder="搜索模型名称 / 官方 ID / 接口模型 ID" aria-label="搜索模型" />
      <el-select v-model="query.region" clearable placeholder="全部部署节点" aria-label="部署节点">
        <el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" />
      </el-select>
      <el-select v-model="query.state" clearable placeholder="全部接入状态" aria-label="接入状态">
        <el-option v-for="(label, value) in resourceStateLabels" :key="value" :label="label" :value="value" />
      </el-select>
      <el-button type="primary" native-type="submit">查询</el-button>
      <el-button @click="reset">重置</el-button>
    </form>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load()">重新加载</el-button></el-alert>
    <el-table v-loading="loading" :data="data?.list || []" row-key="id" empty-text="暂无模型记录">
      <el-table-column label="标准模型" min-width="220"><template #default="{ row }"><strong>{{ row.model_name }}</strong><div class="muted">{{ row.official_id }}</div></template></el-table-column>
      <el-table-column prop="supplier_model_id" label="接口模型 ID" min-width="170" />
      <el-table-column label="部署 / 采购系数" width="150"><template #default="{ row }">{{ regionLabel(row.region) }}<div class="muted">{{ row.coefficient }}</div></template></el-table-column>
      <el-table-column label="测试" min-width="180"><template #default="{ row }">连通 {{ testLabel(row.test.connectivity_tested, row.test.connectivity_passed) }}<div class="muted">压力 {{ testLabel(row.test.stress_tested, row.test.stress_passed) }}</div></template></el-table-column>
      <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag>{{ resourceStateLabels[row.state] || row.state }}</el-tag><div class="muted">{{ row.quote_readiness.eligible ? '可供报价' : '暂不可报价' }}</div></template></el-table-column>
      <el-table-column label="操作" width="170"><template #default="{ row }"><el-button link type="primary" @click="selected = row; detailOpen = true">查看详情</el-button><el-button link type="primary" @click="priceChange(row)">价格变更</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="data" class="pagination" :current-page="query.page" :page-size="query.size" :total="data.total" layout="total, prev, pager, next" @current-change="(page: number) => { query.page = page; void load(); }" />
  </section>

  <el-drawer v-model="detailOpen" title="模型详情" size="min(720px, 94vw)">
    <template v-if="selected">
      <div class="detail-title"><div><h2>{{ selected.model_name }}</h2><span class="muted">{{ selected.official_id }} · {{ selected.supplier_model_id }}</span></div><el-tag>{{ resourceStateLabels[selected.state] || selected.state }}</el-tag></div>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="部署节点">{{ regionLabel(selected.region) }}</el-descriptions-item>
        <el-descriptions-item label="采购系数">{{ selected.coefficient }}</el-descriptions-item>
        <el-descriptions-item label="连通性测试">{{ testLabel(selected.test.connectivity_tested, selected.test.connectivity_passed) }}</el-descriptions-item>
        <el-descriptions-item label="压力测试">{{ testLabel(selected.test.stress_tested, selected.test.stress_passed) }}</el-descriptions-item>
        <el-descriptions-item label="报告有效性">{{ selected.test.valid ? '有效' : '未验证或已失效' }}</el-descriptions-item>
        <el-descriptions-item label="连接耗时">{{ selected.test.connection_ms || '—' }} ms</el-descriptions-item>
        <el-descriptions-item label="RPS">{{ selected.test.rps || '—' }}</el-descriptions-item>
        <el-descriptions-item label="TPS">{{ selected.test.tps || '—' }}</el-descriptions-item>
        <el-descriptions-item label="报价状态">{{ selected.quote_readiness.eligible ? '可供报价' : '暂不可报价' }}</el-descriptions-item>
      </el-descriptions>
      <el-alert v-if="selected.quote_readiness.blocked_reasons.length" class="detail-alert" title="暂不可供报价" type="warning" :closable="false">
        <ul><li v-for="reason in selected.quote_readiness.blocked_reasons" :key="reason">{{ resourceBlockLabels[reason] || reason }}</li></ul>
      </el-alert>
      <p class="muted safety-note">仅显示当前供应商自身模型资料；测试凭据不会在页面返回或展示。</p>
    </template>
  </el-drawer>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-model-panel { padding: 20px; }
.filters { display: grid; grid-template-columns: minmax(240px, 1fr) 160px 170px auto auto; gap: 10px; margin-bottom: 18px; }
.muted { margin-top: 4px; color: var(--app-text-muted); font-size: 12px; }
.pagination { justify-content: flex-end; margin-top: 18px; }
.detail-title { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
.detail-title h2 { margin: 0 0 6px; font-size: 20px; }
.detail-alert { margin-top: 18px; }
.detail-alert ul { margin: 8px 0 0; padding-left: 18px; }
.safety-note { margin-top: 18px; }
@media(max-width: 800px) { .filters { grid-template-columns: 1fr 1fr; } }
</style>
