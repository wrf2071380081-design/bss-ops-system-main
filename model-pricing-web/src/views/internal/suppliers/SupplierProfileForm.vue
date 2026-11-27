<script setup lang="ts">
import { reactive, ref, watch } from "vue";
import Decimal from "decimal.js";
import { buildSupplierEntitySave, type SupplierEntityDTO, type SupplierEntityForm } from "../../../api/supplierEntities.contract";

const props = withDefaults(defineProps<{
  modelValue: boolean;
  profile?: SupplierEntityDTO;
  aliasesSupported?: boolean;
  saving?: boolean;
  reloading?: boolean;
  reloadKey?: number;
  conflict?: boolean;
  error?: string;
}>(), { profile: undefined, saving: false, reloading: false, reloadKey: 0, conflict: false, error: "" });

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  save: [form: SupplierEntityForm, reason: string];
  reload: [];
}>();

const emptyForm = (): SupplierEntityForm => ({
  name: "", region: "CN", registration_no: "", contact: "", phone: "", tax_rate_percent: "", settlement_currency: "CNY", remark: "",
  contact_title: "", contact_email: "", default_settlement: "", default_payment: "", invoice_preference: "",
  min_consumption: "", data_training_policy: "", log_retention_policy: "", default_sla: "",
});
const form = reactive<SupplierEntityForm>(emptyForm());
const reason = ref("");
const validationError = ref("");

function reset() {
  Object.assign(form, props.profile ? {
    name: props.profile.name,
    region: props.profile.region,
    registration_no: props.profile.registration_no,
    contact: props.profile.contact,
    phone: props.profile.phone,
    tax_rate_percent: props.profile.tax_rate === null ? "" : new Decimal(props.profile.tax_rate).mul(100).toFixed(),
    settlement_currency: props.profile.settlement_currency,
    remark: props.profile.remark,
    contact_title: props.profile.contact_title ?? "",
    contact_email: props.profile.contact_email ?? "",
    default_settlement: props.profile.default_settlement ?? "",
    default_payment: props.profile.default_payment ?? "",
    invoice_preference: props.profile.invoice_preference ?? "",
    min_consumption: props.profile.min_consumption ?? "",
    data_training_policy: props.profile.data_training_policy ?? "",
    log_retention_policy: props.profile.log_retention_policy ?? "",
    default_sla: props.profile.default_sla ?? "",
  } : emptyForm());
  reason.value = "";
  validationError.value = "";
}

watch(() => props.modelValue, open => { if (open) reset(); });
watch(() => props.reloadKey, (value, previous) => { if (props.modelValue && value !== previous) reset(); });
function close() { if (!props.saving && !props.reloading) emit("update:modelValue", false); }
function submit() {
  if (props.saving || props.reloading || props.conflict) return;
  validationError.value = "";
  // 新建走原型「内部主动引入」的极简表单：只有 4 个字段，没有操作说明，
  // 因此用固定文案作为建档原因（原型对应的事件也叫「内部主动引入供应商」）。
  const saveReason = props.profile ? reason.value : "内部主动引入";
  try {
    if (!props.profile) validateInternalIntake();
    buildSupplierEntitySave(form, saveReason, props.profile?.revision);
    emit("save", { ...form }, saveReason);
  } catch (value) { validationError.value = value instanceof Error ? value.message : "请检查主体资料。"; }
}

/** 原型口径：名称 / 企业注册编号（=统一社会信用代码）/ 联系人为必填；电话选填但填了要合法。 */
function validateInternalIntake() {
  if (!form.name.trim() || !form.registration_no.trim() || !form.contact.trim()) {
    throw new Error("请填写供应商名称、注册编号和联系人");
  }
  const phone = form.phone.trim();
  if (phone && !/^\+?[0-9][0-9 -]{5,19}$/.test(phone)) throw new Error("联系电话格式无效");
}
</script>

