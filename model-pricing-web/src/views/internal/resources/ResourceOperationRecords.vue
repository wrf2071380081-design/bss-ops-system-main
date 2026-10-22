<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { apiRequest } from "../../../api/http";
import { standardModelVersion } from "../../../api/standardModels.contract";
import { formatDateTime } from "../../../domain/date";

const props = defineProps<{ resourceID?: string; entityType?: "resource" | "standard_model" }>();
type RecordRow = { action: string; operator_name?: string | null; reason?: string | null; created_at: string };
const rows = ref<RecordRow[]>([]), page = ref(1), total = ref(0), loading = ref(false), error = ref("");
let controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  rows.value = []; total.value = 0; error.value = ""; loading.value = true;
  try {
    const result = await apiRequest<{ list: RecordRow[]; total: number; page: number; size: number }>({
      url: "/internal/workbench/operations", params: { entity_type: props.entityType ?? "resource", ...(props.resourceID ? { entity_id: standardModelVersion(props.resourceID) } : {}), page: page.value, size: 20 }, signal: request.signal,
    });
    if (request.signal.aborted) return;
    if (!result || !Array.isArray(result.list) || result.page !== page.value || result.size !== 20 || !Number.isSafeInteger(result.total) || result.total < result.list.length || result.list.length > 20 || result.list.some(row => !row || typeof row.action !== "string" || typeof row.created_at !== "string" || !Number.isFinite(Date.parse(row.created_at))))
      throw new Error("操作记录响应不符合约定，请重新加载。");
    rows.value = result.list; total.value = result.total;
  } catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "操作记录加载失败。"; }
  finally { if (!request.signal.aborted) loading.value = false; }
}
watch(() => [props.resourceID, props.entityType], () => { page.value = 1; void load(); }, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section v-loading="loading" class="resource-records">
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载操作记录</el-button></el-alert>
    <el-table v-else :data="rows" empty-text="暂无操作记录" aria-label="资源操作记录">
      <el-table-column prop="action" label="操作" min-width="150" />
      <el-table-column label="操作人" min-width="110"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column>
      <el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      <el-table-column prop="reason" label="操作说明" min-width="200" />
    </el-table>
    <el-pagination v-if="!error && total" v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
  </section>
</template>

<style scoped>.resource-records{min-height:180px}.el-pagination{margin-top:20px;justify-content:flex-end}</style>
