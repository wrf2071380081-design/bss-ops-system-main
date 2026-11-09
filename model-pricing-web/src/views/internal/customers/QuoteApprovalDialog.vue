<script setup lang="ts">
/**
 * 逐模型审批弹窗（对齐原型 Token Ops 的「超管逐模型审批」）。
 *
 * 详情已经独立成页（QuoteApprovalDetail.vue）：这里只负责"审批人怎么填"，
 * 所以不再重复渲染报价摘要、审批摘要那一套只读信息。
 *
 * 原型一屏要填四样东西，缺一个都不让提交：
 *  1. 每个模型给结论（批准 / 驳回），驳回必须写原因；
 *  2. 有批准模型时的技术办理人；
 *  3. 必要商务办理人（客户合同与授信未归档时）；
 *  4. 处理意见（进审批摘要）。
 *
 * 抽成组件是因为两个地方都要用：
 *  - 客户详情「该客户报价」（CustomerDetail.vue）
 *  - 客户报价单页签「报价审批」（CustomerWorkspace.vue）——原型里审批人就在这个页签
 *    的待审批行上直接批，不必绕到客户详情。
 * 搬运前两边各写一份的话，「驳回必填原因」「不能批自己提交的单」这类规则迟早走样。
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
  type ApprovalDecisionInput,
  type QuoteApprovalDTO,
} from "../../../api/customerQuoteApproval";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";

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
  /** 关联重报：父组件负责把销售带到报价编辑器并记住来源审批。 */
  (event: "reapply", payload: { approvalId: number; customerId: number; modelNames: string[] }): void;
}>();

const detail = ref<QuoteApprovalDTO>();
const router = useRouter();
const loading = ref(false);
const error = ref("");
const busy = ref(false);
const people = ref<WorkbenchPersonDTO[]>([]);
const opsAssignee = ref<string>("");
const businessAssignee = ref<string>("");
const note = ref("");
const draft = ref<Record<number, { result: "APPROVED" | "REJECTED"; reason: string }>>({});

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const statusLabel = (status: string) => APPROVAL_STATUS_LABELS[status] ?? status;
const statusType = (status: string) => APPROVAL_STATUS_TYPES[status] ?? "info";
const pendingRows = computed(() => (detail.value?.rows ?? []).filter(row => row.result === "PENDING"));
const approvedCount = computed(() => pendingRows.value.filter(row => (draft.value[row.id]?.result ?? "APPROVED") === "APPROVED").length);
/** 被驳回的模型：审批结束后可以只针对它们重报（原型「关联驳回模型新申请」）。 */
const rejectedRows = computed(() => (detail.value?.rows ?? []).filter(row => row.result === "REJECTED"));
const canReapply = computed(() => {
  const current = detail.value;
  if (!current) return false;
  if (current.status !== "PARTIAL" && current.status !== "REJECTED") return false;
  return rejectedRows.value.length > 0;
});

/** 能不能批：M9:A + 不是提交人 + 审批单还在待处理。前端只做提示，服务端仍会挡。 */
const blockReason = computed(() => {
  const current = detail.value;
  if (!current) return "";
  if (current.status !== "PENDING") return `该审批单已结束（${statusLabel(current.status)}），不能重复处理。`;
  if (props.staffId && current.submitted_by === props.staffId) return "不能审批自己提交的报价单，请交给其他超管。";
  if (!props.canApprove) return "当前账号没有报价审批权限（M9:A）。";
  return "";
});

const personName = (id?: number | null) => {
  if (!id) return "—";
  return people.value.find(person => String(person.id) === String(id))?.name || `员工 #${id}`;
};

async function loadPeople() {
  if (people.value.length) return;
  try {
    people.value = await workbenchApi.people();
  } catch {
    people.value = [];
  }
}

