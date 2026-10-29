<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { customerWorkspaceApi, type CustomerApprovalRow, type CustomerQuoteRow, type CustomerWorkspaceRow } from "../../../api/customerWorkspace";
import { customersApi } from "../../../api/customers";
import type { CustomerTransferImpactDTO } from "../../../api/customers.types";
import { customerQuotesApi } from "../../../api/customerQuotes";
import { quoteCandidatesApi, type ResourceQuoteCandidate } from "../../../api/quoteCandidates";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { isDecimalAmount } from "../../../domain/money";
import { PERMISSIONS } from "../../../domain/permissions";
import { usePermissionStore } from "../../../stores/permission";
import { useSessionStore } from "../../../stores/session";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";

const permissions = usePermissionStore();
const session = useSessionStore();
const canTransfer = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_EDIT));
const canQuote = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_EDIT));

const message = (value: unknown) => value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";
const positiveId = (value: string) => /^[1-9]\d*$/.test(value.trim()) && Number.isSafeInteger(Number(value));

const activeTab = ref<"list" | "quote">("list");
const quoteView = ref<"edit" | "drafts" | "exported" | "logs">("edit");

function unique(values: string[]) {
  return Array.from(new Set(values.filter(Boolean)));
}
const regionText = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
const currencyText = (currency: string) => currency === "USD" ? "美元 USD" : "人民币 CNY";
const currencySymbol = (currency: string) => currency === "USD" ? "$" : "¥";
function amountText(currency: string, amount: string) {
  const value = Number(amount || 0);
  if (!Number.isFinite(value)) return amount;
  return `${currencySymbol(currency)}${value.toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: 0 })}`;
}

// ---------------- 客户列表 ----------------
const rows = ref<CustomerWorkspaceRow[]>([]);
const loading = ref(false);
const loadError = ref("");

const filters = reactive<{
  names: string[]; owners: string[]; regions: string[]; contracts: string[];
  services: string[]; currencies: string[]; stages: string[]; handlings: string[];
}>({ names: [], owners: [], regions: [], contracts: [], services: [], currencies: [], stages: [], handlings: [] });

const options = computed(() => ({
  names: unique(rows.value.map(row => row.name)),
  owners: unique(rows.value.map(row => row.owner_name)),
  regions: unique(rows.value.map(row => regionText(row.region))),
  contracts: unique(rows.value.map(row => row.contract_status)),
  services: unique(rows.value.map(row => row.service_status)),
  currencies: unique(rows.value.map(row => currencyText(row.settlement_currency))),
  stages: unique(rows.value.map(row => row.cooperation_stage)),
  handlings: unique(rows.value.map(row => row.handling_status)),
}));

const filteredRows = computed(() => rows.value.filter(row => {
  if (filters.names.length && !filters.names.includes(row.name)) return false;
  if (filters.owners.length && !filters.owners.includes(row.owner_name)) return false;
  if (filters.regions.length && !filters.regions.includes(regionText(row.region))) return false;
  if (filters.contracts.length && !filters.contracts.includes(row.contract_status)) return false;
  if (filters.services.length && !filters.services.includes(row.service_status)) return false;
  if (filters.currencies.length && !filters.currencies.includes(currencyText(row.settlement_currency))) return false;
  if (filters.stages.length && !filters.stages.includes(row.cooperation_stage)) return false;
  if (filters.handlings.length && !filters.handlings.includes(row.handling_status)) return false;
  return true;
}));

async function loadCustomers() {
  loading.value = true;
  loadError.value = "";
  try {
    // 原型列表是单页滚动，不分页；一次取够后前端过滤。
    const result = await customerWorkspaceApi.customers({ page: 1, size: 200 });
    rows.value = result.list;
  } catch (error) {
    rows.value = [];
    loadError.value = message(error);
  } finally {
    loading.value = false;
  }
}
function resetFilters() {
  Object.assign(filters, { names: [], owners: [], regions: [], contracts: [], services: [], currencies: [], stages: [], handlings: [] });
}

// ---------------- 报价单 ----------------
const quoteRows = ref<CustomerQuoteRow[]>([]);
const quoteLoading = ref(false);
const quoteError = ref("");
const approvalRows = ref<CustomerApprovalRow[]>([]);

const form = reactive({
  customerId: "",
  customerName: "",
  ourCompany: "根公司",
  region: "CN",
  currency: "CNY",
  quoteDate: new Date().toISOString().slice(0, 10),
  operator: session.user?.name || "—",
  quoteNo: "",
  remark: "",
});

// 报价行按供应资源口径：一条行 = 一个资源的一个官方价项。
interface QuoteLine {
  resourceId: string;
  priceItemKey: string;
  priceItemLabel: string;
  modelName: string;
  supplierName: string;
  region: string;
  coefficient: string;
  currency: string;
  /** 采购成本（官方价 × 采购系数），毛利率以此为基数。 */
  basePrice: string;
  margin: string;
  price: string;
}

