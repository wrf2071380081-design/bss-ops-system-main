<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { supplyChainApi, type SupplierWorkspaceRow } from "../../../api/supplyChain";
import { formatSupplierTaxRate } from "../../../domain/supplierTax";
import { PERMISSIONS } from "../../../domain/permissions";
import { usePermissionStore } from "../../../stores/permission";
import SupplierQuoteWorkspace from "./SupplierQuoteWorkspace.vue";

const route = useRoute();
const router = useRouter();
const permissions = usePermissionStore();
const routeFilter = (key: string) => typeof route.query[key] === "string" ? route.query[key] : "";
const routePage = Number(routeFilter("page"));
const routeSize = Number(routeFilter("size"));
const supplierSortFields = ["id", "name", "region", "resource_count", "contract_status", "cooperation_stage", "handling_status"] as const;
type SupplierSortField = (typeof supplierSortFields)[number];
// 二级 Tab：供应商列表 / 供应商报价。没有报价查看权限时「供应商报价」不渲染，深链回落列表。
type WorkspaceTab = "list" | "quote";
const canViewQuotes = computed(() => permissions.canAction(PERMISSIONS.SUPPLIER_QUOTE_VIEW));
const activeTab = ref<WorkspaceTab>(routeFilter("tab") === "quote" && canViewQuotes.value ? "quote" : "list");

/** 只改 query，不 push，避免污染浏览器历史。 */
function stringQuery() {
  const query: Record<string, string> = {};
  for (const [key, value] of Object.entries(route.query)) if (typeof value === "string") query[key] = value;
  return query;
}
function syncTabQuery(tab: WorkspaceTab) {
  const current = stringQuery();
  const next: Record<string, string> = { ...current };
  if (tab === "quote") next.tab = "quote";
  else { delete next.tab; delete next.sub; delete next.approval; }
  const keys = new Set([...Object.keys(current), ...Object.keys(next)]);
  for (const key of keys) if ((current[key] ?? "") !== (next[key] ?? "")) { void router.replace({ path: route.path, query: next }); return; }
}
watch(activeTab, tab => syncTabQuery(tab));
watch(() => route.query.tab, value => { const next: WorkspaceTab = value === "quote" && canViewQuotes.value ? "quote" : "list"; if (next !== activeTab.value) activeTab.value = next; });

const suppliers = ref<SupplierWorkspaceRow[]>([]);
const supplierLoading = ref(false);
const supplierError = ref("");
const supplierPage = ref(Number.isSafeInteger(routePage) && routePage > 0 ? routePage : 1);
const supplierPageSize = ref([10, 20, 50, 100].includes(routeSize) ? routeSize : 20);
const supplierTotal = ref(0);
let supplierLoadRevision = 0;
const supplierSortBy = ref<SupplierSortField>(supplierSortFields.includes(routeFilter("sort_by") as SupplierSortField) ? routeFilter("sort_by") as SupplierSortField : "id");
const supplierSortOrder = ref<"asc" | "desc">(routeFilter("sort_order") === "desc" ? "desc" : "asc");
type SupplierContractStatus = SupplierWorkspaceRow["contract_status"];
const contractStatuses: SupplierContractStatus[] = ["已归档", "办理中", "未登记"];
const routeContractStatus = routeFilter("contract");
const filters = reactive<{ keyword: string; region: string; contract: SupplierContractStatus | ""; stage: string; handling: string }>({ keyword: routeFilter("keyword"), region: routeFilter("region"), contract: contractStatuses.includes(routeContractStatus as SupplierContractStatus) ? routeContractStatus as SupplierContractStatus : "", stage: routeFilter("stage"), handling: routeFilter("handling") });

const regionLabel = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
// 原型文案：合作阶段显示「合作」，合同状态「未登记」显示「暂无」（筛选值仍是后端枚举）。
const stageText = (stage: string) => stage === "已有" ? "合作" : stage;
const contractStatusText = (status: string) => status === "未登记" ? "暂无" : status;
const contractStatusType = (status: SupplierWorkspaceRow["contract_status"]) => status === "已归档" ? "info" : status === "办理中" ? "primary" : "info";
// 原型：结算币种显示「人民币 CNY / 美元 USD」。
const currencyText = (code: string) => code === "USD" ? "美元 USD" : code === "CNY" ? "人民币 CNY" : code || "—";
const contractStatusPlain = (status: string) => status === "未登记";
const currencyAmount = (row: SupplierWorkspaceRow) => {
  if (row.credit_status === "AMBIGUOUS_VERSION") return "版本冲突，待核对";
  if (row.credit_status === "INVALID_LEGACY_AMOUNT") return "额度异常，待核对";
  if (!row.credit_amount) return "待记录";
  const symbol = row.settlement_currency === "USD" ? "$" : "¥";
  const amount = Number(row.credit_amount);
  return Number.isFinite(amount) ? `${symbol}${amount.toLocaleString()}` : row.credit_amount;
};

