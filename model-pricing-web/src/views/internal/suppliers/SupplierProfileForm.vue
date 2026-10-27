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
  name: "", region: "CN", registration_no: "", contact: "", phone: "", tax_rate_percent: "", settlement_currency: "CNY",
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
  try {
    buildSupplierEntitySave(form, reason.value, props.profile?.revision);
    emit("save", { ...form }, reason.value);
  } catch (value) { validationError.value = value instanceof Error ? value.message : "请检查主体资料。"; }
}
</script>

<template>
  <el-dialog :model-value="modelValue" :title="profile ? '维护供应商主体' : '新建供应商主体'" width="min(680px, 94vw)" destroy-on-close :close-on-click-modal="false" :close-on-press-escape="!saving && !reloading" :show-close="!saving && !reloading" @update:model-value="close">
    <el-alert v-if="validationError" :title="validationError" type="error" show-icon :closable="false" />
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <template v-if="conflict && profile">
        <p>服务器资料已变化。为避免覆盖其他人的更新，请先载入最新资料，再检查并重新填写本次修改。</p>
        <el-button text :loading="reloading" :disabled="saving" @click="emit('reload')">载入最新资料</el-button>
      </template>
    </el-alert>
    <el-alert v-if="profile && aliasesSupported === false" title="当前数据库尚未提供供应商曾用名结构，暂不能修改主体名称；其他资料仍可维护。" type="warning" show-icon :closable="false" />
    <el-alert v-if="!profile && aliasesSupported === false" title="当前数据库尚未提供曾用名结构。新建主体可正常保存，但后续主体改名需等结构交付。" type="info" :closable="false" />
    <el-form label-position="top" class="supplier-entity-form" :disabled="saving || reloading" @submit.prevent="submit">
      <div class="entity-form-grid">
        <el-form-item label="供应商名称" required><el-input v-model="form.name" maxlength="128" :disabled="!!profile && aliasesSupported === false" /></el-form-item>
        <el-form-item label="主体区域" required><el-select v-model="form.region"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select></el-form-item>
        <el-form-item label="统一社会信用代码 / 登记号"><el-input v-model="form.registration_no" maxlength="64" /></el-form-item>
        <el-form-item label="联系人"><el-input v-model="form.contact" maxlength="64" /></el-form-item>
        <el-form-item label="联系电话"><el-input v-model="form.phone" maxlength="32" /></el-form-item>
        <el-form-item label="税率（百分比）"><el-input v-model="form.tax_rate_percent" inputmode="decimal" placeholder="可留空；例如 6 或 6.25" /></el-form-item>
        <el-form-item label="结算币种" required><el-select v-model="form.settlement_currency"><el-option label="人民币 CNY" value="CNY" /><el-option label="美元 USD" value="USD" /></el-select></el-form-item>
        <el-form-item label="操作说明" required><el-input v-model="reason" maxlength="500" placeholder="说明建档或资料调整原因" /></el-form-item>
      </div>
      <el-alert v-if="!profile" title="初始归属采购人自动绑定当前登录员工；首次办理完成前，主体状态为“尚未完成首次接入”。" type="info" :closable="false" />
    </el-form>
    <template #footer>
      <el-button :disabled="saving || reloading" @click="close">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="saving || reloading || conflict" @click="submit">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.entity-form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.entity-form-grid :deep(.el-select){width:100%}
@media(max-width:700px){.entity-form-grid{grid-template-columns:1fr}}
</style>
