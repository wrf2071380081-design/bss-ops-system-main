<script setup lang="ts">
/**
 * 合同与授信办理（客户侧）——对齐原型的极简弹窗：
 *   抬头进度摘要 → 本次开通的模型（只读）→ 上传合同资料 / 上传授信资料 →
 *   说明 → 勾选「已完成合同与授信并上传资料」→ 确认商务完成。
 *
 * 两个上传弹窗**逐字复用供应商模型申报那套**（F12SubjectPanel 的「上传合同资料 / 上传授信资料」）：
 * 两列四格（资料名称 / 资料编号 / 授信额度 / 附件上传）+ 备注整行 + 取消 / 保存资料。
 * 字段、文案、校验提示都与供应商侧一致，商务在这两处看到的界面不该是两套。
 *
 * 与供应商侧的差异只有一处：这里**上传即归档**。后端完成 CONTRACT 的判据是
 * 「已归档合同 + 授信额度 > 0」，供应商侧要多点一次「归档」，客户这边让商务少这一步。
 *
 * 模型清单来自客户「接入模型」投影（报价审批通过即出现），不是 flow.models——
 * 审批建流程时不写模型明细（那时还没有供应来源），从 flow 取会是空表。
 */
import { computed, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import {
  customerDetailApi,
  type CustomerModelServiceRow,
  type OnboardingFlowDTO,
  type OnboardingNodeDTO,
} from "../../../api/customerDetail";
import { f12SubjectRecordsApi } from "../../../api/f12SupplierRecords";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";

const props = defineProps<{
  visible: boolean;
  customerId: string;
  customerName: string;
  /** 开通流程 id：抬头标题用（原型「合同与授信 · 11」）。 */
  flowId?: number;
  /** CONTRACT 节点 id：完成动作打在这个节点上。 */
  nodeId: number | null;
  /** 只用来取「负责人」这一句，不再渲染节点表。 */
  nodes: OnboardingNodeDTO[];
  /** 本次开通的模型（客户「接入模型」投影：报价批准即出现）。 */
  models: CustomerModelServiceRow[];
  canManage: boolean;
  currency?: string;
}>();

const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "completed", flow: OnboardingFlowDTO): void;
}>();

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const busy = ref(false);
const note = ref("");
const attested = ref(false);
const error = ref("");

/** 与供应商面板同一句币种文案（授信额度标签用）。 */
const currencyText = computed(() => (props.currency === "USD" ? "美元 USD" : "人民币 CNY"));

/** 合同与授信资料是否齐备（与后端 CustomerBusinessDocsReady 同一判据）。 */
const checking = ref(false);
const contractOk = ref(false);
const creditOk = ref(false);
const docsReady = computed(() => contractOk.value && creditOk.value);

const contractNode = computed(() => props.nodes.find(node => node.key === "CONTRACT"));
const assigneeName = computed(() => contractNode.value?.assignee_name || "—");

/** 本次开通的模型：进度按「是否已有未归档绑定」算，与「接入模型」页签同口径。 */
const modelRows = computed(() => props.models.map(row => {
  const link = (row.links ?? [])[0];
  return {
    standard_model_id: row.standard_model_id,
    model_name: row.model_name || `标准模型 #${row.standard_model_id}`,
    opened: (row.bound_count ?? 0) > 0,
    supplier: link?.supplier_name || "",
    updatedAt: link?.activated_at || "",
  };
}));
const openedCount = computed(() => modelRows.value.filter(row => row.opened).length);
/** 已取消：我们没有「取消开通」这个动作，恒为 0，不编数。 */
const canceledCount = computed(() => 0);

async function checkDocs() {
  if (!props.customerId) return;
  checking.value = true;
  try {
    const [contracts, credits] = await Promise.all([
      f12SubjectRecordsApi.contracts("CUSTOMER", props.customerId),
      f12SubjectRecordsApi.credits("CUSTOMER", props.customerId),
    ]);
    const latest = contracts.list.find(item => item.is_latest) ?? contracts.list[0];
    contractOk.value = Boolean(latest && latest.status === "ARCHIVED");
    const current = credits.current?.find(item => item.direction === "US_TO_CUSTOMER");
    creditOk.value = Boolean(current?.record && Number(current.record.amount) > 0);
  } catch {
    contractOk.value = false;
    creditOk.value = false;
  } finally {
    checking.value = false;
  }
}

watch(() => props.visible, visible => {
  if (!visible) return;
  contractOpen.value = false;
  creditOpen.value = false;
  resetContractForm();
  resetCreditForm();
  error.value = "";
  note.value = "";
  attested.value = false;
  contractOk.value = false;
  creditOk.value = false;
  void checkDocs();
});