const filteredSuppliers = computed(() => suppliers.value);

async function loadSuppliers() {
  const revision = ++supplierLoadRevision;
  supplierLoading.value = true;
  supplierError.value = "";
  try {
    const result = await supplyChainApi.suppliers({
      page: supplierPage.value, size: supplierPageSize.value,
      keyword: filters.keyword.trim(), region: filters.region, contract_status: filters.contract || undefined,
      cooperation_stage: filters.stage, handling_status: filters.handling,
      sort_by: supplierSortBy.value, sort_order: supplierSortOrder.value,
    });
    if (revision !== supplierLoadRevision) return;
    if (supplierPage.value > 1 && result.list.length === 0 && result.total > 0) {
      supplierPage.value = Math.ceil(result.total / supplierPageSize.value);
      await loadSuppliers(); return;
    }
    suppliers.value = result.list;
    supplierTotal.value = result.total;
  }
  catch (value) {
    if (revision !== supplierLoadRevision) return;
    suppliers.value = [];
    supplierTotal.value = 0;
    supplierError.value = value instanceof Error ? value.message : "供应商列表加载失败。";
  }
  finally { if (revision === supplierLoadRevision) supplierLoading.value = false; }
}

function searchSuppliers() { supplierPage.value = 1; void loadSuppliers(); }
function changeSupplierPage(page: number) { supplierPage.value = page; void loadSuppliers(); }
function changeSupplierPageSize(size: number) { supplierPageSize.value = size; supplierPage.value = 1; void loadSuppliers(); }
function supplierSortChanged({ prop, order }: { prop?: string; order?: string | null }) {
  if (!order) { supplierSortBy.value = "id"; supplierSortOrder.value = "asc"; }
  else {
    const allowed = ["id", "name", "region", "resource_count", "contract_status", "cooperation_stage", "handling_status"] as const;
    if (!prop || !allowed.includes(prop as (typeof allowed)[number])) return;
    supplierSortBy.value = prop as (typeof allowed)[number];
    supplierSortOrder.value = order === "descending" ? "desc" : "asc";
  }
  searchSuppliers();
}

function openSupplier(row: SupplierWorkspaceRow) {
  const id = String(row.id);
  if (!/^[1-9]\d*$/.test(id)) { ElMessage.error("供应商标识无效，无法打开详情。"); return; }
  void router.push({ path: `/internal/suppliers/${id}/models`, query: {
    return_to: "suppliers", supplier_name: row.name,
    supplier_keyword: filters.keyword, supplier_region: filters.region,
    supplier_contract: filters.contract, supplier_stage: filters.stage, supplier_handling: filters.handling,
    supplier_page: String(supplierPage.value), supplier_size: String(supplierPageSize.value),
    supplier_sort_by: supplierSortBy.value, supplier_sort_order: supplierSortOrder.value,
  } });
}
function openSupplierEntities() {
  const query: Record<string, string> = { return_to: "suppliers" };
  if (filters.keyword) query.keyword = filters.keyword;
  if (filters.region) query.region = filters.region;
  if (filters.keyword) query.return_keyword = filters.keyword;
  if (filters.region) query.return_region = filters.region;
  if (filters.contract) query.return_contract = filters.contract;
  if (filters.stage) query.return_stage = filters.stage;
  if (filters.handling) query.return_handling = filters.handling;
  query.return_page = String(supplierPage.value); query.return_size = String(supplierPageSize.value);
  query.return_sort_by = supplierSortBy.value; query.return_sort_order = supplierSortOrder.value;
  void router.push({ path: "/internal/supplier-entities", query });
}

