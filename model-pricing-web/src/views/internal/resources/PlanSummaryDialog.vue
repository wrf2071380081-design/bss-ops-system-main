<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { planKindLabels, planStatusLabels } from "../../../api/supplyWorkbench.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";
const props = defineProps<{ id: string }>();
const emit = defineEmits<{ close: [] }>();
const data = ref<PlanSummary>(), loading = ref(false), error = ref("");
let controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  data.value = undefined; loading.value = true; error.value = "";
  try { const result = await supplyWorkbenchApi.plan(props.id, request.signal); if (!request.signal.aborted) data.value = result; }
  catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "计划摘要暂时无法加载。"; }
  finally { if (!request.signal.aborted) loading.value = false; }
}
watch(() => props.id, load, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <el-dialog :model-value="true" title="资源计划详情" width="min(640px, 94vw)" class="supply-plan-dialog" @close="emit('close')">
    <div v-loading="loading" class="plan-summary">
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载计划详情</el-button></el-alert>
      <template v-if="data"><header><h2>{{ planKindLabels[data.kind] }}计划 · {{ resourceRegionLabels[data.region] }}</h2><el-tag>{{ planStatusLabels[data.status] }}</el-tag></header>
        <el-alert v-if="!data.precondition_matches" title="资源已变更，计划执行前需重新核对。" type="warning" :closable="false" />
        <dl><dt>关联资源</dt><dd>{{ data.resource_id }}</dd><dt>原因</dt><dd>{{ data.reason }}</dd><dt>发起人</dt><dd>{{ data.creator_name || `人员 ${data.created_by}` }}{{ data.is_initiator ? '（本人）' : '' }}</dd><dt>发起时间</dt><dd>{{ formatDateTime(data.created_at) }}</dd><dt>预计时间</dt><dd>{{ formatDateTime(data.due_at) }}</dd><dt>实际完成时间</dt><dd>{{ data.executed_at ? formatDateTime(data.executed_at) : '尚未登记完成' }}</dd><dt>资源版本</dt><dd>计划依据 {{ data.expected_resource_revision }} · 当前 {{ data.current_resource_revision }}</dd></dl>
        <p class="muted">当前为只读摘要。执行登记须由计划发起人在执行入口核对实际操作与资源状态。</p>
      </template>
    </div>
    <template #footer><el-button @click="emit('close')">关闭计划详情</el-button></template>
  </el-dialog>
</template>
<style scoped>
.plan-summary{min-height:180px;color:#20313c}header{display:flex;justify-content:space-between;align-items:center;gap:16px;margin-bottom:20px}h2{font-size:19px;margin:0}dl{display:grid;grid-template-columns:110px minmax(0,1fr);gap:16px;margin:24px 0}dt,.muted{color:#79868f}dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}.muted{font-size:13px;line-height:1.8}@media(max-width:500px){dl{grid-template-columns:1fr;gap:8px}dd{padding-bottom:8px}}
</style>
