<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { customerWorkspaceApi, type CustomerApprovalRow, type CustomerQuoteRow, type CustomerWorkspaceRow } from "../../../api/customerWorkspace";
import { customerDetailApi } from "../../../api/customerDetail";
import { customerQuotesApi } from "../../../api/customerQuotes";
import { quoteApprovalApi, type QuoteApprovalSummaryDTO } from "../../../api/customerQuoteApproval";
import { quoteCandidatesApi, type ResourceQuoteCandidate } from "../../../api/quoteCandidates";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { isDecimalAmount } from "../../../domain/money";
import { formatSupplierTaxRate } from "../../../domain/supplierTax";
import { PERMISSIONS } from "../../../domain/permissions";
import { usePermissionStore } from "../../../stores/permission";
import { useSessionStore } from "../../../stores/session";
import CustomerTransferDialog from "./CustomerTransferDialog.vue";
import QuoteApprovalDialog from "./QuoteApprovalDialog.vue";

const route = useRoute();
const router = useRouter();
const permissions = usePermissionStore();
const session = useSessionStore();
const canTransfer = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_EDIT));
const canQuote = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_EDIT));

const message = (value: unknown) => value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";
const positiveId = (value: string) => /^[1-9]\d*$/.test(value.trim()) && Number.isSafeInteger(Number(value));

const activeTab = ref<"list" | "quote">("list");
const quoteView = ref<"edit" | "records" | "approvals" | "logs">("edit");

function unique(values: string[]) {
  return Array.from(new Set(values.filter(Boolean)));
}
const regionText = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
const currencyText = (currency: string) => currency === "USD" ? "美元 USD" : "人民币 CNY";
const currencySymbol = (currency: string) => currency === "USD" ? "$" : "¥";
/**
 * 授信额度展示：**0 不等于「没有授信」**，而是「还没登记」——原型对未登记客户显示「待记录」。
 * 这里把 0/空串都归到「待记录」，避免把"未登记"读成"额度为零"。
 */
function creditText(currency: string, amount: string) {
  const value = Number(amount || 0);
  if (!Number.isFinite(value) || value <= 0) return "待记录";
  // 有小数才保留两位（去掉尾零），整数与原型一致显示 ¥50,000 而不是 ¥50,000.00。
  const text = value.toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: 2 });
  return `${currencySymbol(currency)}${text}`;
}
function amountText(currency: string, amount: string) {
  const value = Number(amount || 0);
  if (!Number.isFinite(value)) return amount;
  return `${currencySymbol(currency)}${value.toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: 0 })}`;
}

// ---------------- 客户列表 ----------------
const rows = ref<CustomerWorkspaceRow[]>([]);
const loading = ref(false);
const loadError = ref("");

/**
 * 列表筛选（B3b 坑 2）：做成独立详情路由后，从详情返回列表会重新 onMounted，
 * 纯内存态会让 7 个筛选条件全部丢失，用户会当 bug 报。
 * 这里落 sessionStorage（不用 query：避免 URL 变脏、分享链接语义变化；不用全局 store：页面级状态不该跨页面共享）。
 */
const FILTERS_STORAGE_KEY = "customer-workspace:filters";
type CustomerFilters = {
  /** 原型是「搜索客户名称 / 曾用名」的自由文本；我们库里没有曾用名字段，只按法定名称匹配。 */
  keyword: string;
  owners: string[]; regions: string[]; contracts: string[]; currencies: string[]; stages: string[]; handlings: string[];
};
const emptyFilters = (): CustomerFilters => ({
  keyword: "", owners: [], regions: [], contracts: [], currencies: [], stages: [], handlings: [],
});
function readFilters(): CustomerFilters {
  const base = emptyFilters();
  try {
    const raw = sessionStorage.getItem(FILTERS_STORAGE_KEY);
    if (!raw) return base;
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return base;
    for (const key of Object.keys(base) as (keyof CustomerFilters)[]) {
      const value = (parsed as Record<string, unknown>)[key];
      if (key === "keyword") {
        if (typeof value === "string") base.keyword = value;
        continue;
      }
      if (Array.isArray(value)) base[key] = value.filter((item): item is string => typeof item === "string");
    }
  } catch {
    // 存储损坏就当没存过：筛选丢失比整页报错好。
  }
  return base;
}
const filters = reactive<CustomerFilters>(readFilters());
// 持久化只由这个 watch 负责（resetFilters 不再单独 removeItem）：
// 全部为空时**移除** key 而不是写回 `{"names":[],...}`——
// 否则「重置」之后 storage 里仍有一条"看起来有筛选"的空记录，排查时会被误判成没清干净。
watch(filters, value => {
  const empty = Object.entries(value).every(([key, item]) => key === "keyword" ? item === "" : item.length === 0);
  try {
    if (empty) {
      sessionStorage.removeItem(FILTERS_STORAGE_KEY);
    } else {
      sessionStorage.setItem(FILTERS_STORAGE_KEY, JSON.stringify(value));
    }
  } catch {
    // 隐私模式/配额满：静默忽略，不影响筛选本身可用。
  }
}, { deep: true });

