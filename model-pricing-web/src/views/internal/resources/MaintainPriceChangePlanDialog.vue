<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../../domain/common";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";

const props = defineProps<{ plan: PlanSummary }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const people = ref<WorkbenchPersonDTO[]>([]), supplierModelID = ref(props.plan.after_supplier_model_id || props.plan.supplier_model_id || ""), coefficient = ref(props.plan.after_coefficient || ""), assigneeID = ref(props.plan.assignee_id || "");
const reason = ref(props.plan.reason), error = ref(""), loading = ref(false), busy = ref(false), conflict = ref(false);
const dueDate = new Date(props.plan.due_at);
const due = ref(new Date(dueDate.getTime() - dueDate.getTimezoneOffset() * 60000).toISOString().slice(0, 16));
const lifecycle = crypto.randomUUID();
let active = true;
async function loadPeople() {
  loading.value = true;
  try { people.value = await workbenchApi.people(); }
  catch (value) { if (active) error.value = value instanceof Error ? value.message : "办理人员暂时无法加载。"; }
  finally { if (active) loading.value = false; }
}
async function save() {
  if (busy.value || conflict.value) return;
  busy.value = true; error.value = "";
  try { await supplyWorkbenchApi.revisePriceChangePlan(props.plan, supplierModelID.value, coefficient.value, assigneeID.value, reason.value, due.value, lifecycle); if (active) emit("saved"); }
  catch (value) {
    if (active) { conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus); error.value = value instanceof Error ? value.message : "修改改价计划失败。"; }
  } finally { if (active) busy.value = false; }
}
onMounted(loadPeople);
onBeforeUnmount(() => { active = false; });
</script>

<template>
  <el-dialog :model-value="true" title="修改内部改价计划" width="min(640px, 94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <el-alert title="修改只更新计划版本和提醒；计划办理完成后才会更新资源接口模型 ID 或采购系数。" type="info" :closable="false" />
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="调整后供应商接口模型 ID"><el-input v-model="supplierModelID" maxlength="128" aria-label="调整后供应商接口模型 ID" :disabled="busy" /></el-form-item>
      <el-form-item label="调整后采购系数"><el-input v-model="coefficient" inputmode="decimal" aria-label="调整后采购系数" :disabled="busy" /></el-form-item>
      <el-form-item label="办理人"><el-select v-model="assigneeID" filterable aria-label="办理人" style="width:100%" :disabled="busy || loading || !people.length"><el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" /></el-select></el-form-item>
      <el-form-item label="预计调整时间"><input v-model="due" class="plan-due" type="datetime-local" aria-label="预计调整时间" :disabled="busy" /></el-form-item>
      <el-form-item label="修改原因（必填）"><el-input v-model="reason" type="textarea" :rows="3" aria-label="修改原因" :disabled="busy" /></el-form-item>
    </el-form>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="conflict" title="计划权限或版本已变化，输入已保留。请返回详情重新加载后核对。" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">返回计划详情</el-button><el-button type="primary" :loading="busy" :disabled="loading || !people.length || conflict" @click="save">保存修改</el-button></template>
  </el-dialog>
</template>

<style scoped>
.el-form{margin-top:20px}.plan-due{width:100%;padding:8px 12px;border:1px solid #dcdfe6;border-radius:4px;color:#303133;font:inherit;box-sizing:border-box}.el-alert{margin-top:12px}
</style>
