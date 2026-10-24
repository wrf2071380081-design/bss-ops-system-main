<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../../domain/common";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import type { ResourceRow } from "../../../api/resources.types";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";
import { useSessionStore } from "../../../stores/session";

const props = defineProps<{ resource: ResourceRow }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const session = useSessionStore();
const people = ref<WorkbenchPersonDTO[]>([]), supplierModelID = ref(props.resource.supplier_model_id), coefficient = ref(""), assigneeID = ref(""), reason = ref("");
const due = ref(new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 16));
const loading = ref(false), checked = ref(false), pending = ref(false), busy = ref(false), conflict = ref(false), error = ref("");
const lifecycle = crypto.randomUUID();
let active = true, controller: AbortController | undefined;

async function check() {
  controller?.abort(); const request = controller = new AbortController();
  loading.value = true; checked.value = false; pending.value = false; error.value = "";
  try {
    const [staff, pendingPlans, executingPlans] = await Promise.all([
      workbenchApi.people(),
      supplyWorkbenchApi.plans({ resource_id: props.resource.id, region: props.resource.region, kind: "PRICE_CHANGE", status: "PENDING", page: 1, size: 1 }, request.signal),
      supplyWorkbenchApi.plans({ resource_id: props.resource.id, region: props.resource.region, kind: "PRICE_CHANGE", status: "EXECUTING", page: 1, size: 1 }, request.signal),
    ]);
    if (active && !request.signal.aborted) {
      people.value = staff;
      const current = String(session.user?.staffId || "");
      assigneeID.value = staff.some(person => String(person.id) === current) ? current : String(staff[0]?.id || "");
      pending.value = pendingPlans.total > 0 || executingPlans.total > 0;
      checked.value = true;
    }
  } catch (value) {
    if (active && !request.signal.aborted) error.value = value instanceof Error ? value.message : "无法检查已有计划或办理人员。";
  } finally { if (active && !request.signal.aborted) loading.value = false; }
}
async function save() {
  if (busy.value || !checked.value || pending.value || conflict.value) return;
  busy.value = true; error.value = "";
  try {
    await supplyWorkbenchApi.createPriceChangePlan(props.resource, supplierModelID.value, coefficient.value, assigneeID.value, reason.value, due.value, lifecycle);
    if (active) emit("saved");
  } catch (value) {
    if (active) {
      conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus);
      error.value = value instanceof Error ? value.message : "创建改价计划失败。";
    }
  } finally { if (active) busy.value = false; }
}
onMounted(check);
onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>

<template>
  <el-dialog :model-value="true" title="创建内部改价计划" width="min(640px, 94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <p>{{ resource.supplier_name }} / {{ resource.model_name }} · {{ resourceRegionLabels[resource.region] }} · 当前采购系数 {{ resource.coefficient }}</p>
    <el-alert v-if="pending" title="该资源已有待执行的改价计划，请先查看原计划。" type="warning" :closable="false" />
    <el-alert v-if="checked && !people.length" title="当前没有可选的在职办理人员，暂时不能创建计划。" type="warning" :closable="false" />
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="调整后供应商接口模型 ID"><el-input v-model="supplierModelID" maxlength="128" aria-label="调整后供应商接口模型 ID" :disabled="busy" /></el-form-item>
      <el-form-item label="调整后采购系数（0 到 1）"><el-input v-model="coefficient" inputmode="decimal" placeholder="例如 0.8500" aria-label="调整后采购系数" :disabled="busy" /></el-form-item>
      <el-form-item label="办理人"><el-select v-model="assigneeID" filterable placeholder="选择办理人" aria-label="办理人" style="width:100%" :disabled="busy || !people.length"><el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" /></el-select></el-form-item>
      <el-form-item label="预计调整时间"><input v-model="due" class="plan-due" type="datetime-local" aria-label="预计调整时间" :disabled="busy" /></el-form-item>
      <el-form-item label="改价说明（必填）"><el-input v-model="reason" type="textarea" :rows="3" placeholder="填写调整原因和业务依据" aria-label="改价说明" :disabled="busy" /></el-form-item>
    </el-form>
    <p class="muted">计划创建时不改变资源。办理人完成外部调整后登记实际依据，系统更新接口模型 ID 和/或采购系数并留痕；接口模型 ID 变化会使旧目标测试报告失效。预计时间只用于提醒。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button v-if="!checked" text @click="check">重新检查</el-button></el-alert>
    <el-alert v-if="conflict" title="资源权限、版本或计划状态已变化，输入已保留。请返回资源详情刷新后重试。" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">关闭</el-button><el-button type="primary" :loading="busy || loading" :disabled="!checked || pending || !people.length || !assigneeID || conflict" @click="save">创建计划</el-button></template>
  </el-dialog>
</template>

<style scoped>
p{line-height:1.8;overflow-wrap:anywhere}.el-form{margin-top:20px}.plan-due{width:100%;padding:8px 12px;border:1px solid #dcdfe6;border-radius:4px;color:#303133;font:inherit;box-sizing:border-box}.muted{color:#8a9099;font-size:13px;line-height:1.8}.el-alert+.el-alert{margin-top:12px}
</style>
