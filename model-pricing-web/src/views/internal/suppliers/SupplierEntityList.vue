<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import Decimal from "decimal.js";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO, SupplierEntityForm } from "../../../api/supplierEntities.contract";
import { ApiError } from "../../../domain/common";
import SupplierProfileForm from "./SupplierProfileForm.vue";
import SupplierMergePreviewDialog from "./SupplierMergePreviewDialog.vue";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";

const regionLabels = { CN: "国内", OVERSEAS: "海外" } as const;
const currencyLabels = { CNY: "人民币 CNY", USD: "美元 USD" } as const;
const route = useRoute(), router = useRouter();
const routeValue = (value: unknown) => typeof value === "string" ? value : Array.isArray(value) && typeof value[0] === "string" ? value[0] : "";
const rows = ref<SupplierEntityDTO[]>([]), loading = ref(false), error = ref("");
const aliasesSupported = ref<boolean>();
const filters = reactive({ keyword: routeValue(route.query.keyword), region: routeValue(route.query.region) });
const page = ref(1), size = ref(20), total = ref(0);
const dialogOpen = ref(false), saving = ref(false), reloadingConflict = ref(false), saveError = ref(""), saveConflict = ref(false), profileReloadKey = ref(0), current = ref<SupplierEntityDTO>();
const mergePreviewOpen = ref(false), mergePreviewSource = ref<SupplierEntityDTO>();
const f12Open = ref(false), f12Supplier = ref<SupplierEntityDTO>();
const message = (value: unknown) => value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";
let listRequest: AbortController | undefined;
let active = true;

