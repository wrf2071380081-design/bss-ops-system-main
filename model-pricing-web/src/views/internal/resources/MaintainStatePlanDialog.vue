<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { ApiError } from "../../../domain/common";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";
import type { PlanMaintenanceAction } from "../../../api/planMaintenance.contract";
const props = defineProps<{ plan: PlanSummary; action: PlanMaintenanceAction }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const reason = ref(props.action === "revise" ? props.plan.reason : ""), error = ref(""), busy = ref(false), conflict = ref(false);
const date = new Date(props.plan.due_at);
const due = ref(new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16));
const lifecycle = crypto.randomUUID();
let active = true;
async function save() {
  if (busy.value || conflict.value) return;
  busy.value = true; error.value = "";
  try {
    if (props.action === "revise") await supplyWorkbenchApi.revisePlan(props.plan, reason.value, due.value, lifecycle);
    else await supplyWorkbenchApi.cancelPlan(props.plan, reason.value, lifecycle);
    if (active) emit("saved");
  } catch (value) {
    if (active) {
      conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus);
      error.value = value instanceof Error ? value.message : "计划操作失败。";
    }
  } finally { if (active) busy.value = false; }
}
onBeforeUnmount(() => { active = false; });
</script>
<template>
  <el-dialog :model-value="true" :title="action === 'revise' ? '修改上下架计划' : '取消上下架计划'" width="min(620px, 94vw)" class="maintain-state-plan" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item :label="action === 'revise' ? '原因（必填）' : '取消原因（必填）'"><el-input v-model="reason" :aria-label="action === 'revise' ? '计划原因' : '取消原因'" type="textarea" :rows="3" :disabled="busy" /></el-form-item>
      <el-form-item v-if="action === 'revise'" label="预计时间"><input v-model="due" class="plan-due" type="datetime-local" aria-label="预计时间" :disabled="busy" /></el-form-item>
    </el-form>
    <p class="muted">{{ action === 'revise' ? '预计时间仅用于提醒，修改计划不会登记实际完成。' : '取消后不再执行本计划，资源当前状态保持不变。' }}</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="conflict" title="计划权限或版本已变化，输入已保留。请返回详情重新加载后核对。" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">返回计划详情</el-button><el-button type="primary" :loading="busy" :disabled="conflict" @click="save">{{ action === 'revise' ? '保存修改' : '确认取消计划' }}</el-button></template>
  </el-dialog>
</template>
<style scoped>.plan-due{width:100%;padding:8px 12px;border:1px solid #dcdfe6;border-radius:4px;color:#303133;font:inherit;box-sizing:border-box}.muted{color:#8a9099;font-size:13px;line-height:1.8}.el-alert+.el-alert{margin-top:12px}</style>
