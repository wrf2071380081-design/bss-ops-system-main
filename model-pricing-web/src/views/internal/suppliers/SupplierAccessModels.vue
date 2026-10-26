<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO } from "../../../api/supplierEntities.contract";
import { f12SupplierRecordsApi } from "../../../api/f12SupplierRecords";
import type { F12ContractRecord, F12CreditRecord, F12CurrentCredit } from "../../../api/f12SupplierRecords.contract";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import { supplierQuotesApi } from "../../../api/supplierQuotes";
import type { QuotePendingDTO } from "../../../api/quoteContract.types";
import { supplierSourceOperationsApi } from "../../../api/supplierSourceOperations";
import type { SourceOperation } from "../../../api/sourceOperations.contract";
import { standardModelVersion } from "../../../api/standardModels.contract";
import type { ResourceRow } from "../../../api/resources.types";
import { formatDateTime } from "../../../domain/date";
import ResourceDetailDrawer from "../resources/ResourceDetailDrawer.vue";

const route = useRoute();
const router = useRouter();
const supplierID = computed(() => String(route.params.supplierId || ""));
const rows = ref<ResourceRow[]>([]);
const selectedID = ref("");
const loading = ref(false);
const error = ref("");
const activeTab = ref("models");
const profile = ref<SupplierEntityDTO>();
const contracts = ref<F12ContractRecord[]>([]), creditRecords = ref<F12CreditRecord[]>([]), currentCredits = ref<F12CurrentCredit[]>([]);
const profileLoading = ref(false), profileError = ref("");
const operationRows = ref<SourceOperation[]>([]), operationPage = ref(1), operationTotal = ref(0), operationLoading = ref(false), operationError = ref("");
const pendingQuotes = ref<QuotePendingDTO[]>([]), quotePage = ref(1), quoteTotal = ref(0), quoteLoading = ref(false), quoteError = ref("");
let request: AbortController | undefined;
let profileRequest: AbortController | undefined, operationRequest: AbortController | undefined, quoteRequest: AbortController | undefined;
let active = true;

const selected = computed(() => rows.value.find(row => row.id === selectedID.value));
const supplierName = computed(() => rows.value[0]?.supplier_name || profile.value?.name || `供应商 ${supplierID.value}`);
const stateLabel = (state: string) => resourceStateLabels[state] || state;
const regionLabel = (region: string) => resourceRegionLabels[region] || region;

function queryString(value: unknown) {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return "";
}

const returnContextKeys = [
  "return_keyword", "return_company", "return_model_type", "return_supplier_id", "return_supplier_name", "return_supplier_region",
  "return_region", "return_state", "return_page", "return_size", "return_sort_by", "return_order",
  "source_keyword", "source_state", "source_coefficient_min", "source_coefficient_max", "source_connection_max", "source_rps_min", "source_tps_min", "source_sort_by", "source_order",
];
function preservedReturnContext() {
  return Object.fromEntries(returnContextKeys.flatMap(key => {
    const value = queryString(route.query[key]);
    return value ? [[key, value]] : [];
  }));
}

function back() {
  const modelID = queryString(route.query.from_model_id);
  if (modelID) {
    try { void router.push({ path: `/internal/model-detail/${standardModelVersion(modelID)}`, query: preservedReturnContext() }); return; }
    catch { /* invalid return context falls back to model plaza */ }
  }
  void router.push({ path: "/internal/model-plaza", query: preservedReturnContext() });
}

function focus(row: ResourceRow) {
  selectedID.value = row.id;
  void router.replace({ query: { ...route.query, resource_id: row.id, model_id: row.standard_model_id, region: row.region } });
}

async function loadProfile() {
  profileRequest?.abort(); const current = profileRequest = new AbortController();
  profileLoading.value = true; profileError.value = "";
  try {
    const id = standardModelVersion(supplierID.value);
    const [entity, contractPage, creditPage] = await Promise.all([
      supplierEntitiesApi.get(id, current.signal), f12SupplierRecordsApi.contracts(id, current.signal), f12SupplierRecordsApi.credits(id, current.signal),
    ]);
    if (entity.id !== id || contractPage.list.some(item => item.subject_id !== id) || creditPage.list.some(item => item.subject_id !== id)) throw new Error("档案或合同授信记录与当前供应商不匹配。");
    if (active && !current.signal.aborted) { profile.value = entity; contracts.value = contractPage.list; creditRecords.value = creditPage.list; currentCredits.value = creditPage.current; }
  } catch (value) { if (active && !current.signal.aborted) profileError.value = value instanceof Error ? value.message : "供应商档案暂时无法加载。"; }
  finally { if (active && !current.signal.aborted) profileLoading.value = false; }
}