<template>
  <el-dialog :model-value="modelValue" :title="profile ? '维护供应商主体' : '新增供应商 · 内部主动引入'" :width="profile ? 'min(760px, 94vw)' : 'min(560px, 94vw)'" destroy-on-close :close-on-click-modal="false" :close-on-press-escape="!saving && !reloading" :show-close="!saving && !reloading" @update:model-value="close">
    <el-alert v-if="validationError" :title="validationError" type="error" show-icon :closable="false" />
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <template v-if="conflict && profile">
        <p>服务器资料已变化。为避免覆盖其他人的更新，请先载入最新资料，再检查并重新填写本次修改。</p>
        <el-button text :loading="reloading" :disabled="saving" @click="emit('reload')">载入最新资料</el-button>
      </template>
    </el-alert>
    <el-alert v-if="profile && aliasesSupported === false" title="当前数据库尚未提供供应商曾用名结构，暂不能修改主体名称；其他资料仍可维护。" type="warning" show-icon :closable="false" />

    <!-- 新建：完全按原型「新增供应商 · 内部主动引入」——只有 4 个字段，无区域/税率/币种/条款。 -->
    <el-form v-if="!profile" label-position="top" class="supplier-entity-form" :disabled="saving || reloading" @submit.prevent="submit">
      <p class="entity-form-hint">创建潜在供应商档案；不生成供应商自主申请或审批记录。后续内部测试、合同授信与接入分别办理。</p>
      <div class="entity-form-grid">
        <el-form-item label="供应商名称" required><el-input v-model="form.name" maxlength="128" /></el-form-item>
        <el-form-item label="企业注册编号" required><el-input v-model="form.registration_no" maxlength="64" /></el-form-item>
        <el-form-item label="联系人" required><el-input v-model="form.contact" maxlength="64" /></el-form-item>
        <el-form-item label="联系电话"><el-input v-model="form.phone" maxlength="32" /></el-form-item>
      </div>
    </el-form>

    <el-form v-else label-position="top" class="supplier-entity-form" :disabled="saving || reloading" @submit.prevent="submit">
      <div class="entity-form-grid">
        <el-form-item label="供应商名称" required><el-input v-model="form.name" maxlength="128" :disabled="aliasesSupported === false" /></el-form-item>
        <el-form-item label="统一社会信用代码"><el-input v-model="form.registration_no" maxlength="64" /></el-form-item>
        <el-form-item label="联系人"><el-input v-model="form.contact" maxlength="64" /></el-form-item>
        <el-form-item label="联系人职务"><el-input v-model="form.contact_title" maxlength="64" /></el-form-item>
        <el-form-item label="联系人邮箱"><el-input v-model="form.contact_email" maxlength="128" /></el-form-item>
        <el-form-item label="联系电话"><el-input v-model="form.phone" maxlength="32" /></el-form-item>
        <el-form-item label="税率（百分比）"><el-input v-model="form.tax_rate_percent" inputmode="decimal" placeholder="可留空；例如 6 或 6.25" /></el-form-item>
        <el-form-item label="结算币种" required><el-select v-model="form.settlement_currency"><el-option label="人民币 CNY" value="CNY" /><el-option label="美元 USD" value="USD" /></el-select></el-form-item>
        <el-form-item label="备注" class="span-2"><el-input v-model="form.remark" type="textarea" :rows="2" maxlength="500" placeholder="选填，例如合作背景或特殊约定" /></el-form-item>
      </div>

      <h3 class="entity-form-section">主体默认商务与合规条款</h3>
      <p class="entity-form-hint">以下条款均可留空；示例文案只是提示，不会写入档案。</p>
      <div class="entity-form-grid">
        <el-form-item label="默认账期"><el-input v-model="form.default_settlement" maxlength="128" placeholder="预付 / 月结 / T+30" /></el-form-item>
        <el-form-item label="默认付款方式"><el-input v-model="form.default_payment" maxlength="128" placeholder="银行转账 / 在线支付 / 其他" /></el-form-item>
        <el-form-item label="发票偏好"><el-input v-model="form.invoice_preference" maxlength="128" placeholder="专用发票 / 普通发票 / 其他" /></el-form-item>
        <el-form-item label="最低消费"><el-input v-model="form.min_consumption" maxlength="128" placeholder="无 / 金额 + 币种 + 周期" /></el-form-item>
        <el-form-item label="数据训练政策"><el-input v-model="form.data_training_policy" maxlength="128" placeholder="不用于训练 / 经授权使用 / 按协议约定" /></el-form-item>
        <el-form-item label="日志留存与删除策略"><el-input v-model="form.log_retention_policy" maxlength="128" placeholder="不留存 / 留存 X 天 / 按协议删除" /></el-form-item>
        <el-form-item label="默认 SLA"><el-input v-model="form.default_sla" maxlength="128" placeholder="99.9% / 按合同约定" /></el-form-item>
      </div>

      <div class="entity-form-grid">
        <el-form-item label="操作说明" required class="span-2"><el-input v-model="reason" maxlength="500" placeholder="说明资料调整原因" /></el-form-item>
      </div>
    </el-form>

    <template #footer>
      <el-button :disabled="saving || reloading" @click="close">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="saving || reloading || conflict" @click="submit">{{ profile ? "保存" : "创建档案" }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.entity-form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.entity-form-grid :deep(.el-select){width:100%}
.entity-form-grid .span-2{grid-column:1 / -1}
.entity-form-section{margin:6px 0 4px;font-size:14px;font-weight:500}
.entity-form-hint{margin:0 0 10px;color:var(--app-text-muted);font-size:12px}
@media(max-width:700px){.entity-form-grid{grid-template-columns:1fr}}
</style>
