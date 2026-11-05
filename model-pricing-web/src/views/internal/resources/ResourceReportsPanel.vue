<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiError } from "../../../domain/common";
import { resourceReportsApi, type ResourceReport, type ResourceReportDetail } from "../../../api/resourceReports";
import { apiRequest } from "../../../api/http";
import { standardModelVersion } from "../../../api/standardModels.contract";
import { resourceCacheDisplay, resourceMetricDisplay } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
import { resourceRetestsApi,type RetestCategory,type RetestOptions } from "../../../api/resourceRetests";

// 原型 §g02「测试与报告」：一个抽屉 / 面板，三个视图互相切换——历史列表 → 单条详情 → 上传专项报告。
// 不在这里嵌二级弹窗：原型是从列表点「查看」进详情、再「返回历史列表」，路径可逆。
const props = defineProps<{ resourceID: string; currentReportID?: string; modelName?: string }>();
const rows = ref<ResourceReport[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref("");
const view = ref<"history" | "detail" | "upload">("history");
const selectedID = ref(""), detail = ref<ResourceReportDetail>(), detailLoading = ref(false), detailError = ref("");
const downloading = ref(""), fileError = ref(""), retestOpen = ref(false);
const retestOptions=ref<RetestOptions>(),retestLoading=ref(false),retestBusy=ref(false),retestError=ref(""),retestMessage=ref("");
const retestCategory=ref<RetestCategory>("baseline");
// 原型「上传专项报告」：外部生成报告只做留档，不改变平台测试结论与准入资格。
const uploadFile=ref<File>(),uploadScene=ref(""),uploadBusy=ref(false),uploadError=ref("");
// 外部上传报告没有平台测试任务，单独列示；平台报告历史仍只认 test_job 证据链。
type UploadedReport={id:string;file_name:string;format:string;scene:string;uploaded_at:string;operator_name:string};
const uploaded=ref<UploadedReport[]>([]);
let uploadIntent=crypto.randomUUID();
async function loadUploaded(){
  try{
    const result=await apiRequest<{list?:unknown[]|null}>({url:`/internal/resources/${standardModelVersion(props.resourceID)}/uploaded-reports`});
    uploaded.value=(result?.list??[]).map(item=>{
      const row=(item??{}) as Record<string,unknown>;
      return {id:String(row.id??""),file_name:String(row.file_name??""),format:String(row.format??""),scene:String(row.scene??""),uploaded_at:String(row.uploaded_at??""),operator_name:String(row.operator_name??"")};
    }).filter(row=>row.id);
  }catch{uploaded.value=[];}
}
function openUpload(){view.value="upload";uploadFile.value=undefined;uploadScene.value="";uploadError.value="";uploadIntent=crypto.randomUUID();}
function pickFile(event:Event){const input=event.target as HTMLInputElement;uploadFile.value=input.files?.[0];}
function fileBase64(file:File){return new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>{const result=String(reader.result||"");resolve(result.includes(",")?result.split(",")[1]||"":"");};reader.onerror=()=>reject(new Error("文件读取失败，请重试。"));reader.readAsDataURL(file);});}
async function submitUpload(){
  const file=uploadFile.value;
  if(!file||uploadBusy.value)return;
  if(file.size>8*1024*1024){uploadError.value="报告文件不能超过 8MB。";return;}
  uploadBusy.value=true;uploadError.value="";
  try{
    const content=await fileBase64(file);
    await resourceReportsApi.upload(props.resourceID,{file_name:file.name,content_base64:content,...(uploadScene.value.trim()?{scene:uploadScene.value.trim()}:{})},uploadIntent);
    uploadIntent=crypto.randomUUID();view.value="history";page.value=1;await Promise.all([load(),loadUploaded()]);
  }catch(error){uploadError.value=error instanceof Error?error.message:"上传失败，请重试。";}
  finally{uploadBusy.value=false;}
}
let retestRequest:AbortController|undefined,pollTimer:ReturnType<typeof setTimeout>|undefined,retestIntent=crypto.randomUUID();
let listRequest: AbortController | undefined, detailRequest: AbortController | undefined, active = true;
const labels: Record<string, string> = { RUNNING: "测试中", COMPLETED: "已完成", FAILED: "执行失败" };
const resultText = (value: boolean | null) => value === null ? "未提供结论" : value ? "通过" : "不通过";
// 空 source 是平台自己的测试（供应商申报的准入测试也走这条链），不是「没记录」。
const sourceText = (value: ResourceReport["source"]) => value === "SUPPLIER" ? "供应商" : value === "INTERNAL" ? "内部" : value === "UPLOADED" ? "上传" : "平台测试";
const metricText = (value: string | null, status: ResourceReport["status"]) => value === null && status === "RUNNING" ? "测试中" : resourceMetricDisplay(value, "COMPLETED");
const cacheText = (report: ResourceReport) => report.cache_stats.status === "NOT_TESTED" ? report.status === "RUNNING" ? "测试中" : "未提供" : resourceCacheDisplay(report.cache_stats, "COMPLETED");

