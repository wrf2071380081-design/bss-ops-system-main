<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { ApiError } from "../../../domain/common";
import { useSessionStore } from "../../../stores/session";
import type { ResourceRow } from "../../../api/resources.types";
const props = defineProps<{ resource: ResourceRow }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const session = useSessionStore(), reason = ref(""), due = ref(""), error = ref(""), checking = ref(false), checked = ref(false), pending = ref(false), busy = ref(false), conflict = ref(false);
const lifecycle = crypto.randomUUID();
let active = true, controller: AbortController | undefined;
const local = new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 16); due.value = local;
async function check() {
  controller?.abort(); const request = controller = new AbortController(); checking.value = true; checked.value = false; error.value = "";
  try {
    const results = await Promise.all(["PENDING", "EXECUTING"].flatMap(status => ["ONLINE", "OFFLINE"].map(kind => supplyWorkbenchApi.plans({ resource_id: props.resource.id, region: props.resource.region, status, kind, page: 1, size: 1 }, request.signal))));
    if (active && !request.signal.aborted) { pending.value = results.some(result => result.total > 0); checked.value = true; }
  } catch (value) { if (active && !request.signal.aborted) error.value = value instanceof Error ? value.message : "无法检查已有计划。"; }
  finally { if (active && !request.signal.aborted) checking.value = false; }
}
async function save() {
  if (busy.value || !checked.value || pending.value || conflict.value) return;
  busy.value = true; error.value = "";
  try { await supplyWorkbenchApi.createPlan(props.resource, reason.value, due.value, lifecycle); if (active) emit("saved"); }
  catch (value) { if (active) { conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus); error.value = value instanceof Error ? value.message : "保存计划失败。"; } }
  finally { if (active) busy.value = false; }
}
onMounted(check); onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <el-dialog :model-value="true" :title="resource.state === 'OFFLINE' ? '创建上架计划' : '创建下架计划'" width="min(620px, 94vw)" class="create-state-plan" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <p>{{ resource.supplier_name }} / {{ resource.model_name }} · {{ resourceRegionLabels[resource.region] }} B 端</p>
    <el-alert v-if="pending" title="该资源已有待执行或执行中的上下架计划，请先查看原计划。" type="warning" :closable="false" />
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="原因（必填）"><el-input v-model="reason" type="textarea" :rows="3" aria-label="计划原因" :disabled="busy" /></el-form-item>
      <el-form-item :label="resource.state === 'OFFLINE' ? '预计上架时间' : '预计下架时间'"><input v-model="due" class="plan-due" type="datetime-local" aria-label="预计时间" :disabled="busy" /></el-form-item>
      <el-form-item label="执行人"><el-input :model-value="session.user?.name || '当前登录人员'" disabled aria-label="计划执行人" /></el-form-item>
    </el-form>
    <p class="muted">到点只提醒。实际在对应平台操作并由计划发起人登记完成后，资源状态才会变化。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button v-if="!checked" text @click="check">重新检查已有计划</el-button></el-alert>
    <el-alert v-if="conflict" title="资源权限、版本或待执行变更已变化，输入已保留。请关闭后刷新资源，重新核对。" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">关闭创建计划</el-button><el-button type="primary" :loading="busy || checking" :disabled="!checked || pending || conflict" @click="save">保存计划</el-button></template>
  </el-dialog>
</template>
<style scoped>p{line-height:1.8;overflow-wrap:anywhere}.el-form{margin-top:20px}.plan-due{width:100%;padding:8px 12px;border:1px solid #dcdfe6;border-radius:4px;color:#303133;font:inherit;box-sizing:border-box}.muted{color:#8a9099;font-size:13px}.el-alert+.el-alert{margin-top:12px}</style>
