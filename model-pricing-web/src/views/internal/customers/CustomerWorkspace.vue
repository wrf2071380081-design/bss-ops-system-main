<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { customerWorkspaceApi, type CustomerApprovalRow, type CustomerQuoteRow, type CustomerWorkspaceRow } from "../../../api/customerWorkspace";
import { customerDetailApi } from "../../../api/customerDetail";
import { customerQuotesApi, type QuoteForEditLineDTO } from "../../../api/customerQuotes";
import { APPROVAL_STATUS_LABELS, APPROVAL_STATUS_TYPES, quoteApprovalApi, type QuoteApprovalSummaryDTO } from "../../../api/customerQuoteApproval";
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
import QuoteApprovalDetail from "./QuoteApprovalDetail.vue";

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
  /** 我方签约主体：原型是可填的空输入框（不锁死成系统默认主体）。 */
  ourCompany: "",
  // 原型：区域不选就点「添加模型」会拦。默认留空，逼一次显式选择。
  region: "",
  currency: "CNY",
  remark: "",
});

// 报价行按「模型」口径（对齐原型）：一行 = 一个模型 + 一个供应来源。
// 采购系数与报价系数都是"对官方价的倍数"，与具体价项无关，
// 所以一个模型一行就能覆盖它的全部价项（输入/输出…），提交时再逐项换算单价。
interface QuoteItem { key: string; label: string; amount: string; unitCost: string; currency: string }
interface QuoteLine {
  /** 模型分组键：官方 ID + 模型公司，用于同模型多来源之间切换与去重。 */
  modelKey: string;
  /** 当前选中的供应来源（一个 resource 就是"某供应商的某模型"）。 */
  resourceId: string;
  modelName: string;
  company: string;
  supplierName: string;
  region: string;
  coefficient: string;
  /** 来源最近一次测试结果，随候选一起带过来（原型报价行要展示）。 */
  connectionMS: string;
  rps: string;
  tps: string;
  cacheHitRate: string;
  currency: string;
  /** 该来源的全部官方价项；提交时按报价系数逐项换算单价。 */
  items: QuoteItem[];
  /** 目标毛利率（百分数，15 表示 15%）。 */
  margin: string;
  /** 报价系数 = 报价单价 ÷ 官方价 = 采购系数 ÷ (1 − 毛利率)。 */
  quoteCoefficient: string;
}

const lines = ref<QuoteLine[]>([]);
/** 模型 → 该模型全部可用来源（行内换供应商、对比来源都用它）。 */
const sourcesByModel = ref(new Map<string, ResourceQuoteCandidate[]>());
/**
 * 毛利不再设红线（后端 min_gross_margin=0，只保「不低于采购成本」这条成本底线）。
 * 只剩一条提示线：毛利率 < 3% 时提交前弹「严重报价提示」，销售点「已知悉」才继续——
 * 与原型一致：平台只提示风险，把关交给审批人。
 */
const ALERT_MARGIN = 3;
const DEFAULT_MARGIN = "10";
// 候选资源（供应域已判可报价）。资源口径报价不再走价目表：基准价 = 官方价 × 采购系数。
const candidateTotal = ref(0);

const customerOptions = computed(() => rows.value.map(row => ({
  id: String(row.id), name: row.name, region: row.region,
  currency: row.settlement_currency, owner: row.owner_name,
})));
/** 下拉首项「+ 新建客户」：报价单是客户进入系统的唯一入口。 */
const NEW_CUSTOMER_ID = "__new__";
const filteredCustomers = computed(() => [{ id: NEW_CUSTOMER_ID, name: "+ 新建客户", region: "CN", currency: "CNY", owner: "" }, ...customerOptions.value]);

/** 结算币种跟业务区域走：国内=人民币、海外=美元（客户档案的 settlement_currency 优先）。 */
const currencyOfRegion = (region: string) => region === "OVERSEAS" ? "USD" : "CNY";
// 业务区域一变，之前选的模型可能来自另一个区域（海外客户买国内节点这种组合不成立），
// 统一清空并明确告知，避免带着混区明细继续提交。
function clearLinesOnRegionChange(next: string) {
  const prev = form.region;
  form.region = next;
  if (prev && next && prev !== next && lines.value.length) {
    const count = lines.value.length;
    lines.value = [];
    ElMessage.warning(`业务区域已切换为${regionText(next)}，已清空 ${count} 个${regionText(prev)}模型，请重新选择对应区域的资源。`);
  }
}

function onRegionChange(region: string) {
  clearLinesOnRegionChange(region);
  form.currency = currencyOfRegion(region);
}

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
  clearLinesOnRegionChange(option.region);
  form.currency = option.currency || currencyOfRegion(option.region);
}

// 关联客户变化时不再重算基准价——资源口径的基准价来自候选资源的采购成本。

function startNewQuote(row?: CustomerWorkspaceRow) {
  activeTab.value = "quote";
  quoteView.value = "edit";
  // 手动新建/继续编辑一律退出「关联重报」上下文，避免把上一张单的驳回范围带到新单上。
  reapplyFrom.value = null;
  editingQuote.value = null;
  form.customerId = row ? String(row.id) : "";
  form.customerName = "";
  form.isNewCustomer = false;
  form.region = row?.region || "";
  form.currency = row?.settlement_currency || "CNY";
  form.remark = "";
  lines.value = [];
  if (row) onCustomerChange(String(row.id));
}

// 采购系数、报价系数都是"对官方价的倍数"，所以一个模型一个系数就能覆盖它全部价项：
//   报价系数 = 采购系数 ÷ (1 − 毛利率)；毛利率 = 1 − 采购系数 ÷ 报价系数。
// 服务端 floor 是"成本 ÷ (1 − 最低毛利)"，与这套换算是同一口径，填出来的价不会天然破线。
/** 四舍五入到固定位数并去掉尾零：0.61111111 这种 8 位小数塞进输入框没人愿意看。 */
function roundText(value: number, digits: number) {
  return String(Number(value.toFixed(digits)));
}
/**
 * 报价系数 = 采购系数 ÷ (1 − 毛利率)，向上取整到 6 位。
 * 向上取整是有意的：毛利率压着 15% 红线时，四舍五入可能把单价算到 floor 下面，
 * 提交必被 409 拒掉；向上取整保证单价 ≥ floor（差 1e-6 以内）。
 */