// ---------------- 上传合同资料（字段与供应商面板一致） ----------------
const contractOpen = ref(false);
const contractSaving = ref(false);
const contractError = ref("");
/**
 * `amount` 是原型「授信额度（人民币 CNY），合同可留空」那一格。
 * 供应商面板同样只收不落库（合同接口没有额度字段，授信走「上传授信资料」），
 * 这里保持同一形态，避免两处表单长得不一样。
 */
const contractForm = ref({ amount: "", documentName: "", documentNo: "", reason: "" });
const contractFile = ref<File | null>(null);

function resetContractForm() {
  contractForm.value = { amount: "", documentName: "", documentNo: "", reason: "" };
  contractFile.value = null;
  contractError.value = "";
}

function openContractUpload() {
  if (!props.canManage) return;
  resetContractForm();
  contractOpen.value = true;
}

function onContractFileChange(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0] ?? null;
  if (!file) { contractFile.value = null; return; }
  if (file.size < 1 || file.size > 2 * 1024 * 1024) {
    contractFile.value = null;
    input.value = "";
    contractError.value = "附件必须在 1 字节到 2 MiB 之间。";
    return;
  }
  if (!/\.(pdf|png|jpe?g)$/i.test(file.name)) {
    contractFile.value = null;
    input.value = "";
    contractError.value = "仅支持 PDF、PNG 或 JPEG 附件。";
    return;
  }
  contractError.value = "";
  contractFile.value = file;
}

/** 上传 → 建/改版本 → 立即归档：三步合一，商务只看到「保存资料」。 */
async function saveContract() {
  if (contractSaving.value) return;
  if (!contractForm.value.documentName.trim() || !contractForm.value.documentNo.trim()) {
    contractError.value = "请填写资料名称和资料编号。";
    return;
  }
  if (!contractFile.value) { contractError.value = "首次登记必须上传附件（PDF / PNG / JPEG，≤2MB）。"; return; }
  contractSaving.value = true;
  contractError.value = "";
  try {
    const subjectID = props.customerId;
    const reason = contractForm.value.reason.trim() || "上传合同资料";
    const uploaded = await f12SubjectRecordsApi.uploadAttachment(
      "CUSTOMER", subjectID, "CONTRACT", contractFile.value, reason, crypto.randomUUID(),
    );
    const input = {
      subjectType: "CUSTOMER" as const,
      subjectID,
      documentName: contractForm.value.documentName,
      documentNo: contractForm.value.documentNo,
      fileID: uploaded.id,
      reason,
    };
    const before = await f12SubjectRecordsApi.contracts("CUSTOMER", subjectID);
    const latest = before.list.find(item => item.is_latest) ?? before.list[0];
    if (latest?.contract_key) {
      await f12SubjectRecordsApi.reviseContract(latest.contract_key, latest.version, input, crypto.randomUUID());
    } else {
      await f12SubjectRecordsApi.createContract(input, crypto.randomUUID());
    }
    // 归档：后端完成 CONTRACT 校验的是「已归档合同」，不归档等于没传。
    const after = await f12SubjectRecordsApi.contracts("CUSTOMER", subjectID);
    const created = after.list.find(item => item.is_latest) ?? after.list[0];
    if (created && created.status !== "ARCHIVED") {
      await f12SubjectRecordsApi.archiveContract(created, "上传合同资料后自动归档", crypto.randomUUID());
    }
    contractOpen.value = false;
    await checkDocs();
    ElMessage.success("合同资料已上传并归档。");
  } catch (value) {
    contractError.value = message(value);
  } finally {
    contractSaving.value = false;
  }
}

// ---------------- 上传授信资料（字段与供应商面板一致） ----------------
const creditOpen = ref(false);
const creditSaving = ref(false);
const creditError = ref("");
const creditForm = ref({ amount: "", documentName: "", documentNo: "", reason: "" });
const creditFile = ref<File | null>(null);

function resetCreditForm() {
  creditForm.value = { amount: "", documentName: "", documentNo: "", reason: "" };
  creditFile.value = null;
  creditError.value = "";
}

function openCreditUpload() {
  if (!props.canManage) return;
  resetCreditForm();
  creditOpen.value = true;
}

function onCreditFileChange(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0] ?? null;
  if (!file) { creditFile.value = null; return; }
  if (file.size < 1 || file.size > 2 * 1024 * 1024) {
    creditFile.value = null;
    input.value = "";
    creditError.value = "附件必须在 1 字节到 2 MiB 之间。";
    return;
  }
  if (!/\.(pdf|png|jpe?g)$/i.test(file.name)) {
    creditFile.value = null;
    input.value = "";
    creditError.value = "仅支持 PDF、PNG 或 JPEG 附件。";
    return;
  }
  creditError.value = "";
  creditFile.value = file;
}

