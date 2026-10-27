<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import { supplierTestCredentialApi } from "../../api/supplierTestCredential";
import type { SupplierSubmissionDTO, SupplierSubmissionTestDTO } from "../../api/supplierSubmissions.types";

/**
 * 「查看申报」只读弹窗（原型 2026-10-27 版）。
 *
 * 原型里这一屏还有「历史版本」「申报操作记录」两块，但库里 submission_version
 * 表从来没有写入、审批侧也没有事件表——这两块留到后续单独做，本次只给
 * 模型明细 + 说明 + 审核状态，避免用当前值冒充历史快照。
 */
const props = defineProps<{ modelValue: boolean; submissionId: string }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();

const loading = ref(false);
const error = ref("");
const detail = ref<SupplierSubmissionDTO | null>(null);
const credentialUrl = ref("");
const hasCredentialKey = ref(false);

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit("update:modelValue", value),
});

const kindLabel = computed(() => (detail.value?.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增"));
const submitterLabel = computed(() => detail.value?.submitter_name || "—");
/** 原型的时间写法：2026/10/27 16:32:31。 */
function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
const submittedAt = computed(() => formatTime(detail.value?.submitted_at || detail.value?.updated_at));
const reasonText = computed(() => detail.value?.reason?.trim() || "无补充说明");

const STATUS_LABELS: Record<string, string> = {
  DRAFT: "草稿",
  PENDING_TEST: "等待测试补齐",
  PENDING_APPROVAL: "待审批",
  PROCESSING: "办理中",
  APPROVED: "通过",
  REJECTED: "不通过",
  WITHDRAWN: "已撤回",
};
function statusLabel(value?: string) { return value ? STATUS_LABELS[value] || value : "—"; }

function testText(job?: SupplierSubmissionTestDTO) {
  if (!job) return "未完成";
  if (job.stale) return "已失效";
  if (!job.tested) return job.status === "RUNNING" || job.status === "QUEUED" ? "测试中" : "未完成";
  return job.passed ? "完成 · 通过" : job.verdict ? "完成 · 未通过" : "完成";
}
function testType(job?: SupplierSubmissionTestDTO): "success" | "danger" | "info" | "warning" {
  if (!job || job.stale) return "info";
  if (!job.tested) return "info";
  if (job.passed) return "success";
  // 压测没有判定（verdict 为空）：只算「完成」，用中性色，不要染红。
  if (!job.verdict) return "info";
  return "danger";
}
function connectivityMetrics(job?: SupplierSubmissionTestDTO) {
  return job?.connection_ms ? `${job.connection_ms} ms` : "";
}
function stressMetrics(job?: SupplierSubmissionTestDTO) {
  const parts: string[] = [];
  if (job?.rps) parts.push(`RPS ${job.rps}`);
  if (job?.tps) parts.push(`TPS ${job.tps}`);
  return parts.join(" · ");
}
function reportText(job?: SupplierSubmissionTestDTO) {
  return job?.report_id ? `报告 ${job.report_id}` : "暂无报告附件";
}

async function load() {
  if (!props.submissionId) return;
  loading.value = true;
  error.value = "";
  try {
    const [record, credential] = await Promise.all([
      supplierSubmissionsApi.get(props.submissionId),
      supplierTestCredentialApi.get().catch(() => null),
    ]);
    detail.value = record;
    credentialUrl.value = credential?.base_url || "";
    hasCredentialKey.value = !!credential?.has_api_key;
  } catch (value) {
    error.value = value instanceof Error ? value.message : "申报详情暂时无法加载。";
  } finally {
    loading.value = false;
  }
}

watch(() => [props.modelValue, props.submissionId], ([open]) => {
  if (open) { detail.value = null; void load(); }
}, { immediate: true });
</script>

<template>
  <el-dialog v-model="visible" title="查看申报" width="880px" :close-on-click-modal="false">
    <div v-loading="loading">
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <template v-if="detail">
        <p class="detail-head">
          <strong>{{ detail.submission_no || detail.id }}</strong> · {{ kindLabel }} · {{ statusLabel(detail.status) }}
        </p>
        <p class="detail-meta">提交时间：{{ submittedAt }} · 提交人：{{ submitterLabel }}</p>

        <el-table :data="detail.rows" size="small" class="detail-table">
          <el-table-column label="对应标准模型" min-width="150">
            <template #default="{ row }">
              {{ row.standard_model_name || row.standard_model_id }}
              <small v-if="row.official_id" class="muted">{{ row.official_id }}</small>
            </template>
          </el-table-column>
          <el-table-column label="接口模型 ID" prop="interface_model_id" min-width="150" />
          <el-table-column label="申报供货报价系数" prop="coefficient" width="130" />
          <el-table-column label="连通性测试" min-width="130">
            <template #default="{ row }">
              <el-tag size="small" :type="testType(row.connectivity_job)">{{ testText(row.connectivity_job) }}</el-tag>
              <small v-if="connectivityMetrics(row.connectivity_job)" class="muted">{{ connectivityMetrics(row.connectivity_job) }}</small>
            </template>
          </el-table-column>
          <el-table-column label="压力测试" min-width="150">
            <template #default="{ row }">
              <el-tag size="small" :type="testType(row.stress_job)">{{ row.participate_test || row.stress_job ? testText(row.stress_job) : "未选择" }}</el-tag>
              <small v-if="stressMetrics(row.stress_job)" class="muted">{{ stressMetrics(row.stress_job) }}</small>
            </template>
          </el-table-column>
          <el-table-column label="已有报告" min-width="120">
            <template #default="{ row }">
              <small class="muted">{{ reportText(row.stress_job || row.connectivity_job) }}</small>
            </template>
          </el-table-column>
        </el-table>

        <div class="detail-body">
          <p class="muted">供货报价系数沿用既有计价口径。<template v-if="credentialUrl">测试接口：{{ credentialUrl }}；测试 Key：{{ hasCredentialKey ? '••••••••' : '未登记' }}。</template></p>
          <p>申报说明：{{ reasonText }}</p>
          <p>审核状态：{{ statusLabel(detail.status) }}</p>
          <p v-if="detail.status === 'APPROVED' || detail.status === 'REJECTED'">审核结果详见办理记录。</p>
          <p v-else class="muted">本次尚无审核结果。</p>
        </div>
      </template>
    </div>
    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.detail-head { margin: 0 0 4px; font-size: 14px; }
.detail-meta { margin: 0 0 14px; color: var(--app-text-muted); font-size: 12px; }
.detail-table { width: 100%; }
.detail-table .muted { display: block; margin-top: 2px; }
.detail-body { margin-top: 14px; display: grid; gap: 6px; font-size: 13px; }
.detail-body p { margin: 0; line-height: 1.7; }
.muted { color: var(--app-text-muted); font-size: 12px; }
</style>
