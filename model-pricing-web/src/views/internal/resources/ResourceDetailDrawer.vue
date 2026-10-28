<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { resourcesApi } from "../../../api/resources";
import { resourceBlockLabels, resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import { formatStandardPrice, isOfficialURL, modelTypeLabels } from "../../../api/standardModels.contract";
import { formatDateTime } from "../../../domain/date";
import type { ResourceRow } from "../../../api/resources.types";
import ResourceNoticesPanel from "./ResourceNoticesPanel.vue";
import ResourcePlansPanel from "./ResourcePlansPanel.vue";
import CreateStatePlanDialog from "./CreateStatePlanDialog.vue";
import CreatePriceChangePlanDialog from "./CreatePriceChangePlanDialog.vue";
import ResourceOperationsPanel from "./ResourceOperationsPanel.vue";
import ResourceOperationRecords from "./ResourceOperationRecords.vue";
import StandardModelMaintainDialog from "../models/StandardModelMaintainDialog.vue";
import ResourceReportsPanel from "./ResourceReportsPanel.vue";
import { canCreateStatePlan } from "../../../api/planCreate.contract";
const props = defineProps<{ id: string; region: string; presentation?: "drawer" | "page"; expectedSupplierID?: string; expectedModelID?: string; initialPlanID?: string; initialTab?: "profile" | "report" | "reports" | "operations" | "notices" | "plans" | "records" }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const containerComponent = computed(() => props.presentation === "page" ? "section" : "el-drawer");
const containerProps = computed(() => props.presentation === "page" ? { class: "resource-detail-page" } : { modelValue: true, title: "供应资源详情", size: "min(760px, 96vw)" });
const data = ref<ResourceRow>(), loading = ref(false), error = ref(""), tab = ref(props.initialTab ?? "profile");
const createOpen = ref(false), pricePlanOpen = ref(false), planGeneration = ref(0);
const modelOpen = ref(false);
function planCreated() { createOpen.value = false; pricePlanOpen.value = false; planGeneration.value++; tab.value = "plans"; }
async function resourceUpdated() { await load(); emit("changed"); if (data.value?.ever_admitted) tab.value = "profile"; }
let active = true, controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController(); loading.value = true; error.value = "";
  try {
    const result = await resourcesApi.get(props.id, props.region, request.signal);
    if ((props.expectedSupplierID && result.supplier_id !== props.expectedSupplierID) || (props.expectedModelID && result.standard_model_id !== props.expectedModelID)) throw new Error("资源与当前供应商或模型不匹配，请返回来源列表后重试。");
    if (active && !request.signal.aborted) data.value = result;
  }
  catch (value) { if (active && !request.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "资源详情暂时无法加载，请重试。"; } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
onMounted(load); onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <component :is="containerComponent" v-bind="containerProps" @close="emit('close')">
    <el-button v-if="presentation === 'page'" text @click="emit('close')">← 返回供应商模型详情</el-button>
    <div v-loading="loading" class="resource-detail">
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载资源详情</el-button></el-alert>
      <template v-if="data"><h2>{{ data.model_name || '待补充模型名称' }}</h2><p class="muted">{{ data.supplier_name }} · {{ resourceRegionLabels[data.region] }} · {{ modelTypeLabels[data.model_type] || '类型待补齐' }}</p><div class="status-line"><el-tag :type="data.state === 'ADMITTED' ? 'success' : 'info'">{{ resourceStateLabels[data.state] }}</el-tag><strong>{{ data.quote_readiness.eligible ? '可用于报价' : '暂不可用于报价' }}</strong></div>
        <el-alert v-if="!data.quote_readiness.eligible" type="warning" title="当前阻断项" :closable="false"><ul><li v-for="reason in data.quote_readiness.blocked_reasons" :key="reason">{{ resourceBlockLabels[reason] || reason }}</li></ul></el-alert>
        <el-tabs v-model="tab"><el-tab-pane label="资源资料" name="profile"><dl><dt>官方 ID</dt><dd>{{ data.official_id || '待补齐' }}</dd><dt>供应商接口模型 ID</dt><dd>{{ data.supplier_model_id }}</dd><dt>采购系数</dt><dd class="coefficient">{{ data.coefficient }}</dd><dt>归属采购人</dt><dd>{{ data.buyer_id }}</dd><dt>正式配置</dt><dd>{{ data.formal_config_id ? `配置 ${data.formal_config_id} · 引用版本 ${data.formal_config_revision}` : '尚未关联' }}</dd><dt>资源修订号</dt><dd>{{ data.revision }}</dd><dt>最近更新</dt><dd>{{ formatDateTime(data.updated_at) }}</dd><dt>官方价格快照</dt><dd v-if="!data.official_price_items.length">价格尚未补齐</dd><dd v-for="price in data.official_price_items" :key="price.item_key"><strong>{{ formatStandardPrice(price) }}</strong><p>{{ price.condition }}</p><a v-if="price.source_url && isOfficialURL(price.source_url)" :href="price.source_url" target="_blank" rel="noopener noreferrer">官方价格来源 ↗</a></dd></dl></el-tab-pane>
          <el-tab-pane label="当前报告摘要" name="report"><el-alert v-if="data.test.status === 'STALE'" title="测试目标已变化；原报告不作为当前测试与报价依据。" type="warning" :closable="false" /><dl><dt>报告</dt><dd>{{ data.test.report_id || '未测试' }}</dd><dt>报告依据</dt><dd>{{ data.test.status === 'NOT_TESTED' ? '未测试' : data.test.status === 'STALE' ? '旧目标报告' : data.test.valid ? '当前有效' : '已失效' }}</dd><dt>连通 / 压力</dt><dd>{{ data.test.status === 'COMPLETED' ? `${data.test.connectivity_passed ? '连通通过' : '连通未通过'} / ${data.test.pressure_passed ? '压力通过' : '压力未通过'}` : data.test.status === 'STALE' ? '目标已变化' : '未测试' }}</dd><dt>连通耗时 · ms</dt><dd>{{ resourceMetricDisplay(data.test.metrics.connection_ms, data.test.status) }}</dd><dt>压力 RPS</dt><dd>{{ resourceMetricDisplay(data.test.metrics.rps, data.test.status) }}</dd><dt>压力 TPS</dt><dd>{{ resourceMetricDisplay(data.test.metrics.tps, data.test.status) }}</dd><dt>输入缓存命中率</dt><dd class="cache-percent">{{ resourceCacheDisplay(data.test.cache_stats, data.test.status) }}</dd><template v-if="data.test.status === 'COMPLETED' && data.test.cache_stats.status === 'PROVIDED'"><dt>命中输入 Token / 总输入 Token</dt><dd>{{ data.test.cache_stats.hit_tokens }} / {{ data.test.cache_stats.input_tokens }}</dd><dt>测试场景</dt><dd>{{ data.test.cache_stats.scene || '未提供' }}</dd></template></dl><p class="muted">缓存统计只表示本次报告口径，不作为新增测试通过阈值。</p></el-tab-pane>
          <el-tab-pane label="报告历史" name="reports" lazy><ResourceReportsPanel :key="data.id" :resource-i-d="data.id" :current-report-i-d="data.test.report_id" /></el-tab-pane>
          <el-tab-pane :label="data.ever_admitted ? '配置与改价' : '接入与配置'" name="operations" lazy><ResourceOperationsPanel :key="data.id" :resource="data" @updated="resourceUpdated" /></el-tab-pane>
          <el-tab-pane label="公告" name="notices" lazy><ResourceNoticesPanel :resource-i-d="data.id" :region="data.region" /></el-tab-pane>
          <el-tab-pane label="计划" name="plans" lazy><ResourcePlansPanel :key="planGeneration" :resource-i-d="data.id" :region="data.region" :initial-plan-i-d="initialPlanID" /></el-tab-pane>
          <el-tab-pane label="操作记录" name="records" lazy><ResourceOperationRecords :resource-i-d="data.id" /></el-tab-pane>
        </el-tabs>
      </template>
    </div>
    <CreateStatePlanDialog v-if="createOpen && data" :resource="data" @close="createOpen = false" @saved="planCreated" />
    <CreatePriceChangePlanDialog v-if="pricePlanOpen && data" :resource="data" @close="pricePlanOpen = false" @saved="planCreated" />
    <StandardModelMaintainDialog v-if="modelOpen && data" :id="data.standard_model_id" @close="modelOpen = false" @saved="resourceUpdated" />
    <template v-if="presentation !== 'page'" #footer><el-button @click="emit('close')">关闭资源详情</el-button><el-button v-if="data" :disabled="loading" @click="modelOpen = true">查看标准模型资料</el-button><el-button v-if="data && canCreateStatePlan(data)" :disabled="loading" @click="pricePlanOpen = true">创建内部改价计划</el-button><el-button v-if="data && canCreateStatePlan(data)" :disabled="loading" type="primary" @click="createOpen = true">{{ data.state === 'OFFLINE' ? '创建上架计划' : '创建下架计划' }}</el-button></template>
    <div v-if="presentation === 'page'" class="resource-detail-page-actions"><el-button v-if="data" :disabled="loading" @click="modelOpen = true">查看标准模型资料</el-button><el-button v-if="data && canCreateStatePlan(data)" :disabled="loading" @click="pricePlanOpen = true">创建内部改价计划</el-button><el-button v-if="data && canCreateStatePlan(data)" :disabled="loading" type="primary" @click="createOpen = true">{{ data.state === 'OFFLINE' ? '创建上架计划' : '创建下架计划' }}</el-button></div>
  </component>
</template>
<style scoped>
.resource-detail{min-height:220px;color:#1c1d1f;--el-color-primary:#315ee8}h2{font-size:23px;margin:0 0 10px}.muted{color:#8a9099;font-size:13px;line-height:1.8}.status-line{display:flex;align-items:center;gap:12px;margin:18px 0}dl{display:grid;grid-template-columns:140px minmax(0,1fr);gap:16px 20px;padding:18px 0}dt{color:#8a9099}dd{margin:0;overflow-wrap:anywhere}dd p{font-size:13px;margin:7px 0}.coefficient,.cache-percent{font-weight:600;color:#315ee8}a{color:#315ee8}@media(max-width:500px){dl{grid-template-columns:1fr;gap:8px}dd{padding-bottom:14px}}
.resource-detail-page{max-width:1180px;margin:0 auto;background:#fff;border:1px solid #e6e8ed;border-radius:8px;padding:22px 26px;--el-color-primary:#315ee8}.resource-detail-page-actions{display:flex;justify-content:flex-end;gap:10px;border-top:1px solid #e6e8ed;padding-top:16px;margin-top:20px}
</style>
