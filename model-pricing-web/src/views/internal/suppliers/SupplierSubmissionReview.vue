<script setup lang="ts">
/**
 * 供应商申报审批页（原型：工作台待办 → 供应商管理 / {供应商} / 申报与审批记录 / {类型}申请）。
 *
 * 为什么从弹窗改成整页：
 *  - 原型的审批是在一张独立页上做的，单子大时可以筛选（模型公司 / 模型类型 / 关键词）、
 *    可以把逐模型结论摆开看；弹窗里这两件事都做不了；
 *  - 待办点进来时能看清「这是谁的哪一单」，审批完还能回到同一个供应商的记录页。
 *
 * 权限由后端 `can_approve` 决定（当前审批人资格 + 单据仍是 PENDING_APPROVAL），
 * 前端只按它切换「可勾选」与「只读」两种形态，不自己推角色。
 */
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { submissionFlowsApi, type SubmissionFlowView } from "../../../api/submissionFlows";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import { formatDateTime } from "../../../domain/date";
import ResourceReportsPanel from "../resources/ResourceReportsPanel.vue";

const route = useRoute();
const router = useRouter();
const submissionID = computed(() => String(route.params.submissionId || ""));

const view = ref<SubmissionFlowView>();
const people = ref<WorkbenchPersonDTO[]>([]);
const loading = ref(false);
const error = ref("");
let active = true;
let controller: AbortController | undefined;

/** 逐模型勾选（原型：勾=通过，不勾=不通过）。默认全不勾，避免"没看就全放行"。 */
const picked = ref<Record<string, boolean>>({});
const test = ref("");
const contract = ref("");
const reason = ref("");
const busy = ref(false);
/** 二次确认态：第一次点只亮确认条，再点才真正提交（与原型一致）。 */
const confirmed = ref(false);
let lifecycle = crypto.randomUUID();

const reportsRow = ref<SubmissionFlowView["rows"][number]>();
const reportsVisible = ref(false);

const filters = ref({ company: "", modelType: "", keyword: "" });
const companies = computed(() => [...new Set((view.value?.rows ?? []).map(row => row.company).filter(Boolean))].sort((a, b) => String(a).localeCompare(String(b), "zh-CN")));
const filteredRows = computed(() => {
  const keyword = filters.value.keyword.trim().toLowerCase();
  return (view.value?.rows ?? []).filter(row => {
    if (filters.value.company && row.company !== filters.value.company) return false;
    if (filters.value.modelType && row.model_type !== filters.value.modelType) return false;
    if (!keyword) return true;
    return [row.model_name, row.interface_model_id].some(value => Boolean(value) && String(value).toLowerCase().includes(keyword));
  });
});
function resetFilters() { filters.value = { company: "", modelType: "", keyword: "" }; }