async function load() {
  if (!props.approvalId) return;
  loading.value = true;
  error.value = "";
  detail.value = undefined;
  draft.value = {};
  opsAssignee.value = "";
  businessAssignee.value = "";
  note.value = "";
  await loadPeople();
  try {
    const current = await quoteApprovalApi.get(props.approvalId);
    detail.value = current;
    draft.value = Object.fromEntries(
      current.rows.filter(row => row.result === "PENDING").map(row => [row.id, { result: "APPROVED" as const, reason: "" }]),
    );
    // 有草稿就先回填「上次填到哪」，没有再用提交时的办理人兜底。
    const savedDraft = current.draft;
    if (savedDraft) {
      for (const row of savedDraft.rows ?? []) {
        if (!draft.value[row.row_id]) continue;
        draft.value[row.row_id] = {
          result: row.result === "REJECTED" ? "REJECTED" : "APPROVED",
          reason: row.reason || "",
        };
      }
      if (savedDraft.note) note.value = savedDraft.note;
      if (savedDraft.ops_assignee_id) opsAssignee.value = String(savedDraft.ops_assignee_id);
      if (savedDraft.business_assignee_id) businessAssignee.value = String(savedDraft.business_assignee_id);
    }
    if (current.ops_assignee_id && !opsAssignee.value) opsAssignee.value = String(current.ops_assignee_id);
    if (current.business_assignee_id && !businessAssignee.value) businessAssignee.value = String(current.business_assignee_id);
    if (current.status !== "PENDING" && !note.value) note.value = current.note || "";
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
  const missing = pending.filter(row => draft.value[row.id]?.result === "REJECTED" && !draft.value[row.id]?.reason.trim());
  if (missing.length) {
    error.value = `以下模型还没填驳回原因：${missing.map(row => row.model_name).join("、")}`;
    return;
  }
  if (approvedCount.value > 0 && !opsAssignee.value) {
    error.value = "请选择技术办理人（有批准模型时必填）。";
    return;
  }
  if (!note.value.trim()) {
    error.value = "请填写处理意见。";
    return;
  }
  const decisions: ApprovalDecisionInput[] = pending.map(row => {
    const item = draft.value[row.id];
    return { row_id: row.id, result: item.result, reason: item.result === "REJECTED" ? item.reason.trim() : undefined };
  });
  const approved = decisions.filter(item => item.result === "APPROVED").length;
  const rejected = decisions.length - approved;
  const conclusion = approved === decisions.length ? "已分派" : approved === 0 ? "已驳回" : "部分批准";
  busy.value = true;
  error.value = "";
  try {
    await ElMessageBox.confirm(
      `客户 ${current.customer_name || `#${current.customer_id}`}：批准 ${approved} 个、驳回 ${rejected} 个模型，结论为「${conclusion}」。`
      + (approved > 0 ? " 批准后立即给技术/商务办理人派待办。" : " 全部驳回时报价单退回草稿，销售可改价后重新提交。"),
      "提交逐模型审批",
      { confirmButtonText: "确认提交", cancelButtonText: "取消", type: approved === decisions.length ? "info" : "warning" },
    );
    detail.value = await quoteApprovalApi.decide(current.id, {
      rows: decisions,
      note: note.value.trim(),
      ops_assignee_id: Number(opsAssignee.value) || 0,
      business_assignee_id: Number(businessAssignee.value) || 0,
    }, crypto.randomUUID());
    draft.value = {};
    ElMessage.success(`已提交，审批结论：${conclusion}`);
    emit("decided");
  } catch (value) {
    if (value !== "cancel" && value !== "close") error.value = message(value);
  } finally {
    busy.value = false;
  }
}

/** 保存进度：只存草稿，不改审批状态（原型弹窗的「保存进度」）。 */
const savingDraft = ref(false);
async function saveProgress() {
  const current = detail.value;
  if (!current || savingDraft.value) return;
  const rows = current.rows
    .filter(row => row.result === "PENDING" && draft.value[row.id])
    .map(row => ({
      row_id: row.id,
      result: draft.value[row.id].result,
      reason: draft.value[row.id].reason.trim() || undefined,
    }));
  savingDraft.value = true;
  error.value = "";
  try {
    detail.value = await quoteApprovalApi.saveDraft(current.id, {
      rows,
      note: note.value.trim(),
      ops_assignee_id: Number(opsAssignee.value) || 0,
      business_assignee_id: Number(businessAssignee.value) || 0,
    }, crypto.randomUUID());
    ElMessage.success("进度已保存，下次打开这张单会带出");
  } catch (value) {
    error.value = message(value);
  } finally {
    savingDraft.value = false;
  }
}

/** 关联重报：把销售带到报价编辑器，并记住「本次只对驳回模型再审一次」。 */
function onReapply() {
  const current = detail.value;
  if (!current) return;
  emit("reapply", {
    approvalId: current.id,
    customerId: current.customer_id,
    modelNames: rejectedRows.value.map(row => row.model_name || `标准模型 #${row.standard_model_id}`),
  });
  emit("update:visible", false);
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
  error.value = "";
  try {
    detail.value = await quoteApprovalApi.withdraw(current.id, reason, crypto.randomUUID());
    ElMessage.success("已撤回");
    emit("decided");
  } catch (value) {
    error.value = message(value);
  } finally {
    busy.value = false;
  }
}

watch(() => [props.visible, props.approvalId] as const, ([visible]) => { if (visible) void load(); }, { immediate: true });
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="逐模型审批"
    width="min(980px, 96vw)"
    :close-on-click-modal="false"
    @update:model-value="(value: boolean) => emit('update:visible', value)"
  >
    <div v-loading="loading">
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="block-gap" />
      <template v-if="detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="客户">{{ detail.customer_name || `客户 #${detail.customer_id}` }}</el-descriptions-item>
          <el-descriptions-item label="报价单">#{{ detail.quote_id }} · V{{ detail.quote_version }}</el-descriptions-item>
          <el-descriptions-item label="审批状态">
            <el-tag size="small" :type="statusType(detail.status)">{{ statusLabel(detail.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="模型数">{{ detail.model_count }}</el-descriptions-item>
          <el-descriptions-item label="提交人">{{ personName(detail.submitted_by) }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ formatDateTime(detail.submitted_at, "—", false) }}</el-descriptions-item>
          <el-descriptions-item label="处理人">{{ personName(detail.decided_by) }}</el-descriptions-item>
          <el-descriptions-item label="处理时间">{{ detail.decided_at ? formatDateTime(detail.decided_at, "—", false) : "—" }}</el-descriptions-item>
        </el-descriptions>

        <div class="sub-block">
          <div class="selected-title">逐模型审批（{{ detail.rows.length }} 个模型）</div>
          <el-table :data="detail.rows" size="small" class="grid" row-key="id" empty-text="没有模型明细">
            <el-table-column label="标准模型" min-width="180">
              <template #default="{ row }">
                <div class="cell-main">{{ row.model_name || `标准模型 #${row.standard_model_id}` }}</div>
                <div class="cell-sub">{{ row.official_id || "官方 ID 待补齐" }}</div>
              </template>
            </el-table-column>
            <el-table-column label="报价系数" width="100">
              <template #default="{ row }">
                <span class="mono coef">{{ approvalCoefficientText(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="报价明细" min-width="240">
              <template #default="{ row }">
                <div v-if="(row.price_detail ?? []).length" class="price-lines">
                  <div v-for="item in row.price_detail ?? []" :key="item.key" class="mono price">{{ priceItemText(item) }}</div>
                </div>
                <span v-else class="hint">无价目快照</span>
              </template>
            </el-table-column>
            <el-table-column label="审核决定" width="160">
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
                  {{ row.result === "APPROVED" ? "批准" : "驳回" }}
                </el-tag>
                <span v-else class="hint">待处理</span>
              </template>
            </el-table-column>
            <el-table-column label="驳回原因" min-width="200">
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

        <div v-if="!blockReason" class="approve-form">
          <div class="form-row">
            <div class="field">
              <label>有批准模型时的技术办理人 <span class="req">*</span></label>
              <el-select v-model="opsAssignee" filterable clearable placeholder="选择办理人" :disabled="approvedCount === 0">
                <el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" />
              </el-select>
            </div>
            <div class="field">
              <label>必要商务办理人</label>
              <el-select v-model="businessAssignee" filterable clearable placeholder="客户合同未归档时必填">
                <el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" />
              </el-select>
            </div>
          </div>
          <div class="field">
            <label>处理意见 <span class="req">*</span></label>
            <el-input v-model="note" type="textarea" :rows="2" maxlength="200" show-word-limit placeholder="写清本次审批结论与依据，会进审批摘要" />
          </div>
          <p class="hint-line">批准后系统立即给技术办理人派「客户模型开通」、给商务办理人派「合同与授信」待办。</p>
        </div>

        <div v-if="detail.status !== 'PENDING'" class="result-block">
          <div class="result-line">
            <b>审批：</b>{{ statusLabel(detail.status) }} · {{ personName(detail.decided_by) }}
            <template v-if="detail.decided_at"> · {{ formatDateTime(detail.decided_at, "—", false) }}</template>
            <template v-if="detail.note"> · {{ detail.note }}</template>
          </div>
          <div class="result-line">
            <b>技术办理人：</b>{{ personName(detail.ops_assignee_id) }}　<b>商务办理人：</b>{{ personName(detail.business_assignee_id) }}
          </div>
          <div v-if="detail.withdraw_reason" class="result-line"><b>撤回原因：</b>{{ detail.withdraw_reason }}</div>
        </div>

        <!-- 关联重报：审批结束后，只对被驳回的模型再走一次审批（原型「关联驳回模型新申请」） -->
        <div v-if="canReapply" class="reapply-block">
          <div class="result-line">
            <b>可重报的模型：</b>{{ rejectedRows.map(row => row.model_name || `标准模型 #${row.standard_model_id}`).join("、") }}
          </div>
          <p class="hint-line">用新一版报价单（可改价、可换来源）重新提交时，只会把上面这些模型再送审一次，已批准的不会重复派发。</p>
          <el-button type="primary" plain size="small" @click="onReapply">对驳回模型重新报价</el-button>
        </div>

        <el-alert v-if="blockReason" :title="blockReason" type="info" show-icon :closable="false" class="block-gap" />
      </template>
    </div>
    <template #footer>
      <el-button @click="emit('update:visible', false)">关闭</el-button>
      <!-- 原型审批详情底部的「查看办理」：批完直接去客户详情接着办开通 -->
      <el-button
        v-if="detail && detail.status !== 'PENDING' && detail.status !== 'WITHDRAWN' && detail.status !== 'REJECTED'"
        @click="router.push({ path: `/internal/customers/${detail.customer_id}`, query: { tab: 'models' } })"
      >查看办理</el-button>
      <el-button
        v-if="detail && detail.status === 'PENDING' && detail.submitted_by === staffId"
        type="warning" plain :disabled="busy"
        @click="withdraw"
      >撤回申请</el-button>
      <el-button
        v-if="detail && detail.status === 'PENDING' && !blockReason && pendingRows.length"
        :loading="savingDraft" :disabled="busy"
        @click="saveProgress"
      >保存进度</el-button>
      <el-button
        v-if="detail && detail.status === 'PENDING' && !blockReason && pendingRows.length"
        type="primary" :loading="busy"
        @click="submit"
      >提交逐模型审批</el-button>
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
.price { font-size: 12px; color: var(--app-text-muted); }
.price-lines { line-height: 1.8; }
.coef { font-weight: 600; }
.approve-form { margin-top: 18px; padding-top: 16px; border-top: 1px dashed var(--app-border, #e5e7eb); }
.form-row { display: flex; gap: 16px; }
.form-row .field { flex: 1; }
.field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px; }
.field label { font-size: 12.5px; color: var(--app-text-muted); }
.req { color: var(--el-color-danger); }
.hint-line { margin: 4px 0 0; font-size: 12px; color: var(--app-text-muted); }
.result-block { margin-top: 18px; padding: 12px 14px; background: var(--app-bg-muted, #f7f9fc); border-radius: 8px; }
.result-line { font-size: 13px; line-height: 1.9; }
.reapply-block { margin-top: 14px; padding: 12px 14px; border: 1px dashed var(--el-color-warning); border-radius: 8px; background: #fffbf0; }
</style>
