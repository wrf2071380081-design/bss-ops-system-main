<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { customerWorkspaceApi, type CustomerApprovalRow, type CustomerQuoteRow, type CustomerWorkspaceRow } from "../../../api/customerWorkspace";
import { customersApi } from "../../../api/customers";
import type { CustomerTransferImpactDTO } from "../../../api/customers.types";
import { customerQuotesApi } from "../../../api/customerQuotes";
import { modelsApi } from "../../../api/models";
import type { ModelSkuContractDTO } from "../../../api/models.types";
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

interface QuoteLine {
  skuId: string;
  skuCode: string;
  name: string;
  basePrice: string;
  margin: string;
  price: string;
}

const lines = ref<QuoteLine[]>([]);
const marginAll = ref("0.10");
const priceBookVersion = ref<number | null>(null);
const priceBookMap = ref(new Map<string, string>());
const priceBookMissing = ref(false);

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
  void loadPriceBook(customerId);
}

async function loadPriceBook(customerId: string) {
  priceBookMap.value = new Map();
  priceBookVersion.value = null;
  priceBookMissing.value = false;
  if (!positiveId(customerId)) return;
  try {
    const context = await customerQuotesApi.context(customerId);
    if (context.priceBook) {
      priceBookVersion.value = context.priceBook.versionNo;
      const map = new Map<string, string>();
      context.priceBook.items.forEach(item => map.set(String(item.skuId), String(item.unitPrice)));
      priceBookMap.value = map;
    } else {
      priceBookMissing.value = true;
    }
  } catch (error) {
    priceBookMissing.value = true;
    quoteError.value = message(error);
  }
  // 已选行同步基准单价（价目表里没有的 SKU 保持为空，由人填）
  lines.value = lines.value.map(line => {
    const base = priceBookMap.value.get(line.skuId) || "";
    return { ...line, basePrice: base, price: line.price || base };
  });
}

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
  priceBookMap.value = new Map();
  priceBookVersion.value = null;
  priceBookMissing.value = false;
  if (row) void onCustomerChange(String(row.id));
}

function priceFromMargin(base: string, margin: string) {
  if (!isDecimalAmount(base)) return "";
  const baseValue = Number(base);
  const marginValue = Number(margin);
  if (!Number.isFinite(baseValue) || !Number.isFinite(marginValue)) return "";
  const computedValue = baseValue * (1 + marginValue);
  return computedValue.toFixed(8).replace(/0+$/, "").replace(/\.$/, "");
}
function applyPrice(line: QuoteLine) {
  line.price = line.price || priceFromMargin(line.basePrice, line.margin);
}
function applyMarginAll() {
  if (!isDecimalAmount(marginAll.value)) { ElMessage.error("统一毛利率必须是合法十进制数，例如 0.10"); return; }
  if (!lines.value.length) { ElMessage.warning("请先添加模型"); return; }
  lines.value = lines.value.map(line => ({ ...line, margin: marginAll.value, price: priceFromMargin(line.basePrice, marginAll.value) }));
  ElMessage.success(`已按毛利率 ${marginAll.value} 统一 ${lines.value.length} 个模型`);
}
function removeLine(skuId: string) { lines.value = lines.value.filter(line => line.skuId !== skuId); }

// 添加/选择模型
const pickerVisible = ref(false);
const pickerKeyword = ref("");
const pickerLoading = ref(false);
const pickerError = ref("");
const pickerRows = ref<ModelSkuContractDTO[]>([]);
const pickerSelected = reactive(new Set<string>());
const pickerPage = ref(1);
const pickerTotal = ref(0);
let pickerSequence = 0;

async function openPicker() {
  pickerVisible.value = true;
  pickerKeyword.value = "";
  pickerSelected.clear();
  pickerRows.value = [];
  await searchModels(false);
}
async function searchModels(append: boolean) {
  const sequence = ++pickerSequence;
  pickerLoading.value = true;
  pickerError.value = "";
  const page = append ? pickerPage.value + 1 : 1;
  try {
    const result = await modelsApi.list({ view: "sku", keyword: pickerKeyword.value.trim() || undefined, page, size: 20, lifecycle_status: "PUBLISHED" });
    if (sequence !== pickerSequence) return;
    const list = result.list.filter((row): row is ModelSkuContractDTO => "sku_code" in row);
    pickerRows.value = append ? [...pickerRows.value, ...list] : list;
    pickerPage.value = page;
    pickerTotal.value = result.total;
  } catch (error) {
    if (sequence === pickerSequence) pickerError.value = message(error);
  } finally {
    if (sequence === pickerSequence) pickerLoading.value = false;
  }
}
function togglePick(skuId: string) {
  if (pickerSelected.has(skuId)) pickerSelected.delete(skuId); else pickerSelected.add(skuId);
}
function confirmPicker() {
  const chosen = pickerRows.value.filter(row => pickerSelected.has(String(row.id)));
  const next = [...lines.value];
  chosen.forEach(row => {
    const skuId = String(row.id);
    if (next.some(line => line.skuId === skuId)) return;
    const base = priceBookMap.value.get(skuId) || "";
    next.push({
      skuId, skuCode: row.sku_code,
      name: `${row.vendor_name} / ${row.family_name}`,
      basePrice: base, margin: marginAll.value,
      price: base ? priceFromMargin(base, marginAll.value) : "",
    });
  });
  lines.value = next;
  pickerVisible.value = false;
}