function openWorkbenchTodos() { void router.push({ path: "/internal/workbench" }); }
function resetFilters() { Object.assign(filters, { keyword: "", region: "", contract: "", stage: "", handling: "" }); searchSuppliers(); }
onMounted(() => {
  // 无报价权限时把 ?tab=quote 回落到列表 Tab 并清掉参数，避免刷新后停在空白页。
  if (routeFilter("tab") || routeFilter("sub") || routeFilter("approval")) syncTabQuery(activeTab.value);
  void loadSuppliers();
});
onBeforeUnmount(() => { supplierLoadRevision += 1; });
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">INTERNAL / SUPPLIERS</div><h1>供应商管理</h1><p>查看供应商主体、合作阶段、合同状态与业务摘要。</p></div>
    <div class="heading-actions">
      <el-button @click="openSupplierEntities">供应商主体档案</el-button>
      <el-button @click="openWorkbenchTodos">打开工作台待办</el-button>
      <el-button :loading="supplierLoading" @click="loadSuppliers">刷新</el-button>
    </div>
  </section>

  <section class="panel supplier-workspace">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="供应商列表" name="list">
        <el-alert v-if="supplierError" :title="supplierError" type="error" show-icon :closable="false" />
        <div class="filters">
          <el-select v-model="filters.region" placeholder="国内 / 海外主体" clearable style="width: 140px" @change="searchSuppliers">
            <el-option value="CN" label="国内" /><el-option value="OVERSEAS" label="海外" />
          </el-select>
          <el-select v-model="filters.contract" placeholder="合同状态" clearable style="width: 120px" @change="searchSuppliers">
            <el-option value="已归档" label="已归档" /><el-option value="办理中" label="办理中" /><el-option value="未登记" label="暂无" />
          </el-select>
          <el-select v-model="filters.stage" placeholder="合作/潜在" clearable style="width: 120px" @change="searchSuppliers">
            <el-option value="潜在" label="潜在" /><el-option value="已有" label="已有" />
          </el-select>
          <el-select v-model="filters.handling" placeholder="办理状态" clearable style="width: 150px" @change="searchSuppliers">
            <el-option value="有未完成事项" label="有未完成事项" /><el-option value="无未完成事项" label="无未完成事项" />
          </el-select>
          <el-input v-model="filters.keyword" placeholder="搜索供应商名称 / 曾用名" clearable class="filters-keyword" @change="searchSuppliers" @keyup.enter="searchSuppliers" />
          <el-button @click="resetFilters">重置</el-button>
        </div>
        <el-table v-loading="supplierLoading" :data="filteredSuppliers" :default-sort="{ prop: supplierSortBy, order: supplierSortOrder === 'desc' ? 'descending' : 'ascending' }" style="width: 100%" @sort-change="supplierSortChanged">
          <el-table-column prop="name" label="供应商名称" min-width="160" sortable="custom" />
          <el-table-column prop="resource_count" label="资源数" width="80" sortable="custom" />
          <el-table-column prop="region" label="国内 / 海外" width="100" sortable="custom"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
          <el-table-column label="授信额度" width="130">
            <template #default="{ row }"><strong>{{ currencyAmount(row) }}</strong></template>
          </el-table-column>
          <el-table-column prop="contract_status" label="合同状态" width="110" sortable="custom">
            <template #default="{ row }"><el-tag :type="contractStatusType(row.contract_status)" :effect="contractStatusPlain(row.contract_status) ? 'plain' : 'light'" size="small">{{ contractStatusText(row.contract_status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="结算币种" width="120"><template #default="{ row }">{{ currencyText(row.settlement_currency) }}</template></el-table-column>
          <el-table-column label="税率" width="90"><template #default="{ row }">{{ formatSupplierTaxRate(row.tax_rate) }}</template></el-table-column>
          <el-table-column prop="cooperation_stage" label="合作/潜在" width="110" sortable="custom">
            <template #default="{ row }"><el-tag type="info" :effect="row.cooperation_stage === '已有' ? 'light' : 'plain'" size="small">{{ stageText(row.cooperation_stage) }}</el-tag></template>
          </el-table-column>
          <el-table-column prop="handling_status" label="办理状态" width="130" sortable="custom">
            <template #default="{ row }">{{ row.handling_status }}</template>
          </el-table-column>
          <el-table-column label="详情" width="80"><template #default="{ row }"><el-button size="small" @click="openSupplier(row)">详情</el-button></template></el-table-column>
          <template #empty><el-empty description="暂无供应商" :image-size="64" /></template>
        </el-table>
        <el-pagination v-if="supplierTotal > 0" class="supplier-pagination" :current-page="supplierPage" :page-size="supplierPageSize" :page-sizes="[10, 20, 50, 100]" :total="supplierTotal" layout="total, sizes, prev, pager, next" @current-change="changeSupplierPage" @size-change="changeSupplierPageSize" />
      </el-tab-pane>

      <el-tab-pane v-if="canViewQuotes" label="供应商报价" name="quote" lazy>
        <SupplierQuoteWorkspace />
      </el-tab-pane>
    </el-tabs>
  </section>

</template>

<style scoped>
.supplier-workspace { padding: 18px 20px; }
.filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.filters-keyword { flex: 1; min-width: 240px; }
.supplier-pagination { justify-content: flex-end; margin-top: 16px; }
.muted { display: block; color: var(--app-text-muted); font-size: 12px; margin-top: 4px; }
</style>