const lines = ref<QuoteLine[]>([]);
const marginAll = ref("0.10");
// 候选资源（供应域已判可报价）。资源口径报价不再走价目表：基准价 = 官方价 × 采购系数。
const candidateTotal = ref(0);

const customerOptions = computed(() => rows.value.map(row => ({
  id: String(row.id), name: row.name, region: row.region,
  currency: row.settlement_currency, owner: row.owner_name,
})));
const filteredCustomers = computed(() => customerOptions.value);

function onCustomerChange(customerId: string) {
  const option = customerOptions.value.find(item => item.id === customerId);
  if (!option) { form.customerName = ""; return; }
  form.customerName = option.name;
  form.region = option.region;
  form.currency = option.currency;
}

// 关联客户变化时不再重算基准价——资源口径的基准价来自候选资源的采购成本。

function startNewQuote(row?: CustomerWorkspaceRow) {
  activeTab.value = "quote";
  quoteView.value = "edit";
  form.customerId = row ? String(row.id) : "";
  form.customerName = "";
  form.region = row?.region || "CN";
  form.currency = row?.settlement_currency || "CNY";
  form.quoteNo = "";
  form.remark = "";
  form.quoteDate = new Date().toISOString().slice(0, 10);
  lines.value = [];
  if (row) onCustomerChange(String(row.id));
}

// priceFromMargin 纯函数：报价单价 = 采购成本 ÷ (1 − 毛利率)。
// 与服务端 floor 同一口径（floor = 成本 ÷ (1 − 最低毛利)），保证填出来的价不会天然破线。
function priceFromMargin(base: string, margin: string) {
  if (!isDecimalAmount(base)) return "";
  const baseValue = Number(base);
  const marginValue = Number(margin);
  if (!Number.isFinite(baseValue) || !Number.isFinite(marginValue)) return "";
  if (marginValue >= 1 || marginValue < 0) return "";
  const computedValue = baseValue / (1 - marginValue);
  if (!Number.isFinite(computedValue)) return "";
  return computedValue.toFixed(8).replace(/0+$/, "").replace(/\.$/, "");
}
function applyPrice(line: QuoteLine) {
  line.price = line.price || priceFromMargin(line.basePrice, line.margin);
}
function applyMarginAll() {
  if (!isDecimalAmount(marginAll.value)) { ElMessage.error("统一毛利率必须是合法十进制数，例如 0.15"); return; }
  if (!lines.value.length) { ElMessage.warning("请先添加模型"); return; }
  lines.value = lines.value.map(line => ({ ...line, margin: marginAll.value, price: priceFromMargin(line.basePrice, marginAll.value) }));
  ElMessage.success(`已按毛利率 ${marginAll.value} 统一 ${lines.value.length} 个模型`);
}
function removeLine(resourceId: string, priceItemKey: string) {
  lines.value = lines.value.filter(line => !(line.resourceId === resourceId && line.priceItemKey === priceItemKey));
}
function lineKey(line: QuoteLine) { return `${line.resourceId}:${line.priceItemKey}`; }

// 添加/选择资源
const pickerVisible = ref(false);
const pickerKeyword = ref("");
const pickerLoading = ref(false);
const pickerError = ref("");
const pickerRows = ref<ResourceQuoteCandidate[]>([]);
// 选中的是「资源:价项」，一条资源有多个官方价项时可分别报价。
const pickerSelected = reactive(new Set<string>());
const pickerPage = ref(1);
const pickerTotal = ref(0);
let pickerSequence = 0;

async function openPicker() {
  pickerVisible.value = true;
  pickerKeyword.value = "";
  pickerSelected.clear();
  pickerRows.value = [];
  await searchCandidates(false);
}
async function searchCandidates(append: boolean) {
  const sequence = ++pickerSequence;
  pickerLoading.value = true;
  pickerError.value = "";
  const page = append ? pickerPage.value + 1 : 1;
  try {
    const result = await quoteCandidatesApi.list(pickerKeyword.value.trim(), page, 20);
    if (sequence !== pickerSequence) return;
    pickerRows.value = append ? [...pickerRows.value, ...result.list] : result.list;
    pickerPage.value = page;
    pickerTotal.value = result.total;
    candidateTotal.value = result.total;
  } catch (error) {
    if (sequence === pickerSequence) pickerError.value = message(error);
  } finally {
    if (sequence === pickerSequence) pickerLoading.value = false;
  }
}
function togglePick(resourceId: number, priceItemKey: string) {
  const key = `${resourceId}:${priceItemKey}`;
  if (pickerSelected.has(key)) pickerSelected.delete(key); else pickerSelected.add(key);
}
function confirmPicker() {
  const next = [...lines.value];
  pickerRows.value.forEach(row => {
    row.price_items.forEach(item => {
      const key = `${row.resource_id}:${item.item_key}`;
      if (!pickerSelected.has(key)) return;
      if (next.some(line => lineKey(line) === key)) return;
      next.push({
        resourceId: String(row.resource_id), priceItemKey: item.item_key, priceItemLabel: item.label,
        modelName: row.model_name, supplierName: row.supplier_name, region: row.region,
        coefficient: row.coefficient, currency: item.currency,
        basePrice: item.unit_cost, margin: marginAll.value,
        price: priceFromMargin(item.unit_cost, marginAll.value),
      });
    });
  });
  lines.value = next;
  pickerVisible.value = false;
}

