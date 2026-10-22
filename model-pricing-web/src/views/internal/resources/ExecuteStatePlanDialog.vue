<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { ElMessage } from "element-plus";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { validatePlanExecutionReceipt } from "../../../api/planExecution.contract";
import { planKindLabels } from "../../../api/supplyWorkbench.contract";
import { formatDateTime } from "../../../domain/date";
import { ApiError } from "../../../domain/common";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";

const props = defineProps<{ plan: PlanSummary }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const confirmed = ref(false), completionNote = ref(""), reason = ref("");
const busy = ref(false), error = ref(""), conflict = ref(false), blocked = ref(false);
const lifecycle = crypto.randomUUID();
let active = true;
async function save() {
  if (busy.value || conflict.value) return;
  busy.value = true; error.value = "";
  try {
    await supplyWorkbenchApi.executePlan(props.plan, confirmed.value, completionNote.value, reason.value, lifecycle);
    if (active) { ElMessage.success("实际完成已登记"); emit("saved"); }
  } catch (value) {
    if (!active) return;
    conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus);
    error.value = value instanceof Error ? value.message : "登记失败，请重试。";
    if (value instanceof ApiError && value.httpStatus === 409 && value.details) {
      try { validatePlanExecutionReceipt(value.details, props.plan, true); blocked.value = true; }
      catch { /* Only an exact plan/revision receipt proves the BLOCKED outcome. */ }
    }
  } finally { if (active) busy.value = false; }
}
onBeforeUnmount(() => { active = false; });
</script>

<template>
  <el-dialog :model-value="true" title="登记计划实际完成" width="min(640px, 94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <p>{{ planKindLabels[plan.kind] }}计划 · 资源 {{ plan.resource_id }} · 预计 {{ formatDateTime(plan.due_at) }}</p>
    <p>本次登记依据资源版本 {{ plan.expected_resource_revision }}；当前版本 {{ plan.current_resource_revision }}。</p>
    <el-alert v-if="!plan.precondition_matches" title="资源版本已经变化，请核对外部操作；登记可能被阻断。" type="warning" :closable="false" />
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="实际完成依据（必填）"><el-input v-model="completionNote" type="textarea" :rows="3" placeholder="填写外部操作结果、工单号或其他可追溯依据" :disabled="busy || conflict" /></el-form-item>
      <el-form-item label="登记原因（必填）"><el-input v-model="reason" type="textarea" :rows="2" :disabled="busy || conflict" /></el-form-item>
      <el-checkbox v-model="confirmed" :disabled="busy || conflict">已在外部平台完成本计划对应的操作，并核对上述依据</el-checkbox>
    </el-form>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="conflict" :title="blocked ? '本次登记已阻断。输入已保留，请返回详情查看原因和最新状态。' : '计划状态或权限已变化。输入已保留，请返回详情重新核对。'" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">返回计划详情</el-button><el-button type="primary" :loading="busy" :disabled="conflict || !plan.permissions.can_execute" @click="save">确认登记实际完成</el-button></template>
  </el-dialog>
</template>

<style scoped>p{line-height:1.8;color:#526970}.el-form{margin:18px 0}.el-alert{margin-top:14px}.el-checkbox{white-space:normal;height:auto}</style>
