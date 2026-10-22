<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { apiRequest } from "../../../api/http";
import { standardModelVersion } from "../../../api/standardModels.contract";
import { sourceOperationLocation, validateSourceOperations, type SourceOperation } from "../../../api/sourceOperations.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
const props = defineProps<{ modelID: string }>();
const router = useRouter();
const rows = ref<SourceOperation[]>([]), page = ref(1), total = ref(0), loading = ref(false), error = ref("");
const planActions: Record<string, string> = { CREATE: "创建上下架计划", REVISE: "修改上下架计划", CANCEL: "取消上下架计划", EXECUTE: "登记计划实际完成", BLOCKED: "计划执行阻断" };
let controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  rows.value = []; total.value = 0; loading.value = true; error.value = "";
  try {
    const result = validateSourceOperations(await apiRequest<unknown>({ url: `/internal/standard-models/${standardModelVersion(props.modelID)}/resource-operations`, params: { page: page.value, size: 20 }, signal: request.signal }), page.value, 20);
    if (!request.signal.aborted) { rows.value = result.list; total.value = result.total; }
  } catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "操作记录暂时无法加载。"; }
  finally { if (!request.signal.aborted) loading.value = false; }
}
watch(() => props.modelID, () => { page.value = 1; void load(); }, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section v-loading="loading" class="model-source-records">
    <p class="scope-note">汇总本模型全部供应资源及上下架计划的操作记录，不受来源列表筛选影响。模型资料维护记录在标准模型库查看。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载操作记录</el-button></el-alert>
    <el-table v-else :data="rows" empty-text="暂无供应资源操作记录" aria-label="模型供应资源操作记录">
      <el-table-column label="操作" min-width="160"><template #default="{ row }">{{ row.entity_type === 'plan' ? planActions[row.action] || row.action : row.action }}</template></el-table-column>
      <el-table-column label="业务对象" min-width="210"><template #default="{ row }"><strong>{{ row.supplier_name }}</strong><div class="scope-note">{{ resourceRegionLabels[row.region] }} · {{ row.supplier_model_id }}</div></template></el-table-column>
      <el-table-column prop="operator_name" label="操作人" min-width="110"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column>
      <el-table-column label="时间" min-width="165"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      <el-table-column prop="reason" label="操作说明" min-width="180" />
      <el-table-column label="详情" width="100" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="router.push(sourceOperationLocation(row))">{{ row.entity_type === 'plan' ? '查看计划' : '查看资源' }}</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total" v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
  </section>
</template>
<style scoped>.model-source-records{min-height:180px;padding:0 18px 18px}.scope-note{font-size:12px;line-height:1.8;color:#8a9099}.el-pagination{margin-top:20px;justify-content:flex-end}</style>