const kindLabel = computed(() => (view.value?.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增"));
const statusLabels: Record<string, string> = {
  DRAFT: "草稿", PENDING_APPROVAL: "待审批", APPROVED: "已批准", REJECTED: "已驳回",
  PROCESSING: "办理中", COMPLETED: "已完成", WITHDRAWN: "已撤回",
};
const statusLabel = computed(() => statusLabels[view.value?.status || ""] || view.value?.status || "—");
const nodeNames: Record<string, string> = { CONFIG: "测试与资源配置", CONTRACT: "合同与授信" };
const nodeStates: Record<string, string> = { IN_PROGRESS: "办理中", WAITING_PREDECESSOR: "等待前序办理", COMPLETED: "已完成", CANCELED: "已取消" };

const approvedCount = computed(() => (view.value?.rows ?? []).filter(row => picked.value[row.id]).length);
const rejectedCount = computed(() => (view.value?.rows.length ?? 0) - approvedCount.value);

async function load() {
  controller?.abort();
  const current = controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const [detail, staff] = await Promise.all([
      submissionFlowsApi.get(submissionID.value, false, current.signal),
      workbenchApi.people(),
    ]);
    if (!active || current.signal.aborted) return;
    view.value = detail;
    people.value = staff;
    if (detail.can_approve) {
      // 已决策的行保持原结论，其余默认不勾。
      picked.value = Object.fromEntries(detail.rows.map(row => [row.id, row.decision === "APPROVED"]));
    }
  } catch (value) {
    if (!active || current.signal.aborted) return;
    error.value = value instanceof Error ? value.message : "申报详情加载失败，请重试。";
  } finally {
    if (active && !current.signal.aborted) loading.value = false;
  }
}

function toggle(rowID: string, checked: boolean) {
  picked.value = { ...picked.value, [rowID]: checked };
  confirmed.value = false;
}

function backToSupplier() {
  const supplierID = view.value?.supplier_id;
  if (!supplierID) { void router.push({ path: "/internal/workbench" }); return; }
  // 回到该供应商的「申报与审批记录」页签（原型审批完也是回这张表）。
  void router.push({ path: `/internal/suppliers/${supplierID}/models`, query: { tab: "quotes" } });
}

async function approve() {
  const detail = view.value;
  if (!detail || busy.value) return;
  if (approvedCount.value === 0) { ElMessage.warning("未勾选任何模型：整单驳回请用「驳回申报」。"); return; }
  if (!test.value) { ElMessage.warning("请选择「测试与资源配置」办理人。"); return; }
  if (detail.first_model && !contract.value) { ElMessage.warning("首次模型申报需要指定「合同与授信」办理人。"); return; }
  if (rejectedCount.value > 0 && !reason.value.trim()) { ElMessage.warning("有模型不通过时必须填写审批意见（说明不通过原因）。"); return; }
  if (!confirmed.value) { confirmed.value = true; return; }
  busy.value = true;
  try {
    await submissionFlowsApi.approve(detail, test.value, contract.value, reason.value, lifecycle, picked.value);
    lifecycle = crypto.randomUUID();
    ElMessage.success(
      rejectedCount.value > 0
        ? `审批已提交：通过 ${approvedCount.value} 个，不通过 ${rejectedCount.value} 个`
        : "审批通过，已分派办理任务",
    );
    backToSupplier();
  } catch (value) {
    error.value = value instanceof Error ? value.message : "审批失败，输入已保留，请刷新后核对。";
  } finally {
    busy.value = false;
  }
}

async function reject() {
  const detail = view.value;
  if (!detail || busy.value) return;
  if (!reason.value.trim()) { ElMessage.warning("整单驳回必须填写原因。"); return; }
  try {
    await ElMessageBox.confirm(`驳回后供应商可修改并重新提交，本次申报全部模型都不会进入办理。`, "驳回申报", {
      confirmButtonText: "确认驳回", cancelButtonText: "取消", type: "warning",
    });
  } catch { return; }
  busy.value = true;
  try {
    await submissionFlowsApi.reject(detail, reason.value, lifecycle);
    lifecycle = crypto.randomUUID();
    ElMessage.success("已驳回，供应商可修改后重新提交");
    backToSupplier();
  } catch (value) {
    error.value = value instanceof Error ? value.message : "驳回失败，原因已保留，请刷新核对。";
  } finally {
    busy.value = false;
  }
}

function openReports(row: SubmissionFlowView["rows"][number]) {
  if (!row.resource_id) return;
  reportsRow.value = row;
  reportsVisible.value = true;
}
function openCompare(row: SubmissionFlowView["rows"][number]) {
  if (!view.value) return;
  void router.push({ path: `/internal/model-detail/${row.model_id}`, query: { from_submission: view.value.id } });
}
function openResource(row: SubmissionFlowView["rows"][number]) {
  if (!row.resource_id || !view.value) return;
  void router.push({ path: `/internal/model-plaza/${row.resource_id}`, query: { region: view.value.region, tab: "operations" } });
}

watch(submissionID, () => { confirmed.value = false; reason.value = ""; void load(); }, { immediate: true });
onBeforeUnmount(() => { active = false; controller?.abort(); });
</script>

<template>
  <section class="submission-review">
    <div class="review-head">
      <el-button text @click="backToSupplier">← 返回申报与审批记录</el-button>
      <div class="crumb">业务运营 / 供应商管理 / {{ view?.supplier_name || "—" }} / 申报与审批记录 / {{ kindLabel }}申请 · {{ submissionID }}</div>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>

    <div v-loading="loading">
      <template v-if="view">
        <header class="review-title">
          <div>
            <h1>{{ view.supplier_name }} · {{ kindLabel }}申请</h1>
            <p class="muted">{{ view.region === "OVERSEAS" ? "海外" : "国内" }} · {{ view.id }}</p>
          </div>
          <div class="title-tags">
            <el-tag size="small" :type="view.status === 'PENDING_APPROVAL' ? 'warning' : view.status === 'REJECTED' ? 'danger' : 'success'">{{ statusLabel }}</el-tag>
            <el-tag v-if="view.approval_result" size="small" type="info">{{ view.approval_result }}</el-tag>
          </div>
        </header>

        <div class="card">
          <div class="card-body meta">
            <span>供应商：<strong>{{ view.supplier_name }}</strong></span>
            <span>区域：{{ view.region === "OVERSEAS" ? "海外" : "国内" }}</span>
            <span>申请类型：{{ kindLabel }}</span>
            <span>申请编号：{{ view.id }}</span>
            <span>提交时间：{{ view.submitted_at ? formatDateTime(view.submitted_at) : "未记录" }}</span>
            <span v-if="view.first_model">首次接入：需同时办理测试与资源配置、合同与授信</span>
          </div>
        </div>

        <div class="card">
          <div class="filterbar">
            <label>模型公司<el-select v-model="filters.company" clearable placeholder="全部" aria-label="模型公司" style="width:150px"><el-option v-for="company in companies" :key="company" :label="company" :value="company" /></el-select></label>
            <label>模型类型<el-select v-model="filters.modelType" clearable placeholder="全部" aria-label="模型类型" style="width:140px"><el-option v-for="(name, key) in modelTypeLabels" :key="key" :label="name" :value="key" /></el-select></label>
            <el-input v-model="filters.keyword" clearable aria-label="搜索模型名称 / 接口模型 ID" placeholder="搜索模型名称 / 接口模型 ID" style="width:240px" />
            <el-button @click="resetFilters">重置</el-button>
            <span class="muted">共 {{ filteredRows.length }} / {{ view.rows.length }} 个模型</span>
          </div>

          <el-table :data="filteredRows" size="small" row-key="id" empty-text="没有匹配的模型">
            <el-table-column v-if="view.can_approve" label="通过" width="60">
              <template #default="{ row }">
                <input type="checkbox" :checked="!!picked[row.id]" :aria-label="`通过模型 ${row.model_name}`" :disabled="busy" @change="toggle(row.id, ($event.target as HTMLInputElement).checked)">
              </template>
            </el-table-column>
            <el-table-column label="平台标准模型" min-width="160">
              <template #default="{ row }">
                <div class="cell-main">{{ row.model_name }}</div>
                <div class="cell-sub">{{ row.company || "—" }} · {{ modelTypeLabels[row.model_type] || "未分类" }}</div>
              </template>
            </el-table-column>
            <el-table-column label="供应商接口模型 ID" min-width="170"><template #default="{ row }">{{ row.interface_model_id }}</template></el-table-column>
            <el-table-column label="采购系数" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template></el-table-column>
            <el-table-column label="办理状态" width="120">
              <template #default="{ row }">{{ row.status === "CANCELED" ? "本次办理已取消" : row.status === "CONFIGURED" ? "配置已完成" : row.status === "ADMITTED" ? "已接入" : row.status === "COMPLETED" ? "已完成" : "待办理" }}</template>
            </el-table-column>
            <el-table-column label="审批结果" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.decision === 'APPROVED'" size="small" type="success">通过</el-tag>
                <el-tag v-else-if="row.decision === 'REJECTED'" size="small" type="danger">不通过</el-tag>
                <el-tag v-else size="small" type="info">待审核</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="测试与报告" width="110">
              <template #default="{ row }"><el-button v-if="row.resource_id" link type="primary" @click="openReports(row)">测试与报告</el-button><span v-else class="muted">未接入</span></template>
            </el-table-column>
            <el-table-column label="对比" width="80">
              <template #default="{ row }"><el-button link type="primary" @click="openCompare(row)">对比</el-button></template>
            </el-table-column>
            <el-table-column label="资源" width="110">
              <template #default="{ row }"><el-button v-if="row.resource_id" link type="primary" @click="openResource(row)">{{ view.can_handle && row.status !== 'CANCELED' ? "去办理" : "查看资源" }}</el-button><span v-else class="muted">{{ view.can_approve ? "批准后生成" : "未接入" }}</span></template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="view.can_approve" class="card approval">
          <h2>审批意见</h2>
          <p class="decision-summary">通过 {{ approvedCount }} 个 · 不通过 {{ rejectedCount }} 个 · 共 {{ view.rows.length }} 个。勾选为通过，未勾选为不通过。</p>
          <el-form label-position="top" :disabled="busy">
            <el-form-item label="审批意见（有模型不通过时必填）">
              <el-input v-model="reason" type="textarea" :rows="3" maxlength="500" aria-label="审批意见" placeholder="说明本次判断依据；不通过的模型要写清原因" @input="confirmed = false" />
            </el-form-item>
            <div class="assign-row">
              <el-form-item label="测试与资源配置办理人">
                <el-select v-model="test" placeholder="选择一个办理人" aria-label="测试与资源配置办理人" style="width:220px">
                  <el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="view.first_model" label="合同与授信办理人">
                <el-select v-model="contract" placeholder="首次模型申报需选择" aria-label="合同与授信办理人" style="width:220px">
                  <el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name" />
                </el-select>
              </el-form-item>
            </div>
          </el-form>
          <div v-if="confirmed" class="confirm-bar">
            确认提交：通过 {{ approvedCount }} 个，不通过 {{ rejectedCount }} 个；仅通过的模型进入接入办理。
          </div>
          <div class="actions">
            <el-button type="danger" plain :disabled="busy" @click="reject">驳回申报</el-button>
            <el-button type="primary" :loading="busy" :disabled="approvedCount === 0" @click="approve">{{ confirmed ? "确认提交审批" : "核对并提交审批" }}</el-button>
          </div>
        </div>

        <div v-else class="card approval readonly">
          <h2>审批结论</h2>
          <p class="muted">
            {{ view.status === "PENDING_APPROVAL" ? "当前账号没有审批资格，或不是本单指定的审批人；可查看申报内容与测试证据。" : `本单已结束（${statusLabel}${view.approval_result ? " · " + view.approval_result : ""}），以下为只读记录。` }}
          </p>
          <p v-if="view.approver">审批人：{{ view.approver }}<span v-if="view.approved_at"> · {{ formatDateTime(view.approved_at) }}</span></p>
        </div>

        <div v-if="view.nodes.length" class="card">
          <h2>办理进度</h2>
          <el-table :data="view.nodes" size="small">
            <el-table-column label="办理事项"><template #default="{ row }">{{ nodeNames[row.key] || row.key }}</template></el-table-column>
            <el-table-column label="办理人" width="160"><template #default="{ row }">{{ row.assignee_name || "未分派" }}</template></el-table-column>
            <el-table-column label="进度" width="140"><template #default="{ row }">{{ nodeStates[row.status] || row.status }}</template></el-table-column>
          </el-table>
        </div>
      </template>
    </div>

    <el-dialog v-model="reportsVisible" title="测试与报告" width="min(1180px,96vw)" append-to-body align-center>
      <p class="muted">{{ reportsRow?.model_name }} · {{ reportsRow?.interface_model_id }}</p>
      <ResourceReportsPanel v-if="reportsRow?.resource_id" :resource-i-d="reportsRow.resource_id" />
      <template #footer><el-button @click="reportsVisible = false">关闭测试报告</el-button></template>
    </el-dialog>
  </section>
</template>

<style scoped>
.submission-review { padding: 18px 22px 32px; color: var(--app-text-primary); }
.review-head { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; margin-bottom: 12px; }
.crumb { font-size: 12px; color: var(--app-text-muted); }
.review-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.review-title h1 { margin: 0; font-size: 18px; font-weight: 600; }
.title-tags { display: flex; gap: 8px; }
.card { border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 16px 18px; margin-bottom: 16px; background: var(--el-bg-color); }
.card h2 { margin: 0 0 12px; font-size: 15px; font-weight: 600; }
.meta { display: flex; flex-wrap: wrap; gap: 8px 28px; font-size: 13px; }
.filterbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.filterbar label { display: inline-flex; align-items: center; gap: 8px; font-size: 12px; white-space: nowrap; }
.decision-summary { margin: 0 0 12px; font-size: 13px; }
.assign-row { display: flex; gap: 24px; flex-wrap: wrap; }
.confirm-bar { margin: 4px 0 10px; padding: 8px 12px; border-radius: 8px; background: var(--el-color-primary-light-9); font-size: 13px; }
.actions { display: flex; justify-content: flex-end; gap: 10px; }
.readonly p { margin: 0 0 8px; }
.cell-main { font-weight: 500; }
.cell-sub { font-size: 12px; color: var(--app-text-muted); }
.mono { font-variant-numeric: tabular-nums; }
.muted { color: var(--app-text-muted); font-size: 12px; }
</style>
