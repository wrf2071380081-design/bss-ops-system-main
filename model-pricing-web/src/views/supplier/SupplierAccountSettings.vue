<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, type UploadFile } from "element-plus";
import { supplierAccountApi } from "../../api/supplierAccount";
import type {
  SupplierAccountDTO,
  SupplierOperationLogDTO,
} from "../../api/supplierAccount.types";
import { ApiError } from "../../domain/common";
import { formatDateTime } from "../../domain/date";
import { registrationLabelOf } from "../../api/supplierAccount.contract";

const activeTab = ref<"profile" | "logs">("profile");
const account = ref<SupplierAccountDTO | null>(null);
const logs = ref<SupplierOperationLogDTO[]>([]);
const logsTotal = ref(0);
const loading = ref(false);
const logsLoading = ref(false);
const saving = ref(false);
const error = ref("");
const logsError = ref("");
const detail = ref<SupplierOperationLogDTO | null>(null);
/** 已选但还没上传的企业证明文件。保存时才真正提交（原型只有一个「保存资料」）。 */
const pendingFile = ref<File | null>(null);

const form = reactive({
  name: "", registrationNo: "", contact: "",
  contactTitle: "", contactEmail: "",
  defaultSettlement: "", defaultPayment: "", invoicePreference: "", minConsumption: "",
  dataTrainingPolicy: "", logRetentionPolicy: "", defaultSla: "",
});

/** 页头副标题：主体名 · 国内/海外主体（原型「澄海智算（演示） · 国内主体」）。 */
const regionLabel = computed(() => (account.value?.region === "OVERSEAS" ? "海外主体" : "国内主体"));

/** 注册编号的 label 由**已保存**的 region 决定（原型实测如此，见测试用例 B05）。
 * 区域字段本身已按原型从账户设置里去掉，但它的值仍决定这行文案。 */
const registrationLabel = computed(() => registrationLabelOf(account.value?.region || "CN"));

const licenseName = computed(() => pendingFile.value?.name || account.value?.license?.name || "");

const errorMessage = (value: unknown, fallback = "操作失败，请稍后重试。") =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : fallback;

function syncForm(value: SupplierAccountDTO) {
  form.name = value.name;
  form.registrationNo = value.registrationNo;
  form.contact = value.contact;
  form.contactTitle = value.contactTitle ?? "";
  form.contactEmail = value.contactEmail ?? "";
  form.defaultSettlement = value.defaultSettlement ?? "";
  form.defaultPayment = value.defaultPayment ?? "";
  form.invoicePreference = value.invoicePreference ?? "";
  form.minConsumption = value.minConsumption ?? "";
  form.dataTrainingPolicy = value.dataTrainingPolicy ?? "";
  form.logRetentionPolicy = value.logRetentionPolicy ?? "";
  form.defaultSla = value.defaultSla ?? "";
  pendingFile.value = null;
}

async function loadAccount() {
  loading.value = true;
  error.value = "";
  try {
    const value = await supplierAccountApi.getAccount();
    account.value = value;
    syncForm(value);
  } catch (value) {
    account.value = null;
    error.value = errorMessage(value, "账户资料暂时无法加载。");
  } finally {
    loading.value = false;
  }
}

/**
 * 静默刷新（不打 loading 遮罩）：保存成功后用它回读一次服务端状态。
 *
 * 为什么不直接用 PUT 的响应：PUT 挂了幂等，同一份请求体重复提交时可能拿到上一次的缓存结果，
 * 而「先传企业证明再保存资料」的典型流程恰好是"资料字段没变、只有证明变了"——
 * 这时 PUT 响应里的 license 可能是旧的。回读一次 GET 才是权威值。
 */
async function refreshAccount() {
  try {
    const value = await supplierAccountApi.getAccount();
    account.value = value;
    syncForm(value);
  } catch {
    // 回读失败不影响保存结果，页面保留 PUT 响应里的值。
  }
}

async function loadLogs() {
  logsLoading.value = true;
  logsError.value = "";
  try {
    const page = await supplierAccountApi.listOperationLogs({ page: 1, size: 50 });
    logs.value = page.list;
    logsTotal.value = page.total;
  } catch (value) {
    logs.value = [];
    logsTotal.value = 0;
    logsError.value = errorMessage(value, "操作记录暂时无法加载。");
  } finally {
    logsLoading.value = false;
  }
}