function validateBeforeSubmit() {
  if (!positiveId(form.customerId)) return "请先选择关联客户。";
  if (!lines.value.length) return "请至少添加一个模型。";
  const bad = lines.value.find(line => !positiveId(line.resourceId) || !isDecimalAmount(line.price));
  if (bad) return `${bad.modelName} / ${bad.priceItemLabel} 的报价单价必须是大于 0 的普通十进制字符串。`;
  return "";
}

const submitting = ref(false);
async function saveDraft(exportAfter: boolean) {
  const invalid = validateBeforeSubmit();
  if (invalid) { ElMessage.error(invalid); return; }
  if (submitting.value) return;
  submitting.value = true;
  try {
    const validTo = new Date(Date.now() + 30 * 24 * 3600 * 1000).toISOString();
    const result = await customerQuotesApi.generateResource({
      customerId: form.customerId,
      validTo,
      items: lines.value.map(line => ({ resourceId: line.resourceId, priceItemKey: line.priceItemKey, unitPrice: line.price })),
    });
    form.quoteNo = `#${result.id}`;
    if (exportAfter) {
      const blob = await customerQuotesApi.exportXlsx(result.id);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `customer_quote_${result.id}.xlsx`;
      link.click();
      URL.revokeObjectURL(url);
      ElMessage.success(`报价单 #${result.id} 已生成并导出。`);
    } else {
      ElMessage.success(`报价草稿 #${result.id} 已保存。`);
    }
    await loadQuotes();
  } catch (error) {
    ElMessage.error(message(error));
  } finally {
    submitting.value = false;
  }
}

async function loadQuotes() {
  quoteLoading.value = true;
  quoteError.value = "";
  try {
    const result = await customerWorkspaceApi.quotes({ page: 1, size: 200 });
    quoteRows.value = result.list;
    approvalRows.value = await customerWorkspaceApi.approvals();
  } catch (error) {
    quoteRows.value = [];
    quoteError.value = message(error);
  } finally {
    quoteLoading.value = false;
  }
}

const draftRows = computed(() => quoteRows.value.filter(row => row.status === "DRAFT" || row.status === "PENDING" || row.status === "APPROVED"));
const exportedRows = computed(() => quoteRows.value.filter(row => row.status === "EFFECTIVE" || row.status === "EXPIRED" || row.status === "FORMAL"));
// 报价操作日志：当前没有独立审计表，用报价单自身的时间/状态/操作人派生（不编造动作）。
const logRows = computed(() => quoteRows.value.map(row => ({
  id: row.id, customer: row.customer_name, version: row.version_no,
  status: row.status, operator: row.owner_name, at: row.created_at,
})));

const statusText = (status: string) => ({ EFFECTIVE: "生效", DRAFT: "草稿", PENDING: "待审批", APPROVED: "已批准", EXPIRED: "已过期", REJECTED: "已驳回", FORMAL: "正式" } as Record<string, string>)[status] || status;
const quoteTypeText = (type: string) => ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", RESOURCE: "按资源报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;
const approvalStatusText = (status: string) => ({ PENDING: "待审批", APPROVED: "已通过", REJECTED: "已驳回" } as Record<string, string>)[status] || status;
const approvalStatusType = (status: string) => status === "APPROVED" ? "success" : status === "REJECTED" ? "danger" : "warning";

function switchQuoteView(view: "edit" | "drafts" | "exported" | "logs") {
  quoteView.value = view;
  if (view !== "edit" && !quoteRows.value.length) void loadQuotes();
}

function openRowQuote(row: CustomerQuoteRow) {
  activeTab.value = "quote";
  quoteView.value = "edit";
  form.customerId = String(row.customer_id);
  form.customerName = row.customer_name;
  form.quoteNo = `#${row.id}`;
  lines.value = [];
  void onCustomerChange(String(row.customer_id));
}

// ---------------- 详情与移交 ----------------
const detailVisible = ref(false);
const detailRow = ref<CustomerWorkspaceRow>();
const canManageF12 = computed(() => Boolean(detailRow.value && session.user?.staffId && String(detailRow.value.owner_sales_operator_id) === String(session.user.staffId)));
function openDetail(row: CustomerWorkspaceRow) { detailRow.value = row; detailVisible.value = true; }