const options = computed(() => ({
  owners: unique(rows.value.map(row => row.owner_name)),
  regions: unique(rows.value.map(row => regionText(row.region))),
  contracts: unique(rows.value.map(row => row.contract_status)),
  currencies: unique(rows.value.map(row => currencyText(row.settlement_currency))),
  stages: unique(rows.value.map(row => row.cooperation_stage)),
  handlings: unique(rows.value.map(row => row.handling_status)),
}));

const filteredRows = computed(() => rows.value.filter(row => {
  const keyword = filters.keyword.trim().toLowerCase();
  if (keyword && !row.name.toLowerCase().includes(keyword)) return false;
  if (filters.owners.length && !filters.owners.includes(row.owner_name)) return false;
  if (filters.regions.length && !filters.regions.includes(regionText(row.region))) return false;
  if (filters.contracts.length && !filters.contracts.includes(row.contract_status)) return false;
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
  // 只清内存态：storage 的清理由上面的 watch 统一负责（全空即 removeItem），
  // 两处都写会出现"先 remove 再被 watch 回写"的竞态——QA 实测过这个现象。
  Object.assign(filters, emptyFilters());
}

// ---------------- 报价单 ----------------
const quoteRows = ref<CustomerQuoteRow[]>([]);
const quoteLoading = ref(false);
const quoteError = ref("");
const approvalRows = ref<CustomerApprovalRow[]>([]);

const form = reactive({
  customerId: "",
  customerName: "",
  /** 原型里客户不是从客户管理新建的，而是在报价单「关联客户」选「+ 新建客户」时顺手建出来。 */
  isNewCustomer: false,
  ourCompany: "根公司",
  // 原型：区域不选就点「添加模型」会拦。默认留空，逼一次显式选择。
  region: "",
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
  /** 来源最近一次测试结果，随候选一起带过来（原型报价行要展示）。 */
  connectionMS: string;
  rps: string;
  tps: string;
  cacheHitRate: string;
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
/** 下拉首项「+ 新建客户」：报价单是客户进入系统的唯一入口。 */
const NEW_CUSTOMER_ID = "__new__";
const filteredCustomers = computed(() => [{ id: NEW_CUSTOMER_ID, name: "+ 新建客户", region: "CN", currency: "CNY", owner: "" }, ...customerOptions.value]);

function onCustomerChange(customerId: string) {
  if (customerId === NEW_CUSTOMER_ID) {
    form.isNewCustomer = true;
    form.customerName = "";
    return;
  }
  form.isNewCustomer = false;
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
  form.isNewCustomer = false;
  form.region = row?.region || "";
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
// 原型 3.10：一个下拉选"统一定价方式"，一个值，一个「统一当前全部模型」。
const pricingMode = ref<"margin" | "coefficient" | "delta">("margin");
const pricingValue = ref("0.10");
const PRICING_MODES = [
  { value: "margin", label: "统一毛利率 %" },
  { value: "coefficient", label: "统一报价系数" },
  { value: "delta", label: "采购系数＋小数增量" },
] as const;
const pricingHint = computed(() => pricingMode.value === "margin" ? "如 0.15 = 15%" : pricingMode.value === "coefficient" ? "如 0.62" : "如 0.05（在采购系数上加）");
function onPricingModeChange(value: "margin" | "coefficient" | "delta") {
  pricingValue.value = value === "margin" ? "0.10" : value === "coefficient" ? "0.62" : "0.05";
}
function applyPricing() {
  if (!isDecimalAmount(pricingValue.value)) { ElMessage.error("统一定价值必须是合法十进制数，例如 0.15"); return; }
  if (!lines.value.length) { ElMessage.warning("请先添加模型"); return; }
  const value = Number(pricingValue.value);
  if (value <= 0) { ElMessage.error("统一定价值必须大于 0"); return; }
  let applied = 0;
  lines.value = lines.value.map(line => {
    const cost = Number(line.basePrice), coefficient = Number(line.coefficient);
    if (![cost, coefficient].every(Number.isFinite) || cost <= 0 || coefficient <= 0) return line;
    applied += 1;
    if (pricingMode.value === "margin") {
      // 毛利率口径：报价单价 = 采购成本 ÷ (1 − 毛利率)，与服务端 floor 同源。
      const price = priceFromMargin(line.basePrice, pricingValue.value);
      return { ...line, margin: pricingValue.value, price };
    }
    if (pricingMode.value === "coefficient") {
      // 直接指定报价系数：报价系数 = 报价单价 × 采购系数 ÷ 采购成本，反解报价单价。
      const price = (cost * value) / coefficient;
      const margin = (price - cost) / price;
      return { ...line, price: trimNumber(price), margin: trimNumber(margin) };
    }
    // 采购系数 + 小数增量：客户价 = 官方价 × (采购系数 + 增量)。
    const nextCoefficient = coefficient + value;
    if (nextCoefficient <= 0) return line;
    const price = (cost / coefficient) * nextCoefficient;
    return { ...line, price: trimNumber(price), margin: trimNumber((price - cost) / price) };
  });
  ElMessage.success(`已按「${PRICING_MODES.find(mode => mode.value === pricingMode.value)?.label}」统一 ${applied} 个价项`);
}
/** 统一去掉尾零（0.620000 → 0.62），避免把 8 位小数塞进表格。 */
function trimNumber(value: number) {
  return value.toFixed(8).replace(/0+$/, "").replace(/\.$/, "");
}
function quoteCoefficient(line: QuoteLine) {
  const cost = Number(line.basePrice), coefficient = Number(line.coefficient), price = Number(line.price);
  if (![cost, coefficient, price].every(Number.isFinite) || cost <= 0 || coefficient <= 0 || price <= 0) return "";
  return ((price * coefficient) / cost).toFixed(4).replace(/0+$/, "").replace(/\.$/, "");
}
// 行内「提示」：把踩线情况直接写在行上，别让人自己去算。
function lineHint(line: QuoteLine) {
  if (!isDecimalAmount(line.price)) return "待填报价";
  const cost = Number(line.basePrice), price = Number(line.price);
  if (![cost, price].every(Number.isFinite)) return "—";
  if (price < cost) return "低于采购成本";
  const margin = (price - cost) / price;
  return margin < 0.1 ? `毛利率 ${(margin * 100).toFixed(1)}% 低于 10%` : "—";
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
  if (!form.region) { ElMessage.error("请先选择客户业务区域"); return; }
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
        connectionMS: row.test_connection_ms || "", rps: row.test_rps || "",
        tps: row.test_tps || "", cacheHitRate: row.test_cache_hit_rate || "",
        basePrice: item.unit_cost, margin: marginAll.value,
        price: priceFromMargin(item.unit_cost, marginAll.value),
      });
    });
  });
  lines.value = next;
  pickerVisible.value = false;
}

