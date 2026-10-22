<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import Decimal from "decimal.js";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO, SupplierEntityForm } from "../../../api/supplierEntities.contract";
import { f12SupplierRecordsApi } from "../../../api/f12SupplierRecords";
import type { F12ContractRecord, F12CreditRecord, F12CurrentCredit } from "../../../api/f12SupplierRecords.contract";
import { ApiError } from "../../../domain/common";

const regionLabels = { CN: "国内", OVERSEAS: "海外" } as const;
const currencyLabels = { CNY: "人民币 CNY", USD: "美元 USD" } as const;
const rows = ref<SupplierEntityDTO[]>([]), loading = ref(false), error = ref("");
const filters = reactive({ keyword: "", region: "" });
const page = ref(1), size = ref(20), total = ref(0);
const dialogOpen = ref(false), saving = ref(false), saveError = ref(""), current = ref<SupplierEntityDTO>();
const f12Open = ref(false), f12Loading = ref(false), f12Error = ref(""), f12Supplier = ref<SupplierEntityDTO>();
const contracts = ref<F12ContractRecord[]>([]), credits = ref<F12CreditRecord[]>([]), currentCredits = ref<F12CurrentCredit[]>([]);
const form = reactive<SupplierEntityForm>({ name: "", region: "CN", registration_no: "", contact: "", phone: "", tax_rate_percent: "0", settlement_currency: "CNY" });
const reason = ref("");
const message = (value: unknown) => value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