const transferVisible = ref(false);
const transferSubmitting = ref(false);
const transferTarget = ref<CustomerWorkspaceRow>();
const transferForm = reactive({ toOperatorId: "", reason: "" });
const transferImpact = ref<CustomerTransferImpactDTO>();
const transferError = ref("");
let transferSequence = 0;

function openTransfer(row: CustomerWorkspaceRow) {
  ++transferSequence;
  transferTarget.value = row;
  transferSubmitting.value = false;
  Object.assign(transferForm, { toOperatorId: "", reason: "" });
  transferImpact.value = undefined;
  transferError.value = "";
  transferVisible.value = true;
}
function invalidateTransferPreview() { ++transferSequence; transferImpact.value = undefined; transferError.value = ""; }
async function previewTransfer() {
  if (!transferTarget.value || transferSubmitting.value) return;
  if (!positiveId(transferForm.toOperatorId)) { transferError.value = "请输入有效的目标销售人员 ID。"; return; }
  const sequence = ++transferSequence;
  const customerId = String(transferTarget.value.id);
  const draft = { toOperatorId: transferForm.toOperatorId.trim(), reason: transferForm.reason.trim() };
  transferSubmitting.value = true;
  transferError.value = "";
  try {
    const impact = await customersApi.transferPreview(customerId, draft);
    if (sequence === transferSequence) transferImpact.value = impact;
  } catch (error) {
    if (sequence === transferSequence) { transferImpact.value = undefined; transferError.value = message(error); }
  } finally {
    if (sequence === transferSequence) transferSubmitting.value = false;
  }
}
async function confirmTransfer() {
  if (!transferTarget.value || !transferImpact.value || transferSubmitting.value) return;
  const customerId = String(transferTarget.value.id);
  const impact = transferImpact.value;
  const draft = { toOperatorId: transferForm.toOperatorId.trim(), reason: transferForm.reason.trim() };
  transferSubmitting.value = true;
  try {
    await ElMessageBox.confirm(`确认将客户从“${impact.fromOperatorName}”移交给“${impact.toOperatorName}”？${impact.quoteCount} 份报价将随迁。`, "确认客户移交", { type: "warning" });
  } catch { transferSubmitting.value = false; return; }
  transferError.value = "";
  try {
    const result = await customersApi.transferConfirm(customerId, draft);
    transferVisible.value = false;
    ElMessage.success(`客户移交完成，已迁移 ${result.quotesMigrated} 份报价。`);
    await loadCustomers();
  } catch (error) {
    transferError.value = message(error);
  } finally {
    transferSubmitting.value = false;
  }
}

onMounted(() => { void loadCustomers(); void loadQuotes(); });
</script>

