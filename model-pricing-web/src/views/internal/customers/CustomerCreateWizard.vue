<script setup lang="ts">
/**
 * 建客户向导（B3b 第 2 项）。
 *
 * 为什么是"一个向导两步"而不是"建完跳详情再开通"：
 * 业务上「建了客户但没开通」= 客户不能用，两个动作必须连着做。
 * 后端两个接口、两个幂等键分开是对的，但 UI 要把"必须连着做"表达出来。
 *
 * 失败恢复（重点）：
 *   Step1 成功、Step2 失败时**客户已经建好了**。此时绝不从头再来——
 *   那会再建一个同名客户（后端 409，用户困惑）。向导保留 customerId，
 *   并给两个出口：① 用下面的表单重试发起开通（复用已建客户）② 稍后在详情页处理。
 */
import { onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { customerDetailApi, type CustomerProfileDTO } from "../../../api/customerDetail";
import { ApiError } from "../../../domain/common";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";
import CustomerOnboardingStartForm from "./CustomerOnboardingStartForm.vue";

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "created", profile: CustomerProfileDTO): void;
}>();

const router = useRouter();
const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const step = ref<1 | 2>(1);
const people = ref<WorkbenchPersonDTO[]>([]);
const submitting = ref(false);
const error = ref("");
/** Step1 建成的客户；有值就代表"再也不能回到 Step1 重建"。 */
const created = ref<CustomerProfileDTO>();

const form = reactive({
  legalName: "",
  ownerSalesOperatorId: "",
  region: "CN",
  settlementCurrency: "CNY",
  taxRate: "",
  billingCycle: "",
  paymentType: "PREPAID",
  uscc: "",
});

const TAX_RATE = /^(?:0(?:\.\d{1,4})?|1(?:\.0{1,4})?)$/;

function reset() {
  step.value = 1;
  submitting.value = false;
  error.value = "";
  created.value = undefined;
  Object.assign(form, {
    legalName: "", ownerSalesOperatorId: "", region: "CN", settlementCurrency: "CNY",
    taxRate: "", billingCycle: "", paymentType: "PREPAID", uscc: "",
  });
}

watch(() => props.visible, value => {
  if (!value) return;
  // 重开时**不能**无条件 reset：Step2 失败后用户关掉再打开是"继续"，不是"重新开始"。
  // 一旦清空 created，就只能重填客户名并撞后端 409「该客户名称已存在」——
  // 提示语承诺的"可直接在本页重试"就成了空话。
  // 想换一个客户建，走 Step2 的「放弃本次，重新建客户」出口（会显式 reset）。
  if (!created.value) {
    reset();
  } else {
    step.value = 2;
  }
  void loadPeople();
});

async function loadPeople() {
  try {
    people.value = await workbenchApi.people();
  } catch {
    people.value = [];
  }
}

const positiveId = (value: string) => /^[1-9]\d*$/.test(value.trim()) && Number.isSafeInteger(Number(value));

function validateStep1() {
  const name = form.legalName.trim();
  if (!name) return "请填写客户名称。";
  if ([...name].length > 128) return "客户名称不能超过 128 个字符。";
  if (!positiveId(form.ownerSalesOperatorId)) return "请选择销售归属。";
  if (!["CN", "OVERSEAS"].includes(form.region)) return "请选择国内或海外主体。";
  if (!["CNY", "USD"].includes(form.settlementCurrency)) return "结算币种仅支持 CNY 或 USD。";
  if (form.taxRate.trim() && !TAX_RATE.test(form.taxRate.trim())) return "税率必须是 0 到 1 之间、最多 4 位小数的数字（如 0.06）。";
  if (form.billingCycle.trim()) {
    const cycle = Number(form.billingCycle.trim());
    if (!Number.isSafeInteger(cycle) || cycle < 1 || cycle > 365) return "账期天数必须在 1 到 365 之间（不填则默认 30）。";
  }
  return "";
}

async function submitStep1() {
  const invalid = validateStep1();
  if (invalid) {
    error.value = invalid;
    return;
  }
  if (submitting.value || created.value) return;
  error.value = "";
  submitting.value = true;
  try {
    const profile = await customerDetailApi.createCustomer({
      legal_name: form.legalName.trim(),
      subject_type: "COMPANY", // v1 只支持企业主体（个人主体后端直接 400）
      uscc: form.uscc.trim() || undefined,
      region: form.region,
      settlement_currency: form.settlementCurrency,
      tax_rate: form.taxRate.trim() || undefined,
      owner_sales_operator_id: Number(form.ownerSalesOperatorId),
      payment_type: form.paymentType,
      billing_cycle: form.billingCycle.trim() ? Number(form.billingCycle.trim()) : undefined,
      reason: "新建客户",
    });
    created.value = profile;
    step.value = 2;
    emit("created", profile);
  } catch (value) {
    // 重名等 409 直接展示后端文案（"该客户名称已存在"），不要自己编。
    error.value = message(value);
  } finally {
    submitting.value = false;
  }
}

