<script setup lang="ts">
/**
 * 报价审批详情（独立视图，对齐原型：列表点「查看」进入详情页，详情里再开「逐模型审批」弹窗）。
 *
 * 为什么从弹窗里拆出来：原型里「详情」和「审批表单」是两层——详情只读，谁都能看；
 * 审批表单才是审批人填的地方。混在一个弹窗里，销售点「查看」也会看到一堆禁用控件，
 * 而且详情的信息量（报价明细 + 逐模型结论 + 审批摘要 + 办理人）在弹窗里放不下。
 */
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  quoteApprovalApi,
  approvalCoefficientText,
  priceItemText,
  APPROVAL_STATUS_LABELS,
  APPROVAL_STATUS_TYPES,
  type QuoteApprovalDTO,
} from "../../../api/customerQuoteApproval";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";

const props = defineProps<{ approvalId: number; canApprove: boolean; staffId: number }>();
const emit = defineEmits<{
  (event: "back"): void;
  /** 打开「逐模型审批」弹窗（父组件负责挂载，详情页只管触发）。 */
  (event: "decide", approvalId: number): void;
  /** 状态变了（撤回）后让父组件刷新列表。 */
  (event: "changed"): void;
  (event: "reapply", payload: { approvalId: number; customerId: number; modelNames: string[] }): void;
}>();

const router = useRouter();
const detail = ref<QuoteApprovalDTO>();
const loading = ref(false);
const error = ref("");
const busy = ref(false);
const people = ref<WorkbenchPersonDTO[]>([]);

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const statusLabel = (status: string) => APPROVAL_STATUS_LABELS[status] ?? status;
const statusType = (status: string) => APPROVAL_STATUS_TYPES[status] ?? "info";
const rejectedRows = computed(() => (detail.value?.rows ?? []).filter(row => row.result === "REJECTED"));
const canReapply = computed(() => {
  const current = detail.value;
  if (!current) return false;
  if (current.status !== "PARTIAL" && current.status !== "REJECTED") return false;
  return rejectedRows.value.length > 0;
});
const isSubmitter = computed(() => Boolean(detail.value && detail.value.submitted_by === props.staffId));
const canDecide = computed(() => Boolean(detail.value && detail.value.status === "PENDING"
  && props.canApprove && !isSubmitter.value && detail.value.submitted_by !== props.staffId));

const personName = (id?: number | null) => {
  if (!id) return "—";
  return people.value.find(person => String(person.id) === String(id))?.name || `员工 #${id}`;
};

async function load() {
  loading.value = true;
  error.value = "";
  try {
    detail.value = await quoteApprovalApi.get(props.approvalId);
  } catch (value) {
    error.value = message(value);
  } finally {
    loading.value = false;
  }
}

async function loadPeople() {
  if (people.value.length) return;
  try {
    people.value = await workbenchApi.people();
  } catch {
    people.value = [];
  }
}

async function withdraw() {
  const current = detail.value;
  if (!current || busy.value) return;
  let reason = "";
  try {
    const result = await ElMessageBox.prompt("撤回后报价单回到草稿，审批单作废。请说明撤回原因。", "撤回客户报价审批", {
      confirmButtonText: "提交撤回",
      cancelButtonText: "取消",
      inputPlaceholder: "撤回原因（必填）",
      inputValidator: (value: string) => (value && value.trim() ? true : "请填写撤回原因"),
      inputType: "textarea",
    });
    reason = String(result.value || "").trim();
  } catch {
    return;
  }
  busy.value = true;
  try {
    detail.value = await quoteApprovalApi.withdraw(current.id, reason, crypto.randomUUID());
    ElMessage.success("已撤回");
    emit("changed");
  } catch (value) {
    error.value = message(value);
  } finally {
    busy.value = false;
  }
}

function onReapply() {
  const current = detail.value;
  if (!current) return;
  emit("reapply", {
    approvalId: current.id,
    customerId: current.customer_id,
    modelNames: rejectedRows.value.map(row => row.model_name || `标准模型 #${row.standard_model_id}`),
  });
}

function goHandle() {
  const current = detail.value;
  if (!current) return;
  void router.push({ path: `/internal/customers/${current.customer_id}`, query: { tab: "models" } });
}

watch(() => props.approvalId, () => { void load(); void loadPeople(); }, { immediate: true });
</script>

