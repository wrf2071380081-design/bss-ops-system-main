<script setup lang="ts">
/**
 * 客户移交弹窗（B3b 第 4 项：从 CustomerWorkspace 抽出来共用）。
 *
 * 为什么抽：列表页与客户详情页都要能移交，两处各写一份"预览影响 → 二次确认"的逻辑，
 * 迟早出现一处带二次确认、一处漏了的情况。这里把整段行为收进组件，调用方只关心"选哪个客户"。
 *
 * 行为与抽取前完全一致（纯抽取，无功能变化）：
 *   - 「检查影响」调 transferPreview，展示随迁报价/关联价目表；
 *   - 「确认移交」先 ElMessageBox 二次确认，再 transferConfirm；
 *   - 输入变化即作废已取到的预览（避免拿旧影响面去确认）。
 */
import { reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { customersApi } from "../../../api/customers";
import type { CustomerTransferImpactDTO } from "../../../api/customers.types";
import { ApiError } from "../../../domain/common";

const props = defineProps<{
  visible: boolean;
  customerId: string;
  customerName: string;
  ownerName: string;
}>();

const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "transferred"): void;
}>();

const form = reactive({ toOperatorId: "", reason: "" });
const impact = ref<CustomerTransferImpactDTO>();
const error = ref("");
const submitting = ref(false);
// sequence 用来丢弃过期响应：用户改输入或重开弹窗后，旧的预览结果不能覆盖新的。
let sequence = 0;

const positiveId = (value: string) => /^[1-9]\d*$/.test(value.trim()) && Number.isSafeInteger(Number(value));
const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

function reset() {
  ++sequence;
  submitting.value = false;
  Object.assign(form, { toOperatorId: "", reason: "" });
  impact.value = undefined;
  error.value = "";
}

watch(() => props.visible, value => {
  if (value) reset();
});

function invalidatePreview() {
  ++sequence;
  impact.value = undefined;
  error.value = "";
}

function close() {
  if (submitting.value) return;
  emit("update:visible", false);
}

async function preview() {
  if (!positiveId(props.customerId) || submitting.value) return;
  if (!positiveId(form.toOperatorId)) {
    error.value = "请输入有效的目标销售人员 ID。";
    return;
  }
  const current = ++sequence;
  const draft = { toOperatorId: form.toOperatorId.trim(), reason: form.reason.trim() };
  submitting.value = true;
  error.value = "";
  try {
    const result = await customersApi.transferPreview(props.customerId, draft);
    if (current === sequence) impact.value = result;
  } catch (value) {
    if (current === sequence) {
      impact.value = undefined;
      error.value = message(value);
    }
  } finally {
    if (current === sequence) submitting.value = false;
  }
}

async function confirm() {
  if (!impact.value || submitting.value) return;
  const draft = { toOperatorId: form.toOperatorId.trim(), reason: form.reason.trim() };
  const current = impact.value;
  submitting.value = true;
  try {
    await ElMessageBox.confirm(
      `确认将客户从“${current.fromOperatorName}”移交给“${current.toOperatorName}”？${current.quoteCount} 份报价将随迁。`,
      "确认客户移交",
      { type: "warning" },
    );
  } catch {
    submitting.value = false;
    return;
  }
  error.value = "";
  try {
    const result = await customersApi.transferConfirm(props.customerId, draft);
    submitting.value = false;
    emit("update:visible", false);
    ElMessage.success(`客户移交完成，已迁移 ${result.quotesMigrated} 份报价。`);
    emit("transferred");
  } catch (value) {
    error.value = message(value);
    submitting.value = false;
  }
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="移交客户"
    width="min(680px, 96vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!submitting"
    :show-close="!submitting"
    @update:model-value="close"
  >
    <p v-if="customerName">客户：<b>{{ customerName }}</b> · 当前销售 {{ ownerName || "—" }}</p>
    <el-form class="dialog-form" label-position="top" :disabled="submitting">
      <el-form-item label="目标销售人员 ID *">
        <el-input v-model="form.toOperatorId" inputmode="numeric" @input="invalidatePreview" />
      </el-form-item>
      <el-form-item label="移交原因">
        <el-input v-model="form.reason" type="textarea" :rows="3" maxlength="200" show-word-limit @input="invalidatePreview" />
      </el-form-item>
    </el-form>
    <el-descriptions v-if="impact" :column="2" border>
      <el-descriptions-item label="原归属">{{ impact.fromOperatorName }}（{{ impact.fromOperatorId }}）</el-descriptions-item>
      <el-descriptions-item label="新归属">{{ impact.toOperatorName }}（{{ impact.toOperatorId }}）</el-descriptions-item>
      <el-descriptions-item label="随迁报价">{{ impact.quoteCount }} 份</el-descriptions-item>
      <el-descriptions-item label="关联价目表">{{ impact.priceBookCount }} 条</el-descriptions-item>
    </el-descriptions>
    <el-alert v-if="error" class="form-error" :title="error" type="error" show-icon :closable="false" />
    <template #footer>
      <el-button :disabled="submitting" @click="close">取消</el-button>
      <el-button v-if="!impact" type="primary" :loading="submitting" @click="preview">检查影响</el-button>
      <el-button v-else type="danger" :loading="submitting" @click="confirm">确认移交</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.dialog-form { margin-top: 16px; }
.form-error { margin-top: 14px; }
</style>
