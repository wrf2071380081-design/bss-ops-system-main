<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { noticeImpactText } from "../../../api/supplyWorkbench.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import type { NoticeView } from "../../../api/supplyWorkbench.types";

const props = defineProps<{ id: string; selectedStaffID?: string; readOnly?: boolean }>();
const emit = defineEmits<{ close: []; read: [] }>();
const router = useRouter();
async function openResource() {
  if (!data.value) return;
  await router.push({ path: `/internal/model-plaza/${data.value.notice.resource_id}`, query: { region: data.value.notice.region, tab: "notices" } });
  emit("close");
}
const data = ref<NoticeView>(), loading = ref(false), marking = ref(false), error = ref(""), writeError = ref("");
let controller: AbortController | undefined, writeController: AbortController | undefined, epoch = 0;
async function load() {
  const generation = ++epoch;
  controller?.abort(); writeController?.abort();
  const request = controller = new AbortController();
  data.value = undefined; loading.value = true; marking.value = false; error.value = ""; writeError.value = "";
  try {
    const result = await supplyWorkbenchApi.notice(props.id, props.selectedStaffID, request.signal);
    if (generation === epoch && !request.signal.aborted) data.value = result;
  } catch (value) {
    if (generation === epoch && !request.signal.aborted) error.value = value instanceof Error ? value.message : "公告详情暂时无法加载。";
  } finally { if (generation === epoch && !request.signal.aborted) loading.value = false; }
}
async function markRead() {
  const view = data.value;
  if (!view || props.readOnly || !view.permissions.can_mark_read || marking.value) return;
  const generation = epoch, request = writeController = new AbortController();
  marking.value = true; writeError.value = "";
  try {
    const receipt = await supplyWorkbenchApi.markRead(view.notice.id, view.staff_id, request.signal);
    if (generation !== epoch || request.signal.aborted) return;
    data.value = { ...view, read_status: "READ", read_at: receipt.read_at, permissions: { can_mark_read: false } };
    emit("read");
  } catch (value) {
    if (generation === epoch && !request.signal.aborted) writeError.value = value instanceof Error ? value.message : "标记已读失败，请重试。";
  } finally { if (generation === epoch && !request.signal.aborted) marking.value = false; }
}
watch(() => [props.id, props.selectedStaffID], load, { immediate: true });
onBeforeUnmount(() => { ++epoch; controller?.abort(); writeController?.abort(); });
</script>

<template>
  <el-dialog :model-value="true" title="公告详情" width="min(680px, 94vw)" class="supply-notice-dialog" :close-on-click-modal="false" @close="emit('close')">
    <div v-loading="loading" class="notice-body">
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载公告详情</el-button></el-alert>
      <article v-if="data" class="notice-card" :class="{ 'notice-read': data.read_status === 'READ' }">
        <header><h2>{{ data.notice.title }}</h2><el-tag :type="data.read_status === 'READ' ? 'info' : 'warning'">{{ data.read_status === 'READ' ? '已读' : '未读' }}</el-tag></header>
        <p class="muted">{{ data.notice.resource_name || `资源 ${data.notice.resource_id}` }} · {{ resourceRegionLabels[data.notice.region] }} · {{ formatDateTime(data.notice.published_at) }}</p>
        <dl><dt>原因</dt><dd>{{ data.notice.reason || '未提供' }}</dd><dt>受影响客户</dt><dd>{{ noticeImpactText(data.notice) }}</dd><dt>阅读人员</dt><dd>{{ data.staff_name || `人员 ${data.staff_id}` }}</dd><template v-if="data.read_at"><dt>首次阅读时间</dt><dd>{{ formatDateTime(data.read_at) }}</dd></template></dl>
        <p class="notice-content">{{ data.notice.content || '暂无补充说明' }}</p>
        <el-alert v-if="readOnly || (!data.permissions.can_mark_read && data.read_status === 'UNREAD')" title="当前为只读查看，不能代他人标记已读。" type="info" :closable="false" />
      </article>
      <el-alert v-if="writeError" :title="writeError" type="error" :closable="false" class="write-error" />
    </div>
    <template #footer><el-button @click="emit('close')">关闭公告详情</el-button><el-button v-if="data" @click="openResource">查看关联资源</el-button><el-button v-if="data?.permissions.can_mark_read && !readOnly" type="primary" :loading="marking" @click="markRead">标记已读</el-button></template>
  </el-dialog>
</template>

<style scoped>
.notice-body{min-height:180px;color:#1c1d1f}.notice-card{border:1px solid #e3e8e9;border-left:3px solid #d8a14a;border-radius:8px;padding:22px}.notice-read{border-left-color:#9eadaa}header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}h2{font-size:19px;line-height:1.5;margin:0}.muted{color:#8a9099;font-size:13px;line-height:1.8}dl{display:grid;grid-template-columns:110px minmax(0,1fr);gap:14px;margin:24px 0}dt{color:#8a9099}dd{margin:0;overflow-wrap:anywhere}.notice-content{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.9}.write-error{margin-top:16px}@media(max-width:500px){.notice-card{padding:16px}dl{grid-template-columns:1fr;gap:8px}dd{padding-bottom:8px}}
</style>
