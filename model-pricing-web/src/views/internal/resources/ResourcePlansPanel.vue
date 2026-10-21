<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { planKindLabels, planStatusLabels } from "../../../api/supplyWorkbench.contract";
import { formatDateTime } from "../../../domain/date";
import type { PlanSummary } from "../../../api/supplyWorkbench.types";
import PlanSummaryDialog from "./PlanSummaryDialog.vue";
const props = defineProps<{ resourceID: string; region: string }>();
const rows = ref<PlanSummary[]>([]), total = ref(0), page = ref(1), status = ref(""), loading = ref(false), error = ref(""), selected = ref<string>();
let active = true, controller: AbortController | undefined;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  loading.value = true; error.value = ""; rows.value = []; total.value = 0;
  try {
    const result = await supplyWorkbenchApi.plans({ resource_id: props.resourceID, region: props.region, status: status.value, page: page.value, size: 10 }, request.signal);
    if (active && !request.signal.aborted) { rows.value = result.list; total.value = result.total; }
  } catch (value) { if (active && !request.signal.aborted) error.value = value instanceof Error ? value.message : "计划列表暂时无法加载。"; }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function filterChanged() { page.value = 1; void load(); }
watch(() => [props.resourceID, props.region], () => { page.value = 1; selected.value = undefined; void load(); }, { immediate: true });
onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <section v-loading="loading" class="resource-plans">
    <div class="plan-tools"><span>预计时间用于提醒，不会自动改变资源状态。</span><el-select v-model="status" aria-label="计划状态" clearable placeholder="全部状态" style="width:150px" @change="filterChanged"><el-option v-for="(label, key) in planStatusLabels" :key="key" :label="label" :value="key" /></el-select></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载计划列表</el-button></el-alert>
    <el-table v-else :data="rows" empty-text="暂无符合条件的计划" aria-label="资源计划列表">
      <el-table-column label="计划" min-width="90"><template #default="{row}">{{ planKindLabels[row.kind] }}</template></el-table-column>
      <el-table-column label="发起人" min-width="100"><template #default="{row}">{{ row.creator_name || `人员 ${row.created_by}` }}{{ row.is_initiator ? '（本人）' : '' }}</template></el-table-column>
      <el-table-column label="预计时间" min-width="160"><template #default="{row}">{{ formatDateTime(row.due_at) }}</template></el-table-column>
      <el-table-column label="状态" min-width="95"><template #default="{row}"><el-tag :type="row.status === 'BLOCKED' ? 'warning' : 'info'">{{ planStatusLabels[row.status] }}</el-tag></template></el-table-column>
      <el-table-column label="前提" min-width="100"><template #default="{row}">{{ row.precondition_matches ? '版本匹配' : '资源已变更' }}</template></el-table-column>
      <el-table-column label="详情" width="75" fixed="right"><template #default="{row}"><el-button link type="primary" @click="selected = row.id">详情</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total > 0" v-model:current-page="page" :page-size="10" :total="total" layout="total, prev, pager, next" @current-change="load" />
    <PlanSummaryDialog v-if="selected" :id="selected" @close="selected = undefined" @changed="load" />
  </section>
</template>
<style scoped>.resource-plans{min-height:220px}.plan-tools{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:14px 0 20px}.plan-tools>span{font-size:13px;color:#79868f;line-height:1.8}.el-pagination{justify-content:flex-end;margin-top:20px}@media(max-width:500px){.plan-tools{align-items:flex-start;flex-direction:column}.el-pagination{justify-content:flex-start}}</style>