async function loadOperations() {
  operationRequest?.abort(); const current = operationRequest = new AbortController();
  operationLoading.value = true; operationError.value = "";
  try {
    const result = await supplierSourceOperationsApi.list(supplierID.value, operationPage.value, 20, current.signal);
    if (active && !current.signal.aborted) { operationRows.value = result.list; operationTotal.value = result.total; }
  } catch (value) { if (active && !current.signal.aborted) { operationRows.value = []; operationTotal.value = 0; operationError.value = value instanceof Error ? value.message : "供应商操作记录暂时无法加载。"; } }
  finally { if (active && !current.signal.aborted) operationLoading.value = false; }
}

async function loadQuotes() {
  quoteRequest?.abort(); const current = quoteRequest = new AbortController();
  quoteLoading.value = true; quoteError.value = "";
  try {
    const id = standardModelVersion(supplierID.value);
    const result = await supplierQuotesApi.list({ page: quotePage.value, size: 20, supplier_id: id }, current.signal);
    if (result.list.some(item => String(item.supplier_id) !== id)) throw new Error("报价结果包含其他供应商数据，已停止显示。");
    if (active && !current.signal.aborted) { pendingQuotes.value = result.list; quoteTotal.value = result.total; }
  } catch (value) { if (active && !current.signal.aborted) { pendingQuotes.value = []; quoteTotal.value = 0; quoteError.value = value instanceof Error ? value.message : "该供应商报价暂时无法加载。"; } }
  finally { if (active && !current.signal.aborted) quoteLoading.value = false; }
}

function onTabChange(tab: string | number) {
  if (tab === "profile") void loadProfile();
  else if (tab === "operations") void loadOperations();
  else if (tab === "quotes") void loadQuotes();
}

function operationLabel(row: SourceOperation) {
  const labels: Record<string, string> = { CREATE: "创建计划", REVISE: "修改计划", CANCEL: "取消计划", EXECUTE: "登记实际完成", BLOCK: "计划执行阻断", ADMIT: "登记接入", BIND_FORMAL_CONFIG: "绑定正式配置" };
  return labels[row.action] || row.action;
}

function openQuote(quote: QuotePendingDTO) { void router.push({ path: "/internal/supplier-quotes", query: { quote_id: String(quote.id) } }); }
function openOperation(row: SourceOperation) {
  const query = row.entity_type === "plan" ? { region: row.region, tab: "plans", plan_id: row.entity_id } : { region: row.region, tab: "records" };
  void router.push({ path: `/internal/model-plaza/${row.resource_id}`, query });
}

async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  rows.value = [];
  selectedID.value = "";
  try {
    const supplier = standardModelVersion(supplierID.value);
    const resource = standardModelVersion(queryString(route.query.resource_id));
    const model = standardModelVersion(queryString(route.query.model_id));
    const region = queryString(route.query.region);
    const query = buildResourceQuery({ view: "flat", page: 1, size: 100, sort_by: "supplier_name", order: "asc", supplier_id: supplier, region });
    const result = await resourcesApi.allRows(query, current.signal);
    if (!active || current.signal.aborted) return;
    const selectedRow = result.find(row => row.id === resource);
    if (!selectedRow || selectedRow.supplier_id !== supplier || selectedRow.standard_model_id !== model || selectedRow.region !== region) {
      throw new Error("当前供应来源已变化或与供应商不匹配，请从模型广场重新打开。");
    }
    rows.value = result;
    selectedID.value = selectedRow.id;
  } catch (value) {
    if (!active || current.signal.aborted) return;
    error.value = value instanceof Error ? value.message : "供应商模型列表暂时无法加载，请重试。";
  } finally {
    if (active && !current.signal.aborted) loading.value = false;
  }
}

function clearSelected() { selectedID.value = ""; }
onMounted(() => { void load(); });
watch(supplierID, () => { void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); profileRequest?.abort(); operationRequest?.abort(); quoteRequest?.abort(); });
</script>