function onTabChange(name: string | number) {
  if (name === "logs" && !logs.value.length && !logsLoading.value) void loadLogs();
}

function chooseLicense(file: UploadFile) {
  if (file.size && file.size > 10 * 1024 * 1024) {
    ElMessage.error("企业证明不能超过 10 MB。");
    return;
  }
  if (!file.raw) {
    ElMessage.error("无法读取所选文件，请重新选择。");
    return;
  }
  pendingFile.value = file.raw;
  ElMessage.success("已选择文件，保存后生效。");
}

async function save() {
  if (saving.value || !account.value) return;
  const name = form.name.trim();
  // 原型：清空企业名称保存 → 统一 toast「企业资料必填」。
  if (!name) {
    ElMessage.error("企业资料必填");
    return;
  }
  saving.value = true;
  try {
    // 企业证明走独立接口：先传文件拿到 fileRef，再保存资料。
    // 两步各自留痕（「更换企业证明」+「维护企业资料」），与原型操作记录的样子一致。
    if (pendingFile.value) {
      const license = await supplierAccountApi.uploadLicense(pendingFile.value, form.contact.trim());
      account.value = { ...account.value, license };
      pendingFile.value = null;
    }
    const updated = await supplierAccountApi.updateAccount({
      name,
      registrationNo: form.registrationNo.trim(),
      contact: form.contact.trim(),
      contactTitle: form.contactTitle.trim(),
      contactEmail: form.contactEmail.trim(),
      defaultSettlement: form.defaultSettlement.trim(),
      defaultPayment: form.defaultPayment.trim(),
      invoicePreference: form.invoicePreference.trim(),
      minConsumption: form.minConsumption.trim(),
      dataTrainingPolicy: form.dataTrainingPolicy.trim(),
      logRetentionPolicy: form.logRetentionPolicy.trim(),
      defaultSla: form.defaultSla.trim(),
    });
    account.value = updated;
    syncForm(updated);
    ElMessage.success("已保存并留痕");
    // 回读一次拿服务端权威值（含企业证明），见 refreshAccount 注释。
    void refreshAccount();
    void loadLogs();
  } catch (value) {
    ElMessage.error(errorMessage(value, "保存失败，请稍后重试。"));
  } finally {
    saving.value = false;
  }
}

const formatTime = (value?: string) => formatDateTime(value, "—", true);
const payloadText = (value?: Record<string, unknown>) =>
  value && Object.keys(value).length ? JSON.stringify(value, null, 2) : "{}";

