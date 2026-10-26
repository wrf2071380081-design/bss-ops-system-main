<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { ApiError } from "../../../domain/common";
import { resourceReportsApi, type ResourceReport, type ResourceReportDetail } from "../../../api/resourceReports";
import { resourceCacheDisplay, resourceMetricDisplay } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";

const props = defineProps<{ resourceID: string; currentReportID?: string }>();
const rows = ref<ResourceReport[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref("");
const selectedID = ref(""), detail = ref<ResourceReportDetail>(), detailLoading = ref(false), detailError = ref("");
const downloading = ref(""), fileError = ref(""), retestOpen = ref(false);
let listRequest: AbortController | undefined, detailRequest: AbortController | undefined, active = true;
const labels: Record<string, string> = { RUNNING: "测试中", COMPLETED: "已完成", FAILED: "执行失败" };
const resultText = (value: boolean | null) => value === null ? "未提供结论" : value ? "通过" : "不通过";
const sourceText = (value: ResourceReport["source"]) => value === "SUPPLIER" ? "供应商" : value === "INTERNAL" ? "内部" : "未记录";
const metricText = (value: string | null, status: ResourceReport["status"]) => value === null && status === "RUNNING" ? "测试中" : resourceMetricDisplay(value, "COMPLETED");
const cacheText = (report: ResourceReport) => report.cache_stats.status === "NOT_TESTED" ? report.status === "RUNNING" ? "测试中" : "未提供" : resourceCacheDisplay(report.cache_stats, "COMPLETED");
function message(value: unknown) {
  if (value instanceof ApiError && [404, 501, 503].includes(value.httpStatus)) return "报告服务暂不可用，请联系测试办理人员；当前报告摘要仍可查看。";
  return value instanceof Error ? value.message : "报告加载失败，请重试。";
}
async function load() {
  listRequest?.abort(); const request = listRequest = new AbortController();
  loading.value = true; error.value = ""; rows.value = []; total.value = 0;
  try { const result = await resourceReportsApi.list(props.resourceID, page.value, request.signal); if (!request.signal.aborted) { rows.value = result.list; total.value = result.total; } }
  catch (value) { if (!request.signal.aborted) error.value = message(value); }
  finally { if (!request.signal.aborted) loading.value = false; }
}
async function openDetail(id: string) {
  detailRequest?.abort(); const request = detailRequest = new AbortController();
  selectedID.value = id; detail.value = undefined; detailError.value = ""; fileError.value = ""; detailLoading.value = true;
  try { const result = await resourceReportsApi.detail(props.resourceID, id, request.signal); if (!request.signal.aborted) detail.value = result; }
  catch (value) { if (!request.signal.aborted) detailError.value = message(value); }
  finally { if (!request.signal.aborted) detailLoading.value = false; }
}
function closeDetail() { detailRequest?.abort(); selectedID.value = ""; detail.value = undefined; }
async function download(file: ResourceReportDetail["files"][number]) {
  const report = detail.value;
  if (!report || !file.can_download || downloading.value) return;
  downloading.value = file.id; fileError.value = "";
  try {
    const blob = await resourceReportsApi.file(props.resourceID, report.id, file.id);
    if (!active || detail.value?.id !== report.id) return;
    const url = URL.createObjectURL(blob), link = document.createElement("a");
    link.href = url; link.download = file.name.replace(/[\\/\u0000-\u001f\u007f]/g, "_"); link.rel = "noopener";
    document.body.append(link); link.click(); link.remove(); window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (value) { if (active && detail.value?.id === report.id) fileError.value = message(value); }
  finally { if (active) downloading.value = ""; }
}
watch(() => props.resourceID, () => { page.value = 1; closeDetail(); retestOpen.value = false; void load(); }, { immediate: true });
onBeforeUnmount(() => { active = false; listRequest?.abort(); detailRequest?.abort(); });
</script>

<template>
  <section class="resource-reports">
    <header class="report-tools"><h3>测试报告历史</h3><div><el-button size="small" :loading="loading" @click="load">刷新报告</el-button><el-button size="small" @click="retestOpen = true">补测指引</el-button></div></header>
    <el-alert v-if="error" :title="error" type="warning" :closable="false"><el-button text @click="load">重试</el-button></el-alert>
    <el-table v-else v-loading="loading" :data="rows" empty-text="暂无测试报告" aria-label="资源测试报告历史">
      <el-table-column label="报告" min-width="120"><template #default="{ row }">{{ row.id }}<el-tag v-if="row.id === currentReportID" size="small">当前引用</el-tag></template></el-table-column>
      <el-table-column label="测试日期" min-width="160"><template #default="{ row }">{{ formatDateTime(row.tested_at) }}</template></el-table-column>
      <el-table-column label="测试项" min-width="120"><template #default="{ row }">{{ row.test_item }}</template></el-table-column>
      <el-table-column label="操作人" min-width="110"><template #default="{ row }">{{ row.operator_name || '未记录' }}</template></el-table-column>
      <el-table-column label="来源" min-width="100"><template #default="{ row }">{{ sourceText(row.source) }}</template></el-table-column>
      <el-table-column label="场景" min-width="140"><template #default="{ row }">{{ row.scene || '未记录' }}</template></el-table-column>
      <el-table-column label="状态 / 目标" min-width="130"><template #default="{ row }">{{ labels[row.status] }}<p :class="{ warning: !row.target_matches }">{{ row.target_matches ? '当前测试目标' : '旧测试目标' }}</p></template></el-table-column>
      <el-table-column label="连通 / 压力" min-width="130"><template #default="{ row }">{{ resultText(row.connectivity_passed) }} / {{ resultText(row.pressure_passed) }}</template></el-table-column>
      <el-table-column label="详情" width="110"><template #default="{ row }"><el-button link type="primary" @click="openDetail(row.id)">查看本次测试</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total" v-model:current-page="page" :page-size="10" :total="total" layout="total, prev, pager, next" @current-change="load" />
    <el-dialog v-if="selectedID" :model-value="true" title="测试报告详情" width="min(720px, 94vw)" append-to-body @close="closeDetail">
      <div v-loading="detailLoading" class="report-detail"><el-alert v-if="detailError" :title="detailError" type="error" :closable="false"><el-button text @click="openDetail(selectedID)">重新加载报告</el-button></el-alert>
        <template v-if="detail"><p>报告 {{ detail.id }} · {{ labels[detail.status] }}</p><div class="report-meta"><div>测试日期<strong>{{ formatDateTime(detail.tested_at) }}</strong></div><div>测试项<strong>{{ detail.test_item }}</strong></div><div>操作人<strong>{{ detail.operator_name || '未记录' }}</strong></div><div>来源<strong>{{ sourceText(detail.source) }}</strong></div><div>场景<strong>{{ detail.scene || '未记录' }}</strong></div></div><el-alert v-if="!detail.target_matches" title="这是旧测试目标的历史报告，不作为当前准入依据。" type="warning" :closable="false" />
          <div class="report-metrics"><div>连通性<strong>{{ resultText(detail.connectivity_passed) }}</strong></div><div>压力测试<strong>{{ resultText(detail.pressure_passed) }}</strong></div><div>连通耗时 · ms<strong>{{ metricText(detail.metrics.connection_ms, detail.status) }}</strong></div><div>RPS<strong>{{ metricText(detail.metrics.rps, detail.status) }}</strong></div><div>TPS<strong>{{ metricText(detail.metrics.tps, detail.status) }}</strong></div><div>输入缓存命中率<strong>{{ cacheText(detail) }}</strong></div></div>
          <p v-if="detail.cache_stats.status === 'PROVIDED'">命中输入 Token / 总输入 Token：{{ detail.cache_stats.hit_tokens }} / {{ detail.cache_stats.input_tokens }}</p><p class="muted">{{ detail.cache_stats.scene || '未提供缓存测试场景' }}</p>
          <h3>报告文件</h3><el-empty v-if="!detail.files.length" description="本报告未提供附件" :image-size="50" /><div v-for="file in detail.files" :key="file.id" class="report-file"><span>{{ file.name }}<small v-if="file.unavailable_reason">{{ file.unavailable_reason }}</small></span><el-button link type="primary" :disabled="!file.can_download || Boolean(downloading)" :loading="downloading === file.id" @click="download(file)">{{ file.can_download ? '下载' : '暂不可下载' }}</el-button></div>
        </template><el-alert v-if="fileError" :title="fileError" type="error" :closable="false" /></div>
      <template #footer><el-button @click="closeDetail">关闭报告</el-button></template>
    </el-dialog>
    <el-dialog v-model="retestOpen" title="资源补测" width="min(560px, 94vw)" append-to-body><p>请通过对应申报或办理流程联系测试办理人员发起补测。此入口不直接启动测试。</p><p>测试完成后刷新报告。每条资源使用自己的测试结论；补测不直接改变当前上下架状态。</p><template #footer><el-button @click="retestOpen = false">关闭</el-button><el-button type="primary" @click="retestOpen = false; load()">刷新报告</el-button></template></el-dialog>
  </section>
</template>

<style scoped>.resource-reports{margin-top:24px;border-top:1px solid #e6e8ed;padding-top:12px}.report-tools,.report-file{display:flex;justify-content:space-between;align-items:center;gap:12px}.report-tools h3,h3{font-size:15px}.report-detail{min-height:140px}.report-meta{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:16px 0}.report-meta>div{background:#f8f9fb;border:1px solid #e6e8ed;border-radius:6px;padding:10px;color:#8a9099;font-size:12px}.report-meta strong{display:block;color:#30343b;font-size:13px;margin-top:5px}.report-metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin:20px 0}.report-metrics>div{background:#f8f9fb;border:1px solid #e6e8ed;border-radius:6px;padding:14px;color:#8a9099;font-size:12px}.report-metrics strong{display:block;color:#1c1d1f;font-size:17px;margin-top:8px}.report-file{padding:12px 0;border-bottom:1px solid #e6e8ed}.report-file small{display:block;color:#8a9099;margin-top:4px}.muted{color:#8a9099}.warning{color:#b87327}.el-pagination{margin-top:16px;justify-content:flex-end}.resource-reports p{line-height:1.7}@media(max-width:500px){.report-meta,.report-metrics{grid-template-columns:repeat(2,1fr)}.report-tools{align-items:flex-start;flex-direction:column}}</style>
