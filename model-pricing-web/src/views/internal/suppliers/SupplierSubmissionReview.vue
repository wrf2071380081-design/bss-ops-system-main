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
import { resourcesApi } from "../../../api/resources";
import { resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import type { ResourceRow } from "../../../api/resources.types";
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
/**
 * 申报行的四项指标取「该资源最近一次实际测试」，不是「当前生效报告」。
 * 两者要分开说：压力测试没过时资源没有生效报告（不能准入），但确实测过、有实测值，
 * 审批人要看的正是这个数值 + 没过这个事实。
 */
function metricText(row: SubmissionFlowView["rows"][number], key: "connection_ms" | "rps" | "tps") {
  if (row.test_status === "NOT_TESTED") return "未测试";
  const value = row[key];
  return value ? String(value) : "未记录";
}
function cacheText(row: SubmissionFlowView["rows"][number]) {
  if (row.test_status === "NOT_TESTED") return "未测试";
  if (!row.cache_available) return "未提供";
  const rate = Number(row.cache_hit_rate);
  return Number.isFinite(rate) ? `${rate.toFixed(2)}%` : "未记录";
}
/**
 * 最近一次测试的结论行：给四列数值一个可读的上下文（悬停与按钮上都能看到）。
 * 只说连通性——压测平台对压力测试不返回通过/不通过（verdict 恒空），
 * 这部分在页面上不表达，只保留实测数值。
 */
function testConclusion(row: SubmissionFlowView["rows"][number]) {
  if (row.test_status === "NOT_TESTED" || !row.test_status) return "本轮还没有测试报告";
  const connectivity = row.connection_passed === true ? "连通性通过"
    : row.connection_passed === false ? "连通性未通过" : "连通性未判定";
  const when = row.tested_at ? formatDateTime(row.tested_at, "—", false) : "时间未记录";
  return `最近一次测试 ${when} · ${connectivity} · 共 ${row.report_count ?? 0} 份报告`;
}
const hasFailedTest = (row: SubmissionFlowView["rows"][number]) => row.test_status === "FAILED";
/** 有报告但连通性没有明确结论：不当通过，也不说不通过。 */
const isIncompleteTest = (row: SubmissionFlowView["rows"][number]) => row.test_status === "INCOMPLETE";
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
// 原型「对比」：行内展开，列平台已接入的同标准模型来源（供应商 / 系数 / 指标），
// 仅供参照，不改变本次申报——所以不跳页、也不提供任何操作。
const compareTable = ref<{ toggleRowExpansion: (row: unknown, expanded?: boolean) => void }>();
const compareRowID = ref("");
const compareRows = ref<Record<string, ResourceRow[]>>({});
const compareLoading = ref<Record<string, boolean>>({});
const compareError = ref<Record<string, string>>({});
function toggleCompare(row: SubmissionFlowView["rows"][number]) {
  const table = compareTable.value;
  if (!table) return;
  if (compareRowID.value === row.id) { table.toggleRowExpansion(row, false); compareRowID.value = ""; return; }
  const previous = (view.value?.rows ?? []).find(item => item.id === compareRowID.value);
  if (previous) table.toggleRowExpansion(previous, false);
  compareRowID.value = row.id;
  table.toggleRowExpansion(row, true);
  if (!compareRows.value[row.id] && !compareLoading.value[row.id]) void loadCompare(row);
}
async function loadCompare(row: SubmissionFlowView["rows"][number]) {
  if (!view.value) return;
  compareLoading.value = { ...compareLoading.value, [row.id]: true };
  compareError.value = { ...compareError.value, [row.id]: "" };
  try {
    const rows = await resourcesApi.allRows({
      view: "flat", page: 1, size: 100, sort_by: "coefficient", order: "asc",
      standard_model_id: row.model_id, region: view.value.region, state: "ADMITTED",
    });
    compareRows.value = { ...compareRows.value, [row.id]: rows };
  } catch (value) {
    compareError.value = { ...compareError.value, [row.id]: value instanceof Error ? value.message : "来源对比加载失败" };
  } finally {
    compareLoading.value = { ...compareLoading.value, [row.id]: false };
  }
}
const compareMetric = (row: ResourceRow, key: "connection_ms" | "rps" | "tps") =>
  resourceMetricDisplay(row.test.metrics[key], row.test.status);
const compareCache = (row: ResourceRow) => resourceCacheDisplay(row.test.cache_stats, row.test.status);
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
            <span>提交人：{{ view.submitter || "未记录" }}</span>
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

          <el-table ref="compareTable" :data="filteredRows" size="small" row-key="id" empty-text="没有匹配的模型">
            <el-table-column type="expand" width="1" class-name="compare-expand">
              <template #default="{ row }">
                <div class="compare-panel">
                  <p class="muted">平台已接入的同一标准模型供应来源；仅供对比，不更改本次申报。</p>
                  <el-alert v-if="compareError[row.id]" :title="compareError[row.id]" type="error" :closable="false" class="block-gap" />
                  <el-table v-else v-loading="compareLoading[row.id]" :data="compareRows[row.id] || []" size="small" empty-text="该模型暂无可对比的已接入来源">
                    <el-table-column label="供应商" min-width="150"><template #default="{ row: source }">{{ source.supplier_name }}</template></el-table-column>
                    <el-table-column label="接口模型 ID" min-width="150"><template #default="{ row: source }">{{ source.supplier_model_id }}</template></el-table-column>
                    <el-table-column label="部署节点" width="100"><template #default="{ row: source }">{{ resourceRegionLabels[source.region] || source.region }}</template></el-table-column>
                    <el-table-column label="采购系数" width="100" align="right"><template #default="{ row: source }"><span class="mono">{{ source.coefficient }}</span></template></el-table-column>
                    <el-table-column label="连通 · ms" width="100" align="right"><template #default="{ row: source }"><span class="mono">{{ compareMetric(source, "connection_ms") }}</span></template></el-table-column>
                    <el-table-column label="RPS" width="90" align="right"><template #default="{ row: source }"><span class="mono">{{ compareMetric(source, "rps") }}</span></template></el-table-column>
                    <el-table-column label="TPS" width="90" align="right"><template #default="{ row: source }"><span class="mono">{{ compareMetric(source, "tps") }}</span></template></el-table-column>
                    <el-table-column label="缓存命中率" width="110" align="right"><template #default="{ row: source }"><span class="mono">{{ compareCache(source) }}</span></template></el-table-column>
                    <el-table-column label="状态" width="100"><template #default="{ row: source }">{{ resourceStateLabels[source.state] || source.state }}</template></el-table-column>
                  </el-table>
                </div>
              </template>
            </el-table-column>
            <el-table-column v-if="view.can_approve" label="通过" width="60">
              <template #default="{ row }">
                <input type="checkbox" :checked="!!picked[row.id]" :aria-label="`通过模型 ${row.model_name}`" :disabled="busy" @change="toggle(row.id, ($event.target as HTMLInputElement).checked)">
              </template>
            </el-table-column>
            <el-table-column label="平台标准模型" min-width="150">
              <template #default="{ row }">
                <div class="cell-main">{{ row.model_name }}</div>
                <div class="cell-sub">{{ row.company || "—" }} · {{ modelTypeLabels[row.model_type] || "未分类" }}</div>
              </template>
            </el-table-column>
            <el-table-column label="供应商接口模型 ID" min-width="150"><template #default="{ row }">{{ row.interface_model_id }}</template></el-table-column>
            <el-table-column label="采购系数" width="100" align="right"><template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template></el-table-column>
            <el-table-column label="连通耗时(ms)" width="105" align="right"><template #default="{ row }"><span class="mono">{{ metricText(row, "connection_ms") }}</span></template></el-table-column>
            <el-table-column label="RPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ metricText(row, "rps") }}</span></template></el-table-column>
            <el-table-column label="TPS" width="90" align="right"><template #default="{ row }"><span class="mono">{{ metricText(row, "tps") }}</span></template></el-table-column>
            <el-table-column label="输入缓存命中率" width="115" align="right"><template #default="{ row }"><span class="mono">{{ cacheText(row) }}</span></template></el-table-column>
            <el-table-column label="审批结果" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.decision === 'APPROVED'" size="small" type="success">通过</el-tag>
                <el-tag v-else-if="row.decision === 'REJECTED'" size="small" type="danger">不通过</el-tag>
                <el-tag v-else size="small" type="info">待审核</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="测试与报告" width="112">
              <template #default="{ row }">
                <template v-if="row.resource_id">
                  <el-tooltip :content="testConclusion(row)" placement="top">
                    <el-button link :type="hasFailedTest(row) || isIncompleteTest(row) ? 'warning' : 'primary'" @click="openReports(row)">测试与报告</el-button>
                  </el-tooltip>
                  <span v-if="hasFailedTest(row)" class="test-failed">未通过</span>
                  <span v-else-if="isIncompleteTest(row)" class="test-failed">未判定</span>
                </template>
                <span v-else class="muted">未接入</span>
              </template>
            </el-table-column>
            <el-table-column label="对比" width="90">
              <template #default="{ row }"><el-button link type="primary" @click="toggleCompare(row)">{{ compareRowID === row.id ? "收起对比" : "对比" }}</el-button></template>
            </el-table-column>
            <el-table-column label="资源" width="104">
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

    <!-- 原型是从右侧滑出的抽屉，标题在面板内部（不再套一层 dialog 标题，避免「测试与报告」出现两遍）。 -->
    <el-drawer v-model="reportsVisible" size="min(860px, 96vw)" :with-header="false" append-to-body destroy-on-close>
      <div class="report-drawer">
        <ResourceReportsPanel v-if="reportsRow?.resource_id" :resource-i-d="reportsRow.resource_id" :model-name="reportsRow.model_name" />
        <div class="report-drawer-foot"><el-button @click="reportsVisible = false">关闭</el-button></div>
      </div>
    </el-drawer>
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
/* 对比是「行内展开」：展开图标藏起来，由表格里的「对比 / 收起对比」按钮控制。 */
:deep(.compare-expand .el-table__expand-icon) { display: none; }
:deep(.compare-expand .cell) { padding: 0; }
.compare-panel { padding: 12px 14px; background: var(--el-fill-color-light); border-radius: 8px; }
.compare-panel .muted { margin: 0 0 8px; }
.cell-main { font-weight: 500; }
.test-failed { display: block; margin-top: 2px; font-size: 11px; color: var(--el-color-warning); }
.report-drawer { padding: 18px 20px; }
.report-drawer-foot { display: flex; justify-content: flex-end; padding-top: 14px; border-top: 1px solid var(--el-border-color-light); }
.cell-sub { font-size: 12px; color: var(--app-text-muted); }
.mono { font-variant-numeric: tabular-nums; }
.muted { color: var(--app-text-muted); font-size: 12px; }
</style>