/** 测试项目：baseline / stress 翻成人话；专项上传用文件名当项目名。 */
const projectText = (item: string) => item === "baseline" ? "连通性" : item === "stress" ? "压力测试" : item || "测试";
/** 结果/内容一列（原型就是这样合的）：连通给 ms，压力给 RPS / TPS，都带结论。 */
function resultSummary(row: ResourceReport) {
  if (row.test_item === "stress") {
    const rps = metricText(row.metrics.rps, row.status), tps = metricText(row.metrics.tps, row.status);
    return `RPS ${rps} / TPS ${tps} · ${resultText(row.pressure_passed)}`;
  }
  if (row.test_item === "baseline") return `${metricText(row.metrics.connection_ms, row.status)} ms · ${resultText(row.connectivity_passed)}`;
  return `${resultText(row.connectivity_passed)} / ${resultText(row.pressure_passed)}`;
}
/** 测试场景：来源 + 场景（原型的「准入 · 模拟」是把来源与真实性合起来说）。 */
const sceneText = (row: ResourceReport) => [sourceText(row.source), row.scene].filter(Boolean).join(" · ");
const timeText = (value: string | null) => value ? formatDateTime(value) : "未记录";

type HistoryRow = {
  key: string; kind: "report" | "upload";
  project: string; scene: string; time: string; summary: string;
  report?: ResourceReport; upload?: UploadedReport;
};
/** 历史列表：平台报告与外部上传报告合成一张表（原型也是同一张表，靠「测试项目」区分）。 */
const historyRows = computed<HistoryRow[]>(() => {
  const merged: HistoryRow[] = [];
  for (const row of rows.value) {
    merged.push({ key: `report-${row.id}`, kind: "report", project: projectText(row.test_item), scene: sceneText(row), time: timeText(row.tested_at), summary: resultSummary(row), report: row });
  }
  // 上传报告没有分页，只在第一页与平台报告一起出现，避免翻页时重复。
  if (page.value === 1) {
    for (const row of uploaded.value) {
      merged.push({ key: `upload-${row.id}`, kind: "upload", project: row.file_name || "专项报告", scene: ["上传", row.scene].filter(Boolean).join(" · "), time: timeText(row.uploaded_at), summary: [row.format, row.operator_name ? `上传人 ${row.operator_name}` : ""].filter(Boolean).join(" · ") || "外部报告留档", upload: row });
    }
  }
  return merged.sort((a, b) => (Date.parse(b.time) || 0) - (Date.parse(a.time) || 0));
});

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
  view.value = "detail";
  selectedID.value = id; detail.value = undefined; detailError.value = ""; fileError.value = ""; detailLoading.value = true;
  try { const result = await resourceReportsApi.detail(props.resourceID, id, request.signal); if (!request.signal.aborted) detail.value = result; }
  catch (value) { if (!request.signal.aborted) detailError.value = message(value); }
  finally { if (!request.signal.aborted) detailLoading.value = false; }
}
function backToHistory() { detailRequest?.abort(); view.value = "history"; selectedID.value = ""; detail.value = undefined; }
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
watch(() => props.resourceID, () => { page.value = 1; backToHistory(); retestRequest?.abort();retestIntent=crypto.randomUUID();retestOpen.value = false; void load(); void loadUploaded(); }, { immediate: true });
onBeforeUnmount(() => { active = false; listRequest?.abort(); detailRequest?.abort();retestRequest?.abort();if(pollTimer)clearTimeout(pollTimer); });
</script>