function coefficientFromMargin(supplierCoefficient: string, marginPercent: string) {
  const coefficient = Number(supplierCoefficient), margin = Number(marginPercent) / 100;
  if (!Number.isFinite(coefficient) || coefficient <= 0) return "";
  if (!Number.isFinite(margin) || margin < 0 || margin >= 1) return "";
  const value = coefficient / (1 - margin);
  if (!Number.isFinite(value)) return "";
  return roundText(Math.ceil(value * 1e6) / 1e6, 6);
}
function marginFromCoefficient(supplierCoefficient: string, quoteCoefficient: string) {
  const coefficient = Number(supplierCoefficient), quoted = Number(quoteCoefficient);
  if (!Number.isFinite(coefficient) || coefficient <= 0) return "";
  if (!Number.isFinite(quoted) || quoted <= 0) return "";
  return roundText((1 - coefficient / quoted) * 100, 2);
}
/** 行内改「毛利率 %」→ 反算报价系数。 */
function onMarginChange(line: QuoteLine) {
  line.quoteCoefficient = coefficientFromMargin(line.coefficient, line.margin);
}
/** 行内改「报价系数」→ 反算毛利率。 */
function onQuoteCoefficientChange(line: QuoteLine) {
  line.margin = marginFromCoefficient(line.coefficient, line.quoteCoefficient);
}
/** 换供应商来源：系数、测试指标、价项全部换掉，但保留已填的目标毛利率。 */
function onSupplierChange(line: QuoteLine, resourceId: string) {
  const source = sourcesOf(line.modelKey).find(item => String(item.resource_id) === resourceId);
  if (!source) return;
  const next = buildLine(line.modelKey, source);
  next.margin = line.margin;
  next.quoteCoefficient = coefficientFromMargin(next.coefficient, next.margin);
  lines.value = lines.value.map(item => item.modelKey === line.modelKey ? next : item);
}
function sourcesOf(modelKey: string) {
  return sourcesByModel.value.get(modelKey) || [];
}
/** 展开「对比来源」的行：展开态由自己的按钮控制，表格自带的小箭头藏起来。 */
const expandedModels = ref<string[]>([]);
function toggleCompare(modelKey: string) {
  expandedModels.value = expandedModels.value.includes(modelKey)
    ? expandedModels.value.filter(key => key !== modelKey)
    : [...expandedModels.value, modelKey];
}
// 原型 3.10：一个下拉选"统一定价方式"，一个值，一个「统一当前全部模型」。
const pricingMode = ref<"margin" | "coefficient">("margin");
const pricingValue = ref("10");
const PRICING_MODES = [
  { value: "margin", label: "统一毛利率 %" },
  { value: "coefficient", label: "统一报价系数" },
] as const;
const pricingHint = computed(() => pricingMode.value === "margin" ? "如 10 = 10%" : "如 0.68");
function onPricingModeChange(value: "margin" | "coefficient") {
  pricingValue.value = value === "margin" ? "10" : "0.68";
}
function applyPricing() {
  if (!isDecimalAmount(pricingValue.value)) { ElMessage.error("统一定价值必须是合法十进制数，例如 10 或 0.68"); return; }
  if (!lines.value.length) { ElMessage.warning("请先添加模型"); return; }
  const value = Number(pricingValue.value);
  if (value <= 0) { ElMessage.error("统一定价值必须大于 0"); return; }
  let applied = 0;
  lines.value = lines.value.map(line => {
    if (pricingMode.value === "margin") {
      const quoteCoefficient = coefficientFromMargin(line.coefficient, pricingValue.value);
      if (!quoteCoefficient) return line;
      applied += 1;
      return { ...line, margin: pricingValue.value, quoteCoefficient };
    }
    applied += 1;
    return { ...line, quoteCoefficient: pricingValue.value, margin: marginFromCoefficient(line.coefficient, pricingValue.value) };
  });
  ElMessage.success(`已按「${PRICING_MODES.find(mode => mode.value === pricingMode.value)?.label}」统一 ${applied} 个模型`);
}
/** 统一去掉尾零（0.620000 → 0.62），避免把 8 位小数塞进表格。 */
function trimNumber(value: number) {
  return value.toFixed(8).replace(/0+$/, "").replace(/\.$/, "");
}
/** 行内「实际毛利率」：由当前报价系数反算，供填单时核对。 */
function actualMargin(line: QuoteLine) {
  if (!isDecimalAmount(line.quoteCoefficient)) return "—";
  const margin = marginFromCoefficient(line.coefficient, line.quoteCoefficient);
  return margin ? `${margin}%` : "—";
}
/** 单价的换算口径：官方价 × 报价系数（与采购系数、价项都无关）。 */
function priceFor(item: QuoteItem, line: QuoteLine) {
  const amount = Number(item.amount), quoted = Number(line.quoteCoefficient);
  if (!Number.isFinite(amount) || amount <= 0) return "";
  if (!Number.isFinite(quoted) || quoted <= 0) return "";
  return trimNumber(amount * quoted);
}
// 行内「提示」：把踩线情况直接写在行上，别让人自己去算。
// 平台不再设毛利红线，只有「低于 3%」这条提示线（对齐原型），低于采购成本才真的提交不过。
function lineHint(line: QuoteLine) {
  if (!isDecimalAmount(line.quoteCoefficient) || Number(line.quoteCoefficient) <= 0) return "待填报价系数";
  const margin = lineMargin(line);
  if (margin < 0) return "低于采购成本";
  // 不设红线，但要让人一眼看到「这行低于 3%，提交时会弹严重提示」。
  return margin < ALERT_MARGIN ? `毛利率 ${margin}% 低于 ${ALERT_MARGIN}%` : "—";
}
/** 行的实际毛利率（百分数，两位小数）。报价系数没填时返回 NaN，调用方先判空。 */
function lineMargin(line: QuoteLine) {
  const coefficient = Number(line.coefficient), quoted = Number(line.quoteCoefficient);
  if (!Number.isFinite(coefficient) || coefficient <= 0) return 0;
  if (!Number.isFinite(quoted) || quoted <= 0) return Number.NaN;
  return Number(((1 - coefficient / quoted) * 100).toFixed(2));
}
/** 毛利率低于 3% 的行：提交前要弹「严重报价提示」让销售知悉。 */
function severeLines() {
  return lines.value
    .filter(line => isDecimalAmount(line.quoteCoefficient) && Number(line.quoteCoefficient) > 0)
    .map(line => ({ modelName: line.modelName, margin: lineMargin(line) }))
    .filter(item => Number.isFinite(item.margin) && item.margin < ALERT_MARGIN);
}
function removeLine(modelKey: string) {
  lines.value = lines.value.filter(line => line.modelKey !== modelKey);
}

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

