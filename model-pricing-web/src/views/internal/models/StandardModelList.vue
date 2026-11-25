<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { standardModelsApi } from "../../../api/standardModels";
import { factLabels, modelTypeLabels } from "../../../api/standardModels.contract";
import type { ModelFilterOptions, StandardFact, StandardModelDTO, StandardModelQuery } from "../../../api/standardModels.types";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import StandardModelMaintainDialog from "./StandardModelMaintainDialog.vue";
import StandardModelOperationRecords from "./StandardModelOperationRecords.vue";
import CatalogFilterSelect from "./CatalogFilterSelect.vue";
import { modelParameterLabel, modelPriceGroups, modelFactText } from "../../../api/standardModelPresentation";
import { agentCatalog, catalogVendors, type Source, type Task, type Vendor } from "../../../api/agentCatalog";
import { useSessionStore } from "../../../stores/session";
import CatalogScheduleDialog from "./CatalogScheduleDialog.vue";
import CatalogCrawlReview from "./CatalogCrawlReview.vue";
import type {ModelCrawlStatus} from '../../../api/agentCatalog';
const modelStatuses=ref<Record<string,ModelCrawlStatus>>({});
const modelStateLabels:Record<string,string>={COMPLETE:'完整抓取',PARTIAL:'部分成功',UNAVAILABLE:'读取失败',NOT_OBSERVED:'本批未观察到',RUNNING:'抓取中',UNKNOWN:'状态待核实'};
async function loadModelStatuses(list:StandardModelDTO[],signal:AbortSignal){
  const states:Record<string,ModelCrawlStatus>={};
  try {for(let start=0;start<list.length;start+=100)Object.assign(states,await agentCatalog.modelStatus(list.slice(start,start+100).map(m=>m.id),signal));}
  catch { /* Model records remain readable when collection history is unavailable. */ }
  if(!signal.aborted)modelStatuses.value=states;
}
const activeTab = ref("models");
const modelNames = ref<Record<string, string>>({});
const companyOptions = computed(() => options.value.companies.map(value => ({ label: value, value })));
const typeOptions = Object.entries(modelTypeLabels).map(([value, label]) => ({ label, value }));
const regionOptions = [{ label: "国内", value: "CN" }, { label: "海外", value: "OVERSEAS" }];
let keywordTimer: ReturnType<typeof setTimeout> | undefined;