onMounted(loadAccount);
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">TOKEN OPS / PORTAL</div>
    <h1>供应商报价系统</h1>
    <p>{{ account?.name || "—" }} · {{ regionLabel }}</p>
  </section>

  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
    <el-button text @click="loadAccount">重新加载</el-button>
  </el-alert>

      <section v-loading="loading" class="panel account-card">
        <div class="account-card__head"><h2>账户设置</h2></div>

        <div class="account-form">
          <label class="account-field">
            <span class="account-field__label">企业名称</span>
            <el-input v-model="form.name" maxlength="128" placeholder="请输入企业名称" />
          </label>

          <label class="account-field">
            <span class="account-field__label">{{ registrationLabel }}</span>
            <el-input v-model="form.registrationNo" maxlength="64" placeholder="请输入注册编号" />
          </label>

          <label class="account-field">
            <span class="account-field__label">对接负责人</span>
            <el-input v-model="form.contact" maxlength="64" placeholder="请输入对接负责人" />
          </label>

          <label class="account-field">
            <span class="account-field__label">手机号</span>
            <el-input :model-value="account?.phone || ''" disabled aria-label="登录手机号（不可修改）" />
          </label>

          <label class="account-field">
            <span class="account-field__label">联系人职务</span>
            <el-input v-model="form.contactTitle" maxlength="64" />
          </label>

          <label class="account-field">
            <span class="account-field__label">联系人邮箱</span>
            <el-input v-model="form.contactEmail" maxlength="128" />
          </label>

          <div class="account-field account-field--license">
            <span class="account-field__label">企业证明</span>
            <div class="license-row">
              <span class="license-name">{{ licenseName || "未选择文件" }}</span>
              <el-upload
                accept=".pdf,.png,.jpg,.jpeg,.txt"
                :auto-upload="false"
                :show-file-list="false"
                :on-change="chooseLicense"
              >
                <el-button size="small">选择文件</el-button>
              </el-upload>
            </div>
          </div>
        </div>

        <!-- 主体默认商务与合规条款：原型里是独立一块，全部可选自由文本，
             占位文案只是提示，留空即「未填写」（示例不入库）。 -->
        <div class="account-section">
          <h3 class="account-section__title">主体默认商务与合规条款</h3>
          <div class="account-form account-form--terms">
            <label class="account-field">
              <span class="account-field__label">默认账期</span>
              <el-input v-model="form.defaultSettlement" maxlength="128" placeholder="预付 / 月结 / T+30" />
            </label>
            <label class="account-field">
              <span class="account-field__label">默认付款方式</span>
              <el-input v-model="form.defaultPayment" maxlength="128" placeholder="银行转账 / 在线支付 / 其他" />
            </label>
            <label class="account-field">
              <span class="account-field__label">发票偏好</span>
              <el-input v-model="form.invoicePreference" maxlength="128" placeholder="专用发票 / 普通发票 / 其他" />
            </label>
            <label class="account-field">
              <span class="account-field__label">最低消费</span>
              <el-input v-model="form.minConsumption" maxlength="128" placeholder="无 / 金额 + 币种 + 周期" />
            </label>
            <label class="account-field">
              <span class="account-field__label">数据训练政策</span>
              <el-input v-model="form.dataTrainingPolicy" maxlength="128" placeholder="不用于训练 / 经授权使用 / 按协议约定" />
            </label>
            <label class="account-field">
              <span class="account-field__label">日志留存与删除策略</span>
              <el-input v-model="form.logRetentionPolicy" maxlength="128" placeholder="不留存 / 留存 X 天 / 按协议删除" />
            </label>
            <label class="account-field">
              <span class="account-field__label">默认 SLA</span>
              <el-input v-model="form.defaultSla" maxlength="128" placeholder="99.9% / 按合同约定" />
            </label>
          </div>
        </div>

        <div class="account-actions">
          <el-button type="primary" size="small" :loading="saving" :disabled="!account" @click="save">保存账户设置</el-button>
        </div>
      </section>

  <el-dialog :model-value="Boolean(detail)" :title="detail?.action || '操作详情'" width="min(560px, 94vw)" @close="detail = null">
    <template v-if="detail">
      <p class="detail-meta">{{ detail.operator }} · {{ formatTime(detail.operatedAt) }}</p>
      <pre class="detail-payload">{{ payloadText(detail.payload) }}</pre>
    </template>
    <template #footer>
      <el-button @click="detail = null">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-tabs :deep(.el-tabs__header) { margin-bottom: 16px; }

.account-card { padding: 0; overflow: hidden; }
.account-card__head { padding: 18px 22px; border-bottom: 1px solid var(--app-border); }
.account-card__head h2 { margin: 0; font-size: 17px; }

.account-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 22px; padding: 22px 22px 6px; max-width: 760px; }
.account-field { display: flex; flex-direction: column; gap: 8px; }
.account-field__label { color: var(--app-text-muted); font-size: 12px; }
/* 企业证明独占一行（原型如此）。 */
.account-field--license { grid-column: 1 / -1; max-width: 520px; }
.license-row { display: flex; align-items: center; gap: 12px; min-height: 32px; }
.license-name { color: var(--app-text); font-size: 13px; word-break: break-all; }

.account-actions { padding: 18px 22px 22px; }

/* 主体默认商务与合规条款：与基础资料同一张卡里，用分隔线分成两块（原型如此）。 */
.account-section { border-top: 1px solid var(--app-border); margin-top: 8px; }
.account-section__title { margin: 18px 22px 0; font-size: 15px; font-weight: 500; }
.account-form--terms { padding-top: 14px; }

.account-logs { padding: 0; overflow: hidden; }
.account-logs__table { padding-top: 6px; }
.account-logs__total { margin: 0; padding: 12px 22px 18px; color: var(--app-text-muted); font-size: 12px; }


.detail-meta { margin: 0 0 12px; color: var(--app-text-muted); font-size: 12px; }
.detail-payload { margin: 0; padding: 12px; border-radius: 6px; background: var(--app-bg); font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all; }

@media (max-width: 760px) {
  .account-form { grid-template-columns: 1fr; }
  .account-field--license { max-width: none; }
}
</style>
