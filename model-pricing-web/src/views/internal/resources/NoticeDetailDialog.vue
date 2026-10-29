<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { noticeImpactText } from "../../../api/supplyWorkbench.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import type { NoticeView } from "../../../api/supplyWorkbench.types";

const props = defineProps<{ id: string; selectedStaffID?: string; readOnly?: boolean }>();
const emit = defineEmits<{ close: []; read: [] }>();
const router = useRouter();
const data = ref<NoticeView>(), loading = ref(false), marking = ref(false), error = ref(""), writeError = ref("");
let controller: AbortController | undefined, writeController: AbortController | undefined, epoch = 0;

// 弹窗标题：原通知 · 发布人（执行人优先，缺失时用查看人）。
const dialogTitle = computed(() => `原通知 · ${data.value?.notice.executor_name || data.value?.staff_name || ""}`.trim());
const regionLabel = computed(() => resourceRegionLabels[data.value?.notice.region || ""] || "");
// 预计时间按本地时区展示，形如 2026-10-23 07:13。
function compactTime(value?: string) {
  if (!value) return "";
  const at = new Date(value);
  if (!Number.isFinite(at.getTime())) return "";
  return new Date(at.getTime() + 8 * 3600 * 1000).toISOString().slice(0, 16).replace("T", " ");
}
const scheduleLine = computed(() => {
  const notice = data.value?.notice;
  if (!notice) return "";
  const parts: string[] = [];
  if (notice.scheduled_at) parts.push(`预计时间：${compactTime(notice.scheduled_at)}`);
  if (notice.executor_name) parts.push(`执行人：${notice.executor_name}`);
  if (notice.content) parts.push(notice.content);
  return parts.join("；") + (parts.length ? "。" : "");
});
const canMarkRead = computed(() => Boolean(data.value?.permissions.can_mark_read) && !props.readOnly && data.value?.read_status === "UNREAD");

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
// 登记执行结果：到资源详情的「计划」页签里登记实际执行。
async function openExecution() {
  const notice = data.value?.notice;
  if (!notice) return;
  await router.push({ path: `/internal/model-plaza/${notice.resource_id}`, query: { region: notice.region, tab: "plans" } });
  emit("close");
}
watch(() => [props.id, props.selectedStaffID], load, { immediate: true });
onBeforeUnmount(() => { ++epoch; controller?.abort(); writeController?.abort(); });
</script>

<template>
  <el-dialog :model-value="true" :title="dialogTitle" width="min(760px, 94vw)" class="supply-notice-dialog" :close-on-click-modal="false" @close="emit('close')">
    <div v-loading="loading" class="notice-body">
      <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载公告详情</el-button></el-alert>
      <article v-if="data" class="notice-card" :class="{ 'notice-read': data.read_status === 'READ' }">
        <header>
          <h2>{{ data.notice.title }}</h2>
          <el-tag size="small" :type="data.read_status === 'READ' ? 'info' : 'warning'">{{ data.read_status === "READ" ? "已读" : "未读" }}</el-tag>
        </header>
        <p class="meta">{{ data.notice.supplier_name || data.notice.resource_name }}<template v-if="regionLabel"> · {{ regionLabel }}</template> · {{ formatDateTime(data.notice.published_at, "—", true) }}</p>
        <dl>
          <dt>说明</dt>
          <dd>{{ data.notice.reason || "未提供" }}</dd>
          <dt>受影响客户</dt>
          <dd>{{ noticeImpactText(data.notice) }}</dd>
        </dl>
        <p v-if="scheduleLine" class="schedule">{{ scheduleLine }}</p>
        <el-alert v-if="readOnly" title="当前为只读查看，不能代他人标记已读。" type="info" :closable="false" />
        <div class="notice-actions">
          <el-button v-if="canMarkRead" type="primary" :loading="marking" @click="markRead">已读</el-button>
          <el-button @click="openExecution">登记执行结果</el-button>
        </div>
      </article>
      <el-alert v-if="writeError" :title="writeError" type="error" :closable="false" class="write-error" />
    </div>
    <template #footer><el-button @click="emit('close')">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.notice-body{min-height:180px;color:#1c1d1f}
.notice-card{border:1px solid #e3e8e9;border-left:3px solid #d8a14a;border-radius:8px;padding:22px}
.notice-read{border-left-color:#9eadaa}
header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}
h2{font-size:19px;line-height:1.5;margin:0}
.meta{color:#8a9099;font-size:13px;line-height:1.8;margin:10px 0 0}
dl{display:grid;grid-template-columns:96px minmax(0,1fr);gap:12px;margin:20px 0 0}
dt{color:#8a9099}
dd{margin:0;overflow-wrap:anywhere}
.schedule{color:#5b6472;font-size:13px;line-height:1.9;margin:14px 0 0;white-space:pre-wrap}
.notice-actions{display:flex;gap:10px;margin-top:18px}
.write-error{margin-top:16px}
@media(max-width:500px){.notice-card{padding:16px}dl{grid-template-columns:1fr;gap:8px}}
</style>