/** 模型分组键：官方 ID 优先，缺官方 ID 时退回模型名；再带上模型公司，避免同名不同厂。 */
function modelKeyOf(candidate: ResourceQuoteCandidate) {
  return `${candidate.official_id || candidate.model_name}::${candidate.company || ""}`;
}
/** 弹窗里一行 = 一个模型，来源（供应商）在报价行里选，与原型一致。 */
interface PickerGroup {
  key: string; modelName: string; company: string; modelType: string; sources: ResourceQuoteCandidate[];
}
const pickerGroups = computed<PickerGroup[]>(() => {
  const groups = new Map<string, PickerGroup>();
  for (const row of pickerRows.value) {
    const key = modelKeyOf(row);
    let group = groups.get(key);
    if (!group) {
      group = { key, modelName: row.model_name, company: row.company || "—", modelType: row.model_type || "—", sources: [] };
      groups.set(key, group);
    }
    group.sources.push(row);
  }
  // 表头写的就是「供应商（采购系数升序）」：来源一律按系数从低到高排。
  groups.forEach(group => group.sources.sort((a, b) => Number(a.coefficient) - Number(b.coefficient)));
  return [...groups.values()];
});
function coefficientRange(group: PickerGroup) {
  if (!group.sources.length) return "—";
  const values = group.sources.map(source => source.coefficient);
  return values.length === 1 ? values[0] : `${values[0]} – ${values[values.length - 1]}`;
}
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
    // 只取客户所在区域的资源：海外客户看不到国内资源，反之亦然。
    const result = await quoteCandidatesApi.list(pickerKeyword.value.trim(), page, 20, form.region);
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
function togglePick(modelKey: string) {
  if (pickerSelected.has(modelKey)) pickerSelected.delete(modelKey); else pickerSelected.add(modelKey);
}
function selectAllGroups() {
  pickerGroups.value.forEach(group => pickerSelected.add(group.key));
}
/** 采购成本 = 官方价 × 采购系数（行内展示「成本」，与候选口径一致）。 */
function unitCostOf(amount: string, coefficient: string) {
  const price = Number(amount), factor = Number(coefficient);
  if (!Number.isFinite(price) || price <= 0) return "";
  if (!Number.isFinite(factor) || factor <= 0) return "";
  return trimNumber(price * factor);
}

/**
 * 「继续编辑」回填的一行 → 编辑器行。
 *
 * 报价系数优先取**落库的原值**（000068 起每张单都存）。历史单据没有这个值，
 * 才用「已存单价 ÷ 官方价」兜一个编辑初值——那只是让编辑器能打开，
 * 用户保存后就会把系数正式写入，导出永远走落库值、不做反算。
 */
function lineFromEdit(dto: QuoteForEditLineDTO): QuoteLine {
  const coefficient = dto.coefficient || "";
  const items: QuoteItem[] = dto.price_items.map(item => ({
    key: item.item_key,
    label: item.label || item.item_key,
    amount: item.amount,
    unitCost: unitCostOf(item.amount, coefficient),
    currency: item.currency || form.currency,
  }));
  const stored = Number(dto.quotation_coefficient);
  let quoteCoefficient = Number.isFinite(stored) && stored > 0 ? trimNumber(stored) : "";
  if (!quoteCoefficient) {
    const ratios = dto.price_items
      .map(item => {
        const amount = Number(item.amount), unitPrice = Number(item.unit_price);
        return amount > 0 && unitPrice > 0 ? unitPrice / amount : NaN;
      })
      .filter(value => Number.isFinite(value) && value > 0);
    const quoted = ratios.length ? Math.min(...ratios) : NaN;
    quoteCoefficient = Number.isFinite(quoted) ? trimNumber(quoted) : "";
  }
  const supplierCoefficient = Number(coefficient);
  const margin = quoteCoefficient && Number.isFinite(supplierCoefficient) && supplierCoefficient > 0
    ? String(Number((100 * (1 - supplierCoefficient / Number(quoteCoefficient))).toFixed(1)))
    : DEFAULT_MARGIN;
  return {
    modelKey: `${dto.official_id || dto.model_name}::${dto.company || ""}`,
    resourceId: String(dto.resource_id),
    modelName: dto.model_name,
    company: dto.company || "",
    supplierName: dto.supplier_name,
    region: dto.region,
    coefficient,
    // 实测指标随明细一起带回来：只填模型不给指标，销售会以为这个来源没跑过测试。
    connectionMS: dto.test_connection_ms || "",
    rps: dto.test_rps || "",
    tps: dto.test_tps || "",
    cacheHitRate: dto.test_cache_hit_rate || "",
    currency: items[0]?.currency || form.currency,
    items,
    margin,
    quoteCoefficient,
  };
}

/** 一个来源 → 一条报价行；默认毛利率 10%，报价系数按采购系数换算。 */
function buildLine(modelKey: string, source: ResourceQuoteCandidate): QuoteLine {
  return {
    modelKey,
    resourceId: String(source.resource_id),
    modelName: source.model_name,
    company: source.company || "",
    supplierName: source.supplier_name,
    region: source.region,
    coefficient: source.coefficient,
    connectionMS: source.test_connection_ms || "",
    rps: source.test_rps || "",
    tps: source.test_tps || "",
    cacheHitRate: source.test_cache_hit_rate || "",
    currency: source.price_items[0]?.currency || form.currency,
    items: source.price_items.map(item => ({ key: item.item_key, label: item.label, amount: item.amount, unitCost: item.unit_cost, currency: item.currency })),
    margin: DEFAULT_MARGIN,
    quoteCoefficient: coefficientFromMargin(source.coefficient, DEFAULT_MARGIN),
  };
}
function confirmPicker() {
  if (!pickerSelected.size) { ElMessage.warning("请先在弹窗里勾选模型"); return; }
  const next = [...lines.value];
  let added = 0;
  for (const group of pickerGroups.value) {
    if (!pickerSelected.has(group.key)) continue;
    if (next.some(line => line.modelKey === group.key)) continue;
    const source = group.sources[0];
    if (!source) continue;
    next.push(buildLine(group.key, source));
    added += 1;
  }
  // 记录每个模型的可选来源，报价行里换供应商、看「对比来源」都靠它。
  const pool = new Map(sourcesByModel.value);
  pickerGroups.value.forEach(group => pool.set(group.key, group.sources));
  sourcesByModel.value = pool;
  lines.value = next;
  pickerVisible.value = false;
  if (!added) ElMessage.info("所选模型已在报价表中，未重复添加。");
}