<template>
  <div class="approval-detail">
    <div class="detail-head">
      <el-button link type="primary" @click="emit('back')">← 返回报价审批</el-button>
      <span v-if="detail" class="head-sub">报价单 #{{ detail.quote_id }} · V{{ detail.quote_version }}</span>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="block-gap" />

    <div v-loading="loading">
      <template v-if="detail">
        <div class="card">
          <h3 class="card-title">报价审批</h3>
          <el-descriptions :column="3" border size="small">
            <el-descriptions-item label="客户">{{ detail.customer_name || `客户 #${detail.customer_id}` }}</el-descriptions-item>
            <el-descriptions-item label="审批状态">
              <el-tag size="small" :type="statusType(detail.status)">{{ statusLabel(detail.status) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="模型数">{{ detail.model_count }}</el-descriptions-item>
            <el-descriptions-item label="提交人">{{ personName(detail.submitted_by) }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ formatDateTime(detail.submitted_at, "—", false) }}</el-descriptions-item>
            <el-descriptions-item label="处理时间">{{ detail.decided_at ? formatDateTime(detail.decided_at, "—", false) : "—" }}</el-descriptions-item>
            <el-descriptions-item v-if="detail.previous_approval_id" label="关联重报" :span="3">
              来源审批 #{{ detail.previous_approval_id }}（只对上一轮被驳回的模型再审一次）
            </el-descriptions-item>
          </el-descriptions>
        </div>

        <div class="card">
          <h3 class="card-title">逐模型明细（{{ detail.rows.length }} 个模型）</h3>
          <el-table :data="detail.rows" size="small" class="grid" row-key="id" empty-text="没有模型明细">
            <el-table-column label="标准模型" min-width="180">
              <template #default="{ row }">
                <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
                <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
              </template>
            </el-table-column>
            <el-table-column label="报价系数" width="110">
              <template #default="{ row }">
                <span class="mono coef">{{ approvalCoefficientText(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="报价明细" min-width="280">
              <template #default="{ row }">
                <div v-if="(row.price_detail ?? []).length" class="price-lines">
                  <div v-for="item in row.price_detail ?? []" :key="item.key" class="mono price">{{ priceItemText(item) }}</div>
                </div>
                <span v-else class="hint">无价目快照</span>
              </template>
            </el-table-column>
            <el-table-column label="审核结果" width="110">
              <template #default="{ row }">
                <el-tag v-if="row.result !== 'PENDING'" size="small" :type="row.result === 'APPROVED' ? 'success' : 'danger'">
                  {{ row.result === "APPROVED" ? "批准" : "驳回" }}
                </el-tag>
                <span v-else class="hint">待处理</span>
              </template>
            </el-table-column>
            <el-table-column label="驳回原因" min-width="180">
              <template #default="{ row }">{{ row.reason || "—" }}</template>
            </el-table-column>
            <el-table-column label="审核人 / 时间" width="200">
              <template #default="{ row }">
                {{ row.decided_by ? personName(row.decided_by) : "—" }}
                {{ row.decided_at ? ` · ${formatDateTime(row.decided_at, "—", false)}` : "" }}
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="detail.status !== 'PENDING'" class="card">
          <h3 class="card-title">审批结论</h3>
          <div class="result-line">
            <b>结论：</b>{{ statusLabel(detail.status) }} · {{ personName(detail.decided_by) }}
            <template v-if="detail.decided_at"> · {{ formatDateTime(detail.decided_at, "—", false) }}</template>
            <template v-if="detail.note"> · {{ detail.note }}</template>
          </div>
          <div class="result-line">
            <b>技术办理人：</b>{{ personName(detail.ops_assignee_id) }}　
            <b>商务办理人：</b>{{ personName(detail.business_assignee_id) }}
          </div>
          <div v-if="detail.withdraw_reason" class="result-line"><b>撤回原因：</b>{{ detail.withdraw_reason }}</div>
          <div v-if="canReapply" class="reapply-block">
            <div class="result-line"><b>可重报的模型：</b>{{ rejectedRows.map(row => row.model_name).join("、") }}</div>
            <p class="hint-line">用新一版报价单（可改价、可换来源）重新提交时，只会把上面这些模型再送审一次。</p>
            <el-button type="primary" plain size="small" @click="onReapply">对驳回模型重新报价</el-button>
          </div>
        </div>

        <div class="detail-actions">
          <el-button v-if="detail.status === 'PENDING' && isSubmitter" type="warning" plain :disabled="busy" @click="withdraw">撤回申请</el-button>
          <el-button v-if="canDecide" type="primary" @click="emit('decide', detail.id)">逐模型审批</el-button>
          <el-button v-if="detail.status === 'APPROVED' || detail.status === 'PARTIAL'" @click="goHandle">查看办理</el-button>
        </div>

        <el-alert
          v-if="detail.status === 'PENDING' && !canDecide && !isSubmitter"
          type="info"
          show-icon
          :closable="false"
          class="block-gap"
          title="当前账号不能审批这张单"
          description="报价审批需要 M9:A 权限，且不能审批自己提交的单。"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.block-gap { margin: 8px 0; }
.detail-head { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.head-sub { color: var(--app-text-muted); font-size: 12.5px; }
.card { border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 14px 16px; margin-bottom: 12px; }
.card-title { font-size: 14px; margin: 0 0 10px; }
.cell-main { font-weight: 600; }
.cell-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 2px; }
.price { font-size: 12px; color: var(--app-text-muted); }
.price-lines { line-height: 1.8; }
.coef { font-weight: 600; font-size: 14px; }
.hint { color: var(--app-text-muted); font-size: 12px; }
.result-line { font-size: 13px; line-height: 1.9; }
.hint-line { margin: 4px 0 8px; font-size: 12px; color: var(--app-text-muted); }
.reapply-block { margin-top: 10px; padding: 10px 12px; border: 1px dashed var(--el-color-warning); border-radius: 8px; background: #fffbf0; }
.detail-actions { display: flex; gap: 10px; justify-content: flex-end; padding: 4px 0 8px; }
</style>