function goStep2() {
  if (created.value) step.value = 2;
}

// 明确出口：用户确实想放弃这个已创建的客户、重新建一个新的时才清空。
// 没有这个出口，重开后会一直停在 Step2，用户被锁在"只能重试当前客户"里，同样是死路。
function abandonAndRestart() {
  reset();
  ElMessage.info("已清空本次填写，可以重新创建客户。");
}

function gotoDetailLater() {
  const profile = created.value;
  emit("update:visible", false);
  if (profile) void router.push({ path: `/internal/customers/${profile.id}`, query: { tab: "onboarding" } });
}

function close() {
  if (submitting.value) return;
  const profile = created.value;
  emit("update:visible", false);
  if (profile && step.value === 2) {
    ElMessage.info(`客户「${profile.legal_name}」已创建；开通尚未完成，可稍后在客户详情继续发起。`);
  }
}

function onStarted() {
  ElMessage.success("客户已创建并完成开通发起。");
  emit("update:visible", false);
}

onMounted(() => { void loadPeople(); });
</script>

<template>
  <el-dialog
    :model-value="visible"
    title="新建客户"
    width="min(1000px, 96vw)"
    :close-on-click-modal="false"
    :show-close="!submitting"
    @update:model-value="close"
  >
    <el-steps :active="step - 1" finish-status="success" align-center class="steps">
      <el-step title="客户信息" description="建客户档案" />
      <el-step title="发起开通" description="选来源并派办理人" />
    </el-steps>

    <!-- ================= Step 1 ================= -->
    <template v-if="step === 1">
      <el-form label-position="top" :disabled="submitting">
        <div class="form-grid">
          <el-form-item label="客户名称 *">
            <el-input v-model="form.legalName" maxlength="128" show-word-limit placeholder="营业执照上的企业名称" />
          </el-form-item>
          <el-form-item label="销售归属 *">
            <el-select v-model="form.ownerSalesOperatorId" filterable placeholder="选择销售" class="full">
              <el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" />
            </el-select>
          </el-form-item>
          <el-form-item label="国内 / 海外 *">
            <el-radio-group v-model="form.region">
              <el-radio value="CN">国内</el-radio>
              <el-radio value="OVERSEAS">海外</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="结算币种 *">
            <el-select v-model="form.settlementCurrency" class="full">
              <el-option label="人民币 CNY" value="CNY" />
              <el-option label="美元 USD" value="USD" />
            </el-select>
          </el-form-item>
          <el-form-item label="税率">
            <el-input v-model="form.taxRate" placeholder="如 0.06（0~1，最多 4 位小数）" />
          </el-form-item>
          <el-form-item label="账期天数">
            <el-input v-model="form.billingCycle" inputmode="numeric" placeholder="不填默认 30（1~365）" />
          </el-form-item>
          <el-form-item label="结算方式">
            <el-select v-model="form.paymentType" class="full">
              <el-option label="预付 PREPAID" value="PREPAID" />
              <el-option label="月结 MONTHLY" value="MONTHLY" />
              <el-option label="按量付费 PAYG" value="PAYG" />
            </el-select>
          </el-form-item>
          <el-form-item label="统一社会信用代码">
            <el-input v-model="form.uscc" maxlength="18" placeholder="18 位字母数字，可留空" />
          </el-form-item>
        </div>
      </el-form>
      <div class="hint">客户主体类型当前仅支持企业（个人主体待后续开放）。重名会直接提示，不会建出重复客户。</div>
    </template>

    <!-- ================= Step 2 ================= -->
    <template v-else>
      <el-alert
        v-if="created"
        type="success"
        show-icon
        :closable="false"
        class="created-alert"
        :title="`客户「${created.legal_name}」已创建（#${created.id}）`"
        description="下一步发起开通。若发起失败，客户不会重复创建——可直接在本页重试，或稍后到客户详情继续。"
      />
      <CustomerOnboardingStartForm
        v-if="created"
        :customer-id="String(created.id)"
        :region="created.region"
        @started="onStarted"
      />
      <div class="step-actions">
        <el-button text @click="abandonAndRestart">放弃本次，重新建客户</el-button>
        <el-button text @click="gotoDetailLater">稍后在客户详情处理</el-button>
      </div>
    </template>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="form-error" />

    <template #footer>
      <template v-if="step === 1">
        <el-button :disabled="submitting" @click="close">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitStep1">创建并继续</el-button>
      </template>
      <template v-else>
        <el-button :disabled="submitting" @click="close">关闭</el-button>
        <el-button v-if="created" text @click="goStep2">重试发起开通</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<style scoped>
.steps { margin-bottom: 20px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 20px; }
.full { width: 100%; }
.hint { color: var(--app-text-muted); font-size: 12px; margin-top: 4px; }
.created-alert { margin-bottom: 14px; }
.step-actions { display: flex; justify-content: flex-end; margin-top: 8px; }
.form-error { margin-top: 12px; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
