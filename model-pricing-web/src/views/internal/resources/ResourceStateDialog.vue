<script setup lang="ts">
import { computed, ref } from "vue";
import { resourcesApi } from "../../../api/resources";
import { resourceStateAction } from "../../../api/resourceState.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { ApiError } from "../../../domain/common";
import type { ResourceRow } from "../../../api/resources.types";
const props = defineProps<{ resource: ResourceRow }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const confirmed = ref(false), note = ref(""), reason = ref(""), busy = ref(false), error = ref(""), conflict = ref(false);
const action = computed(() => resourceStateAction(props.resource)), title = computed(() => action.value === "ONLINE" ? "重新上架登记" : "下架登记");
const lifecycle = crypto.randomUUID();
async function submit() {
  if (busy.value || conflict.value) return;
  error.value = ""; busy.value = true;
  try { await resourcesApi.registerState(props.resource, confirmed.value, note.value, reason.value, lifecycle); emit("saved"); }
  catch (value) { conflict.value = value instanceof ApiError && [403, 409].includes(value.httpStatus); error.value = value instanceof Error ? value.message : "登记失败，请重试。"; }
  finally { busy.value = false; }
}
</script>
<template>
  <el-dialog :model-value="true" :title="title" width="min(620px, 94vw)" class="resource-state-dialog" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="!busy && emit('close')">
    <p>{{ resource.model_name }} · {{ resource.supplier_name }} · {{ resourceRegionLabels[resource.region] }}</p>
    <el-alert :title="action === 'ONLINE' ? '重新上架不会自动恢复原客户配置；登记时会重新校验模型、正式配置与测试依据。' : '下架后资源停止供新报价和开通使用，原客户配置的影响按实际变更处理。'" type="warning" :closable="false" />
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="操作原因"><el-input v-model="reason" aria-label="操作原因" type="textarea" :disabled="busy" /></el-form-item>
      <el-form-item><el-checkbox v-model="confirmed" :disabled="busy">已在{{ resourceRegionLabels[resource.region] }} B 端平台完成实际{{ action === 'ONLINE' ? '上架' : '下架' }}操作</el-checkbox></el-form-item>
      <el-form-item label="完成依据"><el-input v-model="note" aria-label="完成依据" type="textarea" :rows="4" :disabled="busy" placeholder="填写实际平台操作的完成依据" /></el-form-item>
    </el-form>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="conflict" title="权限或资源状态已变化，输入已保留。请复制需要保留的内容，关闭后刷新资源，再重新核对登记。" type="warning" :closable="false" />
    <template #footer><el-button :disabled="busy" @click="emit('close')">关闭登记</el-button><el-button type="primary" :loading="busy" :disabled="conflict || !action" @click="submit">确认登记{{ action === 'ONLINE' ? '上架' : '下架' }}</el-button></template>
  </el-dialog>
</template>
<style scoped>.el-form{margin-top:22px}.el-alert+.el-alert{margin-top:12px}p{line-height:1.8;overflow-wrap:anywhere}.el-checkbox{height:auto;white-space:normal}</style>