function validateBeforeSubmit() {
  if (!positiveId(form.customerId)) return "请先选择关联客户。";
  if (!lines.value.length) return "请至少添加一个模型。";
  const bad = lines.value.find(line => !positiveId(line.skuId) || !isDecimalAmount(line.price));
  if (bad) return `${bad.skuCode || bad.skuId} 的报价单价必须是大于 0 的普通十进制字符串。`;
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
    const result = await customerQuotesApi.generate({
      customerId: form.customerId,
      quoteType: "TEMP",
      validTo,
      items: lines.value.map(line => ({ skuId: line.skuId, unitPrice: line.price })),
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
const quoteTypeText = (type: string) => ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;
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
              <el-button type="primary" :disabled="!canQuote" @click="openPicker">+ 添加/选择模型</el-button>
              <span class="muted">已选 {{ lines.length }} 个</span>
              <span v-if="priceBookVersion" class="muted">· 基准价目表 V{{ priceBookVersion }}</span>
              <span v-else-if="priceBookMissing" class="muted">· 该客户等级暂无生效价目表，请逐行填写单价</span>
            </div>
            <div class="card-head-right">
              <span class="muted">统一毛利率</span>
              <el-input v-model="marginAll" class="margin-input" placeholder="输入数值" />
              <span class="muted">%</span>
              <el-button @click="applyMarginAll">统一当前全部模型</el-button>
            </div>
          </div>

          <el-table v-if="lines.length" :data="lines" row-key="skuId" class="grid">
            <el-table-column label="模型" min-width="200"><template #default="{ row }">{{ row.name }}</template></el-table-column>
            <el-table-column label="SKU" width="140"><template #default="{ row }"><span class="mono">{{ row.skuCode }}</span></template></el-table-column>
            <el-table-column label="基准单价（价目表）" width="170" align="right">
              <template #default="{ row }"><span class="mono">{{ row.basePrice || "—" }}</span></template>
            </el-table-column>
            <el-table-column label="毛利率" width="120">
              <template #default="{ row }"><el-input v-model="row.margin" size="small" @change="applyPrice(row)" /></template>
            </el-table-column>
            <el-table-column label="报价单价" width="160">
              <template #default="{ row }"><el-input v-model="row.price" size="small" placeholder="必填" /></template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }"><el-button link type="danger" @click="removeLine(row.skuId)">移除</el-button></template>
            </el-table-column>
          </el-table>
          <div v-else class="empty-hint">点击添加模型，在弹窗中搜索并勾选</div>

          <div class="card-foot">
            <span class="foot-note">统一为一次性操作，随后可逐行修改；新增行不自动继承。毛利率按「基准单价 × (1 + 毛利率)」计算报价单价，仅用于填单辅助，最终以服务端生成结果为准。</span>
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

    <!-- 添加/选择模型 -->
    <el-dialog v-model="pickerVisible" title="添加/选择模型" width="min(760px, 96vw)">
      <div class="picker-bar">
        <el-input v-model="pickerKeyword" placeholder="搜索模型 / SKU" clearable @keyup.enter="searchModels(false)" />
        <el-button type="primary" @click="searchModels(false)">搜索</el-button>
        <span class="muted">已勾选 {{ pickerSelected.size }} 个</span>
      </div>
      <el-alert v-if="pickerError" :title="pickerError" type="error" show-icon :closable="false" />
      <el-table v-loading="pickerLoading" :data="pickerRows" row-key="id" max-height="360">
        <el-table-column width="48">
          <template #default="{ row }"><el-checkbox :model-value="pickerSelected.has(String(row.id))" @change="togglePick(String(row.id))" /></template>
        </el-table-column>
        <el-table-column label="厂商 / 产品" min-width="200"><template #default="{ row }">{{ row.vendor_name }} / {{ row.family_name }}</template></el-table-column>
        <el-table-column label="SKU" width="150"><template #default="{ row }"><span class="mono">{{ row.sku_code }}</span></template></el-table-column>
      </el-table>
      <el-button v-if="pickerRows.length < pickerTotal" text :loading="pickerLoading" @click="searchModels(true)">加载更多</el-button>
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
