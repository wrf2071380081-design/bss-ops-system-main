<script setup lang="ts">
/**
 * 报价审批弹窗：逐模型批准 / 驳回，驳回必填原因。
 *
 * 抽成组件是因为两个地方都要用：
 *  - 客户详情「该客户报价」（CustomerDetail.vue）
 *  - 客户报价单页签「报价审批」（CustomerWorkspace.vue）——原型里审批人就在这个页签
 *    的待审批行上直接批，不必绕到客户详情。
 * 搬运前两边各写一份的话，「驳回必填原因」「不能批自己提交的单」这类规则迟早走样。
 */
import { computed, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { quoteApprovalApi, type ApprovalDecisionInput, type QuoteApprovalDTO } from "../../../api/customerQuoteApproval";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";

const props = defineProps<{
  visible: boolean;
  approvalId: number | null;
  canApprove: boolean;
  staffId: number;
}>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  /** 审批单状态被改变（批准 / 驳回 / 撤回）后触发，父组件据此刷新列表。 */
  (event: "decided"): void;
}>();

const detail = ref<QuoteApprovalDTO>();
const loading = ref(false);
const error = ref("");
const busy = ref(false);
const draft = ref<Record<number, { result: "APPROVED" | "REJECTED"; reason: string }>>({});

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const STATUS: Record<string, { label: string; type: "success" | "danger" | "info" | "warning" }> = {
  PENDING: { label: "待审批", type: "warning" },
  APPROVED: { label: "已批准", type: "success" },
  REJECTED: { label: "已驳回", type: "danger" },
  WITHDRAWN: { label: "已撤回", type: "info" },
};
const statusLabel = (status: string) => STATUS[status]?.label ?? status;
const statusType = (status: string) => STATUS[status]?.type ?? "info";
const pendingRows = computed(() => (detail.value?.rows ?? []).filter(row => row.result === "PENDING"));

/** 能不能批：M9:A + 不是提交人 + 审批单还在待处理。前端只做提示，服务端仍会挡。 */
const blockReason = computed(() => {
  const current = detail.value;
  if (!current) return "";
  if (current.status !== "PENDING") return `该审批单已结束（${statusLabel(current.status)}），不能重复处理。`;
  if (props.staffId && current.submitted_by === props.staffId) return "不能审批自己提交的报价单，请交给其他超管。";
  if (!props.canApprove) return "当前账号没有报价审批权限（M9:A）。";
  return "";
});
const staffText = (raw?: number | null) => (raw ? `员工 #${raw}` : "—");

async function load() {
  if (!props.approvalId) return;
  loading.value = true;
  error.value = "";
  detail.value = undefined;
  draft.value = {};
  try {
    const current = await quoteApprovalApi.get(props.approvalId);
    detail.value = current;
    draft.value = Object.fromEntries(
      current.rows.filter(row => row.result === "PENDING").map(row => [row.id, { result: "APPROVED" as const, reason: "" }]),
    );
  } catch (value) {
    error.value = message(value);
  } finally {
    loading.value = false;
  }
}

function setResult(rowID: number, result: "APPROVED" | "REJECTED") {
  const current = draft.value[rowID];
  if (!current) return;
  draft.value = { ...draft.value, [rowID]: { ...current, result } };
}
function setReason(rowID: number, reason: string) {
  const current = draft.value[rowID];
  if (!current) return;
  draft.value = { ...draft.value, [rowID]: { ...current, reason } };
}

async function submit() {
  const current = detail.value;
  if (!current || busy.value) return;
  const pending = current.rows.filter(row => row.result === "PENDING");
  if (!pending.length) return;
  const missing = pending.filter(row => row.result === "PENDING" && (!draft.value[row.id] || (draft.value[row.id].result === "REJECTED" && !draft.value[row.id].reason.trim())));
  if (missing.length) {
    error.value = `以下模型还没填驳回原因：${missing.map(row => row.model_name).join("、")}`;
    return;
  }
  const decisions: ApprovalDecisionInput[] = pending.map(row => {
    const item = draft.value[row.id];
    return { row_id: row.id, result: item.result, reason: item.result === "REJECTED" ? item.reason.trim() : undefined };
  });
  const approved = decisions.filter(item => item.result === "APPROVED").length;
  busy.value = true;
  error.value = "";
  try {
    await ElMessageBox.confirm(
      `报价单 #${current.quote_id} V${current.quote_version}：批准 ${approved} 个模型，驳回 ${decisions.length - approved} 个。`
      + (approved === decisions.length ? "" : " 有驳回时报价单会回到草稿，销售可改价后重新提交。"),
      "提交审批结论",
      { confirmButtonText: "确认提交", cancelButtonText: "取消", type: approved === decisions.length ? "info" : "warning" },
    );
    detail.value = await quoteApprovalApi.decide(current.id, decisions, crypto.randomUUID());
    draft.value = {};
    ElMessage.success(approved === decisions.length ? "已全部批准，报价单状态为已批准。" : "已提交结论，报价单回到草稿。");
    emit("decided");
  } catch (value) {
    if (value !== "cancel" && value !== "close") error.value = message(value);
  } finally {
    busy.value = false;
  }
}