async function load() {
  listRequest?.abort();
  const request = listRequest = new AbortController();
  loading.value = true; error.value = "";
  try {
    const result = await supplierEntitiesApi.list({ page: page.value, size: size.value, keyword: filters.keyword.trim() || undefined, region: filters.region || undefined }, request.signal);
    if (!active || request.signal.aborted) return;
    if (page.value > 1 && result.list.length === 0 && result.total > 0) {
      page.value = Math.ceil(result.total / size.value);
      await load(); return;
    }
    rows.value = result.list; total.value = result.total; aliasesSupported.value = result.aliases_supported;
  } catch (value) { if (active && !request.signal.aborted) { rows.value = []; total.value = 0; aliasesSupported.value = undefined; error.value = message(value); } }
  finally { if (active && !request.signal.aborted) loading.value = false; }
}
function query() { page.value = 1; void load(); }
function reset() { Object.assign(filters, { keyword: "", region: "" }); query(); }
function backToSupplierWorkspace() {
  const query: Record<string, string> = {};
  const mappings: Record<string, string> = { keyword: "return_keyword", region: "return_region", contract: "return_contract", stage: "return_stage", handling: "return_handling", page: "return_page", size: "return_size", sort_by: "return_sort_by", sort_order: "return_sort_order" };
  for (const [target, source] of Object.entries(mappings)) {
    const value = routeValue(route.query[source]);
    if (value) query[target] = value;
  }
  void router.push({ path: "/internal/suppliers", query });
}
function resetForm(profile?: SupplierEntityDTO) {
  if (saving.value || reloadingConflict.value || (profile && !profile.can_edit)) return;
  current.value = profile;
  saveError.value = ""; saveConflict.value = false; dialogOpen.value = true;
}
function openMergePreview(profile: SupplierEntityDTO) { mergePreviewSource.value = profile; mergePreviewOpen.value = true; }
async function save(form: SupplierEntityForm, saveReason: string) {
  if (saving.value || reloadingConflict.value || saveConflict.value) return;
  saving.value = true; saveError.value = ""; saveConflict.value = false;
  try {
    await supplierEntitiesApi.save(form, saveReason, crypto.randomUUID(), current.value);
    if (!active) return;
    dialogOpen.value = false; await load();
  } catch (value) { if (active) { saveError.value = message(value); saveConflict.value = value instanceof ApiError && value.httpStatus === 409; } }
  finally { if (active) saving.value = false; }
}
async function reloadConflictProfile() {
  if (!current.value || reloadingConflict.value || saving.value) return;
  const id = current.value.id;
  reloadingConflict.value = true;
  try {
    const latest = await supplierEntitiesApi.get(id);
    if (!active || current.value?.id !== id) return;
    if (latest.id !== id) throw new Error("刷新结果与当前供应商不匹配。");
    current.value = latest;
    if (!latest.can_edit) { dialogOpen.value = false; await load(); return; }
    profileReloadKey.value += 1;
    saveError.value = ""; saveConflict.value = false;
    await load();
  } catch (value) { saveError.value = message(value); }
  finally { reloadingConflict.value = false; }
}
function formatTax(rate: string | null) { return rate === null ? "未填写" : `${new Decimal(rate).mul(100).toFixed(2)}%`; }
function regionLabel(value: SupplierEntityDTO["region"]) { return regionLabels[value]; }
function currencyLabel(value: SupplierEntityDTO["settlement_currency"]) { return currencyLabels[value]; }
function formatDate(value: string) { return new Date(value).toLocaleString("zh-CN", { hour12: false }); }
function changePage(value: number) { page.value = value; void load(); }
function openF12(profile: SupplierEntityDTO) { f12Supplier.value = profile; f12Open.value = true; }
onMounted(load);
onBeforeUnmount(() => { active = false; listRequest?.abort(); });
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">SUPPLIER SUBJECTS</div><h1>供应商主体</h1><p>维护供应商企业主体资料及归属采购人；新建主体默认归属当前登录员工。</p></div>
    <div class="heading-actions">
      <el-button v-if="routeValue(route.query.return_to) === 'suppliers'" @click="backToSupplierWorkspace">← 返回供应商管理</el-button>
      <el-button :loading="loading" @click="load">刷新</el-button><el-button type="primary" @click="resetForm()">新建供应商</el-button>
    </div>
  </section>

  <section class="panel supplier-entity-panel">
    <div class="section-title"><h2>主体档案 <span>{{ total }}</span></h2><span class="muted">版本与归属由服务端管理</span></div>
    <form class="entity-filters" @submit.prevent="query">
      <el-input v-model="filters.keyword" clearable aria-label="主体名称搜索" :placeholder="aliasesSupported === true ? '搜索供应商名称 / 曾用名' : '搜索供应商名称'" @keyup.enter="query" />
      <el-select v-model="filters.region" clearable aria-label="主体区域筛选" placeholder="全部区域"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select>
      <el-button type="primary" native-type="submit">查询</el-button><el-button @click="reset">重置</el-button>
    </form>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
    <el-alert v-if="!error && aliasesSupported === false" title="当前数据库未提供供应商曾用名结构；名称修改和曾用名搜索暂不可用，其他主体资料可继续维护。" type="warning" show-icon :closable="false" />
    <div v-loading="loading" class="supplier-entity-table">
      <el-empty v-if="!rows.length && !loading && !error" description="暂无供应商主体" />
      <el-table v-else :data="rows" row-key="id">
        <el-table-column label="供应商主体" min-width="210"><template #default="{ row }"><strong>{{ row.name }}</strong><div v-if="row.aliases.length" class="muted">曾用名：{{ row.aliases.join("、") }}</div><div class="muted mono">ID {{ row.id }} · 修订 {{ row.revision }}</div></template></el-table-column>
        <el-table-column label="区域" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
        <el-table-column label="税率 / 结算币种" min-width="170"><template #default="{ row }">{{ formatTax(row.tax_rate) }}<div class="muted">{{ currencyLabel(row.settlement_currency) }}</div></template></el-table-column>
        <el-table-column label="联系人" min-width="170"><template #default="{ row }">{{ row.contact || "未填写" }}<div class="muted">{{ row.phone || "未填写电话" }}</div></template></el-table-column>
        <el-table-column label="主体状态" width="145"><template #default="{ row }"><el-tag :type="row.status === 'FROZEN' ? 'danger' : 'success'">{{ row.status === 'FROZEN' ? '已冻结' : '正常' }}</el-tag><div class="muted entity-state">{{ row.established ? "已完成首次接入" : "尚未完成首次接入" }}</div></template></el-table-column>
        <el-table-column label="归属采购人" min-width="150"><template #default="{ row }">员工 ID {{ row.buyer_id }}<div class="muted">{{ row.can_edit ? "当前账号可维护" : "只读" }}</div></template></el-table-column>
        <el-table-column label="更新时间" min-width="160"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
        <el-table-column label="操作" width="250"><template #default="{ row }"><el-button link type="primary" @click="openF12(row)">合同 / 授信</el-button><el-button link type="primary" @click="openMergePreview(row)">合并预览</el-button><el-button link type="primary" :disabled="!row.can_edit" @click="resetForm(row)">维护</el-button></template></el-table-column>
      </el-table>
    </div>
    <div class="catalog-pagination"><el-pagination :current-page="page" :page-size="size" :total="total" layout="total, prev, pager, next" @current-change="changePage" /></div>
  </section>

  <SupplierProfileForm v-model="dialogOpen" :profile="current" :aliases-supported="aliasesSupported" :saving="saving" :reloading="reloadingConflict" :reload-key="profileReloadKey" :conflict="saveConflict" :error="saveError" @save="save" @reload="reloadConflictProfile" />
  <SupplierMergePreviewDialog v-model="mergePreviewOpen" :source="mergePreviewSource" />

  <el-dialog v-model="f12Open" :title="`${f12Supplier?.name || '供应商'} · 合同与授信`" width="min(900px, 96vw)" destroy-on-close>
    <F12SubjectPanel v-if="f12Supplier" subject-type="SUPPLIER" :subject-id="f12Supplier.id" :can-manage="f12Supplier.can_edit" @changed="load" />
  </el-dialog>
</template>

<style scoped>
.supplier-entity-panel{padding:22px}.heading-actions{display:flex;gap:10px}.entity-filters{display:flex;gap:10px;max-width:700px;margin:18px 0}.entity-filters>.el-input{max-width:350px}.entity-filters>.el-select{width:160px}.supplier-entity-table{min-height:280px}.entity-state{margin-top:5px}.entity-form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.entity-form-grid :deep(.el-select){width:100%}.catalog-pagination{display:flex;justify-content:flex-end;padding-top:16px}.muted{color:#8a9099;font-size:12px;margin-top:5px}.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:700px){.entity-filters{display:grid;grid-template-columns:1fr}.entity-filters>.el-input,.entity-filters>.el-select{max-width:none;width:100%}.entity-form-grid{grid-template-columns:1fr}.heading-actions{flex-wrap:wrap}}
</style>