function validateBeforeSubmit() {
  if (!form.region) return "请先选择客户业务区域";
  if (form.isNewCustomer) {
    if (!form.customerName.trim()) return "请填写客户名称。";
  } else if (!positiveId(form.customerId)) return "请先选择关联客户。";
  if (!form.ourCompany.trim()) return "请填写我方公司。";
  if (!lines.value.length) return "请至少添加一个模型。";
  const bad = lines.value.find(line => !positiveId(line.resourceId) || !isDecimalAmount(line.quoteCoefficient) || Number(line.quoteCoefficient) <= 0);
  if (bad) return `${bad.modelName} 的报价系数必须是大于 0 的普通十进制字符串。`;
  const over = lines.value.find(line => Number(line.margin) >= 100);
  if (over) return `${over.modelName} 的毛利率必须低于 100%。`;
  // 红线没了，但「卖得比采购成本还低」服务端仍会拒（floor = 采购成本），这里先拦一次给出人话。
  const under = lines.value.find(line => lineMargin(line) < 0);
  if (under) return `${under.modelName} 的报价低于采购成本，需走特价审批，不能直接提交。`;
  const unpriced = lines.value.find(line => line.items.some(item => !isDecimalAmount(priceFor(item, line))));
  if (unpriced) return `${unpriced.modelName} 有价项缺少官方价，无法换算报价单价。`;
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
/**
 * 提交/保存/导出的统一入口：先做「严重报价提示」拦截。
 * 平台不设毛利红线，只有毛利率 < 3% 时要销售明确知悉一次（点「已知悉」）才继续，与原型一致。
 */
const severeVisible = ref(false);
const severeRows = ref<Array<{ modelName: string; margin: number }>>([]);
let severeNext: null | (() => void) = null;
function requestSave(exportAfter: boolean, submitAfter = false) {
  // 先做基础校验：客户没选、系数没填这类硬问题不该被"严重报价提示"挡在前面。
  const invalid = validateBeforeSubmit();
  if (invalid) { ElMessage.error(invalid); return; }
  const risky = severeLines();
  if (risky.length) {
    severeRows.value = risky;
    severeNext = () => void saveDraft(exportAfter, submitAfter);
    severeVisible.value = true;
    return;
  }
  severeNext = null;
  void saveDraft(exportAfter, submitAfter);
}
function acknowledgeSevere() {
  const action = severeNext;
  severeNext = null;
  severeVisible.value = false;
  action?.();
}
async function saveDraft(exportAfter: boolean, submitAfter = false) {
  const invalid = validateBeforeSubmit();
  if (invalid) { ElMessage.error(invalid); return; }
  if (submitting.value) return;
  submitting.value = true;
  try {
    const customerId = await ensureCustomer();
    const validTo = new Date(Date.now() + 30 * 24 * 3600 * 1000).toISOString();
    // 一条报价行（模型 + 来源）展开成该来源的全部价项：单价 = 官方价 × 报价系数。
    // 系数一并提交——它是主数据，导出按系数出，不能事后从单价倒推。
    const items = lines.value.flatMap(line => line.items.map(item => ({
      resourceId: line.resourceId,
      priceItemKey: item.key,
      unitPrice: priceFor(item, line),
      coefficient: line.quoteCoefficient,
    })));
    const result = await customerQuotesApi.generateResource({ customerId, validTo, ourCompany: form.ourCompany.trim(), items });
    if (submitAfter) {
      // 原型编辑器底部就是「提交审批」：生成报价单后立刻送审，不要求先导出。
      // 关联重报要带上来源审批：后端据此只审被驳回的模型，已批准的不会重复派发。
      await quoteApprovalApi.submit(result.id, crypto.randomUUID(), reapplyFrom.value?.approvalId);
      ElMessage.success(reapplyFrom.value
        ? `报价单 #${result.id} 已按驳回模型重新提交审批（来源审批 #${reapplyFrom.value.approvalId}）。`
        : `报价单 #${result.id} 已提交审批，等待超管逐模型批准。`);
      reapplyFrom.value = null;
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
    // 已经落成新版本，「正在编辑 #x」的提示到此为止，避免下次误以为还在改那张旧单。
    editingQuote.value = null;
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
/**
 * 能不能审客户报价：既要有 M9:A 权限点，又要有超管标记位（can_approve_customer_quote）。
 * 只判 M9:A 不够——销售也持着它，于是销售能看到审批队列甚至批自己的价。
 * 两道门都过才算有审批权；后端 Decide 也按同一个标记位兜底。
 */
const canQuoteApprove = computed(() =>
  permissions.canAction(PERMISSIONS.CUSTOMER_QUOTE_APPROVE) && session.user?.canApproveCustomerQuote === true);
const quoteApprovalView = ref<"pending" | "history">("pending");

/**
 * 原型 3.10 的四项筛选：销售 / 状态 / 时间 / 关键字。
 * 时间字段随页签走（待审批看提交时间、审批记录看处理时间），由 stage 传给服务端决定。
 */
const approvalFilter = reactive({ salesperson: "", status: "", keyword: "", range: [] as string[] });
/** 销售下拉的选项：跨多次查询累积，避免筛选后自己的选项消失导致下拉变空。 */
const approvalSalespeople = ref<Array<{ id: number; name: string }>>([]);

function rememberSalespeople(rows: QuoteApprovalSummaryDTO[]) {
  const known = new Map(approvalSalespeople.value.map(item => [item.id, item]));
  for (const row of rows) {
    if (row.submitted_by && !known.has(row.submitted_by)) {
      known.set(row.submitted_by, { id: row.submitted_by, name: row.submitted_name || `员工 #${row.submitted_by}` });
    }
  }
  approvalSalespeople.value = [...known.values()].sort((a, b) => a.name.localeCompare(b.name, "zh-CN"));
}

const approvalFilterActive = computed(() =>
  Boolean(approvalFilter.salesperson || approvalFilter.status || approvalFilter.keyword.trim() || approvalFilter.range.length === 2));

function resetApprovalFilter() {
  approvalFilter.salesperson = "";
  approvalFilter.status = "";
  approvalFilter.keyword = "";
  approvalFilter.range = [];
}

async function loadQuoteApprovals() {
  quoteApprovalLoading.value = true;
  quoteApprovalError.value = "";
  try {
    quoteApprovalRows.value = await quoteApprovalApi.list({
      stage: quoteApprovalView.value,
      ...(approvalFilter.salesperson ? { submittedBy: approvalFilter.salesperson } : {}),
      ...(approvalFilter.status ? { status: approvalFilter.status } : {}),
      ...(approvalFilter.keyword.trim() ? { keyword: approvalFilter.keyword.trim() } : {}),
      ...(approvalFilter.range.length === 2 ? { dateFrom: approvalFilter.range[0], dateTo: approvalFilter.range[1] } : {}),
    });
    rememberSalespeople(quoteApprovalRows.value);
  } catch (error) {
    quoteApprovalRows.value = [];
    quoteApprovalError.value = message(error);
  } finally {
    quoteApprovalLoading.value = false;
  }
}

/** 当前正在看哪张审批的详情页（原型：列表点「查看」进详情页，详情里再开审批弹窗）。 */
const approvalDetailId = ref<number | null>(null);

function openApprovalDetail(row: QuoteApprovalSummaryDTO) {
  approvalDetailId.value = row.id;
}

/** 详情页里的「逐模型审批」→ 打开审批表单弹窗。 */
function openApprovalDecision(approvalId: number) {
  quoteApprovalTargetId.value = approvalId;
  quoteApprovalVisible.value = true;
}

/** 详情页里撤回后：列表与详情都要刷新。 */
async function onApprovalDetailChanged() {
  await loadQuoteApprovals();
  await loadQuotes();
}

/** 切页签要重新查：时间字段随阶段变（待审批→提交时间，审批记录→处理时间）。 */
function switchApprovalView(view: "pending" | "history") {
  if (quoteApprovalView.value === view) return;
  quoteApprovalView.value = view;
  void loadQuoteApprovals();
}

/** 弹窗处理完（批准 / 驳回 / 撤回）后刷新两张表。 */
async function onQuoteApprovalDecided() {
  await loadQuoteApprovals();
  await loadQuotes();
}

/**
 * 关联重报（原型「关联驳回模型新申请」）。
 *
 * 编辑器本身就是「保存即生成新版本」，所以这里只做两件事：切到报价页签、
 * 记住来源审批 id；提交审批时把它带上，后端会自动只把驳回模型纳入审批。
 */
const reapplyFrom = ref<{ approvalId: number; modelNames: string[] } | null>(null);

function enterReapply(payload: { approvalId: number; customerId: number; modelNames: string[] }, customerRow?: CustomerWorkspaceRow) {
  // 先建新单（startNewQuote 会清掉重报上下文），再挂上下文。
  startNewQuote(customerRow ?? rows.value.find(item => String(item.id) === String(payload.customerId)));
  reapplyFrom.value = { approvalId: payload.approvalId, modelNames: payload.modelNames };
  ElMessage.info(`本次只对驳回的 ${payload.modelNames.length} 个模型重新报价：${payload.modelNames.join("、")}`);
}

function onQuoteApprovalReapply(payload: { approvalId: number; customerId: number; modelNames: string[] }) {
  quoteApprovalVisible.value = false;
  enterReapply(payload);
}

function cancelReapply() {
  reapplyFrom.value = null;
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

const statusText = (status: string) => ({ EFFECTIVE: "历史记录", DRAFT: "草稿", PENDING: "待审批", APPROVED: "已批准", PARTIAL: "部分批准", EXPIRED: "历史记录", REJECTED: "已驳回", FORMAL: "历史记录" } as Record<string, string>)[status] || status;
/** 已生效/过期的报价单只读：不能继续编辑，只能看。 */
const isReadonlyQuote = (status: string) => ["EFFECTIVE", "EXPIRED", "FORMAL"].includes(status);
const quoteStatusType = (status: string) => status === "APPROVED" ? "success" : status === "PENDING" || status === "PARTIAL" ? "warning" : isReadonlyQuote(status) ? "info" : status === "REJECTED" ? "danger" : "";
const quoteTypeText = (type: string) => ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", RESOURCE: "按资源报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;
// 审批状态词与弹窗共用一份字典，避免「已通过 / 已分派」两处各叫各的。
const approvalStatusText = (status: string) => APPROVAL_STATUS_LABELS[status] || status;
const approvalStatusType = (status: string) => APPROVAL_STATUS_TYPES[status] || "warning";

function switchQuoteView(view: "edit" | "records" | "approvals" | "logs") {
  // 离开审批页签时退出详情视图，回来看到的是列表（与原型「返回报价审批」一致）。
  if (view !== "approvals") approvalDetailId.value = null;
  quoteView.value = view;
  if (view !== "edit" && !quoteRows.value.length) void loadQuotes();
  if (view === "approvals") void loadQuoteApprovals();
}

/** 已生效/过期的报价单是只读的：跳到该客户详情的「该客户报价」看明细。 */
function openQuoteRecord(row: CustomerQuoteRow) {
  void router.push({ path: `/internal/customers/${row.customer_id}`, query: { tab: "quotes" } });
}

/**
 * 回填后补「来源池」：行内的「换供应商 / 对比来源」读的是它。
 *
 * 只补池里还没有的模型，且**必须包含当前行已在用的来源**才写入——
 * 否则当前资源已经下架（候选里查不到）时，下拉会显示空白，反而像是来源丢了。
 * 拉不到就静默放弃，保持「单来源摆文本」的既有表现，不打断回填。
 */
async function hydrateSourcePool(rows: QuoteLine[]) {
  const pool = new Map(sourcesByModel.value);
  await Promise.all(rows.map(async line => {
    if (pool.has(line.modelKey)) return;
    try {
      // 按该行自己的区域拉来源（回填的历史单可能跨区域），缺失时回落到表单区域。
      const result = await quoteCandidatesApi.list(line.modelName, 1, 50, line.region || form.region);
      const grouped = new Map<string, ResourceQuoteCandidate[]>();
      for (const row of result.list) {
        const key = modelKeyOf(row);
        const list = grouped.get(key) ?? [];
        list.push(row);
        grouped.set(key, list);
      }
      const sources = grouped.get(line.modelKey);
      if (!sources?.some(source => String(source.resource_id) === line.resourceId)) return;
      pool.set(line.modelKey, sources);
    } catch {
      // 候选拉不到不影响回填：模型、价格、指标都已经在了。
    }
  }));
  sourcesByModel.value = pool;
}

/** 当前正在编辑哪张已存在的单（新建报价时为 null）。只用于顶部提示，不参与提交。 */
const editingQuote = ref<{ id: number; versionNo: number; status: string } | null>(null);
const editLoading = ref(false);

/**
 * 继续编辑一张报价单：把它的模型、来源、已报价全部带回编辑器。
 *
 * 之前这里只填了客户就 `lines = []`，于是「继续编辑」实际是开一张空白单——
 * 销售看到的「模型 0 个」就是这么来的。现在按后端回填的明细重建报价行。
 */
async function openRowQuote(row: CustomerQuoteRow) {
  activeTab.value = "quote";
  quoteView.value = "edit";
  reapplyFrom.value = null;
  form.customerId = String(row.customer_id);
  form.customerName = row.customer_name;
  // 我方公司随单存过就带出来；老单没存过则留空让销售补。
  form.ourCompany = row.our_company || "";
  lines.value = [];
  editingQuote.value = { id: Number(row.id), versionNo: row.version_no, status: row.status };
  editLoading.value = true;
  try {
    const detail = await customerQuotesApi.getForEdit(row.id);
    form.customerId = String(detail.customer_id);
    form.customerName = detail.customer_name || row.customer_name;
    form.ourCompany = detail.our_company || form.ourCompany;
    onCustomerChange(String(detail.customer_id));
    lines.value = detail.lines.map(lineFromEdit);
    // 来源池异步补，不阻塞回填（慢一步也不影响先看到模型与价格）。
    void hydrateSourcePool(lines.value);
    if (detail.skipped_items > 0) {
      ElMessage.warning(`这张单还有 ${detail.skipped_items} 条价目表口径的老明细带不过来，本次只带出 ${detail.lines.length} 个模型。`);
    }
  } catch (error) {
    ElMessage.error(message(error));
  } finally {
    editLoading.value = false;
  }
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
    // 从客户详情/工作台待办带过来的审批详情：进页签就直接看这张单。
    // 没有超管标记位的人（销售）即便是拿着链接进来也不进这个页签——
    // 列表接口对他们返回空，硬切过去只会看到「找不到」。
    const approvalId = Number(route.query.approval);
    if (canQuoteApprove.value && Number.isSafeInteger(approvalId) && approvalId > 0) {
      quoteView.value = "approvals";
      approvalDetailId.value = approvalId;
      void loadQuoteApprovals();
    }
    // 从客户详情/审批弹窗带过来的关联重报：查一次审批单拿到被驳回的模型名。
    const reapply = Number(route.query.reapply);
    if (Number.isSafeInteger(reapply) && reapply > 0) {
      try {
        const approval = await quoteApprovalApi.get(reapply);
        enterReapply({
          approvalId: reapply,
          customerId: Number(approval.customer_id) || Number(preset) || 0,
          modelNames: approval.rows.filter(row => row.result === "REJECTED").map(row => row.model_name || `标准模型 #${row.standard_model_id}`),
        }, positiveId(preset) ? rows.value.find(item => String(item.id) === preset) : undefined);
      } catch (error) {
        ElMessage.warning(`关联重报来源读取失败：${message(error)}`);
      }
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
          <button v-if="canQuoteApprove" class="tab" :class="{ active: quoteView === 'approvals' }" @click="switchQuoteView('approvals')">报价审批</button>
          <button class="tab" :class="{ active: quoteView === 'logs' }" @click="switchQuoteView('logs')">报价操作日志</button>
        </div>
        <el-button v-if="canQuote" @click="startNewQuote()">新建报价单</el-button>
      </div>

      <template v-if="quoteView === 'edit'">
        <!-- 继续编辑：明确「改的是哪一张单」，并说明保存会生成新版本（既有版本语义） -->
        <el-alert
          v-if="editingQuote"
          type="info"
          show-icon
          :closable="false"
          class="block-gap"
          :title="`正在编辑报价单 #${editingQuote.id} · V${editingQuote.versionNo}（${statusText(editingQuote.status)}）`"
        >
          <template #default>
            已带出这张单的模型与报价。保存会生成新版本（V{{ editingQuote.versionNo + 1 }}），原单保持不变；要作废原单请让审批人对它驳回。
          </template>
        </el-alert>
        <!-- 关联重报：只对上一轮被驳回的模型重新报价（原型「关联驳回模型新申请」） -->
        <el-alert
          v-if="reapplyFrom"
          type="warning"
          show-icon
          :closable="true"
          class="block-gap"
          :title="`关联重报 · 来源审批 #${reapplyFrom.approvalId}`"
          @close="cancelReapply"
        >
          <template #default>
            本次提交只会把驳回的 {{ reapplyFrom.modelNames.length }} 个模型送审：{{ reapplyFrom.modelNames.join("、") }}；已批准的模型不会重复送审，也不会重复派待办。
          </template>
        </el-alert>
        <div class="card">
          <h3 class="card-title">报价信息</h3>
          <div class="form-grid">
            <div class="field">
              <label>关联客户</label>
              <el-select v-model="form.customerId" filterable clearable placeholder="搜索客户名称" @change="onCustomerChange">
                <el-option v-for="item in filteredCustomers" :key="item.id" :value="item.id" :label="item.id === NEW_CUSTOMER_ID ? '+ 新建客户' : `${item.name} · ${regionText(item.region)} · ${item.owner}`" />
              </el-select>
            </div>
            <div class="field"><label>客户名称</label><el-input v-model="form.customerName" :disabled="!form.isNewCustomer" :placeholder="form.isNewCustomer ? '填写新客户名称' : '选择关联客户后自动带出'" /></div>
            <div class="field"><label>我方公司</label><el-input v-model="form.ourCompany" maxlength="128" placeholder="填写签约主体公司" /></div>
            <div class="field">
              <label>国内 / 海外业务区域</label>
              <el-select v-model="form.region" placeholder="请选择" @change="onRegionChange">
                <el-option value="CN" label="国内" /><el-option value="OVERSEAS" label="海外" />
              </el-select>
            </div>
            <div class="field"><label>结算币种</label><el-input :model-value="currencyText(form.currency)" disabled /></div>
            <div class="field field-wide"><label>商业条件</label><el-input v-model="form.remark" type="textarea" :rows="3" maxlength="200" placeholder="付款方式 / 账期 / 最低消费等" /></div>
          </div>
        </div>

        <div class="card">
          <div class="card-head">
            <div class="card-head-left">
              <el-button type="primary" :disabled="!canQuote" @click="openPicker">＋ 添加 / 选择模型</el-button>
              <span class="muted">已选 {{ lines.length }} 个</span>
            </div>
            <div class="card-head-right">
              <el-select v-model="pricingMode" style="width: 150px" aria-label="统一定价方式" @change="onPricingModeChange">
                <el-option v-for="mode in PRICING_MODES" :key="mode.value" :value="mode.value" :label="mode.label" />
              </el-select>
              <el-input v-model="pricingValue" class="margin-input" :placeholder="pricingHint" aria-label="统一定价值" />
              <el-button @click="applyPricing">统一当前全部模型</el-button>
            </div>
          </div>

          <el-table v-if="lines.length" :data="lines" row-key="modelKey" :expand-row-keys="expandedModels" class="grid">
            <!-- 对比来源：像原型那样在行下展开一张来源列表，而不是弹一个浮层 -->
            <el-table-column type="expand" width="1" class-name="compare-expand">
              <template #default="{ row: line }">
                <div class="source-compare">
                  <el-table :data="sourcesOf(line.modelKey)" size="small">
                    <el-table-column label="供应商名称" min-width="170">
                      <template #default="{ row: source }">
                        <div>{{ source.supplier_name }}</div>
                        <div class="cell-sub">{{ source.supplier_model_id }}</div>
                      </template>
                    </el-table-column>
                    <el-table-column label="部署节点" width="90"><template #default="{ row: source }">{{ regionText(source.region) }}</template></el-table-column>
                    <el-table-column label="采购系数" width="100" align="right"><template #default="{ row: source }"><span class="mono">{{ source.coefficient }}</span></template></el-table-column>
                    <el-table-column label="连通耗时 · ms" width="120" align="right"><template #default="{ row: source }"><span class="mono">{{ source.test_connection_ms || "未测试" }}</span></template></el-table-column>
                    <el-table-column label="压力 · RPS" width="110" align="right"><template #default="{ row: source }"><span class="mono">{{ source.test_rps || "未测试" }}</span></template></el-table-column>
                    <el-table-column label="压力 · TPS" width="110" align="right"><template #default="{ row: source }"><span class="mono">{{ source.test_tps || "未测试" }}</span></template></el-table-column>
                    <el-table-column label="输入缓存命中率" min-width="140" align="right"><template #default="{ row: source }"><span class="mono">{{ source.test_cache_hit_rate ? source.test_cache_hit_rate + "%" : "未提供" }}</span></template></el-table-column>
                    <el-table-column label="操作" width="80">
                      <template #default="{ row: source }">
                        <el-button link type="primary" :disabled="String(source.resource_id) === line.resourceId" @click="onSupplierChange(line, String(source.resource_id))">
                          {{ String(source.resource_id) === line.resourceId ? "当前" : "选用" }}
                        </el-button>
                      </template>
                    </el-table-column>
                  </el-table>
                  <div class="table-count">共 {{ sourcesOf(line.modelKey).length }} 条</div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="模型" min-width="150">
              <template #default="{ row }">
                <div class="cell-main">{{ row.modelName }}</div>
                <div class="cell-sub">{{ row.company || regionText(row.region) }}</div>
              </template>
            </el-table-column>
            <el-table-column label="供应商（采购系数升序）" min-width="230">
              <template #default="{ row }">
                <!-- 多来源才给下拉+对比；只有一个来源时摆文本，免得"下拉已写明供应商"下面再重复一行。 -->
                <template v-if="sourcesOf(row.modelKey).length > 1">
                  <el-select :model-value="row.resourceId" size="small" aria-label="供应商" @change="onSupplierChange(row, $event)">
                    <el-option v-for="source in sourcesOf(row.modelKey)" :key="source.resource_id" :value="String(source.resource_id)" :label="`${source.supplier_name} · ${source.coefficient}`" />
                  </el-select>
                  <el-button link type="primary" size="small" @click="toggleCompare(row.modelKey)">
                    {{ expandedModels.includes(row.modelKey) ? "收起来源" : `对比来源（${sourcesOf(row.modelKey).length}）` }}
                  </el-button>
                </template>
                <!-- 只有一个来源时没有"对比"可言，直接摆供应商名（区域作为副行），避免和下拉里的文案重复。 -->
                <div v-else class="cell-sub">{{ row.supplierName }} · {{ regionText(row.region) }}</div>
              </template>
            </el-table-column>
            <el-table-column label="采购系数" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template></el-table-column>
            <el-table-column label="连通 · ms" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.connectionMS || "未测试" }}</span></template></el-table-column>
            <el-table-column label="RPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.rps || "未测试" }}</span></template></el-table-column>
            <el-table-column label="TPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.tps || "未测试" }}</span></template></el-table-column>
            <el-table-column label="输入缓存命中率" width="130" align="right"><template #default="{ row }"><span class="mono">{{ row.cacheHitRate ? row.cacheHitRate + "%" : "未提供" }}</span></template></el-table-column>
            <el-table-column label="毛利率 %" width="140">
              <template #default="{ row }">
                <el-input v-model="row.margin" size="small" aria-label="目标毛利率" @change="onMarginChange(row)" />
                <div class="cell-sub" :class="{ warn: lineMargin(row) < ALERT_MARGIN }">实际：{{ actualMargin(row) }}</div>
              </template>
            </el-table-column>
            <el-table-column label="报价系数" width="120">
              <template #default="{ row }"><el-input v-model="row.quoteCoefficient" size="small" aria-label="报价系数" @change="onQuoteCoefficientChange(row)" /></template>
            </el-table-column>
            <el-table-column label="提示" min-width="180">
              <template #default="{ row }"><span :class="{ warn: lineHint(row) !== '—' }">{{ lineHint(row) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }"><el-button link type="danger" @click="removeLine(row.modelKey)">移除</el-button></template>
            </el-table-column>
          </el-table>
          <div v-else class="empty-hint">点击添加模型，在弹窗中搜索并勾选</div>

          <div v-if="lines.length" class="table-count">共 {{ lines.length }} 条</div>

          <div class="card-foot">
            <div class="foot-actions">
              <el-button type="primary" :loading="submitting" :disabled="!canQuote" @click="requestSave(false, true)">提交审批</el-button>
            </div>
            <div class="foot-actions">
              <el-button :loading="submitting" :disabled="!canQuote" @click="requestSave(false)">保存草稿</el-button>
              <el-button :loading="submitting" :disabled="!canQuote" @click="requestSave(true)">导出报价单</el-button>
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
        <QuoteApprovalDetail
          v-if="approvalDetailId"
          :approval-id="approvalDetailId"
          :can-approve="canQuoteApprove"
          :staff-id="session.user?.staffId ? Number(session.user.staffId) : 0"
          @back="approvalDetailId = null"
          @decide="openApprovalDecision"
          @changed="onApprovalDetailChanged"
          @reapply="onQuoteApprovalReapply"
        />
        <template v-else>
        <div class="subtabs">
          <button class="subtab" :class="{ active: quoteApprovalView === 'pending' }" @click="switchApprovalView('pending')">
            待审批{{ quoteApprovalView === "pending" ? `（${quoteApprovalRows.length}）` : "" }}
          </button>
          <button class="subtab" :class="{ active: quoteApprovalView === 'history' }" @click="switchApprovalView('history')">审批记录</button>
        </div>
        <!-- 原型 3.10 的筛选条：销售 / 状态 / 时间 / 关键字 -->
        <div class="approval-filters">
          <label>销售
            <el-select v-model="approvalFilter.salesperson" clearable placeholder="全部" style="width:140px" @change="loadQuoteApprovals">
              <el-option v-for="person in approvalSalespeople" :key="person.id" :label="person.name" :value="String(person.id)" />
            </el-select>
          </label>
          <label>状态
            <el-select v-model="approvalFilter.status" clearable placeholder="全部" style="width:140px" @change="loadQuoteApprovals">
              <el-option v-for="(label, value) in APPROVAL_STATUS_LABELS" :key="value" :label="label" :value="value" />
            </el-select>
          </label>
          <label>{{ quoteApprovalView === "pending" ? "提交时间" : "处理时间" }}
            <el-date-picker
              v-model="approvalFilter.range"
              type="daterange"
              value-format="YYYY-MM-DD"
              start-placeholder="开始"
              end-placeholder="结束"
              style="width:250px"
              @change="loadQuoteApprovals"
            />
          </label>
          <el-input
            v-model="approvalFilter.keyword"
            clearable
            placeholder="搜索报价编号 / 客户"
            style="width:220px"
            @keyup.enter="loadQuoteApprovals"
            @clear="loadQuoteApprovals"
          />
          <el-button @click="resetApprovalFilter(); loadQuoteApprovals()">重置</el-button>
          <span v-if="approvalFilterActive" class="hint">已按条件筛选</span>
        </div>
        <el-alert v-if="quoteApprovalError" :title="quoteApprovalError" type="error" show-icon :closable="false" />
        <el-table
          v-loading="quoteApprovalLoading"
          :data="quoteApprovalRows"
          row-key="id"
          class="grid"
          :empty-text="quoteApprovalView === 'pending' ? '暂无待审批的报价单' : '暂无审批记录'"
        >
          <el-table-column label="报价单 ID" width="120"><template #default="{ row }">#{{ row.quote_id }}</template></el-table-column>
          <el-table-column label="客户" min-width="180">
            <template #default="{ row }">{{ row.customer_name || `客户 #${row.customer_id}` }}</template>
          </el-table-column>
          <el-table-column label="销售" width="120">
            <template #default="{ row }">{{ row.submitted_name || `员工 #${row.submitted_by}` }}</template>
          </el-table-column>
          <el-table-column :label="quoteApprovalView === 'pending' ? '提交时间' : '处理时间'" width="180">
            <template #default="{ row }">
              {{ formatDateTime(quoteApprovalView === "pending" ? row.submitted_at : row.decided_at, "—", false) }}
            </template>
          </el-table-column>
          <el-table-column label="模型数" width="90" align="right"><template #default="{ row }"><span class="mono">{{ row.model_count }}</span></template></el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }"><el-tag size="small" :type="approvalStatusType(row.status)">{{ approvalStatusText(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="操作" width="110">
            <template #default="{ row }">
              <el-button link type="primary" @click="openApprovalDetail(row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        </template>
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

    <!-- 选择报价模型（对齐原型：按模型勾选，供应商在报价行里选）。
         标题写明区域，让销售一眼知道「这里只可能出现该区域的货源」。 -->
    <el-dialog v-model="pickerVisible" :title="`选择报价模型 · 仅显示${regionText(form.region)}资源`" width="min(880px, 96vw)">
      <div class="picker-bar">
        <el-input v-model="pickerKeyword" placeholder="搜索模型 / 供应商 / 官方 ID" clearable @keyup.enter="searchCandidates(false)" />
        <el-button type="primary" @click="searchCandidates(false)">搜索</el-button>
        <el-button text @click="selectAllGroups">全选当前筛选</el-button>
        <el-button text @click="pickerSelected.clear()">取消当前筛选选择</el-button>
        <span class="muted">已选 {{ pickerSelected.size }} 个模型</span>
      </div>
      <el-alert v-if="pickerError" :title="pickerError" type="error" show-icon :closable="false" />
      <el-table v-loading="pickerLoading" :data="pickerGroups" row-key="key" max-height="380">
        <el-table-column width="44">
          <template #default="{ row }">
            <el-checkbox :model-value="pickerSelected.has(row.key)" @change="togglePick(row.key)" />
          </template>
        </el-table-column>
        <el-table-column label="模型" min-width="200">
          <template #default="{ row }">
            <div class="cell-main">{{ row.modelName }}</div>
            <div class="cell-sub">{{ row.company }} · {{ row.modelType }}</div>
          </template>
        </el-table-column>
        <el-table-column label="采购系数" width="150" align="right">
          <template #default="{ row }"><span class="mono">{{ coefficientRange(row) }}</span></template>
        </el-table-column>
        <el-table-column label="可选来源" width="100" align="right">
          <template #default="{ row }">{{ row.sources.length }} 个</template>
        </el-table-column>
        <template #empty><el-empty description="没有可报价的模型（需已接入、正式配置已确认、测试证据有效）" :image-size="64" /></template>
      </el-table>
      <p class="picker-note">只展示对应区域已成功接入的可用资源；在报价表中手动选择具体供应商。</p>
      <el-button v-if="pickerRows.length < pickerTotal" text :loading="pickerLoading" @click="searchCandidates(true)">加载更多</el-button>
      <template #footer>
        <el-button @click="pickerVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmPicker">确认选择</el-button>
      </template>
    </el-dialog>

    <!-- 严重报价提示：毛利率 < 3% 时先让销售明确知悉，点「已知悉」才继续提交（对齐原型） -->
    <el-dialog v-model="severeVisible" title="严重报价提示" width="min(520px, 94vw)" append-to-body>
      <p class="severe-lead">
        以下模型的毛利率低于 {{ ALERT_MARGIN }}%，属于严重低价。平台不会拦截，但提交后审批人会重点核。
      </p>
      <el-table :data="severeRows" size="small">
        <el-table-column prop="modelName" label="模型" min-width="160" />
        <el-table-column label="毛利率" width="110" align="right">
          <template #default="{ row }"><span class="mono warn">{{ row.margin }}%</span></template>
        </el-table-column>
      </el-table>
      <p class="severe-tip">确认无误请点「已知悉」继续；要改价请点「返回修改」。</p>
      <template #footer>
        <el-button @click="severeVisible = false">返回修改</el-button>
        <el-button type="primary" @click="acknowledgeSevere">已知悉</el-button>
      </template>
    </el-dialog>

    <!-- 报价审批（逐模型批准 / 驳回）：与客户详情共用同一组件 -->
    <QuoteApprovalDialog
      v-model:visible="quoteApprovalVisible"
      :approval-id="quoteApprovalTargetId"
      :can-approve="canQuoteApprove"
      :staff-id="session.user?.staffId ? Number(session.user.staffId) : 0"
      @decided="onQuoteApprovalDecided"
      @reapply="onQuoteApprovalReapply"
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
.approval-filters { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-bottom: 12px; font-size: 12.5px; color: var(--app-text-secondary); }
.approval-filters label { display: flex; align-items: center; gap: 6px; }
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
.card-foot { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-top: 16px; flex-wrap: wrap; }
/* 报价单里的「共 N 条」与列表底部计数同款：贴着表格右下，轻量不抢视线。 */
.table-count { text-align: right; color: var(--app-text-muted); font-size: 12px; margin-top: 8px; }
/* 商业条件整行铺满（原型是 textarea，不是单行输入）。 */
.field-wide { grid-column: 1 / -1; }
.picker-note { color: var(--app-text-muted); font-size: 12px; margin: 10px 0 0; }
/* 对比来源：行下展开的来源列表。展开箭头隐藏，靠「对比来源（N）」按钮收放。 */
.compare-expand .el-table__expand-icon { display: none; }
.compare-expand .cell { padding: 0; }
.source-compare { padding: 8px 12px 4px 24px; background: var(--el-fill-color-lighter); }
.source-compare .el-table { background: transparent; }
/* 严重报价提示：语气要"提醒"不要"报错"，所以用暖色而不是红色。 */
.severe-lead { margin: 0 0 12px; font-size: 13px; line-height: 1.7; color: #8a5a12; background: #fff8ea; border: 1px solid #f2e2c2; border-radius: 8px; padding: 10px 12px; }
.severe-tip { margin: 12px 0 0; font-size: 12px; color: var(--app-text-muted); }
.foot-actions { display: flex; gap: 10px; }
.picker-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.approval-grid { margin-top: 18px; }
.dialog-form { margin-top: 16px; }
.form-error { margin-top: 14px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.warn { color: #b87327; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .card-foot { flex-direction: column; align-items: stretch; } }
</style>