<template>
  <section class="customer-page">
    <div class="tabbar">
      <div class="tabbar-left">
        <button class="tab" :class="{ active: activeTab === 'list' }" @click="activeTab = 'list'">客户列表</button>
        <button class="tab" :class="{ active: activeTab === 'quote' }" @click="activeTab = 'quote'">客户报价单</button>
      </div>
      <el-button v-if="activeTab === 'list' && canQuote" type="primary" @click="startNewQuote()">新建报价</el-button>
    </div>

    <!-- ================= 客户列表 ================= -->
    <template v-if="activeTab === 'list'">
      <div class="filterbar">
        <el-select v-model="filters.names" multiple filterable clearable collapse-tags placeholder="客户名称" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.names" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.owners" multiple filterable clearable collapse-tags placeholder="销售" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.owners" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.regions" multiple filterable clearable collapse-tags placeholder="国内/海外" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.regions" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.contracts" multiple filterable clearable collapse-tags placeholder="合同状态" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.contracts" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.services" multiple filterable clearable collapse-tags placeholder="服务状态" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.services" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.currencies" multiple filterable clearable collapse-tags placeholder="结算币种" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.currencies" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.stages" multiple filterable clearable collapse-tags placeholder="合作阶段" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.stages" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.handlings" multiple filterable clearable collapse-tags placeholder="办理状态" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.handlings" :key="item" :label="item" :value="item" />
        </el-select>
        <el-button text @click="resetFilters">重置</el-button>
      </div>

      <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false">
        <el-button text @click="loadCustomers">重新加载</el-button>
      </el-alert>

      <el-table v-loading="loading" :data="filteredRows" row-key="id" class="grid">
        <el-table-column type="selection" width="44" />
        <el-table-column label="客户名称" min-width="200">
          <template #default="{ row }">
            <div class="cell-main">{{ row.name }}</div>
            <div class="cell-sub">销售 {{ row.owner_name }}</div>
          </template>
        </el-table-column>
        <el-table-column label="国内/海外" width="100">
          <template #default="{ row }">{{ regionText(row.region) }}</template>
        </el-table-column>
        <el-table-column label="授信额度" width="150" align="right">
          <template #default="{ row }">
            <div class="cell-main mono">{{ amountText(row.settlement_currency, row.credit_limit) }}</div>
            <div class="cell-sub">我方 → 客户</div>
          </template>
        </el-table-column>
        <el-table-column label="合同信息" width="130">
          <template #default="{ row }">
            <el-tag size="small" :type="row.contract_status === '已归档' ? 'success' : row.contract_status === '办理中' ? 'warning' : 'info'" class="stack-tag">{{ row.contract_status }}</el-tag>
            <el-tag size="small" :type="row.service_status === '服务中' ? 'success' : row.service_status === '接入中' ? 'warning' : 'info'" class="stack-tag">{{ row.service_status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="结算币种" width="120">
          <template #default="{ row }">{{ currencyText(row.settlement_currency) }}</template>
        </el-table-column>
        <el-table-column label="税率" width="100">
          <template #default="{ row }">{{ row.tax_rate ? `${row.tax_rate}%` : "未填写" }}</template>
        </el-table-column>
        <el-table-column label="合作阶段" width="100">
          <template #default="{ row }">{{ row.cooperation_stage }}</template>
        </el-table-column>
        <el-table-column label="办理状态" width="140">
          <template #default="{ row }">{{ row.handling_status }}</template>
        </el-table-column>
        <el-table-column label="详情" width="80" fixed="right">
          <template #default="{ row }"><el-button link type="primary" @click="openDetail(row)">详情</el-button></template>
        </el-table-column>
        <template #empty><el-empty description="没有符合条件的客户" :image-size="64" /></template>
      </el-table>
    </template>

    <!-- ================= 客户报价单 ================= -->
    <template v-else>
      <div class="subtabbar">
        <div class="tabbar-left">
          <button class="tab" :class="{ active: quoteView === 'edit' }" @click="switchQuoteView('edit')">报价</button>
          <button class="tab" :class="{ active: quoteView === 'drafts' }" @click="switchQuoteView('drafts')">报价草稿</button>
          <button class="tab" :class="{ active: quoteView === 'exported' }" @click="switchQuoteView('exported')">已导出报价</button>
          <button class="tab" :class="{ active: quoteView === 'logs' }" @click="switchQuoteView('logs')">报价操作日志</button>
        </div>
        <el-button v-if="canQuote" @click="startNewQuote()">新建报价单</el-button>
      </div>

      <template v-if="quoteView === 'edit'">
        <div class="linked">
          <label class="linked-label">关联客户</label>
          <el-select v-model="form.customerId" filterable clearable placeholder="搜索客户名称" class="linked-input" @change="onCustomerChange">
            <el-option v-for="item in filteredCustomers" :key="item.id" :label="`${item.name} · ${regionText(item.region)} · ${item.owner}`" :value="item.id" />
          </el-select>
          <span class="linked-hint">每份报价关联一个客户；新客户首次成功导出时入库</span>
        </div>

        <div class="card">
          <h3 class="card-title">报价信息</h3>
          <div class="form-grid">
            <div class="field"><label>客户名称</label><el-input v-model="form.customerName" disabled placeholder="选择关联客户后自动带出" /></div>
            <div class="field"><label>我方公司</label><el-input v-model="form.ourCompany" disabled /></div>
            <div class="field">
              <label>国内/海外业务区域</label>
              <el-select v-model="form.region" placeholder="请选择">
                <el-option value="CN" label="国内" /><el-option value="OVERSEAS" label="海外" />
              </el-select>
            </div>
            <div class="field"><label>结算币种</label><el-input :model-value="form.customerId ? currencyText(form.currency) : '待确认'" disabled /></div>
            <div class="field"><label>报价时间</label><el-input v-model="form.quoteDate" disabled /></div>
            <div class="field"><label>操作人</label><el-input v-model="form.operator" disabled /></div>
            <div class="field"><label>报价编号</label><el-input v-model="form.quoteNo" disabled placeholder="导出时自动生成" /></div>
            <div class="field"><label>报价说明</label><el-input v-model="form.remark" maxlength="200" /></div>
          </div>
        </div>

        <div class="card">
          <div class="card-head">
            <div class="card-head-left">
              <el-button type="primary" :disabled="!canQuote" @click="openPicker">+ 添加/选择资源</el-button>
              <span class="muted">已选 {{ lines.length }} 个价项</span>
              <span class="muted">· 候选来自已接入且可报价的资源</span>
            </div>
            <div class="card-head-right">
              <span class="muted">统一毛利率</span>
              <el-input v-model="marginAll" class="margin-input" placeholder="输入数值" />
              <span class="muted">%</span>
              <el-button @click="applyMarginAll">统一当前全部价项</el-button>
            </div>
          </div>

          <el-table v-if="lines.length" :data="lines" row-key="lineKey" class="grid">
            <el-table-column label="模型 / 供应商" min-width="200">
              <template #default="{ row }">
                <div>{{ row.modelName }}</div>
                <div class="muted">{{ row.supplierName }} · {{ regionText(row.region) }}</div>
              </template>
            </el-table-column>
            <el-table-column label="价项" width="130"><template #default="{ row }">{{ row.priceItemLabel }}</template></el-table-column>
            <el-table-column label="采购系数" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template></el-table-column>
            <el-table-column label="采购成本" width="140" align="right">
              <template #default="{ row }"><span class="mono">{{ row.basePrice || "—" }} {{ row.currency }}</span></template>
            </el-table-column>
            <el-table-column label="毛利率" width="120">
              <template #default="{ row }"><el-input v-model="row.margin" size="small" @change="applyPrice(row)" /></template>
            </el-table-column>
            <el-table-column label="报价单价" width="160">
              <template #default="{ row }"><el-input v-model="row.price" size="small" placeholder="必填" /></template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }"><el-button link type="danger" @click="removeLine(row.resourceId, row.priceItemKey)">移除</el-button></template>
            </el-table-column>
          </el-table>
          <div v-else class="empty-hint">点击添加模型，在弹窗中搜索并勾选</div>

          <div class="card-foot">
            <span class="foot-note">统一为一次性操作，随后可逐行修改；新增行不自动继承。报价单价按「采购成本 ÷ (1 − 毛利率)」计算，与服务端最低毛利红线同一口径；仅用于填单辅助，最终以服务端生成结果为准。</span>
            <div class="foot-actions">
              <el-button :loading="submitting" :disabled="!canQuote" @click="saveDraft(false)">保存草稿</el-button>
              <el-button type="primary" :loading="submitting" :disabled="!canQuote" @click="saveDraft(true)">导出报价单</el-button>
            </div>
          </div>
        </div>
      </template>

      <template v-else-if="quoteView === 'drafts'">
        <el-alert v-if="quoteError" :title="quoteError" type="error" show-icon :closable="false" />
        <el-table v-loading="quoteLoading" :data="draftRows" row-key="id" class="grid">
          <el-table-column label="报价编号" width="120"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="客户名称" min-width="200"><template #default="{ row }">{{ row.customer_name }}</template></el-table-column>
          <el-table-column label="版本" width="80"><template #default="{ row }">V{{ row.version_no }}</template></el-table-column>
          <el-table-column label="类型" width="120"><template #default="{ row }">{{ quoteTypeText(row.quote_type) }}</template></el-table-column>
          <el-table-column label="状态" width="100"><template #default="{ row }">{{ statusText(row.status) }}</template></el-table-column>
          <el-table-column label="模型数" width="90"><template #default="{ row }">{{ row.item_count }}</template></el-table-column>
          <el-table-column label="报价时间" width="170"><template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template></el-table-column>
          <el-table-column label="操作" width="120"><template #default="{ row }"><el-button link type="primary" @click="openRowQuote(row)">继续编辑</el-button></template></el-table-column>
          <template #empty><el-empty description="暂无报价草稿" :image-size="64" /></template>
        </el-table>
      </template>

      <template v-else-if="quoteView === 'exported'">
        <el-table v-loading="quoteLoading" :data="exportedRows" row-key="id" class="grid">
          <el-table-column label="报价编号" width="120"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="客户名称" min-width="200"><template #default="{ row }">{{ row.customer_name }}</template></el-table-column>
          <el-table-column label="版本" width="80"><template #default="{ row }">V{{ row.version_no }}</template></el-table-column>
          <el-table-column label="状态" width="100"><template #default="{ row }">{{ statusText(row.status) }}</template></el-table-column>
          <el-table-column label="模型数" width="90"><template #default="{ row }">{{ row.item_count }}</template></el-table-column>
          <el-table-column label="有效期至" width="170"><template #default="{ row }">{{ row.valid_until ? formatDateTime(row.valid_until, "—", false) : "—" }}</template></el-table-column>
          <el-table-column label="报价时间" width="170"><template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template></el-table-column>
          <template #empty><el-empty description="暂无已导出报价" :image-size="64" /></template>
        </el-table>
      </template>

      <template v-else>
        <el-table v-loading="quoteLoading" :data="logRows" row-key="id" class="grid">
          <el-table-column label="时间" width="180"><template #default="{ row }">{{ formatDateTime(row.at, "—", false) }}</template></el-table-column>
          <el-table-column label="报价编号" width="120"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="客户名称" min-width="200"><template #default="{ row }">{{ row.customer }}</template></el-table-column>
          <el-table-column label="版本 / 状态" width="150"><template #default="{ row }">V{{ row.version }} · {{ statusText(row.status) }}</template></el-table-column>
          <el-table-column label="操作人" width="130"><template #default="{ row }">{{ row.operator }}</template></el-table-column>
          <template #empty><el-empty description="暂无报价记录" :image-size="64" /></template>
        </el-table>
        <el-table v-if="approvalRows.length" :data="approvalRows" row-key="id" class="grid approval-grid">
          <el-table-column label="特价变更单" width="130"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="客户名称" min-width="200"><template #default="{ row }">{{ row.customer_name }}</template></el-table-column>
          <el-table-column label="关联报价" width="120"><template #default="{ row }">{{ row.quote_id ? `#${row.quote_id}` : "—" }}</template></el-table-column>
          <el-table-column label="审批结果" width="110">
            <template #default="{ row }"><el-tag size="small" :type="approvalStatusType(row.status)">{{ approvalStatusText(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="申请原因" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.reason || "—" }}</template></el-table-column>
          <el-table-column label="提交时间" width="170"><template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template></el-table-column>
        </el-table>
      </template>
    </template>

    <!-- 添加/选择资源 -->
    <el-dialog v-model="pickerVisible" title="添加/选择资源" width="min(880px, 96vw)">
      <div class="picker-bar">
        <el-input v-model="pickerKeyword" placeholder="搜索模型 / 供应商 / 官方 ID" clearable @keyup.enter="searchCandidates(false)" />
        <el-button type="primary" @click="searchCandidates(false)">搜索</el-button>
        <span class="muted">已勾选 {{ pickerSelected.size }} 个价项</span>
      </div>
      <el-alert v-if="pickerError" :title="pickerError" type="error" show-icon :closable="false" />
      <el-table v-loading="pickerLoading" :data="pickerRows" row-key="resource_id" max-height="380">
        <el-table-column type="expand">
          <template #default="{ row }">
            <el-table :data="row.price_items" size="small" style="margin: 8px 0 4px 46px">
              <el-table-column width="48">
                <template #default="{ row: item }">
                  <el-checkbox :model-value="pickerSelected.has(`${row.resource_id}:${item.item_key}`)" @change="togglePick(row.resource_id, item.item_key)" />
                </template>
              </el-table-column>
              <el-table-column prop="label" label="价项" min-width="120" />
              <el-table-column label="官方价" width="130" align="right"><template #default="{ row: item }"><span class="mono">{{ item.amount }}</span></template></el-table-column>
              <el-table-column label="采购成本" width="130" align="right"><template #default="{ row: item }"><span class="mono">{{ item.unit_cost }}</span></template></el-table-column>
              <el-table-column prop="currency" label="币种" width="80" />
            </el-table>
          </template>
        </el-table-column>
        <el-table-column label="模型" min-width="180">
          <template #default="{ row }">
            <div>{{ row.model_name }}</div>
            <div class="muted">{{ row.company }} · {{ row.official_id }}</div>
          </template>
        </el-table-column>
        <el-table-column label="供应商" min-width="140">
          <template #default="{ row }">
            <div>{{ row.supplier_name }}</div>
            <div class="muted">{{ regionText(row.region) }} · {{ row.supplier_model_id }}</div>
          </template>
        </el-table-column>
        <el-table-column label="采购系数" width="100" align="right">
          <template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template>
        </el-table-column>
        <el-table-column label="价项数" width="80" align="right">
          <template #default="{ row }">{{ row.price_items.length }}</template>
        </el-table-column>
        <template #empty><el-empty description="没有可报价的资源（需已接入、正式配置已确认、测试证据有效）" :image-size="64" /></template>
      </el-table>
      <el-button v-if="pickerRows.length < pickerTotal" text :loading="pickerLoading" @click="searchCandidates(true)">加载更多</el-button>
      <template #footer>
        <el-button @click="pickerVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmPicker">加入报价（{{ pickerSelected.size }}）</el-button>
      </template>
    </el-dialog>

    <!-- 客户详情 -->
    <el-dialog v-model="detailVisible" title="客户详情" width="min(980px, 96vw)">
      <el-descriptions v-if="detailRow" :column="2" border>
        <el-descriptions-item label="客户名称">{{ detailRow.name }}</el-descriptions-item>
        <el-descriptions-item label="客户 ID">{{ detailRow.id }}</el-descriptions-item>
        <el-descriptions-item label="销售">{{ detailRow.owner_name }}</el-descriptions-item>
        <el-descriptions-item label="客户等级">{{ detailRow.level_code }}</el-descriptions-item>
        <el-descriptions-item label="国内/海外">{{ regionText(detailRow.region) }}</el-descriptions-item>
        <el-descriptions-item label="结算币种">{{ currencyText(detailRow.settlement_currency) }}</el-descriptions-item>
        <el-descriptions-item label="授信额度 / 已用">
          <span class="mono">{{ amountText(detailRow.settlement_currency, detailRow.credit_limit) }} / {{ detailRow.credit_used }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="税率">{{ detailRow.tax_rate ? `${detailRow.tax_rate}%` : "未填写" }}</el-descriptions-item>
        <el-descriptions-item label="合同状态">{{ detailRow.contract_status }}</el-descriptions-item>
        <el-descriptions-item label="服务状态">{{ detailRow.service_status }}</el-descriptions-item>
        <el-descriptions-item label="合作阶段">{{ detailRow.cooperation_stage }}</el-descriptions-item>
        <el-descriptions-item label="办理状态">{{ detailRow.handling_status }}</el-descriptions-item>
        <el-descriptions-item label="生效报价">{{ detailRow.active_quotes }} 份</el-descriptions-item>
        <el-descriptions-item label="未完成报价">{{ detailRow.pending_quotes }} 份</el-descriptions-item>
      </el-descriptions>
      <F12SubjectPanel v-if="detailRow" subject-type="CUSTOMER" :subject-id="String(detailRow.id)" :can-manage="canManageF12" @changed="loadCustomers" />
      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
        <el-button v-if="canTransfer" @click="detailVisible = false; detailRow && openTransfer(detailRow)">移交客户</el-button>
        <el-button v-if="canQuote" type="primary" @click="detailVisible = false; startNewQuote(detailRow)">新建报价</el-button>
      </template>
    </el-dialog>

    <!-- 移交客户 -->
    <el-dialog v-model="transferVisible" title="移交客户" width="min(680px, 96vw)" :close-on-click-modal="false" :close-on-press-escape="!transferSubmitting" :show-close="!transferSubmitting">
      <p v-if="transferTarget">客户：<b>{{ transferTarget.name }}</b> · 当前销售 {{ transferTarget.owner_name }}</p>
      <el-form class="dialog-form" label-position="top" :disabled="transferSubmitting">
        <el-form-item label="目标销售人员 ID *"><el-input v-model="transferForm.toOperatorId" inputmode="numeric" @input="invalidateTransferPreview" /></el-form-item>
        <el-form-item label="移交原因"><el-input v-model="transferForm.reason" type="textarea" :rows="3" maxlength="200" show-word-limit @input="invalidateTransferPreview" /></el-form-item>
      </el-form>
      <el-descriptions v-if="transferImpact" :column="2" border>
        <el-descriptions-item label="原归属">{{ transferImpact.fromOperatorName }}（{{ transferImpact.fromOperatorId }}）</el-descriptions-item>
        <el-descriptions-item label="新归属">{{ transferImpact.toOperatorName }}（{{ transferImpact.toOperatorId }}）</el-descriptions-item>
        <el-descriptions-item label="随迁报价">{{ transferImpact.quoteCount }} 份</el-descriptions-item>
        <el-descriptions-item label="关联价目表">{{ transferImpact.priceBookCount }} 条</el-descriptions-item>
      </el-descriptions>
      <el-alert v-if="transferError" class="form-error" :title="transferError" type="error" show-icon :closable="false" />
      <template #footer>
        <el-button :disabled="transferSubmitting" @click="transferVisible = false">取消</el-button>
        <el-button v-if="!transferImpact" type="primary" :loading="transferSubmitting" @click="previewTransfer">检查影响</el-button>
        <el-button v-else type="danger" :loading="transferSubmitting" @click="confirmTransfer">确认移交</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.customer-page { padding: 18px 22px 32px; }
.tabbar, .subtabbar { display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--el-border-color-light); margin-bottom: 16px; }
.subtabbar { margin-top: 4px; }
.tabbar-left { display: flex; gap: 24px; }
.tab { background: none; border: none; padding: 10px 2px; cursor: pointer; font-size: 14px; color: var(--app-text-secondary); border-bottom: 2px solid transparent; }
.tab.active { color: var(--el-color-primary); border-bottom-color: var(--el-color-primary); font-weight: 600; }
.filterbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-bottom: 12px; }
.flt { width: 148px; }
.grid { width: 100%; }
.cell-main { font-weight: 600; }
.cell-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 2px; }
.stack-tag { display: block; width: fit-content; margin: 2px 0; }
.card { border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 18px 20px; margin-bottom: 18px; background: var(--el-bg-color); }
.card-title { margin: 0 0 16px; font-size: 15px; font-weight: 600; }
.form-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px 20px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field label { font-size: 12px; color: var(--app-text-muted); }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; flex-wrap: wrap; }
.card-head-left, .card-head-right { display: flex; align-items: center; gap: 8px; }
.margin-input { width: 120px; }
.empty-hint { text-align: center; color: var(--app-text-muted); padding: 56px 0; }
.card-foot { display: flex; align-items: flex-end; justify-content: space-between; gap: 20px; margin-top: 18px; }
.foot-note { color: var(--app-text-muted); font-size: 12px; max-width: 62%; line-height: 1.6; }
.foot-actions { display: flex; gap: 10px; }
.linked { display: flex; align-items: center; gap: 12px; border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 14px 18px; margin-bottom: 18px; }
.linked-label { font-size: 13px; color: var(--app-text-secondary); }
.linked-input { width: 380px; }
.linked-hint { color: var(--app-text-muted); font-size: 12px; }
.picker-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.approval-grid { margin-top: 18px; }
.dialog-form { margin-top: 16px; }
.form-error { margin-top: 14px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .foot-note { max-width: 100%; } .card-foot { flex-direction: column; align-items: stretch; } }
</style>