<template>
  <section class="resource-reports">
    <!-- 抬头按原型：左标题，右侧是模型名 + 资源号 + 动作。 -->
    <header class="report-head">
      <h3>测试与报告</h3>
      <div class="report-head-right">
        <span v-if="modelName" class="muted">{{ modelName }}</span>
        <span class="muted">资源 #{{ resourceID }}</span>
        <el-button size="small" :loading="loading" @click="load">刷新报告</el-button>
        <el-button size="small" @click="openUpload">上传专项报告</el-button>
        <el-button size="small" @click="openRetest">发起补测</el-button>
      </div>
    </header>

    <template v-if="view === 'history'">
      <el-alert v-if="error" :title="error" type="warning" :closable="false"><el-button text @click="load">重试</el-button></el-alert>
      <el-table v-else v-loading="loading" :data="historyRows" empty-text="暂无测试报告" aria-label="资源测试报告历史">
        <el-table-column label="测试项目" min-width="150">
          <template #default="{ row }">
            <span :class="{ 'is-upload': row.kind === 'upload' }">{{ row.project }}</span>
            <el-tag v-if="row.report && (row.report.test_report_id || row.report.id) === currentReportID" size="small" class="ref-tag">当前引用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="测试场景" min-width="110"><template #default="{ row }">{{ row.scene }}</template></el-table-column>
        <el-table-column label="测试时间" min-width="150"><template #default="{ row }"><span class="mono">{{ row.time }}</span></template></el-table-column>
        <el-table-column label="结果 / 内容" min-width="220"><template #default="{ row }">{{ row.summary }}</template></el-table-column>
        <el-table-column label="查看" width="76">
          <template #default="{ row }">
            <el-button v-if="row.kind === 'report'" link type="primary" @click="openDetail(row.report!.id)">查看</el-button>
            <span v-else class="muted">仅留档</span>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination v-if="!error && total > 10" v-model:current-page="page" :page-size="10" :total="total" layout="total, prev, pager, next" @current-change="load" />
      <p class="muted foot">外部上传报告只作留档，不改变平台测试结论与准入资格。</p>
    </template>

    <!-- 详情是同一面板内的视图（原型：← 返回历史列表），不是再叠一层弹窗。 -->
    <div v-else-if="view === 'detail'" v-loading="detailLoading" class="report-detail">
      <button class="back-link" type="button" @click="backToHistory">← 返回历史列表</button>
      <el-alert v-if="detailError" :title="detailError" type="error" :closable="false"><el-button text @click="openDetail(selectedID)">重新加载报告</el-button></el-alert>
      <template v-if="detail">
        <h3 class="detail-title">{{ projectText(detail.test_item) }}<span v-if="modelName" class="detail-sub"> · {{ modelName }}</span></h3>
        <el-alert v-if="!detail.target_matches" title="本报告尚未确认适用于当前资源目标，不作为当前接入依据。" type="warning" :closable="false" />
        <dl class="g02-facts">
          <dt>报告状态</dt><dd>{{ labels[detail.status] }}<span v-if="(detail.test_report_id || detail.id) === currentReportID"> · 当前引用</span></dd>
          <dt>测试场景</dt><dd>{{ sceneText(detail) }}</dd>
          <dt>报告身份</dt><dd class="mono">{{ detail.test_report_id || detail.id }}</dd>
          <dt>原执行</dt><dd class="mono">{{ detail.test_job_id || "无（外部上传）" }}</dd>
          <dt>实际测试时间</dt><dd class="mono">{{ timeText(detail.tested_at) }}</dd>
          <dt>结果</dt><dd>{{ resultSummary(detail) }}</dd>
          <dt>执行身份</dt><dd>{{ detail.operator_name || "未记录" }}</dd>
          <dt>执行来源</dt><dd>{{ sourceText(detail.source) }}</dd>
          <dt>连通耗时</dt><dd class="mono">{{ detail.metrics.connection_ms === null ? "—" : `${metricText(detail.metrics.connection_ms, detail.status)} ms` }}</dd>
          <dt>RPS / TPS</dt><dd class="mono">{{ metricText(detail.metrics.rps, detail.status) }} / {{ metricText(detail.metrics.tps, detail.status) }}</dd>
          <dt>输入缓存命中率</dt><dd class="mono">{{ cacheText(detail) }}</dd>
          <dt>缓存命中 / 总输入</dt><dd class="mono">{{ detail.cache_stats.status === "PROVIDED" && detail.cache_stats.hit_tokens != null && detail.cache_stats.input_tokens != null ? `${detail.cache_stats.hit_tokens} / ${detail.cache_stats.input_tokens}` : "未提供" }}</dd>
          <dt>原资源</dt><dd class="mono">{{ detail.resource_id }}</dd>
        </dl>
        <p class="muted">{{ detail.cache_stats.scene || "未提供缓存测试场景" }}</p>
        <h4 class="section-title">原附件</h4>
        <el-empty v-if="!detail.files.length" description="本报告未提供附件" :image-size="50" />
        <div v-for="file in detail.files" :key="file.id" class="report-file">
          <span>{{ file.name }}<small v-if="file.unavailable_reason">{{ file.unavailable_reason }}</small></span>
          <el-button link type="primary" :disabled="!file.can_download || Boolean(downloading)" :loading="downloading === file.id" @click="download(file)">{{ file.can_download ? "查看附件" : "暂不可下载" }}</el-button>
        </div>
        <el-alert v-if="fileError" :title="fileError" type="error" :closable="false" />
      </template>
    </div>

    <!-- 上传专项报告也是面板内视图，与原型一致。 -->
    <div v-else class="report-upload">
      <button class="back-link" type="button" :disabled="uploadBusy" @click="view = 'history'">← 返回历史列表</button>
      <h3 class="detail-title">上传专项报告</h3>
      <p class="muted">外部生成的测试报告（如供应商自测、第三方测评）可直接登记留档。提交后只读，不改变资源的测试结论与准入资格；重新测试请用「发起补测」。</p>
      <el-form label-position="top">
        <el-form-item label="报告文件（PDF / XLSX / CSV / JSON / MD / ZIP，≤ 8MB）">
          <input type="file" accept=".pdf,.xlsx,.csv,.json,.md,.markdown,.zip" aria-label="报告文件" :disabled="uploadBusy" @change="pickFile" />
          <span v-if="uploadFile" class="upload-name">{{ uploadFile.name }}</span>
        </el-form-item>
        <el-form-item label="测试场景（选填）"><el-input v-model="uploadScene" :disabled="uploadBusy" placeholder="例如：供应商自测、第三方压测" aria-label="测试场景" /></el-form-item>
      </el-form>
      <el-alert v-if="uploadError" :title="uploadError" type="error" :closable="false" />
      <div class="upload-actions">
        <el-button :disabled="uploadBusy" @click="view = 'history'">取消</el-button>
        <el-button type="primary" :loading="uploadBusy" :disabled="!uploadFile" @click="submitUpload">上传并登记</el-button>
      </div>
    </div>

    <el-dialog v-model="retestOpen" title="资源补测" width="min(560px, 94vw)" append-to-body :close-on-click-modal="!retestBusy" :close-on-press-escape="!retestBusy" :show-close="!retestBusy">
      <p>内部补测使用平台内部接口与额度，由当前测试办理人员发起，无需重新填写供应商Key。</p>
      <div v-loading="retestLoading"><el-radio-group v-model="retestCategory" :disabled="retestBusy"><el-radio value="baseline">连通性测试</el-radio><el-radio value="stress">压力测试</el-radio></el-radio-group>
      <el-alert v-if="retestError" :title="retestError" type="warning" :closable="false"/><el-alert v-else-if="retestOptions&&!retestOptions.can_start" :title="retestOptions.reason" type="warning" :closable="false"/><el-alert v-if="retestMessage" :title="retestMessage" type="success" :closable="false"/></div>
      <p>任务进行中报告会自动刷新；测试结果不直接改变上下架状态或报价资格。</p>
      <template #footer><el-button :disabled="retestBusy" @click="retestOpen=false">关闭</el-button><el-button :disabled="retestBusy" :loading="retestLoading" @click="openRetest">重新检查</el-button><el-button type="primary" :loading="retestBusy" :disabled="retestLoading||!retestOptions?.can_start" @click="startRetest">提交测试</el-button></template>
    </el-dialog>
  </section>