function validateBeforeSubmit() {
  if (!form.region) return "请先选择客户业务区域";
  if (form.isNewCustomer) {
    if (!form.customerName.trim()) return "请填写客户名称和我方公司";
  } else if (!positiveId(form.customerId)) return "请先选择关联客户。";
  if (!lines.value.length) return "请至少添加一个模型。";
  const bad = lines.value.find(line => !positiveId(line.resourceId) || !isDecimalAmount(line.price));
  if (bad) return `${bad.modelName} / ${bad.priceItemLabel} 的报价单价必须是大于 0 的普通十进制字符串。`;
  return "";
}

const submitting = ref(false);
/**
 * 新客户先落档案再出报价：客户档案要销售归属（owner_sales_operator_id），取当前登录人。
 * 同名客户后端直接 409，交给下面统一展示后端文案，不自己编。
 */
async function ensureCustomer(): Promise<string> {
  if (!form.isNewCustomer) return form.customerId;
  const operatorId = session.user?.staffId;
  if (!operatorId) throw new Error("当前登录身份缺少销售归属，无法新建客户；请改用正式内部账号，或先在客户管理选择已有客户。");
  const profile = await customerDetailApi.createCustomer({
    legal_name: form.customerName.trim(),
    subject_type: "COMPANY",
    region: form.region,
    settlement_currency: form.currency,
    owner_sales_operator_id: operatorId,
    reason: "客户报价单新建客户",
  });
  const customerId = String(profile.id);
  form.customerId = customerId;
  form.isNewCustomer = false;
  void loadCustomers();
  return customerId;
}
async function saveDraft(exportAfter: boolean, submitAfter = false) {
  const invalid = validateBeforeSubmit();
  if (invalid) { ElMessage.error(invalid); return; }
  if (submitting.value) return;
  submitting.value = true;
  try {
    const customerId = await ensureCustomer();
    const validTo = new Date(Date.now() + 30 * 24 * 3600 * 1000).toISOString();
    const result = await customerQuotesApi.generateResource({
      customerId,
      validTo,
      items: lines.value.map(line => ({ resourceId: line.resourceId, priceItemKey: line.priceItemKey, unitPrice: line.price })),
    });
    form.quoteNo = `#${result.id}`;
    if (submitAfter) {
      // 原型编辑器底部就是「提交审批」：生成报价单后立刻送审，不要求先导出。
      await quoteApprovalApi.submit(result.id, crypto.randomUUID());
      ElMessage.success(`报价单 #${result.id} 已提交审批，等待超管逐模型批准。`);
    } else if (exportAfter) {
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

// ---------------- 报价审批（原型 3.10：审批人在这个页签里逐模型批） ----------------
// 客户维度之外的全局队列：不传 customer_id，服务端限 200 条。
const quoteApprovalRows = ref<QuoteApprovalSummaryDTO[]>([]);
const quoteApprovalLoading = ref(false);
const quoteApprovalError = ref("");
const quoteApprovalVisible = ref(false);
const quoteApprovalTargetId = ref<number | null>(null);
const canQuoteApprove = computed(() => permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_APPROVE));
const quoteApprovalView = ref<"pending" | "history">("pending");

const pendingApprovalRows = computed(() => quoteApprovalRows.value.filter(row => row.status === "PENDING"));
const decidedApprovalRows = computed(() => quoteApprovalRows.value.filter(row => row.status !== "PENDING"));

async function loadQuoteApprovals() {
  quoteApprovalLoading.value = true;
  quoteApprovalError.value = "";
  try {
    quoteApprovalRows.value = await quoteApprovalApi.list(0);
  } catch (error) {
    quoteApprovalRows.value = [];
    quoteApprovalError.value = message(error);
  } finally {
    quoteApprovalLoading.value = false;
  }
}

function openQuoteApproval(row: QuoteApprovalSummaryDTO) {
  quoteApprovalTargetId.value = row.id;
  quoteApprovalVisible.value = true;
}

/** 弹窗处理完（批准 / 驳回 / 撤回）后刷新两张表。 */
async function onQuoteApprovalDecided() {
  await loadQuoteApprovals();
  await loadQuotes();
}

async function loadQuotes() {
  quoteLoading.value = true;
  quoteError.value = "";  try {
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

// 「报价记录」= 全部报价单（原型 3.10 第三步：新单在最上面，靠「当前状态」区分阶段）。
const recordRows = computed(() => quoteRows.value);
// 报价操作日志：当前没有独立审计表，用报价单自身的时间/状态/操作人派生（不编造动作）。
const logRows = computed(() => quoteRows.value.map(row => ({
  id: row.id, customer: row.customer_name, version: row.version_no,
  status: row.status, operator: row.owner_name, at: row.created_at,
})));

const statusText = (status: string) => ({ EFFECTIVE: "历史记录", DRAFT: "草稿", PENDING: "待审批", APPROVED: "已批准", EXPIRED: "历史记录", REJECTED: "已驳回", FORMAL: "历史记录" } as Record<string, string>)[status] || status;
/** 已生效/过期的报价单只读：不能继续编辑，只能看。 */
const isReadonlyQuote = (status: string) => ["EFFECTIVE", "EXPIRED", "FORMAL"].includes(status);
const quoteStatusType = (status: string) => status === "APPROVED" ? "success" : status === "PENDING" ? "warning" : isReadonlyQuote(status) ? "info" : status === "REJECTED" ? "danger" : "";
const quoteTypeText = (type: string) => ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", RESOURCE: "按资源报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;
const approvalStatusText = (status: string) => ({ PENDING: "待审批", APPROVED: "已通过", REJECTED: "已驳回" } as Record<string, string>)[status] || status;
const approvalStatusType = (status: string) => status === "APPROVED" ? "success" : status === "REJECTED" ? "danger" : "warning";

function switchQuoteView(view: "edit" | "records" | "approvals" | "logs") {
  quoteView.value = view;
  if (view !== "edit" && !quoteRows.value.length) void loadQuotes();
  if (view === "approvals") void loadQuoteApprovals();
}

/** 已生效/过期的报价单是只读的：跳到该客户详情的「该客户报价」看明细。 */
function openQuoteRecord(row: CustomerQuoteRow) {
  void router.push({ path: `/internal/customers/${row.customer_id}`, query: { tab: "quotes" } });
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
// 详情已改成独立路由页（B3b 第 1 项）：这里只负责跳转，原弹窗与 F12 面板都搬到详情页。
function openDetail(row: CustomerWorkspaceRow) {
  void router.push({ path: `/internal/customers/${row.id}`, query: { tab: "models" } });
}

// 移交：逻辑全部收进 CustomerTransferDialog（B3b 第 4 项），这里只负责"对谁移交"与刷新。
const transferVisible = ref(false);
const transferTarget = ref<CustomerWorkspaceRow>();
function openTransfer(row: CustomerWorkspaceRow) {
  transferTarget.value = row;
  transferVisible.value = true;
}

onMounted(async () => {
  await loadCustomers();
  void loadQuotes();
  // 详情页「新建报价」会带 ?tab=quote&customer_id= 过来：预选关联客户，
  // 复用本页的编辑器（不把 380 行编辑器搬进详情页）。
  const preset = typeof route.query.customer_id === "string" ? route.query.customer_id : "";
  if (route.query.tab === "quote") {
    activeTab.value = "quote";
    quoteView.value = "edit";
    if (positiveId(preset)) {
      const row = rows.value.find(item => String(item.id) === preset);
      startNewQuote(row);
    }
  }
});
</script>

<template>
  <section class="customer-page">
    <div class="tabbar">
      <div class="tabbar-left">
        <button class="tab" :class="{ active: activeTab === 'list' }" @click="activeTab = 'list'">客户列表</button>
        <button class="tab" :class="{ active: activeTab === 'quote' }" @click="activeTab = 'quote'">客户报价单</button>
      </div>
      <div class="tabbar-actions">
        <el-button v-if="activeTab === 'list' && canQuote" type="primary" @click="startNewQuote()">新建报价</el-button>
      </div>
    </div>

    <!-- ================= 客户列表 ================= -->
    <template v-if="activeTab === 'list'">
      <div class="filterbar">
        <el-input v-model="filters.keyword" class="flt keyword" placeholder="搜索客户名称" clearable aria-label="搜索客户名称" />
        <el-select v-model="filters.owners" multiple filterable clearable collapse-tags placeholder="销售" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.owners" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.regions" multiple filterable clearable collapse-tags placeholder="国内/海外" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.regions" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.contracts" multiple filterable clearable collapse-tags placeholder="合同状态" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.contracts" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.currencies" multiple filterable clearable collapse-tags placeholder="结算币种" class="flt" filter-placeholder="输入匹配已有选项">
          <el-option v-for="item in options.currencies" :key="item" :label="item" :value="item" />
        </el-select>
        <el-select v-model="filters.stages" multiple filterable clearable collapse-tags placeholder="合作/商机" class="flt" filter-placeholder="输入匹配已有选项">
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
        <el-table-column label="客户名称" min-width="190">
          <template #default="{ row }">
            <div class="cell-main">{{ row.name }}</div>
          </template>
        </el-table-column>
        <el-table-column label="销售" width="110">
          <template #default="{ row }">{{ row.owner_name || "未分配" }}</template>
        </el-table-column>
        <el-table-column label="国内/海外" width="100">
          <template #default="{ row }">{{ regionText(row.region) }}</template>
        </el-table-column>
        <el-table-column label="已开通模型数" width="120" align="right">
          <template #default="{ row }">
            <span class="mono">{{ row.opened_model_count }}</span>
          </template>
        </el-table-column>
        <el-table-column label="授信额度" width="140" align="right">
          <template #default="{ row }">
            <span class="mono">{{ creditText(row.settlement_currency, row.credit_limit) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="合同状态" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.contract_status === '已归档' ? 'success' : row.contract_status === '办理中' ? 'warning' : 'info'">{{ row.contract_status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="结算币种" width="120">
          <template #default="{ row }">{{ currencyText(row.settlement_currency) }}</template>
        </el-table-column>
        <el-table-column label="税率" width="100">
          <template #default="{ row }">{{ formatSupplierTaxRate(row.tax_rate) }}</template>
        </el-table-column>
        <el-table-column label="合作/商机" width="110">
          <template #default="{ row }">{{ row.cooperation_stage }}</template>
        </el-table-column>
        <el-table-column label="办理状态" width="140">
          <template #default="{ row }">{{ row.handling_status }}</template>
        </el-table-column>
        <!-- 行操作：详情（独立页）+ 移交（共用弹窗）。
             移交必须留在列表里：它是"看谁名下、当场调整"的操作，
             进详情再移交会把用户原来的路径变远——技术重构不该改用户的习惯。
             权限沿用 M8:E（CUSTOMER_EDIT），无权限不显示。 -->
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button v-if="canTransfer" link type="primary" @click="openTransfer(row)">移交</el-button>
          </template>
        </el-table-column>
        <template #empty><el-empty description="没有符合条件的客户" :image-size="64" /></template>
      </el-table>
    </template>

    <!-- ================= 客户报价单 ================= -->
    <template v-else>
      <div class="subtabbar">
        <div class="tabbar-left">
          <button class="tab" :class="{ active: quoteView === 'edit' }" @click="switchQuoteView('edit')">报价</button>
          <button class="tab" :class="{ active: quoteView === 'records' }" @click="switchQuoteView('records')">报价记录</button>
          <button class="tab" :class="{ active: quoteView === 'approvals' }" @click="switchQuoteView('approvals')">报价审批</button>
          <button class="tab" :class="{ active: quoteView === 'logs' }" @click="switchQuoteView('logs')">报价操作日志</button>
        </div>
        <el-button v-if="canQuote" @click="startNewQuote()">新建报价单</el-button>
      </div>

      <template v-if="quoteView === 'edit'">
        <div class="linked">
          <label class="linked-label">关联客户</label>
          <el-select v-model="form.customerId" filterable clearable placeholder="搜索客户名称" class="linked-input" @change="onCustomerChange">
            <el-option v-for="item in filteredCustomers" :key="item.id" :value="item.id" :label="item.id === NEW_CUSTOMER_ID ? '+ 新建客户' : `${item.name} · ${regionText(item.region)} · ${item.owner}`" />
          </el-select>
          <span class="linked-hint">每份报价关联一个客户；选「+ 新建客户」即在本单里建档</span>
        </div>

        <div class="card">
          <h3 class="card-title">报价信息</h3>
          <div class="form-grid">
            <div class="field"><label>客户名称</label><el-input v-model="form.customerName" :disabled="!form.isNewCustomer" :placeholder="form.isNewCustomer ? '填写新客户名称' : '选择关联客户后自动带出'" /></div>
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
            <div class="field"><label>商业条件</label><el-input v-model="form.remark" maxlength="200" placeholder="付款方式 / 账期 / 最低消费等" /></div>
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
              <span class="muted">统一定价方式</span>
              <el-select v-model="pricingMode" style="width: 168px" aria-label="统一定价方式" @change="onPricingModeChange">
                <el-option v-for="mode in PRICING_MODES" :key="mode.value" :value="mode.value" :label="mode.label" />
              </el-select>
              <el-input v-model="pricingValue" class="margin-input" :placeholder="pricingHint" aria-label="统一定价值" />
              <el-button @click="applyPricing">统一当前全部模型</el-button>
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
            <el-table-column label="连通 · ms" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.connectionMS || "未记录" }}</span></template></el-table-column>
            <el-table-column label="RPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.rps || "未记录" }}</span></template></el-table-column>
            <el-table-column label="TPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.tps || "未记录" }}</span></template></el-table-column>
            <el-table-column label="输入缓存命中率" width="130" align="right"><template #default="{ row }"><span class="mono">{{ row.cacheHitRate ? row.cacheHitRate + "%" : "未记录" }}</span></template></el-table-column>
            <el-table-column label="采购成本" width="140" align="right">
              <template #default="{ row }"><span class="mono">{{ row.basePrice || "—" }} {{ row.currency }}</span></template>
            </el-table-column>
            <el-table-column label="毛利率" width="120">
              <template #default="{ row }"><el-input v-model="row.margin" size="small" @change="applyPrice(row)" /></template>
            </el-table-column>
            <el-table-column label="报价单价" width="160">
              <template #default="{ row }"><el-input v-model="row.price" size="small" placeholder="必填" /></template>
            </el-table-column>
            <el-table-column label="报价系数" width="110" align="right">
              <template #default="{ row }"><span class="mono">{{ quoteCoefficient(row) || "—" }}</span></template>
            </el-table-column>
            <el-table-column label="提示" min-width="170">
              <template #default="{ row }"><span :class="{ warn: lineHint(row) !== '—' }">{{ lineHint(row) }}</span></template>
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
              <el-button :loading="submitting" :disabled="!canQuote" @click="saveDraft(false, true)">提交审批</el-button>
              <el-button type="primary" :loading="submitting" :disabled="!canQuote" @click="saveDraft(true)">导出报价单</el-button>
            </div>
          </div>
        </div>
      </template>

      <template v-else-if="quoteView === 'records'">
        <!-- 原型 3.10 第三步：报价单 ID / 客户 / 销售 / 提交时间 / 模型数 / 当前状态 / 操作。
             任务从"按导出与否拆两个表"改为"一张表看当前状态"——同一张单会从草稿走到待审批再到历史记录。 -->
        <el-alert v-if="quoteError" :title="quoteError" type="error" show-icon :closable="false" />
        <el-table v-loading="quoteLoading" :data="recordRows" row-key="id" class="grid">
          <el-table-column label="报价单 ID" width="120"><template #default="{ row }">#{{ row.id }}</template></el-table-column>
          <el-table-column label="客户" min-width="200"><template #default="{ row }">{{ row.customer_name }}</template></el-table-column>
          <el-table-column label="销售" width="130"><template #default="{ row }">{{ row.owner_name || "—" }}</template></el-table-column>
          <el-table-column label="提交时间" width="180"><template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template></el-table-column>
          <el-table-column label="模型数" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.item_count }}</span></template></el-table-column>
          <el-table-column label="当前状态" width="120">
            <template #default="{ row }"><el-tag size="small" :type="quoteStatusType(row.status)">{{ statusText(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="操作" width="160">
            <template #default="{ row }">
              <el-button v-if="!isReadonlyQuote(row.status)" link type="primary" @click="openRowQuote(row)">继续编辑</el-button>
              <el-button v-else link type="primary" @click="openQuoteRecord(row)">查看</el-button>
            </template>
          </el-table-column>
          <template #empty><el-empty description="暂无报价记录" :image-size="64" /></template>
        </el-table>
      </template>

      <template v-else-if="quoteView === 'approvals'">
        <div class="subtabs">
          <button class="subtab" :class="{ active: quoteApprovalView === 'pending' }" @click="quoteApprovalView = 'pending'">待审批（{{ pendingApprovalRows.length }}）</button>
          <button class="subtab" :class="{ active: quoteApprovalView === 'history' }" @click="quoteApprovalView = 'history'">审批记录</button>
        </div>
        <el-alert v-if="quoteApprovalError" :title="quoteApprovalError" type="error" show-icon :closable="false" />
        <el-table
          v-loading="quoteApprovalLoading"
          :data="quoteApprovalView === 'pending' ? pendingApprovalRows : decidedApprovalRows"
          row-key="id"
          class="grid"
          :empty-text="quoteApprovalView === 'pending' ? '暂无待审批的报价单' : '暂无审批记录'"
        >
          <el-table-column label="报价单 ID" width="120"><template #default="{ row }">#{{ row.quote_id }}</template></el-table-column>
          <el-table-column label="模型数" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.model_count }}</span></template></el-table-column>
          <el-table-column label="提交人 / 提交时间" min-width="240">
            <template #default="{ row }">员工 #{{ row.submitted_by }} · {{ formatDateTime(row.submitted_at, "—", false) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }"><el-tag size="small" :type="approvalStatusType(row.status)">{{ approvalStatusText(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="处理人 / 处理时间" min-width="240">
            <template #default="{ row }">{{ row.decided_by ? `员工 #${row.decided_by}` : "—" }} · {{ row.decided_at ? formatDateTime(row.decided_at, "—", false) : "—" }}</template>
          </el-table-column>
          <el-table-column label="操作" width="110">
            <template #default="{ row }">
              <el-button link type="primary" :disabled="row.status !== 'PENDING' && !canQuoteApprove" @click="openQuoteApproval(row)">
                {{ row.status === "PENDING" ? "审批" : "查看" }}
              </el-button>
            </template>
          </el-table-column>
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

    <!-- 报价审批（逐模型批准 / 驳回）：与客户详情共用同一组件 -->
    <QuoteApprovalDialog
      v-model:visible="quoteApprovalVisible"
      :approval-id="quoteApprovalTargetId"
      :can-approve="canQuoteApprove"
      :staff-id="session.user?.staffId ? Number(session.user.staffId) : 0"
      @decided="onQuoteApprovalDecided"
    />

    <!-- 移交客户（共用弹窗组件） -->
    <CustomerTransferDialog
      v-model:visible="transferVisible"
      :customer-id="transferTarget ? String(transferTarget.id) : ''"
      :customer-name="transferTarget?.name || ''"
      :owner-name="transferTarget?.owner_name || ''"
      @transferred="loadCustomers"
    />
  </section>
</template>

<style scoped>
.customer-page { padding: 18px 22px 32px; }
.tabbar, .subtabbar { display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--el-border-color-light); margin-bottom: 16px; }
.subtabbar { margin-top: 4px; }
.tabbar-left { display: flex; gap: 24px; }
.tabbar-actions { display: flex; gap: 10px; }
.tab { background: none; border: none; padding: 10px 2px; cursor: pointer; font-size: 14px; color: var(--app-text-secondary); border-bottom: 2px solid transparent; }
.tab.active { color: var(--el-color-primary); border-bottom-color: var(--el-color-primary); font-weight: 600; }
/* 报价审批的两个子标签（待审批 / 审批记录）：原型里是浅色胶囊，不抢外层 Tab 的视觉层级。 */
.subtabs { display: flex; gap: 8px; margin: 0 0 12px; }
.subtab { border: 1px solid var(--el-border-color); background: var(--el-bg-color); border-radius: 999px; padding: 5px 14px; font-size: 12px; cursor: pointer; color: var(--app-text-secondary); }
.subtab.active { border-color: var(--el-color-primary); color: var(--el-color-primary); font-weight: 600; }
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
.warn { color: #b87327; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .foot-note { max-width: 100%; } .card-foot { flex-direction: column; align-items: stretch; } }
</style>
