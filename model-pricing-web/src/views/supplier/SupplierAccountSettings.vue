<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, type UploadFile } from "element-plus";
import { supplierAccountApi } from "../../api/supplierAccount";
import type {
  SupplierAccountDTO,
  SupplierOperationLogDTO,
  SupplierRegion,
} from "../../api/supplierAccount.types";
import { ApiError } from "../../domain/common";
import { formatDateTime } from "../../domain/date";
import { registrationLabelOf, regionFieldLabelOf } from "../../api/supplierAccount.contract";

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

const form = reactive<{ name: string; registrationNo: string; contact: string; region: SupplierRegion }>({
  name: "",
  registrationNo: "",
  contact: "",
  region: "CN",
});

const REGION_OPTIONS: { value: SupplierRegion; label: string }[] = [
  { value: "CN", label: "国内" },
  { value: "OVERSEAS", label: "海外" },
];

/** 页头副标题：主体名 · 国内/海外主体（原型「澄海智算（演示） · 国内主体」）。 */
const regionLabel = computed(() => (account.value?.region === "OVERSEAS" ? "海外主体" : "国内主体"));

/** 注册编号的 label 由**已保存**的 region 决定，不跟下拉即时联动（原型实测如此，见测试用例 B05）。 */
const registrationLabel = computed(() => registrationLabelOf(account.value?.region || "CN"));

/** 主体属性 label：锁定时追加原因，如「主体属性（有上架历史，已锁定）」。 */
const regionFieldLabel = computed(() =>
  regionFieldLabelOf({ regionLocked: Boolean(account.value?.regionLocked), regionLockReason: account.value?.regionLockReason }),
);

const licenseName = computed(() => pendingFile.value?.name || account.value?.license?.name || "");

/** 开发态 + mock 时提供演示主体切换入口，方便肉眼核对两种状态；生产构建不会渲染。 */
const canSwitchMockVariant = import.meta.env.DEV && import.meta.env.VITE_MOCK_ENABLED === "true";
const mockVariant = ref<"locked" | "unlocked">(
  typeof localStorage !== "undefined" && localStorage.getItem("msw:account-variant") === "unlocked"
    ? "unlocked"
    : "locked",
);

const errorMessage = (value: unknown, fallback = "操作失败，请稍后重试。") =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : fallback;

function syncForm(value: SupplierAccountDTO) {
  form.name = value.name;
  form.registrationNo = value.registrationNo;
  form.contact = value.contact;
  form.region = value.region;
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
      region: form.region,
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

function switchMockVariant() {
  const next = mockVariant.value === "locked" ? "unlocked" : "locked";
  localStorage.setItem("msw:account-variant", next);
  mockVariant.value = next;
  void loadAccount();
  void loadLogs();
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

  <el-tabs v-model="activeTab" class="supplier-tabs" @tab-change="onTabChange">
    <el-tab-pane label="账户资料" name="profile">
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
            <span class="account-field__label">{{ regionFieldLabel }}</span>
            <el-select v-model="form.region" :disabled="Boolean(account?.regionLocked)" style="width: 100%">
              <el-option v-for="option in REGION_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
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

        <p class="account-note">主体属性变更前须无资源、无申报；更换法律主体请另行注册。</p>

        <div class="account-actions">
          <el-button type="primary" size="small" :loading="saving" :disabled="!account" @click="save">保存资料</el-button>
        </div>
      </section>
    </el-tab-pane>

    <el-tab-pane label="操作记录" name="logs">
      <section class="panel account-logs">
        <div class="account-card__head"><h2>操作记录</h2></div>
        <el-alert v-if="logsError" :title="logsError" type="error" show-icon :closable="false">
          <el-button text @click="loadLogs">重新加载</el-button>
        </el-alert>
        <el-table
          v-loading="logsLoading"
          :data="logs"
          row-key="id"
          empty-text="暂无操作记录"
          class="account-logs__table"
        >
          <el-table-column label="操作" prop="action" min-width="170" sortable />
          <el-table-column label="对象" prop="object" min-width="260" show-overflow-tooltip />
          <el-table-column label="操作人" prop="operator" width="120" sortable />
          <el-table-column label="时间" prop="operatedAt" min-width="180" sortable>
            <template #default="{ row }">{{ formatTime(row.operatedAt) }}</template>
          </el-table-column>
          <el-table-column label="详情" width="90">
            <template #default="{ row }">
              <el-button link type="primary" @click="detail = row">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        <p class="account-logs__total">共 {{ logsTotal }} 条</p>
      </section>
    </el-tab-pane>
  </el-tabs>

  <p class="supplier-footnote">
    Token Ops · 主体池迭代
    <button v-if="canSwitchMockVariant" type="button" class="mock-switch" @click="switchMockVariant">
      演示数据：切换为{{ mockVariant === "locked" ? "未上架主体" : "已上架主体" }}
    </button>
  </p>

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

.account-note { margin: 10px 22px 0; color: var(--app-text-muted); font-size: 12px; }
.account-actions { padding: 18px 22px 22px; }

.account-logs { padding: 0; overflow: hidden; }
.account-logs__table { padding-top: 6px; }
.account-logs__total { margin: 0; padding: 12px 22px 18px; color: var(--app-text-muted); font-size: 12px; }

.supplier-footnote { margin: 26px 0 0; color: var(--app-text-muted); font-size: 11px; }
.mock-switch { margin-left: 10px; padding: 0; border: 0; background: transparent; color: var(--el-color-primary); font-size: 11px; cursor: pointer; text-decoration: underline; }

.detail-meta { margin: 0 0 12px; color: var(--app-text-muted); font-size: 12px; }
.detail-payload { margin: 0; padding: 12px; border-radius: 6px; background: var(--app-bg); font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all; }

@media (max-width: 760px) {
  .account-form { grid-template-columns: 1fr; }
  .account-field--license { max-width: none; }
}
</style>
