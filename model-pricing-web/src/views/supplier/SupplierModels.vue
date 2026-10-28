<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { supplierModelsApi } from "../../api/supplierModels";
import type { SupplierModelPageDTO, SupplierModelResourceDTO } from "../../api/supplierModels.types";
import { resourceBlockLabels, resourceStateLabels } from "../../api/resources.contract";
import { ApiError } from "../../domain/common";

/**
 * 供应商门户「模型管理」——按 PM 原型 `vPortalModels` 对齐（2026-10-28）。
 *
 * 原型形态：一条「统计时间」筛选 + 7 列表（供方模型名称 / 平台标准名称 / 采购系数 /
 * 调用量 / 错误数 / 状态 / 资源变更），不分页、无关键字搜索；状态列把内部的
 * 「待测试」说成「待接入 + 测试待完成」，不向供应商暴露内部测试状态词。
 *
 * 与原型的两处有意偏差：
 *   1. 调用量 / 错误数没有真实统计源（原型里是造出来的演示数字），这里恒显示「—」，
 *      表头写明「统计接入后开启」，不编数字；
 *   2. 保留「查看详情」抽屉（挂在供方模型名称下的小字），供应商需要看到连通/压力结论。
 */
const router = useRouter();
const query = reactive({ statsFrom: "", statsTo: "" });
const data = ref<SupplierModelPageDTO>();
const selected = ref<SupplierModelResourceDTO>();
const loading = ref(false), error = ref("");
const detailOpen = ref(false);
let request: AbortController | undefined;
let active = true;
const message = (value: unknown) => value instanceof ApiError || value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。";
const regionLabel = (region: string) => region === "CN" ? "国内" : region === "OVERSEAS" ? "海外" : region;
const testLabel = (tested: boolean, passed: boolean) => !tested ? "未测" : passed ? "通过" : "未通过";

/** 原型话术：待测试 → 「待接入」+ 小字「测试待完成」。 */
function stateLabel(state: string) { return state === "PENDING_TEST" ? "待接入" : resourceStateLabels[state] || state; }
function stateHint(state: string) { return state === "PENDING_TEST" ? "测试待完成" : ""; }
function stateType(state: string): "success" | "warning" | "info" {
  if (state === "ADMITTED") return "success";
  if (state === "OFFLINE") return "info";
  return "warning";
}

async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const result = await supplierModelsApi.list({ page: 1, size: 100 }, current.signal);
    if (active && !current.signal.aborted) data.value = result;
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
onMounted(() => { void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / MODELS</div>
    <h1>模型管理</h1>
    <p>本企业已申报的模型与接入状态；价格变更会创建独立申报。</p>
  </section>

  <section class="panel supplier-model-panel">
    <div class="stats-bar">
      <span class="stats-label">统计时间</span>
      <el-date-picker v-model="query.statsFrom" type="date" size="small" placeholder="开始日期" value-format="YYYY-MM-DD" disabled />
      <span class="stats-sep">至</span>
      <el-date-picker v-model="query.statsTo" type="date" size="small" placeholder="结束日期" value-format="YYYY-MM-DD" disabled />
      <span class="stats-note">状态为当前状态；调用统计接入后开启（当前显示 —）</span>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button text @click="load">重新加载</el-button>
    </el-alert>

    <el-table v-loading="loading" :data="data?.list || []" row-key="id" empty-text="暂无模型记录">
      <el-table-column label="供方模型名称" min-width="200">
        <template #default="{ row }">
          <strong>{{ row.supplier_model_id || row.model_name }}</strong>
          <div><el-button link type="primary" size="small" @click="selected = row; detailOpen = true">查看详情</el-button></div>
        </template>
      </el-table-column>
      <el-table-column label="平台标准名称" min-width="200">
        <template #default="{ row }">
          <span>{{ row.model_name }}</span>
          <div class="muted">{{ row.official_id }} · {{ regionLabel(row.region) }}</div>
        </template>
      </el-table-column>
      <el-table-column label="采购系数" width="120"><template #default="{ row }"><strong>{{ row.coefficient }}</strong></template></el-table-column>
      <el-table-column label="调用量" width="110" align="right">
        <template #header><span class="col-head">调用量<em>统计接入后开启</em></span></template>
        <template #default><span class="muted">—</span></template>
      </el-table-column>
      <el-table-column label="错误数" width="110" align="right">
        <template #header><span class="col-head">错误数<em>统计接入后开启</em></span></template>
        <template #default><span class="muted">—</span></template>
      </el-table-column>
      <el-table-column label="状态" width="140">
        <template #default="{ row }">
          <el-tag :type="stateType(row.state)">{{ stateLabel(row.state) }}</el-tag>
          <div v-if="stateHint(row.state)" class="muted">{{ stateHint(row.state) }}</div>
        </template>
      </el-table-column>
      <el-table-column label="资源变更" width="130" align="center">
        <template #default="{ row }"><el-button type="primary" plain size="small" @click="priceChange(row)">发起变更</el-button></template>
      </el-table-column>
    </el-table>
  </section>

  <el-drawer v-model="detailOpen" title="模型详情" size="min(720px, 94vw)">
    <template v-if="selected">
      <div class="detail-title">
        <div><h2>{{ selected.model_name }}</h2><span class="muted">{{ selected.official_id }} · {{ selected.supplier_model_id }}</span></div>
        <el-tag :type="stateType(selected.state)">{{ stateLabel(selected.state) }}</el-tag>
      </div>
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
.stats-bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 16px; }
.stats-label, .stats-sep, .stats-note { color: var(--app-text-muted); font-size: 12px; }
.stats-note { margin-left: 6px; }
.col-head em { display: block; font-style: normal; font-size: 10px; color: var(--app-text-muted); font-weight: 400; }
.muted { margin-top: 4px; color: var(--app-text-muted); font-size: 12px; }
.detail-title { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
.detail-title h2 { margin: 0 0 6px; font-size: 20px; }
.detail-alert { margin-top: 18px; }
.detail-alert ul { margin: 8px 0 0; padding-left: 18px; }
.safety-note { margin-top: 18px; }
</style>