const query = reactive<StandardModelQuery>({ page: 1, size: 100, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" });
const data = ref<{ list: StandardModelDTO[]; total: number }>(), loading = ref(false), error = ref(""), editorOpen = ref(false), editID = ref<string>();
const editorPanel = ref<'profile' | 'history' | 'records' | 'advanced'>('profile');
let request: AbortController | undefined;
let active = true;
const options = ref<ModelFilterOptions>({ companies: [], suppliers: [], has_more: false }), optionsLoading = ref(false), optionsError = ref("");
let optionsRequest: AbortController | undefined;
async function loadOptions(keyword = "") {
  optionsRequest?.abort(); const current = optionsRequest = new AbortController();
  optionsLoading.value = true; optionsError.value = "";
  try { const result = await standardModelsApi.options(keyword, [...(query.regions || [])], current.signal); if (active && !current.signal.aborted) options.value = result; }
  catch (value) { if (active && !current.signal.aborted) { options.value = { companies: [], suppliers: [], has_more: false }; optionsError.value = value instanceof Error ? value.message : "筛选选项暂时无法加载。"; } }
  finally { if (active && !current.signal.aborted) optionsLoading.value = false; }
}
function regionChanged() { query.supplier_ids = []; void loadOptions(); void load(true); }
function filtersChanged() { void load(true); }
function keywordChanged() { clearTimeout(keywordTimer); keywordTimer = setTimeout(() => { void load(true); }, 250); }
async function load(reset = false) {
  if (reset) query.page = 1;
  request?.abort(); const currentRequest = request = new AbortController();
  loading.value = true; error.value = "";
  const params = { ...query, companies: [...(query.companies || [])], model_types: [...(query.model_types || [])], supplier_ids: [...(query.supplier_ids || [])], regions: [...(query.regions || [])] };
  try {
    const list: StandardModelDTO[] = [], ids = new Set<string>(); let total: number | undefined;
    for (let page = 1; ; page++) {
      const result = await standardModelsApi.list({ ...params, page }, currentRequest.signal);
      if (!active || currentRequest.signal.aborted) return;
      if (result.page !== page || result.size !== params.size || result.list.length > params.size || (total !== undefined && total !== result.total)) throw new Error("模型资料已变化，请重新加载。");
      total = result.total;
      for (const model of result.list) { if (ids.has(model.id)) throw new Error("模型资料已变化，请重新加载。"); ids.add(model.id); list.push(model); }
      if (list.length === total) break;
      if (!result.list.length || list.length > total) throw new Error("模型资料分页不完整，请重新加载。");
    }
    data.value = { list, total: total! }; for (const model of list) modelNames.value[model.id] = model.name;
    modelStatuses.value={};void loadModelStatuses(list,currentRequest.signal);
  }
  catch (value) { if (active && !currentRequest.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。"; } }
  finally { if (active && !currentRequest.signal.aborted) loading.value = false; }
}
function reset() { clearTimeout(keywordTimer); Object.assign(query, { page: 1, size: 100, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" }); void load(); void loadOptions(); }
function open(model?: StandardModelDTO, panel: 'profile' | 'history' | 'records' | 'advanced' = 'profile') {
  // 读不出来的行没有可信资料，进详情只会再失败一次：直接提示原因，别让用户点进空白页。
  if (model?.load_error) { ElMessage.warning(model.load_error); return; }
  editID.value = model?.id; editorPanel.value = panel; editorOpen.value = true;
}
function openMore(model: StandardModelDTO, panel: unknown) {
  if (panel === "resource") { void openManualResource(model); return; }
  if (panel === 'history' || panel === 'records' || panel === 'advanced') open(model, panel);
}
// 零货源模型手工登记一条资源（运营兜底）。
// 落库是 PENDING_TEST，仍然要过测试与准入；这里只是省掉"必须先有供应商申报"这一步。
const resOpen = ref(false);
const resModel = ref<StandardModelDTO>();
const resSaving = ref(false);
const resError = ref("");
const resForm = reactive({ supplier_id: "", supplier_model_name: "", coefficient: "", region: "CN", reason: "" });
const resSuppliers = ref<{ id: string; name: string }[]>([]);
async function openManualResource(model: StandardModelDTO) {
  resModel.value = model;
  resError.value = "";
  resForm.supplier_id = "";
  resForm.supplier_model_name = model.official_id || model.name;
  resForm.coefficient = "";
  resForm.region = "CN";
  resForm.reason = "人工补录线下已确认的货源";
  resOpen.value = true;
  if (!resSuppliers.value.length) {
    try {
      const page = await supplierEntitiesApi.list({ page: 1, size: 200 });
      resSuppliers.value = page.list.map((item: { id: number | string; name: string }) => ({ id: String(item.id), name: item.name }));
    } catch { resSuppliers.value = []; }
  }
}
async function submitManualResource() {
  if (resSaving.value || !resModel.value) return;
  resError.value = "";
  resSaving.value = true;
  try {
    await standardModelsApi.manualResource(String(resModel.value.id), { ...resForm }, undefined);
    ElMessage.success("已登记一条资源（待测试）。测试与准入通过后才可用于报价");
    resOpen.value = false;
    await load();
  } catch (value) {
    resError.value = value instanceof Error ? value.message : "登记失败，请重试。";
  } finally { resSaving.value = false; }
}

const factText = modelFactText;
const session = useSessionStore();
const canCrawl = computed(() => session.permissionCodes.includes("M1:E"));
const crawlStorage = computed(() => "bss-catalog-crawl:" + session.user?.id);
const vendorIds = Object.keys(catalogVendors) as Vendor[];
function vendorFor(company: string) {
  const name = company.trim().toLowerCase();
  const aliases: Record<string, Vendor> = { alibaba: "aliyun", "alibaba cloud": "aliyun", "阿里巴巴": "aliyun", "智谱ai": "zhipu" };
  return aliases[name] ?? vendorIds.find(id => id === name || catalogVendors[id].toLowerCase() === name);
}
type PendingCrawl = { requestId: string; vendors: Vendor[]; sessionId?: string };
const batch = ref<PendingCrawl>(), task = ref<Task>(), crawlError = ref("");
const startOpen = ref(false), scheduleOpen = ref(false), starting = ref(false);
const selectedVendors = ref<Vendor[]>([]), reviewSources = ref<Source[]>([]);
const statusLabels: Record<string, string> = {
  RUNNING: "抓取中", SUCCEEDED: "抓取完成", UNKNOWN: "暂未确认结果", PARTIAL: "部分厂商成功", FAILED: "抓取失败", SUPERSEDED: "已有更新批次",
};
const runResults = computed(() => {
  const result = task.value?.result;
  return result ? "runs" in result ? result.runs : [{ vendor: "deepseek" as const, crawl: result }] : [];
});
const completedSources = computed(() => runResults.value.flatMap(({ vendor, crawl }) =>
  crawl?.status === "SUCCEEDED" ? [{ vendor, runId: crawl.runId }] : []));
const latestAttempt = computed(() => {
  const times = runResults.value.flatMap(({ crawl }) => crawl?.run?.startedAt ? [crawl.run.startedAt] : []);
  return times.sort().at(-1);
});
const latestSuccess = computed(() => {
  const times = runResults.value.flatMap(({ crawl }) => crawl?.status === "SUCCEEDED" && crawl.run?.finishedAt ? [crawl.run.finishedAt] : []);
  return times.sort().at(-1);
});
function modelRun(model: StandardModelDTO) {
  return runResults.value.find(item => item.vendor === vendorFor(model.company))?.crawl;
}
const taskRunning = computed(() => task.value?.status === "RUNNING" || (Boolean(batch.value?.sessionId) && !task.value && !crawlError.value));
let crawlPoll: ReturnType<typeof setTimeout> | undefined;
let crawlRequest: AbortController | undefined;
function saveBatch(value: PendingCrawl) {
  batch.value = value;
  sessionStorage.setItem(crawlStorage.value, JSON.stringify(value));
}
function openStart() {
  if (batch.value && !batch.value.sessionId) {
    selectedVendors.value = batch.value.vendors.slice();
  } else {
    const filtered = (query.companies || []).map(vendorFor).filter((vendor): vendor is Vendor => !!vendor);
    selectedVendors.value = query.companies?.length ? [...new Set(filtered)] : vendorIds.slice();
  }
  startOpen.value = true;
}
async function track(id: string, openReview: boolean, failures = 0) {
  clearTimeout(crawlPoll);
  crawlRequest?.abort();
  const controller = crawlRequest = new AbortController();
  try {
    const result = await agentCatalog.task(id, controller.signal);
    if (controller.signal.aborted) return;
    if (!result.task) throw Error("暂时无法读取该采集任务。");
    task.value = result.task;
    crawlError.value = "";
    if (["RUNNING", "UNKNOWN"].includes(result.task.status)) {
      crawlPoll = setTimeout(() => void track(id, openReview), result.task.status === "RUNNING" ? 2200 : 8000);
    } else if (openReview && completedSources.value.length && !scheduleOpen.value && !reviewSources.value.length) {
      reviewSources.value = completedSources.value;
    }
  } catch (cause) {
    if (controller.signal.aborted) return;
    crawlError.value = cause instanceof Error ? cause.message : "读取采集进度失败。";
    if (failures < 3) crawlPoll = setTimeout(() => void track(id, openReview, failures + 1), 4000);
  }
}
async function startCrawl() {
  if (starting.value || !selectedVendors.value.length) return;
  const randomId = () => {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
    return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join("-");
  };
  const pending = batch.value && !batch.value.sessionId ? batch.value : { requestId: randomId(), vendors: selectedVendors.value.slice() };
  saveBatch(pending);
  task.value = undefined;
  starting.value = true;
  crawlError.value = "";
  const controller = crawlRequest = new AbortController();
  try {
    const { sessionId } = await agentCatalog.start(pending.requestId, pending.vendors, controller.signal);
    if (controller.signal.aborted) return;
    saveBatch({ ...pending, sessionId });
    startOpen.value = false;
    void track(sessionId, true);
  } catch (cause) {
    if (!controller.signal.aborted) {
      crawlError.value = cause instanceof Error ? cause.message : "暂未取得受理回执，可重试确认。";
      if (cause instanceof ApiError && [400, 401, 403, 404].includes(cause.httpStatus)) {
        batch.value = undefined;
        sessionStorage.removeItem(crawlStorage.value);
      }
    }
  } finally {
    starting.value = false;
  }
}
function openScheduledReview(sources: Source[]) {
  scheduleOpen.value = false;
  reviewSources.value = sources;
}
watch([crawlStorage, canCrawl], () => {
  clearTimeout(crawlPoll);
  crawlRequest?.abort();
  task.value = undefined; batch.value = undefined; crawlError.value = "";
  startOpen.value = false; scheduleOpen.value = false; reviewSources.value = [];
  if (!canCrawl.value) return;
  try {
    const saved = JSON.parse(sessionStorage.getItem(crawlStorage.value) || "null") as PendingCrawl | null;
    if (saved) {
      batch.value = saved;
      if (saved.sessionId) void track(saved.sessionId, false);
    }
  } catch {
    sessionStorage.removeItem(crawlStorage.value);
  }
}, { immediate: true });
onMounted(() => { void load(); void loadOptions(); });
onBeforeUnmount(() => { active = false; clearTimeout(keywordTimer); request?.abort(); optionsRequest?.abort(); clearTimeout(crawlPoll); crawlRequest?.abort(); });
</script>

<template>
  <section class="standard-library">
    <el-tabs v-model="activeTab" class="catalog-tabs"><el-tab-pane label="标准模型" name="models" /><el-tab-pane label="操作记录" name="records" /></el-tabs>
    <section v-if="activeTab === 'records'" class="catalog-panel records-panel"><header class="catalog-heading"><h2>操作记录</h2></header><StandardModelOperationRecords  /></section>
    <template v-else>
      <div class="crawl-status" role="status">
        <span>上次成功抓取：{{ formatDateTime(latestSuccess, '暂无成功记录', false) }}</span>
        <span>最近尝试：{{ formatDateTime(latestAttempt, '尚未执行', false) }}</span>
        <span v-if="starting">正在发起采集…</span>
        <span v-if="task">{{ statusLabels[task.status] || '进度待确认' }} · 成功 {{ completedSources.length }}/{{ batch?.vendors.length ?? runResults.length }} 家</span>
        <span v-else-if="taskRunning">正在读取采集进度…</span>
        <el-button v-if="crawlError && batch?.sessionId" size="small" @click="track(batch.sessionId, true)">重新读取进度</el-button>
        <span v-if="crawlError" class="crawl-unavailable">{{ crawlError }}</span>
        <div class="crawl-actions">
        <el-button size="small" @click="scheduleOpen = true">定时抓取</el-button>
        <el-button v-if="canCrawl" size="small" :disabled="taskRunning || starting" @click="openStart">模型抓取</el-button>
        <el-button v-if="completedSources.length" size="small" @click="reviewSources = completedSources">查看变更与审核</el-button>
        </div>
        <el-button v-if="canCrawl" class="add-model" type="primary" size="small" @click="open()">＋ 新增模型</el-button>
        <div v-if="runResults.length" class="crawl-vendors"><el-tag v-for="{ vendor, crawl } in runResults" :key="vendor" size="small"
          :type="crawl?.status === 'SUCCEEDED' ? 'success' : crawl?.status === 'FAILED' ? 'danger' : 'info'">
          {{ catalogVendors[vendor] }} · {{ statusLabels[crawl?.status ?? 'UNKNOWN'] || '待确认' }}
        </el-tag></div>
      </div>
      <section class="catalog-panel">
      <form class="catalog-filters" @submit.prevent="load(true)">
        <CatalogFilterSelect v-model="query.companies!" label="模型公司" :options="companyOptions" :loading="optionsLoading" remote @search="loadOptions" @change="filtersChanged" />
        <CatalogFilterSelect v-model="query.model_types!" label="模型类型" :options="typeOptions" @change="filtersChanged" />
        <CatalogFilterSelect v-model="query.regions!" label="部署节点" :options="regionOptions" @change="regionChanged" />
        <el-input v-model="query.keyword" class="catalog-search" clearable aria-label="搜索模型名称或官方ID" placeholder="搜索模型名称 / 官方 ID" @input="keywordChanged" />
        <el-button @click="reset">重置</el-button>
      </form>
      <el-alert v-if="optionsError" :title="optionsError" type="warning" :closable="false"><el-button text @click="loadOptions()">重新加载筛选选项</el-button></el-alert>
      <p v-if="options.has_more" class="options-hint">还有更多选项，请输入名称缩小搜索范围。</p>
      <div class="catalog-note"><span>{{ data ? `${data.total} 个模型 · 官方参数快照` : '官方参数快照' }}</span><span>价格币种和特殊单位逐项标注 · 未核实 ≠ 不支持 · 内容随页面宽度排列，窄屏支持横向滚动</span></div>
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load()">重新加载</el-button></el-alert>
      <div v-loading="loading" class="official-scroll" tabindex="0" role="region" aria-label="官方模型参数表，可横向滚动">
        <table class="official-table"><thead><tr>
          <th scope="col">模型名称</th><th scope="col">模型类型 / 输入输出</th>
          <th scope="col">容量规格</th><th scope="col">核心能力</th><th scope="col">官方价格 / 计费单位</th><th scope="col">API / 访问概况</th><th scope="col">版本 / 生命周期</th><th scope="col">最近更新结果</th><th scope="col">上次成功</th><th scope="col">操作</th>
        </tr></thead><tbody>
          <tr v-for="model in data?.list || []" :key="model.id">
            <td><strong>{{ model.name || model.official_id || '待补充名称' }}</strong><el-tag v-if="model.load_error" class="load-error-tag" size="small" type="danger" effect="plain" :title="model.load_error">资料异常</el-tag></td>
            <td><span class="model-type">{{ modelTypeLabels[model.model_type] || '类型待补齐' }}</span><p class="spec-line">输入：{{ model.input_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p><p class="spec-line">输出：{{ model.output_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p></td>
            <td><p v-for="fact in model.capacity" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ fact.label || modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.capacity.length" class="unknown">待补齐</span></td>
            <td><p v-for="ability in model.abilities" :key="ability.key" class="spec-line" :class="{ unknown: !['SUPPORTED', 'UNSUPPORTED'].includes(ability.state) }">{{ modelParameterLabel(ability.key) }}：{{ ability.state === 'SUPPORTED' ? '支持' : ability.state === 'UNSUPPORTED' ? '不支持' : ability.state === 'UNDISCLOSED' ? '未披露' : '未核实' }}<span v-if="ability.description">（{{ ability.description }}）</span></p><span v-if="!model.abilities.length" class="unknown">待补齐</span></td>
            <td class="price-cell" :title="model.load_error || '点击编辑官方价格与计费项'" @click="open(model)"><div v-for="group in modelPriceGroups(model.price_items)" :key="group.key" class="price-group"><p v-for="line in group.lines" :key="line" class="spec-line">{{ line }}</p><p class="spec-line">{{ group.unit }}</p><p v-if="group.condition" class="spec-line price-condition">{{ group.condition }}</p><p v-for="value in group.dimensions" :key="value" class="spec-line">{{ value }}</p></div><span v-if="!model.price_items.length" class="unknown">{{ model.load_error ? '资料异常' : '未核实' }}</span></td>
            <td><p v-for="fact in model.api_access" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ fact.label || modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.api_access.length" class="unknown">待补齐</span></td>
            <td><p v-for="fact in model.lifecycle" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ fact.label || modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.lifecycle.length" class="unknown">待补齐</span></td>
            <td><span class="update-state">{{ modelStateLabels[modelStatuses[model.id]?.state || 'UNKNOWN'] }}</span></td>
            <td><span>{{ formatDateTime(modelStatuses[model.id]?.lastCompleteAt, '暂无完整成功记录', false) }}</span><span class="meta">资料核验：{{ model.checked_at || '未记录' }}</span></td>
            <td><el-button size="small" :disabled="!!model.load_error" :title="model.load_error || '查看并维护模型资料'" @click="open(model)">详情</el-button></td>
          </tr>
          <tr v-if="!loading && !error && !data?.list.length"><td colspan="10" class="empty-row">当前筛选暂无模型</td></tr>
        </tbody></table>
      </div>

      </section>
    </template>
    <StandardModelMaintainDialog v-if="editorOpen" :id="editID" :initial-panel="editorPanel" @close="editorOpen = false" @saved="load()" />
  </section>

  <el-dialog v-model="startOpen" title="选择抓取厂商" width="min(480px, 94vw)" :show-close="!starting"
    :close-on-press-escape="!starting" :close-on-click-modal="false">
    <p>采集完成后，可在当前页核对变化并审核。</p>
    <el-checkbox-group v-model="selectedVendors" :disabled="starting || !!(batch && !batch.sessionId)">
      <el-checkbox v-for="(name, id) in catalogVendors" :key="id" :value="id">{{ name }}</el-checkbox>
    </el-checkbox-group>
    <el-alert v-if="crawlError" :title="crawlError" :closable="false" type="error" />
    <template #footer><el-button :disabled="starting" @click="startOpen = false">关闭</el-button>
      <el-button type="primary" :loading="starting" :disabled="!selectedVendors.length" @click="startCrawl">
        {{ batch && !batch.sessionId ? '重试确认' : '开始抓取' }}
      </el-button></template>
  </el-dialog>
  <CatalogScheduleDialog v-if="scheduleOpen" @close="scheduleOpen = false" @review="openScheduledReview" />
  <CatalogCrawlReview v-if="reviewSources.length" :sources="reviewSources" @close="reviewSources = []" @saved="load()" />

  <!-- 登记货源：零货源模型手工补一条资源（待测试，不等于准入） -->
  <el-dialog v-model="resOpen" :title="`登记货源 · ${resModel?.name || ''}`" width="min(560px, 94vw)">
    <el-alert type="info" :closable="false" show-icon
      title="登记后资源状态为「待测试」，仍需通过测试与准入才可用于报价；已有货源的模型请到供应商侧维护。" />
    <el-alert v-if="resError" :title="resError" type="error" :closable="false" show-icon />
    <el-form label-position="top">
      <el-form-item label="供应商">
        <el-select v-model="resForm.supplier_id" filterable placeholder="选择供应商" style="width: 100%">
          <el-option v-for="item in resSuppliers" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="接口模型名（供应商侧的调用名）">
        <el-input v-model="resForm.supplier_model_name" maxlength="128" placeholder="例如 deepseek-v3" />
      </el-form-item>
      <el-form-item label="采购系数（相对官方价的折扣）">
        <el-input v-model="resForm.coefficient" inputmode="decimal" placeholder="例如 0.5200" />
      </el-form-item>
      <el-form-item label="部署节点">
        <el-select v-model="resForm.region" style="width: 100%">
          <el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" />
        </el-select>
      </el-form-item>
      <el-form-item label="登记说明（必填，会进操作记录）">
        <el-input v-model="resForm.reason" maxlength="200" placeholder="例如：线下已确认，先补录以便走测试" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button :disabled="resSaving" @click="resOpen = false">取消</el-button>
      <el-button type="primary" :loading="resSaving" @click="submitManualResource">登记</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
/* Standard-model layout and metrics follow Token-Ops-v0.1.0. */
.standard-library{background:#fff;color:#1c1d1f;--el-color-primary:#315ee8}
.library-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:16px}
.library-heading h1{font-size:22px;margin:0 0 7px}
.library-heading p{color:#8a9099;font-size:12px;margin:0}
.catalog-tabs{margin-bottom:16px;padding:16px 10px 0}
.records-panel{margin:0 10px 16px}.records-panel .catalog-heading{padding:18px 22px}
.catalog-tabs:deep(.el-tabs__header){margin:0}
.catalog-tabs:deep(.el-tabs__item){font-size:13px;height:44px;font-weight:500}
.catalog-tabs:deep(.el-tabs__nav-wrap:after){height:1px}
.crawl-status{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:12px 14px;border:1px solid #e6e8eb;border-radius:6px;background:#fafbfc;font-size:12px;margin-bottom:16px}
.crawl-unavailable{color:#9a761d}
.catalog-panel{border:1px solid #e6e8eb;background:#fff;border-radius:8px;overflow:hidden}
.catalog-heading{padding:14px 16px;border-bottom:1px solid #e6e8eb}
.catalog-heading h2{font-size:15px;font-weight:600;margin:0;line-height:20px}
.catalog-filters{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:12px 16px;border-bottom:1px solid #e6e8eb}
.catalog-search{flex:1;min-width:180px;width:220px}
.catalog-filters>.el-button{margin:0}
.options-hint{margin:0;padding:8px 16px;font-size:12px;color:#8d929a}
.catalog-note{display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap;padding:10px 16px;font-size:12px;color:#8d929a}
.official-scroll{width:100%;overflow:auto;min-height:180px;overscroll-behavior:contain}
.official-table{width:100%;min-width:1150px;border-collapse:collapse;table-layout:fixed;font-size:12px;line-height:1.5;font-variant-numeric:tabular-nums}
.official-table th,.official-table td{padding:12px;text-align:left;vertical-align:top;border-bottom:1px solid #eceef1;overflow-wrap:anywhere;white-space:normal}
.official-table th{background:#edf3f3;color:#536a70;font-size:12px;font-weight:500}
.official-table th:first-child{width:12%}
.official-table th:nth-child(5){width:15%}
.official-table th:nth-last-child(2){width:13%}
.official-table th:last-child{width:76px}
.official-table th:first-child,.official-table td:first-child{position:sticky;left:0;z-index:1;border-right:1px solid #eceef1}
.official-table td{background:white}
.official-table tbody tr:nth-child(even) td{background:#f8fafb}
.official-table tbody tr:hover td{background:#f4f7fa}
.official-table strong{display:block;font-size:13px;font-weight:700}
.official-table code{display:block;margin:5px 0;font-size:11px;color:#617a82;word-break:break-word}
.spec-line{margin:3px 0;line-height:1.5;white-space:pre-line}
.update-state{display:inline-block;font-size:10px;padding:2px 6px;border-radius:5px;background:#f0f1f4;color:#727985;margin-bottom:4px;line-height:18px}
.meta{display:block;font-size:12px;color:#8b9098;margin-top:3px;line-height:16px}
.unknown,.price-condition{color:#8b9299}
.price-group+.price-group{margin-top:10px}
.official-table td.empty-row{text-align:center;padding:40px 20px;color:#8b9098}
.load-error-tag{margin-left:6px;vertical-align:middle;cursor:help}
.catalog-pagination{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;padding:10px 16px;font-size:12px;color:#91969f}
.catalog-pagination :deep(.el-pagination){font-weight:400}
:deep(.el-button--primary){--el-button-bg-color:#315ee8;--el-button-border-color:#315ee8}
@media(max-width:900px){.official-table{min-width:1100px}
}
@media(max-width:700px){.library-heading{align-items:flex-start;flex-wrap:wrap}
.catalog-search{flex-basis:100%}
.catalog-note{align-items:flex-start}
.catalog-filters:deep(.catalog-select){width:158px}
}

.price-cell { cursor: pointer; }

.price-cell:hover { background: #f5f9ff; }

.crawl-vendors{flex-basis:100%;display:flex;gap:6px;flex-wrap:wrap}


.model-type{display:inline-block;font-size:10px;line-height:18px;background:#f0f1f4;border-radius:5px;padding:2px 6px;color:#727985;margin-bottom:4px}

.crawl-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}

.crawl-status :deep(.el-button){margin-left:0}

.crawl-status :deep(.add-model){margin-left:auto}

.standard-library :deep(.el-button){border-radius:6px;font-weight:400}

.official-table :deep(.el-button),.crawl-status :deep(.el-button),.catalog-filters>.el-button{height:28px;padding:4px 9px;font-size:12px}

.catalog-search :deep(.el-input__wrapper){min-height:34px;font-size:12px}
</style>