async function saveCredit() {
  if (creditSaving.value) return;
  if (!creditForm.value.documentName.trim() || !creditForm.value.documentNo.trim() || !creditForm.value.amount.trim()) {
    creditError.value = "请填写资料名称、资料编号和授信额度。";
    return;
  }
  if (!creditFile.value) { creditError.value = "每个授信版本都需提供对应依据附件。"; return; }
  creditSaving.value = true;
  creditError.value = "";
  try {
    const subjectID = props.customerId;
    const reason = creditForm.value.reason.trim() || "上传授信资料";
    const currency = (props.currency || "CNY").toUpperCase();
    const uploaded = await f12SubjectRecordsApi.uploadAttachment(
      "CUSTOMER", subjectID, "CREDIT", creditFile.value, reason, crypto.randomUUID(),
    );
    const before = await f12SubjectRecordsApi.credits("CUSTOMER", subjectID);
    const current = before.current?.find(item => item.direction === "US_TO_CUSTOMER" && item.record?.currency === currency);
    await f12SubjectRecordsApi.addCreditVersion({
      subjectType: "CUSTOMER",
      subjectID,
      direction: "US_TO_CUSTOMER",
      amount: creditForm.value.amount.trim(),
      currency,
      documentName: creditForm.value.documentName,
      documentNo: creditForm.value.documentNo,
      fileID: uploaded.id,
      expectedVersion: current?.latest_version ?? current?.record?.version ?? 0,
      reason,
    }, crypto.randomUUID());
    creditOpen.value = false;
    await checkDocs();
    ElMessage.success("授信资料已上传。");
  } catch (value) {
    creditError.value = message(value);
  } finally {
    creditSaving.value = false;
  }
}

