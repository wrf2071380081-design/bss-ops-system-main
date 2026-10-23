<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { supplyChainApi, type ApprovalHistoryRow, type ApprovalRow, type SupplierWorkspaceRow } from "../../../api/supplyChain";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";
import { formatDateTime } from "../../../domain/date";
import { useSessionStore } from "../../../stores/session";

const session = useSessionStore();
const activeTab = ref("suppliers");
const quoteTab = ref("pending");

const suppliers = ref<SupplierWorkspaceRow[]>([]);
const supplierLoading = ref(false);
const supplierError = ref("");
const filters = reactive({ keyword: "", region: "", contract: "", stage: "", handling: "" });
const supplierDetailVisible = ref(false);
const supplierDetail = ref<SupplierWorkspaceRow>();

const approvals = ref<ApprovalRow[]>([]);
const history = ref<ApprovalHistoryRow[]>([]);
const quoteLoading = ref(false);
const quoteError = ref("");
const people = ref<WorkbenchPersonDTO[]>([]);
const approveVisible = ref(false);
const current = ref<ApprovalRow>();
const testAssignee = ref(0);
const contractAssignee = ref(0);
const reason = ref("同意接入");
const busy = ref(false);
const simulatingId = ref(0);
const historyVisible = ref(false);
const historyDetail = ref<ApprovalHistoryRow>();

const regionLabel = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
const statusLabel = (status: string) => status === "PENDING_APPROVAL" ? "待审批" : status === "PROCESSING" ? "办理中" : status === "COMPLETED" ? "已完成" : status === "REJECTED" ? "已驳回" : status;
const resultLabel = (result: string) => result === "APPROVE" ? "通过" : result === "REJECT" ? "不通过" : "历史批次";
const resultType = (result: string) => result === "APPROVE" ? "success" : result === "REJECT" ? "danger" : "info";
const formatTime = (value?: string | null) => formatDateTime(value, "—", false);
const currencyAmount = (row: SupplierWorkspaceRow) => {
  if (!row.credit_amount) return "待记录";
  const symbol = row.settlement_currency === "USD" ? "$" : "¥";
  const amount = Number(row.credit_amount);
  return Number.isFinite(amount) ? `${symbol}${amount.toLocaleString()}` : row.credit_amount;
};

const filteredSuppliers = computed(() => suppliers.value.filter(row => {
  if (filters.keyword && !row.name.toLowerCase().includes(filters.keyword.trim().toLowerCase())) return false;
  if (filters.region && row.region !== filters.region) return false;
  if (filters.contract && row.contract_status !== filters.contract) return false;
  if (filters.stage && row.cooperation_stage !== filters.stage) return false;
  if (filters.handling && row.handling_status !== filters.handling) return false;
  return true;
}));

async function loadSuppliers() {
  supplierLoading.value = true;
  supplierError.value = "";
  try { suppliers.value = await supplyChainApi.suppliers(); }
  catch (value) { supplierError.value = value instanceof Error ? value.message : "供应商列表加载失败。"; }
  finally { supplierLoading.value = false; }
}

async function loadQuotes() {
  quoteLoading.value = true;
  quoteError.value = "";
  try {
    const [pending, records, staff] = await Promise.all([supplyChainApi.approvals(), supplyChainApi.approvalHistory(), workbenchApi.people()]);
    approvals.value = pending;
    history.value = records;
    people.value = staff;
  } catch (value) {
    quoteError.value = value instanceof Error ? value.message : "供应商报价/审批记录加载失败。";
  } finally {
    quoteLoading.value = false;
  }
}

function openSupplier(row: SupplierWorkspaceRow) { supplierDetail.value = row; supplierDetailVisible.value = true; }
function openApprove(row: ApprovalRow) {
  current.value = row;
  const self = session.user?.staffId || people.value[0]?.id || 0;
  testAssignee.value = self;
  contractAssignee.value = self;
  reason.value = "同意接入";
  approveVisible.value = true;
}
function openHistory(row: ApprovalHistoryRow) { historyDetail.value = row; historyVisible.value = true; }

async function approve() {
  if (!current.value || busy.value) return;
  if (!testAssignee.value) { ElMessage.error("请选择测试与资源配置办理人。"); return; }
  busy.value = true;
  try {
    await supplyChainApi.approve(current.value.id, {
      test_assignee_id: testAssignee.value,
      contract_assignee_id: contractAssignee.value || undefined,
      reason: reason.value.trim() || "同意接入",
    });
    ElMessage.success("审批已通过");
    approveVisible.value = false;
    await Promise.all([loadQuotes(), loadSuppliers()]);
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "审批失败，请重试。");
  } finally { busy.value = false; }
}

async function simulate(row: ApprovalRow) {
  if (simulatingId.value) return;
  simulatingId.value = row.id;
  try {
    const result = await supplyChainApi.simulate(row.id);
    ElMessage.success(`资源 ${result.resource_id} 已登记接入`);
    await Promise.all([loadQuotes(), loadSuppliers()]);
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "模拟接入失败；请确认当前账号是办理人。");
  } finally { simulatingId.value = 0; }
}