</template>

<style scoped>
.resource-reports{margin-top:8px}
.report-head{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px}
.report-head h3{margin:0;font-size:15px}
.report-head-right{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.report-head-right .muted{font-size:12px}
.report-file{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:8px 0;border-bottom:1px solid #e6e8ed}
.report-file small{display:block;color:#8a9099;margin-top:4px}
.back-link{border:0;background:none;padding:0 0 10px;color:#3a6ff7;cursor:pointer;font-size:13px}
.back-link:disabled{color:#a8adb5;cursor:not-allowed}
.detail-title{margin:0 0 10px;font-size:15px}
.detail-sub{font-weight:400;color:#8a9099;font-size:13px}
.section-title{margin:16px 0 0;font-size:13px}
/* 事实清单：原型是 dt/dd 两列，比卡片堆更紧凑，字段多也不占高度。 */
.g02-facts{display:grid;grid-template-columns:150px 1fr;gap:8px 14px;margin:12px 0 10px;font-size:13px}
.g02-facts dt{color:#8a9099}
.g02-facts dd{margin:0;color:#1c1d1f;word-break:break-all}
.report-detail{min-height:160px}
.muted{color:#8a9099}
.foot{margin:10px 0 0;font-size:12px}
.upload-name{margin-left:10px;font-size:12px;color:#526970}
.upload-actions{display:flex;justify-content:flex-start;gap:10px;margin-top:20px}
.is-upload{color:#526970}
.ref-tag{margin-left:6px}
.el-pagination{margin-top:12px;justify-content:flex-end}
.mono{font-variant-numeric:tabular-nums}
@media(max-width:600px){.g02-facts{grid-template-columns:110px 1fr}.report-head{align-items:flex-start;flex-direction:column}}
</style>
