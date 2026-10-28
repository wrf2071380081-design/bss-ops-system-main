<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { ApiError } from "../../../domain/common";
import { resourceReportsApi, type ResourceReport, type ResourceReportDetail } from "../../../api/resourceReports";
import { resourceCacheDisplay, resourceMetricDisplay } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import { resourceRetestsApi,type RetestCategory,type RetestOptions } from "../../../api/resourceRetests";

const props = defineProps<{ resourceID: string; currentReportID?: string }>();
const rows = ref<ResourceReport[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref("");
const selectedID = ref(""), detail = ref<ResourceReportDetail>(), detailLoading = ref(false), detailError = ref("");
const downloading = ref(""), fileError = ref(""), retestOpen = ref(false);
const retestOptions=ref<RetestOptions>(),retestLoading=ref(false),retestBusy=ref(false),retestError=ref(""),retestMessage=ref("");
const retestCategory=ref<RetestCategory>("baseline");
let retestRequest:AbortController|undefined,pollTimer:ReturnType<typeof setTimeout>|undefined,retestIntent=crypto.randomUUID();
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
  if(pollTimer)clearTimeout(pollTimer);
  listRequest?.abort(); const request = listRequest = new AbortController();
  loading.value = true; error.value = ""; rows.value = []; total.value = 0;
  try { const result = await resourceReportsApi.list(props.resourceID, page.value, request.signal); if (!request.signal.aborted) { rows.value = result.list; total.value = result.total; } }
  catch (value) { if (!request.signal.aborted) error.value = message(value); }
  finally { if (!request.signal.aborted) {loading.value = false;if(active&&!error.value&&rows.value.some(row=>row.status==="RUNNING"))pollTimer=setTimeout(()=>void load(),5000);} }
}
async function openRetest(){
 retestOpen.value=true;retestOptions.value=undefined;retestError.value="";retestMessage.value="";retestLoading.value=true;
 retestRequest?.abort();const request=retestRequest=new AbortController();
 try{const options=await resourceRetestsApi.options(props.resourceID,request.signal);if(active&&!request.signal.aborted)retestOptions.value=options;}
 catch(error){if(active&&!request.signal.aborted)retestError.value=error instanceof ApiError&&error.httpStatus===403?"当前账号没有有效的测试办理权限，请由当前办理人员发起。":error instanceof ApiError&&error.httpStatus===503?"平台内部测试配置尚未就绪，请联系部署人员。":error instanceof Error?error.message:"补测能力读取失败，请重试。";}
 finally{if(!request.signal.aborted)retestLoading.value=false;}
}
async function startRetest(){
 if(retestBusy.value||!retestOptions.value?.can_start)return;
 const id=props.resourceID;retestBusy.value=true;retestError.value="";retestMessage.value="";
 try{const receipt=await resourceRetestsApi.start(id,retestCategory.value,retestIntent);if(!active||props.resourceID!==id)return;retestMessage.value=`测试已受理，任务号 ${receipt.job_id}。报告会自动刷新。`;retestIntent=crypto.randomUUID();page.value=1;await load();}
 catch(error){if(active&&props.resourceID===id)retestError.value=error instanceof Error?error.message:"测试提交失败，请刷新报告确认任务状态。";}
 finally{retestBusy.value=false;}
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
watch(() => props.resourceID, () => { page.value = 1; closeDetail(); retestRequest?.abort();retestIntent=crypto.randomUUID();retestOpen.value = false; void load(); }, { immediate: true });
onBeforeUnmount(() => { active = false; listRequest?.abort(); detailRequest?.abort();retestRequest?.abort();if(pollTimer)clearTimeout(pollTimer); });
</script>

<template>
  <section class="resource-reports">
    <header class="report-tools"><h3>测试报告历史</h3><div><el-button size="small" :loading="loading" @click="load">刷新报告</el-button><el-button size="small" @click="openRetest">发起补测</el-button></div></header>
    <el-alert v-if="error" :title="error" type="warning" :closable="false"><el-button text @click="load">重试</el-button></el-alert>
    <el-table v-else v-loading="loading" :data="rows" empty-text="暂无测试报告" aria-label="资源测试报告历史">
      <el-table-column label="报告" min-width="120"><template #default="{ row }">{{ row.test_report_id || row.id }}<el-tag v-if="(row.test_report_id || (!row.test_job_id ? row.id : undefined)) === currentReportID && currentReportID" size="small">当前引用</el-tag></template></el-table-column>
      <el-table-column label="测试日期" min-width="160"><template #default="{ row }">{{ formatDateTime(row.tested_at) }}</template></el-table-column>
      <el-table-column label="测试项" min-width="120"><template #default="{ row }">{{ row.test_item }}</template></el-table-column>
      <el-table-column label="操作人" min-width="110"><template #default="{ row }">{{ row.operator_name || '未记录' }}</template></el-table-column>
      <el-table-column label="来源" min-width="100"><template #default="{ row }">{{ sourceText(row.source) }}</template></el-table-column>
      <el-table-column label="场景" min-width="140"><template #default="{ row }">{{ row.scene || '未记录' }}</template></el-table-column>
      <el-table-column label="状态 / 目标" min-width="130"><template #default="{ row }">{{ labels[row.status] }}<p :class="{ warning: !row.target_matches }">{{ row.target_matches ? '当前测试目标' : '目标待确认或已变化' }}</p></template></el-table-column>
      <el-table-column label="连通 / 压力" min-width="130"><template #default="{ row }">{{ resultText(row.connectivity_passed) }} / {{ resultText(row.pressure_passed) }}</template></el-table-column>
      <el-table-column label="详情" width="110"><template #default="{ row }"><el-button link type="primary" @click="openDetail(row.id)">查看本次测试</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total" v-model:current-page="page" :page-size="10" :total="total" layout="total, prev, pager, next" @current-change="load" />
    <el-dialog v-if="selectedID" :model-value="true" title="测试报告详情" width="min(720px, 94vw)" append-to-body @close="closeDetail">
      <div v-loading="detailLoading" class="report-detail"><el-alert v-if="detailError" :title="detailError" type="error" :closable="false"><el-button text @click="openDetail(selectedID)">重新加载报告</el-button></el-alert>
        <template v-if="detail"><p>报告 {{ detail.test_report_id || detail.id }}<span v-if="detail.test_job_id"> · 测试任务 {{ detail.test_job_id }}</span> · {{ labels[detail.status] }}</p><div class="report-meta"><div>测试日期<strong>{{ formatDateTime(detail.tested_at) }}</strong></div><div>测试项<strong>{{ detail.test_item }}</strong></div><div>操作人<strong>{{ detail.operator_name || '未记录' }}</strong></div><div>来源<strong>{{ sourceText(detail.source) }}</strong></div><div>场景<strong>{{ detail.scene || '未记录' }}</strong></div></div><el-alert v-if="!detail.target_matches" title="本报告尚未确认适用于当前资源目标，不作为当前接入依据。" type="warning" :closable="false" />
          <div class="report-metrics"><div>连通性<strong>{{ resultText(detail.connectivity_passed) }}</strong></div><div>压力测试<strong>{{ resultText(detail.pressure_passed) }}</strong></div><div>连通耗时 · ms<strong>{{ metricText(detail.metrics.connection_ms, detail.status) }}</strong></div><div>RPS<strong>{{ metricText(detail.metrics.rps, detail.status) }}</strong></div><div>TPS<strong>{{ metricText(detail.metrics.tps, detail.status) }}</strong></div><div>输入缓存命中率<strong>{{ cacheText(detail) }}</strong></div></div>
          <p v-if="detail.cache_stats.status === 'PROVIDED' && detail.cache_stats.hit_tokens != null && detail.cache_stats.input_tokens != null">命中输入 Token / 总输入 Token：{{ detail.cache_stats.hit_tokens }} / {{ detail.cache_stats.input_tokens }}</p><p class="muted">{{ detail.cache_stats.scene || '未提供缓存测试场景' }}</p>
          <h3>报告文件</h3><el-empty v-if="!detail.files.length" description="本报告未提供附件" :image-size="50" /><div v-for="file in detail.files" :key="file.id" class="report-file"><span>{{ file.name }}<small v-if="file.unavailable_reason">{{ file.unavailable_reason }}</small></span><el-button link type="primary" :disabled="!file.can_download || Boolean(downloading)" :loading="downloading === file.id" @click="download(file)">{{ file.can_download ? '下载' : '暂不可下载' }}</el-button></div>
        </template><el-alert v-if="fileError" :title="fileError" type="error" :closable="false" /></div>
      <template #footer><el-button @click="closeDetail">关闭报告</el-button></template>
    </el-dialog>
    <el-dialog v-model="retestOpen" title="资源补测" width="min(560px, 94vw)" append-to-body :close-on-click-modal="!retestBusy" :close-on-press-escape="!retestBusy" :show-close="!retestBusy">
      <p>内部补测使用平台内部接口与额度，由当前测试办理人员发起，无需重新填写供应商Key。</p>
      <div v-loading="retestLoading"><el-radio-group v-model="retestCategory" :disabled="retestBusy"><el-radio value="baseline">连通性测试</el-radio><el-radio value="stress">压力测试</el-radio></el-radio-group>
      <el-alert v-if="retestError" :title="retestError" type="warning" :closable="false"/><el-alert v-else-if="retestOptions&&!retestOptions.can_start" :title="retestOptions.reason" type="warning" :closable="false"/><el-alert v-if="retestMessage" :title="retestMessage" type="success" :closable="false"/></div>
      <p>任务进行中报告会自动刷新；测试结果不直接改变上下架状态或报价资格。</p>
      <template #footer><el-button :disabled="retestBusy" @click="retestOpen=false">关闭</el-button><el-button :disabled="retestBusy" :loading="retestLoading" @click="openRetest">重新检查</el-button><el-button type="primary" :loading="retestBusy" :disabled="retestLoading||!retestOptions?.can_start" @click="startRetest">提交测试</el-button></template>
    </el-dialog>
  </section>
</template>

<style scoped>.resource-reports{margin-top:24px;border-top:1px solid #e6e8ed;padding-top:12px}.report-tools,.report-file{display:flex;justify-content:space-between;align-items:center;gap:12px}.report-tools h3,h3{font-size:15px}.report-detail{min-height:140px}.report-meta{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:16px 0}.report-meta>div{background:#f8f9fb;border:1px solid #e6e8ed;border-radius:6px;padding:10px;color:#8a9099;font-size:12px}.report-meta strong{display:block;color:#30343b;font-size:13px;margin-top:5px}.report-metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin:20px 0}.report-metrics>div{background:#f8f9fb;border:1px solid #e6e8ed;border-radius:6px;padding:14px;color:#8a9099;font-size:12px}.report-metrics strong{display:block;color:#1c1d1f;font-size:17px;margin-top:8px}.report-file{padding:12px 0;border-bottom:1px solid #e6e8ed}.report-file small{display:block;color:#8a9099;margin-top:4px}.muted{color:#8a9099}.warning{color:#b87327}.el-pagination{margin-top:16px;justify-content:flex-end}.resource-reports p{line-height:1.7}@media(max-width:500px){.report-meta,.report-metrics{grid-template-columns:repeat(2,1fr)}.report-tools{align-items:flex-start;flex-direction:column}}</style>