async function withdraw() {
  const current = detail.value;
  if (!current || busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    await ElMessageBox.confirm(`撤回报价单 #${current.quote_id} 的审批？撤回后报价单回到草稿。`, "撤回审批", {
      confirmButtonText: "撤回", cancelButtonText: "取消", type: "warning",
    });
    detail.value = await quoteApprovalApi.withdraw(current.id, crypto.randomUUID());
    ElMessage.success("已撤回");
    emit("decided");
  } catch (value) {
    if (value !== "cancel" && value !== "close") error.value = message(value);
  } finally {
    busy.value = false;
  }
}

watch(() => [props.visible, props.approvalId] as const, ([visible]) => { if (visible) void load(); }, { immediate: true });
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="报价审批"
    width="min(880px, 96vw)"
    :close-on-click-modal="false"
    @update:model-value="(value: boolean) => emit('update:visible', value)"
  >
    <div v-loading="loading">
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="block-gap" />
      <template v-if="detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="报价单">#{{ detail.quote_id }} · V{{ detail.quote_version }}</el-descriptions-item>
          <el-descriptions-item label="审批状态">
            <el-tag size="small" :type="statusType(detail.status)">{{ statusLabel(detail.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="提交人">{{ staffText(detail.submitted_by) }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ formatDateTime(detail.submitted_at, "—", false) }}</el-descriptions-item>
          <el-descriptions-item label="处理人">{{ staffText(detail.decided_by) }}</el-descriptions-item>
          <el-descriptions-item label="处理时间">{{ detail.decided_at ? formatDateTime(detail.decided_at, "—", false) : "—" }}</el-descriptions-item>
        </el-descriptions>

        <div class="sub-block">
          <div class="selected-title">逐模型结论（{{ detail.rows.length }} 个模型）</div>
          <el-table :data="detail.rows" size="small" class="grid" row-key="id" empty-text="没有模型明细">
            <el-table-column label="模型" min-width="170">
              <template #default="{ row }">
                <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
                <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
              </template>
            </el-table-column>
            <el-table-column label="价项数" width="80" align="right">
              <template #default="{ row }"><span class="mono">{{ row.item_count }}</span></template>
            </el-table-column>
            <el-table-column label="报价区间" width="150" align="right">
              <template #default="{ row }">
                <span class="mono">{{ row.price_min ? (row.price_min === row.price_max ? row.price_min : `${row.price_min} ~ ${row.price_max}`) : "—" }}</span>
              </template>
            </el-table-column>
            <el-table-column label="结论" width="150">
              <template #default="{ row }">
                <el-radio-group
                  v-if="row.result === 'PENDING' && !blockReason"
                  :model-value="draft[row.id]?.result || 'APPROVED'"
                  size="small"
                  @change="(value: string | number | boolean | undefined) => setResult(row.id, value === 'REJECTED' ? 'REJECTED' : 'APPROVED')"
                >
                  <el-radio-button value="APPROVED">批准</el-radio-button>
                  <el-radio-button value="REJECTED">驳回</el-radio-button>
                </el-radio-group>
                <el-tag v-else-if="row.result !== 'PENDING'" size="small" :type="row.result === 'APPROVED' ? 'success' : 'danger'">
                  {{ row.result === "APPROVED" ? "已批准" : "已驳回" }}
                </el-tag>
                <span v-else class="hint">待处理</span>
              </template>
            </el-table-column>
            <el-table-column label="驳回原因 / 意见" min-width="200">
              <template #default="{ row }">
                <el-input
                  v-if="row.result === 'PENDING' && !blockReason && draft[row.id]?.result === 'REJECTED'"
                  :model-value="draft[row.id]?.reason || ''"
                  size="small"
                  maxlength="200"
                  aria-label="驳回原因"
                  placeholder="驳回必须说明原因"
                  @update:model-value="(value: string) => setReason(row.id, value)"
                />
                <span v-else>{{ row.reason || "—" }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <el-alert v-if="blockReason" :title="blockReason" type="info" show-icon :closable="false" class="block-gap" />
      </template>
    </div>
    <template #footer>
      <el-button @click="emit('update:visible', false)">关闭</el-button>
      <el-button
        v-if="detail && detail.status === 'PENDING' && detail.submitted_by === staffId"
        type="warning" plain :disabled="busy"
        @click="withdraw"
      >撤回</el-button>
      <el-button
        v-if="detail && detail.status === 'PENDING' && !blockReason && pendingRows.length"
        type="primary" :loading="busy"
        @click="submit"
      >提交审批结论</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.block-gap { margin: 8px 0; }
.sub-block { margin-top: 18px; }
.selected-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.cell-main { font-weight: 600; }
.cell-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 2px; }
.hint { color: var(--app-text-muted); font-size: 12px; }
</style>
