<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { formatDateTime } from "../../../domain/date";
import type { NoticeView } from "../../../api/supplyWorkbench.types";
import NoticeDetailDialog from "./NoticeDetailDialog.vue";

const props = defineProps<{ resourceID: string; region: string }>();
const rows = ref<NoticeView[]>([]), total = ref(0), page = ref(1), state = ref("ALL"), selected = ref<NoticeView>();
const loading = ref(false), error = ref("");
let controller: AbortController | undefined, active = true;
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  loading.value = true; error.value = ""; rows.value = []; total.value = 0;
  try {
    const result = await supplyWorkbenchApi.notices({ resource_id: props.resourceID, region: props.region, read_status: state.value, page: page.value, size: 10 }, request.signal);
    if (active && !request.signal.aborted) { rows.value = result.list; total.value = result.total; }
  } catch (value) {
    if (active && !request.signal.aborted) error.value = value instanceof Error ? value.message : "公告列表暂时无法加载。";
  } finally { if (active && !request.signal.aborted) loading.value = false; }
}
function filterChanged() { page.value = 1; void load(); }
watch(() => [props.resourceID, props.region], () => { page.value = 1; selected.value = undefined; void load(); }, { immediate: true });
onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>
<template>
  <section v-loading="loading" class="resource-notices">
    <div class="notice-tools"><span class="muted">当前账号的阅读状态</span><el-select v-model="state" aria-label="公告阅读状态" style="width:140px" @change="filterChanged"><el-option label="全部" value="ALL" /><el-option label="未读" value="UNREAD" /><el-option label="已读" value="READ" /></el-select></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载公告列表</el-button></el-alert>
    <el-table v-else :data="rows" empty-text="暂无符合条件的公告" aria-label="资源公告列表">
      <el-table-column label="公告" min-width="170"><template #default="{row}">{{ row.notice.title }}</template></el-table-column>
      <el-table-column label="发布时间" min-width="155"><template #default="{row}">{{ formatDateTime(row.notice.published_at) }}</template></el-table-column>
      <el-table-column label="阅读状态" width="90"><template #default="{row}"><el-tag :type="row.read_status === 'READ' ? 'info' : 'warning'">{{ row.read_status === 'READ' ? '已读' : '未读' }}</el-tag></template></el-table-column>
      <el-table-column label="详情" width="75" fixed="right"><template #default="{row}"><el-button link type="primary" @click="selected = row">详情</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total > 0" v-model:current-page="page" :page-size="10" :total="total" layout="total, prev, pager, next" @current-change="load" />
    <NoticeDetailDialog v-if="selected" :key="`${selected.notice.id}:${selected.staff_id}`" :id="selected.notice.id" :selected-staff-i-d="selected.staff_id" @close="selected = undefined" @read="load" />
  </section>
</template>
<style scoped>
.resource-notices{min-height:220px}.notice-tools{display:flex;justify-content:space-between;align-items:center;gap:16px;margin:12px 0 20px}.muted{color:#79868f;font-size:13px}.el-pagination{justify-content:flex-end;margin-top:20px}@media(max-width:500px){.notice-tools{align-items:flex-start;flex-direction:column}.el-pagination{justify-content:flex-start}}
</style>