// ---------------- 确认商务完成 ----------------
async function submit() {
  if (busy.value || !props.nodeId) return;
  if (!docsReady.value) {
    error.value = `还差：${[contractOk.value ? "" : "合同资料", creditOk.value ? "" : "授信资料"].filter(Boolean).join("、")}，请先用上面两个按钮上传。`;
    return;
  }
  if (!attested.value) { error.value = "请勾选「已完成合同与授信并上传资料」后再确认。"; return; }
  if (!note.value.trim()) { error.value = "请填写说明（例如：合同已用印、授信额度已确认）。"; return; }
  busy.value = true;
  error.value = "";
  try {
    const flow = await customerDetailApi.completeNode(props.nodeId, { reason: note.value.trim() });
    ElMessage.success("合同与授信已完成，工作台待办已结束。");
    emit("completed", flow);
    emit("update:visible", false);
  } catch (value) {
    error.value = message(value);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    :title="flowId ? `合同与授信 · ${flowId}` : '合同与授信'"
    width="min(1080px, 94vw)"
    :close-on-click-modal="false"
    destroy-on-close
    @update:model-value="(value: boolean) => emit('update:visible', value)"
  >
    <div class="contract-dialog">
      <p class="headline">
        进行中 · 接入中：{{ openedCount }} 个已开通、{{ canceledCount }} 个已取消 · 负责人 {{ assigneeName }}。
        本平台登记实际结果，生产操作由人员另外完成。
      </p>

      <el-table :data="modelRows" size="small" class="grid" empty-text="本次开通还没有模型">
        <el-table-column label="模型" min-width="220" prop="model_name" />
        <el-table-column label="进度" width="120">
          <template #default="{ row }">{{ row.opened ? "已开通" : "待配置" }}</template>
        </el-table-column>
        <el-table-column label="供应来源" min-width="180">
          <template #default="{ row }">{{ row.supplier || "未登记" }}</template>
        </el-table-column>
        <el-table-column label="更新时间" width="180">
          <template #default="{ row }">{{ row.updatedAt ? formatDateTime(row.updatedAt, "—", false) : "—" }}</template>
        </el-table-column>
      </el-table>

      <div class="uploads">
        <el-button :disabled="!canManage" @click="openContractUpload">上传合同资料</el-button>
        <el-button :disabled="!canManage" @click="openCreditUpload">上传授信资料</el-button>
        <span class="doc-state">
          合同：<b :class="contractOk ? 'ok' : 'pending'">{{ contractOk ? "已归档" : "未上传" }}</b>
          · 授信：<b :class="creditOk ? 'ok' : 'pending'">{{ creditOk ? "已登记" : "未上传" }}</b>
        </span>
      </div>

      <div class="field">
        <div class="field-label">说明</div>
        <el-input v-model="note" type="textarea" :rows="4" maxlength="200" show-word-limit
          placeholder="本次办理说明（必填）：例如合同已用印、授信额度已确认" />
      </div>

      <el-checkbox v-model="attested" :disabled="!canManage">已完成合同与授信并上传资料</el-checkbox>

      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    </div>

    <template #footer>
      <el-button @click="emit('update:visible', false)">关闭</el-button>
      <el-button type="primary" :loading="busy" :disabled="!canManage || checking" @click="submit">确认商务完成</el-button>
    </template>

    <!-- 上传合同资料：与供应商模型申报同一套表单 -->
    <el-dialog
      v-model="contractOpen"
      append-to-body
      title="上传合同资料"
      width="min(860px, 94vw)"
      destroy-on-close
      :close-on-click-modal="false"
      :close-on-press-escape="!contractSaving"
      :show-close="!contractSaving"
    >
      <el-alert v-if="contractError" type="error" :title="contractError" :closable="false" />
      <el-form label-position="top" :disabled="contractSaving" @submit.prevent="saveContract">
        <div class="f12-form-grid">
          <el-form-item label="资料名称">
            <el-input v-model="contractForm.documentName" maxlength="128" :disabled="contractSaving" aria-label="资料名称" />
          </el-form-item>
          <el-form-item label="资料编号">
            <el-input v-model="contractForm.documentNo" maxlength="128" :disabled="contractSaving" aria-label="资料编号" />
          </el-form-item>
          <el-form-item :label="`授信额度（${currencyText}），合同可留空`">
            <el-input v-model="contractForm.amount" inputmode="decimal" :disabled="contractSaving" aria-label="授信额度" />
          </el-form-item>
          <el-form-item label="附件上传">
            <input class="f12-file" type="file" accept=".pdf,.png,.jpg,.jpeg" :disabled="contractSaving" @change="onContractFileChange">
            <small>2 MB 以内；{{ contractFile?.name || "首次登记必须上传附件" }}</small>
          </el-form-item>
          <el-form-item label="备注" class="full">
            <el-input v-model="contractForm.reason" type="textarea" :rows="2" maxlength="500" :disabled="contractSaving" aria-label="备注" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button :disabled="contractSaving" @click="contractOpen = false">取消</el-button>
        <el-button type="primary" :loading="contractSaving" @click="saveContract">保存资料</el-button>
      </template>
    </el-dialog>

    <!-- 上传授信资料：与供应商模型申报同一套表单 -->
    <el-dialog
      v-model="creditOpen"
      append-to-body
      title="上传授信资料"
      width="min(860px, 94vw)"
      destroy-on-close
      :close-on-click-modal="false"
      :close-on-press-escape="!creditSaving"
      :show-close="!creditSaving"
    >
      <el-alert v-if="creditError" type="error" :title="creditError" :closable="false" />
      <el-form label-position="top" :disabled="creditSaving" @submit.prevent="saveCredit">
        <div class="f12-form-grid">
          <el-form-item label="资料名称">
            <el-input v-model="creditForm.documentName" maxlength="128" :disabled="creditSaving" aria-label="资料名称" />
          </el-form-item>
          <el-form-item label="资料编号">
            <el-input v-model="creditForm.documentNo" maxlength="128" :disabled="creditSaving" aria-label="资料编号" />
          </el-form-item>
          <el-form-item :label="`授信额度（${currencyText}）`">
            <el-input v-model="creditForm.amount" inputmode="decimal" placeholder="正数，最多两位小数" :disabled="creditSaving" aria-label="授信额度" />
          </el-form-item>
          <el-form-item label="附件上传">
            <input class="f12-file" type="file" accept=".pdf,.png,.jpg,.jpeg" :disabled="creditSaving" @change="onCreditFileChange">
            <small>2 MB 以内；{{ creditFile?.name || "每个授信版本都需提供对应依据附件" }}</small>
          </el-form-item>
          <el-form-item label="备注" class="full">
            <el-input v-model="creditForm.reason" type="textarea" :rows="2" maxlength="500" :disabled="creditSaving" aria-label="备注" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button :disabled="creditSaving" @click="creditOpen = false">取消</el-button>
        <el-button type="primary" :loading="creditSaving" @click="saveCredit">保存资料</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<style scoped>
.contract-dialog { display: flex; flex-direction: column; gap: 12px; }
.headline { font-size: 12px; color: var(--app-text-muted); margin: 0; }
.grid { width: 100%; }
.uploads { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.doc-state { font-size: 12px; color: var(--app-text-muted); }
.doc-state .ok { color: var(--el-color-success); }
.doc-state .pending { color: var(--el-color-warning); }
.field-label { font-size: 13px; color: var(--app-text-muted); margin-bottom: 6px; }
/* 与供应商 F12 面板同一套栅格：两列 + 备注整行 */
.f12-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 4px 16px; }
.f12-form-grid .full { grid-column: 1 / -1; }
.f12-form-grid :deep(.el-form-item) { margin-bottom: 14px; }
.f12-file { width: 100%; padding: 5px 0; }
.f12-form-grid small { display: block; color: var(--app-text-muted); font-size: 12px; }
@media (max-width: 640px) { .f12-form-grid { grid-template-columns: 1fr; } }
</style>