<template>
  <section class="supplier-access-page">
    <el-button text class="back-button" @click="back">← 返回模型详情</el-button>
    <header class="page-heading">
      <div><div class="eyebrow">SUPPLIER / ACCESS MODELS</div><h1>{{ supplierName }}</h1><p>供应商接入模型与当前来源详情</p></div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>

    <section class="panel supplier-model-panel" v-loading="loading">
      <el-tabs v-model="activeTab" @tab-change="onTabChange">
        <el-tab-pane label="接入模型" name="models">
          <div class="model-context" v-if="selected">
            <div><span class="muted">当前选中来源</span><strong>{{ selected.model_name }} · {{ selected.supplier_model_id }}</strong></div>
            <el-tag>{{ regionLabel(selected.region) }}</el-tag>
          </div>
          <el-table :data="rows" row-key="id" highlight-current-row :current-row-key="selectedID" style="width:100%">
            <el-table-column label="标准模型 / 官方 ID" min-width="190"><template #default="{ row }"><strong>{{ row.model_name }}</strong><span class="muted">{{ row.official_id }}</span></template></el-table-column>
            <el-table-column label="供应商接口模型 ID" min-width="180"><template #default="{ row }"><code>{{ row.supplier_model_id }}</code></template></el-table-column>
            <el-table-column label="部署节点" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
            <el-table-column prop="coefficient" label="采购系数" width="110" />
            <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="row.state === 'ADMITTED' ? 'success' : 'info'" size="small">{{ stateLabel(row.state) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="110"><template #default="{ row }"><el-button size="small" @click="focus(row)">{{ row.id === selectedID ? "当前详情" : "查看详情" }}</el-button></template></el-table-column>
            <template #empty><el-empty description="该供应商暂无接入模型" :image-size="64" /></template>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="档案与办理" name="profile" lazy>
          <div v-loading="profileLoading" class="supplier-tab-content">
            <el-alert v-if="profileError" :title="profileError" type="error" :closable="false"><el-button text @click="loadProfile">重试</el-button></el-alert>
            <template v-if="profile">
              <h3>主体档案</h3>
              <dl class="supplier-profile"><dt>供应商名称</dt><dd>{{ profile.name }}</dd><dt>主体区域</dt><dd>{{ regionLabel(profile.region) }}</dd><dt>统一社会信用代码</dt><dd>{{ profile.registration_no || '未填写' }}</dd><dt>对接人</dt><dd>{{ profile.contact || '未填写' }}</dd><dt>联系电话</dt><dd>{{ profile.phone || '未填写' }}</dd><dt>税率</dt><dd>{{ (Number(profile.tax_rate) * 100).toFixed(2) }}%</dd><dt>结算币种</dt><dd>{{ profile.settlement_currency }}</dd><dt>归属采购人</dt><dd>人员 {{ profile.buyer_id }}</dd><dt>主体状态</dt><dd>{{ profile.status === 'ACTIVE' ? '正常' : '冻结' }} · 修订 {{ profile.revision }}</dd></dl>
              <el-alert title="共享测试 URL / Key 与办理节点尚未接入当前供应商主体读取契约；此处不显示模拟值。办理责任人与进度需等 B 流程事实接入。" type="info" :closable="false" />
              <h3>接入模型与办理状态</h3>
              <el-table :data="rows" size="small" empty-text="暂无供应资源"><el-table-column prop="model_name" label="模型" min-width="160"/><el-table-column prop="supplier_model_id" label="接口模型 ID" min-width="180"/><el-table-column label="状态" width="120"><template #default="{ row }">{{ stateLabel(row.state) }}</template></el-table-column><el-table-column label="办理节点"><template #default>待 B 流程数据接入</template></el-table-column></el-table>
            </template>
            <h3>合同与授信</h3>
            <el-table v-if="!profileError" :data="contracts" size="small" empty-text="暂无合同记录"><el-table-column prop="id" label="合同记录" width="110"/><el-table-column prop="version" label="版本" width="80"/><el-table-column label="状态" width="100"><template #default="{ row }">{{ row.status === 'ARCHIVED' ? '已归档' : '办理中' }}</template></el-table-column><el-table-column label="附件" width="90"><template #default="{ row }">{{ row.has_attachment ? '已上传' : '无' }}</template></el-table-column><el-table-column label="签署时间" min-width="170"><template #default="{ row }">{{ row.signed_at ? formatDateTime(row.signed_at) : '未记录' }}</template></el-table-column></el-table>
            <h4>当前授信</h4>
            <el-table :data="currentCredits" size="small" empty-text="暂无授信记录"><el-table-column prop="currency" label="币种" width="100"/><el-table-column label="状态" width="160"><template #default="{ row }">{{ row.status === 'AVAILABLE' ? '有效' : row.status === 'AMBIGUOUS_VERSION' ? '版本不明确' : '历史金额无效' }}</template></el-table-column><el-table-column label="额度"><template #default="{ row }">{{ row.record ? `${row.record.amount} ${row.record.currency}` : '不可确认' }}</template></el-table-column></el-table>
            <p class="muted">授信历史 {{ creditRecords.length }} 条。此页为当前可用字段；资料编号、附件下载和办理记录等需待 F12/B 提供方补齐。</p>
          </div>
        </el-tab-pane>
        <el-tab-pane label="操作记录" name="operations" lazy>
          <div v-loading="operationLoading" class="supplier-tab-content">
            <p class="muted">汇总该供应商名下资源和计划的操作摘要；不包含凭据或底层快照。</p>
            <el-alert v-if="operationError" :title="operationError" type="error" :closable="false"><el-button text @click="loadOperations">重试</el-button></el-alert>
            <el-table v-else :data="operationRows" empty-text="暂无操作记录"><el-table-column label="操作" min-width="150"><template #default="{ row }">{{ operationLabel(row) }}</template></el-table-column><el-table-column label="模型 / 资源" min-width="210"><template #default="{ row }"><strong>{{ row.supplier_model_id }}</strong><div class="muted">资源 {{ row.resource_id }} · {{ regionLabel(row.region) }}</div></template></el-table-column><el-table-column prop="operator_name" label="操作人" min-width="120"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column><el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column><el-table-column prop="reason" label="说明" min-width="180"/><el-table-column label="详情" width="90"><template #default="{ row }"><el-button link type="primary" @click="openOperation(row)">查看</el-button></template></el-table-column></el-table>
            <el-pagination v-if="operationTotal" v-model:current-page="operationPage" :page-size="20" :total="operationTotal" layout="total, prev, pager, next" @current-change="loadOperations" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="该供应商报价" name="quotes" lazy>
          <div v-loading="quoteLoading" class="supplier-tab-content">
            <p class="muted">当前报价接口提供该供应商的待审批报价。已处理报价历史需 C 侧历史查询接口接入。</p>
            <el-alert v-if="quoteError" :title="quoteError" type="error" :closable="false"><el-button text @click="loadQuotes">重试</el-button></el-alert>
            <el-table v-else :data="pendingQuotes" empty-text="当前没有待审批报价"><el-table-column prop="id" label="报价单" width="110"/><el-table-column prop="version_no" label="版本" width="80"/><el-table-column prop="item_count" label="明细数" width="100"/><el-table-column label="提交时间" min-width="180"><template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template></el-table-column><el-table-column label="状态" width="110"><template #default>待审批</template></el-table-column><el-table-column label="详情" width="100"><template #default="{ row }"><el-button link type="primary" @click="openQuote(row)">查看报价</el-button></template></el-table-column></el-table>
            <el-pagination v-if="quoteTotal" v-model:current-page="quotePage" :page-size="20" :total="quoteTotal" layout="total, prev, pager, next" @current-change="loadQuotes" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </section>

    <ResourceDetailDrawer v-if="selected && activeTab === 'models'" :key="`${selected.id}:${selected.region}`" :id="selected.id" :region="selected.region" presentation="page" :expected-supplier-i-d="selected.supplier_id" :expected-model-i-d="selected.standard_model_id" initial-tab="profile" @close="clearSelected" @changed="load" />
  </section>
</template>

<style scoped>
.supplier-access-page{color:var(--app-text-primary)}.back-button{margin-bottom:10px}.page-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:18px}.page-heading h1{margin:7px 0;font-size:26px}.page-heading p,.muted{color:var(--app-text-muted);font-size:12px}.eyebrow{color:var(--app-text-muted);font-size:11px;letter-spacing:.14em}.supplier-model-panel{padding:18px 20px}.model-context{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px 14px;margin-bottom:12px;background:#f6f8fb;border:1px solid #e6e8ed;border-radius:8px}.model-context strong,.model-context .muted{display:block}.model-context strong{margin-top:4px}.muted{display:block;margin-top:4px}.supplier-tab-content{min-height:220px;padding:12px 0}.supplier-tab-content h3{margin:20px 0 10px;font-size:16px}.supplier-tab-content h4{margin:18px 0 8px}.supplier-profile{display:grid;grid-template-columns:160px minmax(0,1fr);gap:10px 20px;padding:12px 0}.supplier-profile dt{color:var(--app-text-muted)}.supplier-profile dd{margin:0;overflow-wrap:anywhere}.el-pagination{margin-top:18px;justify-content:flex-end}code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:640px){.page-heading{align-items:flex-start}.supplier-model-panel{padding:12px}.supplier-profile{grid-template-columns:1fr;gap:6px}.supplier-profile dd{margin-bottom:10px}}
</style>
