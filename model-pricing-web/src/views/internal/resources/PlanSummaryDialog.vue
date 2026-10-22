<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { assertPlanContext } from "../../../api/sourceOperations.contract";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { planKindLabels, planStatusLabels } from "../../../api/supplyWorkbench.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";
import type { PlanMaintenanceAction } from "../../../api/planMaintenance.contract";
import MaintainStatePlanDialog from "./MaintainStatePlanDialog.vue";
import ExecuteStatePlanDialog from "./ExecuteStatePlanDialog.vue";
const props = defineProps<{ id: string; resourceID?: string; region?: string }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const router = useRouter();
async function openResource() {
  if (!data.value) return;
  await router.push({ path: `/internal/model-plaza/${data.value.resource_id}`, query: { region: data.value.region, tab: "profile" } });
  emit("close");
}
const action = ref<PlanMaintenanceAction>();
const executing = ref(false);
const blockedLabels: Record<string, string> = { RESOURCE_CHANGED: "资源版本已变化", RESOURCE_NOT_READY: "资源当前不满足执行条件" };
const data = ref<PlanSummary>(), loading = ref(false), error = ref("");
const now = ref(Date.now());
const clock = window.setInterval(() => { now.value = Date.now(); }, 30000);
const isDue = computed(() => data.value?.status === "PENDING" && Date.parse(data.value.due_at) <= now.value);
let controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  data.value = undefined; loading.value = true; error.value = "";
  try { const result = await supplyWorkbenchApi.plan(props.id, request.signal); if (!request.signal.aborted) data.value = assertPlanContext(result, props.resourceID, props.region); }
  catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "计划摘要暂时无法加载。"; }
  finally { if (!request.signal.aborted) loading.value = false; }
}
watch(() => [props.id, props.resourceID, props.region], load, { immediate: true });
onBeforeUnmount(() => { controller?.abort(); window.clearInterval(clock); });
async function maintained() { action.value = undefined; executing.value = false; emit("changed"); await load(); }
async function formClosed() { action.value = undefined; executing.value = false; emit("changed"); await load(); }
</script>
<template>
  <el-dialog :model-value="true" title="资源计划详情" width="min(640px, 94vw)" class="supply-plan-dialog" append-to-body @close="emit('close')">
    <div v-loading="loading" class="plan-summary">
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载计划详情</el-button></el-alert>
      <template v-if="data"><header><h2>{{ planKindLabels[data.kind] }}计划 · {{ resourceRegionLabels[data.region] }}</h2><el-tag>{{ planStatusLabels[data.status] }}</el-tag></header>
        <el-alert v-if="isDue" title="预计执行时间已到，请原发起人核对实际操作；到期不会自动上下架。" type="warning" :closable="false" />
        <el-alert v-if="!data.precondition_matches" title="资源已变更，计划执行前需重新核对。" type="warning" :closable="false" />
        <el-alert v-if="data.status === 'BLOCKED'" type="warning" title="计划已阻断" :closable="false"><p v-for="reason in data.blocked_reasons" :key="reason">{{ blockedLabels[reason] || reason }}</p></el-alert>
        <dl><dt>关联资源</dt><dd>{{ data.resource_id }}</dd><dt>原因</dt><dd>{{ data.reason }}</dd><dt>发起人</dt><dd>{{ data.creator_name || `人员 ${data.created_by}` }}{{ data.is_initiator ? '（本人）' : '' }}</dd><dt>发起时间</dt><dd>{{ formatDateTime(data.created_at) }}</dd><dt>预计时间</dt><dd>{{ formatDateTime(data.due_at) }}</dd><dt>实际完成时间</dt><dd>{{ data.executed_at ? formatDateTime(data.executed_at) : '尚未登记完成' }}</dd><dt>资源版本</dt><dd>计划依据 {{ data.expected_resource_revision }} · 当前 {{ data.current_resource_revision }}</dd></dl>
        <p class="muted">{{ data.permissions.can_execute ? '执行登记须由计划发起人核对实际操作与资源状态。' : '实际完成登记仅对计划发起人开放，且计划必须处于可执行状态；预计时间只作提醒。' }}</p>
      </template>
    </div>
    <template #footer><el-button @click="emit('close')">关闭计划详情</el-button><el-button v-if="data" @click="openResource">查看关联资源</el-button><el-button v-if="data?.permissions.can_revise" @click="action = 'revise'">修改计划</el-button><el-button v-if="data?.permissions.can_cancel" @click="action = 'cancel'">取消计划</el-button><el-button v-if="data?.permissions.can_execute" type="primary" @click="executing = true">登记实际完成</el-button></template>
  </el-dialog>
  <MaintainStatePlanDialog v-if="action && data" :key="`${data.id}:${action}`" :plan="data" :action="action" @close="formClosed" @saved="maintained" />
  <ExecuteStatePlanDialog v-if="executing && data" :key="`${data.id}:${data.revision}`" :plan="data" @close="formClosed" @saved="maintained" />
</template>
<style scoped>
.plan-summary{min-height:180px;color:#1c1d1f}header{display:flex;justify-content:space-between;align-items:center;gap:16px;margin-bottom:20px}h2{font-size:19px;margin:0}dl{display:grid;grid-template-columns:110px minmax(0,1fr);gap:16px;margin:24px 0}dt,.muted{color:#8a9099}dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}.muted{font-size:13px;line-height:1.8}@media(max-width:500px){dl{grid-template-columns:1fr;gap:8px}dd{padding-bottom:8px}}
</style>