async function load() {
  loading.value = true; error.value = "";
  try {
    const result = await supplierEntitiesApi.list({ page: page.value, size: size.value, keyword: filters.keyword.trim() || undefined, region: filters.region || undefined });
    rows.value = result.list; total.value = result.total;
  } catch (value) { rows.value = []; total.value = 0; error.value = message(value); }
  finally { loading.value = false; }
}
function query() { page.value = 1; void load(); }
function reset() { Object.assign(filters, { keyword: "", region: "" }); query(); }
function resetForm(profile?: SupplierEntityDTO) {
  current.value = profile;
  Object.assign(form, profile ? {
    name: profile.name, region: profile.region, registration_no: profile.registration_no, contact: profile.contact, phone: profile.phone,
    tax_rate_percent: new Decimal(profile.tax_rate).mul(100).toFixed(2), settlement_currency: profile.settlement_currency,
  } : { name: "", region: "CN", registration_no: "", contact: "", phone: "", tax_rate_percent: "0", settlement_currency: "CNY" });
  reason.value = ""; saveError.value = ""; dialogOpen.value = true;
}
async function save() {
  saving.value = true; saveError.value = "";
  try {
    await supplierEntitiesApi.save({ ...form }, reason.value, crypto.randomUUID(), current.value);
    dialogOpen.value = false; await load();
  } catch (value) { saveError.value = message(value); }
  finally { saving.value = false; }
}
function formatTax(rate: string) { return `${new Decimal(rate).mul(100).toFixed(2)}%`; }
function regionLabel(value: SupplierEntityDTO["region"]) { return regionLabels[value]; }
function currencyLabel(value: SupplierEntityDTO["settlement_currency"]) { return currencyLabels[value]; }
function formatDate(value: string) { return new Date(value).toLocaleString("zh-CN", { hour12: false }); }
function changePage(value: number) { page.value = value; void load(); }
async function openF12(profile: SupplierEntityDTO) {
  f12Supplier.value = profile; f12Open.value = true; f12Loading.value = true; f12Error.value = "";
  contracts.value = []; credits.value = []; currentCredits.value = [];
  try {
    const [contractPage, creditPage] = await Promise.all([f12SupplierRecordsApi.contracts(profile.id), f12SupplierRecordsApi.credits(profile.id)]);
    contracts.value = contractPage.list; credits.value = creditPage.list; currentCredits.value = creditPage.current;
  } catch (value) { f12Error.value = message(value); }
  finally { f12Loading.value = false; }
}
function creditStatusLabel(value: F12CurrentCredit["status"]) {
  if (value === "AVAILABLE") return "当前授信";
  if (value === "AMBIGUOUS_VERSION") return "版本冲突，需核对";
  return "旧金额无效，需核对";
}
onMounted(load);
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">SUPPLIER SUBJECTS</div><h1>供应商主体</h1><p>维护供应商企业主体资料及归属采购人；新建主体默认归属当前登录员工。</p></div>
    <div class="heading-actions"><el-button :loading="loading" @click="load">刷新</el-button><el-button type="primary" @click="resetForm()">新建供应商</el-button></div>
  </section>

  <section class="panel supplier-entity-panel">
    <div class="section-title"><h2>主体档案 <span>{{ total }}</span></h2><span class="muted">版本与归属由服务端管理</span></div>
    <form class="entity-filters" @submit.prevent="query">
      <el-input v-model="filters.keyword" clearable aria-label="主体名称搜索" placeholder="搜索供应商名称" @keyup.enter="query" />
      <el-select v-model="filters.region" clearable aria-label="主体区域筛选" placeholder="全部区域"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select>
      <el-button type="primary" native-type="submit">查询</el-button><el-button @click="reset">重置</el-button>
    </form>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
    <div v-loading="loading" class="supplier-entity-table">
      <el-empty v-if="!rows.length && !loading && !error" description="暂无供应商主体" />
      <el-table v-else :data="rows" row-key="id">
        <el-table-column label="供应商主体" min-width="210"><template #default="{ row }"><strong>{{ row.name }}</strong><div class="muted mono">ID {{ row.id }} · 修订 {{ row.revision }}</div></template></el-table-column>
        <el-table-column label="区域" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
        <el-table-column label="税率 / 结算币种" min-width="170"><template #default="{ row }">{{ formatTax(row.tax_rate) }}<div class="muted">{{ currencyLabel(row.settlement_currency) }}</div></template></el-table-column>
        <el-table-column label="联系人" min-width="170"><template #default="{ row }">{{ row.contact || "未填写" }}<div class="muted">{{ row.phone || "未填写电话" }}</div></template></el-table-column>
        <el-table-column label="主体状态" width="145"><template #default="{ row }"><el-tag :type="row.status === 'FROZEN' ? 'danger' : 'success'">{{ row.status === 'FROZEN' ? '已冻结' : '正常' }}</el-tag><div class="muted entity-state">{{ row.established ? "已完成首次接入" : "尚未完成首次接入" }}</div></template></el-table-column>
        <el-table-column label="归属采购人" min-width="150"><template #default="{ row }">员工 ID {{ row.buyer_id }}<div class="muted">{{ row.can_edit ? "当前账号可维护" : "只读" }}</div></template></el-table-column>
        <el-table-column label="更新时间" min-width="160"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
        <el-table-column label="操作" width="170"><template #default="{ row }"><el-button link type="primary" @click="openF12(row)">合同 / 授信</el-button><el-button link type="primary" :disabled="!row.can_edit" @click="resetForm(row)">维护</el-button></template></el-table-column>
      </el-table>
    </div>
    <div class="catalog-pagination"><el-pagination :current-page="page" :page-size="size" :total="total" layout="total, prev, pager, next" @current-change="changePage" /></div>
  </section>

  <el-dialog v-model="dialogOpen" :title="current ? '维护供应商主体' : '新建供应商主体'" width="min(680px, 94vw)" destroy-on-close>
    <el-alert v-if="saveError" :title="saveError" type="error" show-icon :closable="false" />
    <el-form label-position="top" class="supplier-entity-form">
      <div class="entity-form-grid">
        <el-form-item label="供应商名称" required><el-input v-model="form.name" maxlength="128" /></el-form-item>
        <el-form-item label="主体区域" required><el-select v-model="form.region"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select></el-form-item>
        <el-form-item label="统一社会信用代码 / 登记号"><el-input v-model="form.registration_no" maxlength="64" /></el-form-item>
        <el-form-item label="联系人"><el-input v-model="form.contact" maxlength="64" /></el-form-item>
        <el-form-item label="联系电话"><el-input v-model="form.phone" maxlength="32" /></el-form-item>
        <el-form-item label="税率（百分比）" required><el-input v-model="form.tax_rate_percent" inputmode="decimal" placeholder="例如 6 或 6.25" /></el-form-item>
        <el-form-item label="结算币种" required><el-select v-model="form.settlement_currency"><el-option label="人民币 CNY" value="CNY" /><el-option label="美元 USD" value="USD" /></el-select></el-form-item>
        <el-form-item label="操作说明" required><el-input v-model="reason" maxlength="500" placeholder="说明建档或资料调整原因" /></el-form-item>
      </div>
      <el-alert v-if="!current" title="初始归属采购人自动绑定当前登录员工；首次办理完成前，主体状态为“尚未完成首次接入”。" type="info" :closable="false" />
    </el-form>
    <template #footer><el-button @click="dialogOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>

  <el-dialog v-model="f12Open" :title="`${f12Supplier?.name || '供应商'} · 合同与授信`" width="min(900px, 96vw)" destroy-on-close>
    <el-alert title="v36 兼容只读视图：合同名称、编号、附件下载和不可变版本历史尚未具备；旧备注与附件路径不会返回。" type="warning" show-icon :closable="false" />
    <el-alert v-if="f12Error" :title="f12Error" type="error" show-icon :closable="false" />
    <div v-loading="f12Loading" class="f12-read-panel">
      <section class="f12-current-credit">
        <div class="section-title"><h3>当前授信</h3><span class="muted">按主体、方向、币种分别读取最高版本</span></div>
        <el-empty v-if="!currentCredits.length && !f12Loading" description="未登记授信" :image-size="64" />
        <div v-else class="f12-credit-cards">
          <article v-for="item in currentCredits" :key="`${item.direction}:${item.currency}`" class="f12-credit-card">
            <el-tag :type="item.status === 'AVAILABLE' ? 'success' : 'warning'">{{ creditStatusLabel(item.status) }}</el-tag>
            <strong v-if="item.record">{{ item.record.amount }} {{ item.currency }}</strong>
            <strong v-else>金额不可确认</strong>
            <span class="muted">{{ item.direction }}<template v-if="item.record"> · 修订 {{ item.record.version }}</template></span>
          </article>
        </div>
      </section>
      <el-tabs>
        <el-tab-pane label="合同记录">
          <el-empty v-if="!contracts.length && !f12Loading" description="暂无合同记录" />
          <el-table v-else :data="contracts" row-key="id">
            <el-table-column label="记录" min-width="150"><template #default="{ row }">合同记录 #{{ row.id }}<div class="muted">修订 {{ row.version }}</div></template></el-table-column>
            <el-table-column label="状态" width="130"><template #default="{ row }"><el-tag>{{ row.status === 'ARCHIVED' ? '已归档' : '处理中' }}</el-tag></template></el-table-column>
            <el-table-column label="附件记录" width="130"><template #default="{ row }">{{ row.has_attachment ? '存在旧附件引用' : '无附件引用' }}</template></el-table-column>
            <el-table-column label="签署时间" min-width="175"><template #default="{ row }">{{ row.signed_at ? formatDate(row.signed_at) : '未登记' }}</template></el-table-column>
            <el-table-column label="更新时间" min-width="175"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="授信记录">
          <el-empty v-if="!credits.length && !f12Loading" description="暂无授信记录" />
          <el-table v-else :data="credits" row-key="id">
            <el-table-column label="授信额度" min-width="175"><template #default="{ row }">{{ row.amount }} {{ row.currency }}<div class="muted">{{ row.direction }}</div></template></el-table-column>
            <el-table-column label="版本" width="100"><template #default="{ row }">{{ row.version }}</template></el-table-column>
            <el-table-column label="生效时间" min-width="175"><template #default="{ row }">{{ row.effective_at ? formatDate(row.effective_at) : '未登记' }}</template></el-table-column>
            <el-table-column label="保存时间" min-width="175"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column>
          </el-table>
          <p v-if="credits.length === 100" class="muted">当前最多显示最近 100 条；完整分页和版本核对待新版结构接入。</p>
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-dialog>
</template>

<style scoped>
.supplier-entity-panel{padding:22px}.heading-actions{display:flex;gap:10px}.entity-filters{display:flex;gap:10px;max-width:700px;margin:18px 0}.entity-filters>.el-input{max-width:350px}.entity-filters>.el-select{width:160px}.supplier-entity-table{min-height:280px}.entity-state{margin-top:5px}.entity-form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.entity-form-grid :deep(.el-select){width:100%}.catalog-pagination{display:flex;justify-content:flex-end;padding-top:16px}.muted{color:#8a9099;font-size:12px;margin-top:5px}.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:700px){.entity-filters{display:grid;grid-template-columns:1fr}.entity-filters>.el-input,.entity-filters>.el-select{max-width:none;width:100%}.entity-form-grid{grid-template-columns:1fr}.heading-actions{flex-wrap:wrap}}
.f12-read-panel{min-height:240px}.f12-current-credit{padding:18px 0}.f12-credit-cards{display:flex;gap:12px;flex-wrap:wrap}.f12-credit-card{display:grid;gap:8px;min-width:200px;padding:14px;border:1px solid #e5e7eb;border-radius:8px}.f12-credit-card strong{font-size:20px}
</style>