function resetFilters() { Object.assign(filters, { keyword: "", region: "", contract: "", stage: "", handling: "" }); }
onMounted(() => { void loadSuppliers(); void loadQuotes(); });
</script>

<template>
  <section class="page-heading">
    <div><div class="eyebrow">INTERNAL / SUPPLIERS</div><h1>供应商管理</h1><p>供应商列表与供应商报价（待审批 / 审批记录）统一在这里。</p></div>
    <el-button :loading="supplierLoading || quoteLoading" @click="loadSuppliers(); loadQuotes()">刷新</el-button>
  </section>

  <section class="panel supplier-workspace">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="供应商列表" name="suppliers">
        <el-alert v-if="supplierError" :title="supplierError" type="error" show-icon :closable="false" />
        <div class="filters">
          <el-input v-model="filters.keyword" placeholder="搜索供应商名称 / 曾用名" clearable style="width: 220px" />
          <el-select v-model="filters.region" placeholder="国内 / 海外主体" clearable style="width: 140px">
            <el-option value="CN" label="国内" /><el-option value="OVERSEAS" label="海外" />
          </el-select>
          <el-select v-model="filters.contract" placeholder="合同状态" clearable style="width: 120px">
            <el-option value="已归档" label="已归档" /><el-option value="办理中" label="办理中" />
          </el-select>
          <el-select v-model="filters.stage" placeholder="合作阶段" clearable style="width: 120px">
            <el-option value="已有" label="已有" /><el-option value="待接入" label="待接入" />
          </el-select>
          <el-select v-model="filters.handling" placeholder="办理状态" clearable style="width: 150px">
            <el-option value="有未完成事项" label="有未完成事项" /><el-option value="无未完成事项" label="无未完成事项" />
          </el-select>
          <el-button @click="resetFilters">重置</el-button>
        </div>
        <el-table v-loading="supplierLoading" :data="filteredSuppliers" style="width: 100%">
          <el-table-column prop="name" label="供应商名称" min-width="160" />
          <el-table-column prop="resource_count" label="资源数" width="80" />
          <el-table-column label="国内 / 海外" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
          <el-table-column label="授信额度" width="130">
            <template #default="{ row }"><strong>{{ currencyAmount(row) }}</strong><span class="muted">供应商-我方</span></template>
          </el-table-column>
          <el-table-column label="合同状态" width="100">
            <template #default="{ row }"><el-tag :type="row.contract_status === '已归档' ? 'success' : 'warning'" size="small">{{ row.contract_status }}</el-tag></template>
          </el-table-column>
          <el-table-column label="结算币种" width="100"><template #default="{ row }">{{ row.settlement_currency || "—" }}</template></el-table-column>
          <el-table-column label="税率" width="80"><template #default="{ row }">{{ row.tax_rate ? `${row.tax_rate}%` : "—" }}</template></el-table-column>
          <el-table-column label="合作阶段" width="100"><template #default="{ row }">{{ row.cooperation_stage }}</template></el-table-column>
          <el-table-column label="办理状态" width="130">
            <template #default="{ row }"><el-tag :type="row.handling_status === '有未完成事项' ? 'warning' : 'info'" size="small">{{ row.handling_status }}</el-tag></template>
          </el-table-column>
          <el-table-column label="详情" width="80"><template #default="{ row }"><el-button size="small" @click="openSupplier(row)">详情</el-button></template></el-table-column>
          <template #empty><el-empty description="暂无供应商" :image-size="64" /></template>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="供应商报价" name="quotes">
        <el-alert v-if="quoteError" :title="quoteError" type="error" show-icon :closable="false" />
        <el-tabs v-model="quoteTab">
          <el-tab-pane label="待审批" name="pending">
            <el-table v-loading="quoteLoading" :data="approvals" style="width: 100%">
              <el-table-column label="供应商 / 申报类型" min-width="200">
                <template #default="{ row }"><strong>{{ row.supplier_name }}</strong><span class="muted">{{ row.kind === "MODEL_NEW" ? "模型新增" : row.kind }}</span></template>
              </el-table-column>
              <el-table-column prop="row_count" label="模型数" width="90" />
              <el-table-column prop="submitter" label="供应商提交人" min-width="130" />
              <el-table-column label="提交时间" width="180"><template #default="{ row }">{{ formatTime(row.submitted_at) }}</template></el-table-column>
              <el-table-column label="当前状态" width="100"><template #default="{ row }"><el-tag :type="row.status === 'PENDING_APPROVAL' ? 'warning' : 'success'" size="small">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
              <el-table-column label="操作" width="180">
                <template #default="{ row }">
                  <el-button size="small" v-if="row.status === 'PENDING_APPROVAL'" @click="openApprove(row)">详情</el-button>
                  <el-button size="small" v-else :loading="simulatingId === row.id" @click="simulate(row)">模拟接入</el-button>
                </template>
              </el-table-column>
              <template #empty><el-empty description="暂无待审批申报" :image-size="64" /></template>
            </el-table>
          </el-tab-pane>

          <el-tab-pane label="审批记录" name="history">
            <el-table v-loading="quoteLoading" :data="history" style="width: 100%">
              <el-table-column label="供应商名称 / 申报" min-width="200">
                <template #default="{ row }"><strong>{{ row.supplier_name }}</strong><span class="muted">#{{ row.id }} · {{ row.kind === "MODEL_NEW" ? "模型新增" : row.kind }}</span></template>
              </el-table-column>
              <el-table-column prop="row_count" label="模型数" width="90" />
              <el-table-column label="办理状态" width="100"><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column>
              <el-table-column prop="approver" label="审批人" width="110" />
              <el-table-column label="审批时间" width="180"><template #default="{ row }">{{ formatTime(row.approved_at) }}</template></el-table-column>
              <el-table-column label="审批结果" width="100"><template #default="{ row }"><el-tag :type="resultType(row.result)" size="small">{{ resultLabel(row.result) }}</el-tag></template></el-table-column>
              <el-table-column prop="note" label="审批意见" min-width="180" show-overflow-tooltip />
              <el-table-column label="详情" width="80"><template #default="{ row }"><el-button size="small" @click="openHistory(row)">详情</el-button></template></el-table-column>
              <template #empty><el-empty description="暂无审批记录" :image-size="64" /></template>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </el-tab-pane>
    </el-tabs>
  </section>

  <el-dialog v-model="supplierDetailVisible" title="供应商详情" width="520px">
    <el-descriptions v-if="supplierDetail" :column="1" border>
      <el-descriptions-item label="供应商">{{ supplierDetail.name }}</el-descriptions-item>
      <el-descriptions-item label="主体 / 手机号">{{ regionLabel(supplierDetail.region) }} · {{ supplierDetail.phone }}</el-descriptions-item>
      <el-descriptions-item label="资源数">{{ supplierDetail.resource_count }}</el-descriptions-item>
      <el-descriptions-item label="授信额度">{{ currencyAmount(supplierDetail) }}</el-descriptions-item>
      <el-descriptions-item label="合同 / 合作">{{ supplierDetail.contract_status }} · {{ supplierDetail.cooperation_stage }}</el-descriptions-item>
      <el-descriptions-item label="办理状态">{{ supplierDetail.handling_status }}</el-descriptions-item>
    </el-descriptions>
    <template #footer><el-button @click="supplierDetailVisible = false">关闭</el-button></template>
  </el-dialog>

  <el-dialog v-model="approveVisible" title="通过申报并分配办理人" width="520px">
    <template v-if="current">
      <p class="dialog-summary">{{ current.supplier_name }} · {{ current.row_count }} 个模型 · {{ statusLabel(current.status) }}</p>
      <el-form label-position="top">
        <el-form-item label="测试与资源配置办理人 *">
          <el-select v-model="testAssignee" filterable style="width: 100%">
            <el-option v-for="person in people" :key="person.id" :value="person.id" :label="person.name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="current.first_model" label="合同与授信办理人">
          <el-select v-model="contractAssignee" filterable style="width: 100%">
            <el-option :value="0" label="暂不分配" />
            <el-option v-for="person in people" :key="person.id" :value="person.id" :label="person.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="审批意见">
          <el-input v-model="reason" type="textarea" :rows="3" maxlength="200" show-word-limit />
        </el-form-item>
      </el-form>
    </template>
    <template #footer>
      <el-button :disabled="busy" @click="approveVisible = false">取消</el-button>
      <el-button type="primary" :loading="busy" @click="approve">通过并分配</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="historyVisible" title="审批记录详情" width="520px">
    <el-descriptions v-if="historyDetail" :column="1" border>
      <el-descriptions-item label="供应商">{{ historyDetail.supplier_name }} · #{{ historyDetail.id }}</el-descriptions-item>
      <el-descriptions-item label="模型数 / 办理状态">{{ historyDetail.row_count }} · {{ statusLabel(historyDetail.status) }}</el-descriptions-item>
      <el-descriptions-item label="审批人 / 时间">{{ historyDetail.approver || "历史批次" }} · {{ formatTime(historyDetail.approved_at) }}</el-descriptions-item>
      <el-descriptions-item label="审批结果">{{ resultLabel(historyDetail.result) }}</el-descriptions-item>
      <el-descriptions-item label="审批意见">{{ historyDetail.note || "—" }}</el-descriptions-item>
    </el-descriptions>
    <template #footer><el-button @click="historyVisible = false">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.supplier-workspace { padding: 18px 20px; }
.filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.muted { display: block; color: var(--app-text-muted); font-size: 12px; margin-top: 4px; }
.dialog-summary { margin: 0 0 14px; color: var(--app-text-secondary); }
</style>
