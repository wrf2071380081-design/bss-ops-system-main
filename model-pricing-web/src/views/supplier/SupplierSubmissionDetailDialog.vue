<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import { supplierTestCredentialApi } from "../../api/supplierTestCredential";
import type { SupplierSubmissionDTO, SupplierSubmissionRowDTO, SupplierSubmissionTestDTO } from "../../api/supplierSubmissions.types";

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
/** 已有审批决定的行（PENDING 视为"还没决定"）。 */
const decidedRows = computed(() => (detail.value?.rows || []).filter(row => row.decision && row.decision !== "PENDING"));

/** 本次申报「配置组 → 模型」的关系（原型下方的表格）。 */
const configGroups = computed(() => {
  const map = new Map<string, { name: string; url: string; hasKey: boolean; enabled: boolean; models: string[] }>();
  for (const row of detail.value?.rows || []) {
    const id = row.test_config_group_id || "";
    if (!map.has(id)) {
      map.set(id, {
        name: row.test_config_group_name || "未分组",
        url: row.test_base_url || "",
        hasKey: Boolean(row.test_api_key_configured),
        enabled: row.test_config_group_enabled !== false,
        models: [],
      });
    }
    const label = row.standard_model_name || row.interface_model_id;
    if (label) map.get(id)!.models.push(label);
  }
  return [...map.values()];
});

/** 供应商查看自己的测试报告：直接列出该行连通/压测的平台结论与报告编号。 */
const reportOpen = ref(false), reportRow = ref<SupplierSubmissionRowDTO | null>(null);
function openReportHint(row: SupplierSubmissionRowDTO) {
  reportRow.value = row;
  reportOpen.value = true;
}
function reportLines(row: SupplierSubmissionRowDTO | null) {
  if (!row) return [];
  const out: { name: string; status: string; verdict: string; extra: string; report: string }[] = [];
  const conn = row.connectivity_job;
  if (conn) out.push({
    name: "连通性测试", status: conn.status || "", verdict: conn.verdict || (conn.passed ? "pass" : ""),
    extra: conn.connection_ms ? `${conn.connection_ms} ms` : "", report: conn.report_id || "",
  });
  const stress = row.stress_job;
  if (stress) out.push({
    name: "压力测试", status: stress.status || "", verdict: stress.verdict || "",
    extra: [stress.rps ? `RPS ${stress.rps}` : "", stress.tps ? `TPS ${stress.tps}` : ""].filter(Boolean).join(" · "),
    report: stress.report_id || "",
  });
  return out;
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
  // 有报告就算完成：压测终态平台不给 verdict，之前被误判成"未完成"（2026-11-23）。
  if (job.report_id) return job.passed ? "完成 · 通过" : "完成";
  if (job.stale) return "已失效";
  if (!job.tested) return job.status === "RUNNING" || job.status === "QUEUED" ? "测试中" : "未完成";
  return job.passed ? "完成 · 通过" : job.verdict ? "完成 · 未通过" : "完成";
}
/** 连通性单元格：平台任务优先（在跑=测试中，终态=结论），否则退回提交时的探测结果。 */
function connectivityTag(row: SupplierSubmissionRowDTO) {
  const job = row.connectivity_job;
  const status = (job?.status || "").toUpperCase();
  if (job && (status === "QUEUED" || status === "RUNNING")) {
    return { text: "测试中", type: "info" as const, note: status === "QUEUED" ? "排队中" : "平台执行中" };
  }
  if (job && job.tested) {
    // 平台对部分门类不给 verdict（如压测）：那是"完成（未判定）"，不能算未通过。
    const base = job.passed
      ? { text: "通过", type: "success" as const }
      : job.verdict
        ? { text: "未通过", type: "danger" as const }
        : { text: "已完成", type: "info" as const };
    // 结论照实显示，失效只作补充说明——用「已失效」顶掉结论会让供应商误读。
    // 不再展示"需重测/测试目标已变更"：交互原型没有这个状态，
    // 而且它会让供应商以为要反复重测（2026-11-23 反馈，第十次了，直接去掉）。
    const reason = job.failure ? `平台中止：${job.failure}` : "";
    return { ...base, note: [connectivityMetrics(job), reason].filter(Boolean).join(" · ") };
  }
  if (job && job.stale) return { text: "已完成", type: "info" as const, note: "" };
  if (row.probe_status === "PASS") return { text: "通过", type: "success" as const, note: "提交时探测" };
  if (row.probe_status === "FAIL") return { text: "未通过", type: "danger" as const, note: row.probe_note || "" };
  return { text: job ? testText(job) : "未测试", type: "info" as const, note: "" };
}

function testType(job?: SupplierSubmissionTestDTO): "success" | "danger" | "info" | "warning" {
  // 有报告就是"做完了"，用绿色；不要因为压测没有 verdict 就染成灰色。
  if (job?.report_id) return "success";
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
          <el-table-column label="连通性" min-width="150">
            <template #default="{ row }">
              <!-- 优先显示"提交时探测"的结果：这是供应商提交那一刻真实打通的证据；
                   没有探测记录（历史数据）时退回平台测试任务的状态。 -->
              <!-- 优先级：平台任务状态 > 提交时探测。
                   平台任务在跑时不能说"通过"——那只是本地探测的结果。 -->
              <el-tag size="small" :type="connectivityTag(row).type">{{ connectivityTag(row).text }}</el-tag>
              <small v-if="connectivityTag(row).note" class="muted" style="display:block">{{ connectivityTag(row).note }}</small>
            </template>
          </el-table-column>
          <el-table-column label="压力测试" min-width="150">
            <template #default="{ row }">
              <el-tag size="small" :type="testType(row.stress_job)">{{ row.participate_test || row.stress_job ? testText(row.stress_job) : "未选择" }}</el-tag>
              <small v-if="stressMetrics(row.stress_job)" class="muted">{{ stressMetrics(row.stress_job) }}</small>
            </template>
          </el-table-column>
          <el-table-column label="已有报告" min-width="130">
            <template #default="{ row }">
              <el-button v-if="row.stress_job?.report_id || row.connectivity_job?.report_id" link type="primary" @click="openReportHint(row)">
                测试与报告
              </el-button>
              <small v-else class="muted">{{ reportText(row.stress_job || row.connectivity_job) }}</small>
            </template>
          </el-table-column>
        </el-table>

        <div class="detail-body">
          <p class="muted">供货报价系数沿用既有计价口径。<template v-if="credentialUrl">测试接口：{{ credentialUrl }}；测试 Key：{{ hasCredentialKey ? '••••••••' : '未登记' }}。</template></p>
          <p>申报说明：{{ reasonText }}</p>
          <p>审核状态：{{ statusLabel(detail.status) }}</p>

          <!-- 审批结果（原型：中间一张表，模型 / 结果 / 原因） -->
          <h4 class="section-title">审批结果</h4>
          <el-table v-if="decidedRows.length" :data="decidedRows" size="small" class="decision-table">
            <el-table-column label="模型" min-width="180">
              <template #default="{ row }">{{ row.standard_model_name || row.interface_model_id }}</template>
            </el-table-column>
            <el-table-column label="结果" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.decision === 'APPROVED' ? 'success' : 'danger'">
                  {{ row.decision === 'APPROVED' ? '通过' : '不通过' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="原因" min-width="180">
              <template #default="{ row }">{{ row.decision === 'APPROVED' ? '—' : (detail.reason || '—') }}</template>
            </el-table-column>
          </el-table>
          <p v-else class="muted">本次尚无审核结果。</p>
          <p v-if="decidedRows.length" class="muted decision-foot">
            审批意见：{{ detail.reason || '—' }} · 审批人：{{ decidedRows[0]?.decided_by_name || '—' }} · 审批时间：{{ formatTime(decidedRows[0]?.decided_at) }}
          </p>

          <!-- 本次申报配置与模型关系（原型同款） -->
          <h4 class="section-title">本次申报配置与模型关系</h4>
          <el-table :data="configGroups" size="small" class="decision-table">
            <el-table-column label="配置组" min-width="160">
              <template #default="{ row }">{{ row.name }}<span v-if="!row.enabled" class="muted">（已停用）</span></template>
            </el-table-column>
            <el-table-column label="准入 URL" min-width="220">
              <template #default="{ row }"><span class="mono">{{ row.url || '未登记' }}</span></template>
            </el-table-column>
            <el-table-column label="API Key" width="110">
              <template #default="{ row }">{{ row.hasKey ? '已配置（脱敏）' : '未配置' }}</template>
            </el-table-column>
            <el-table-column label="关联模型" min-width="220">
              <template #default="{ row }">{{ row.models.join('、') }}</template>
            </el-table-column>
          </el-table>
        </div>
      </template>
    </div>
    <!-- 供应商自己的测试报告（连通/压测）：结论与报告编号，不涉及其他客户数据 -->
    <el-dialog v-model="reportOpen" title="测试与报告" width="min(680px, 94vw)" append-to-body>
      <p class="muted">{{ reportRow?.standard_model_name || reportRow?.interface_model_id }}</p>
      <el-table :data="reportLines(reportRow)" size="small" empty-text="暂无测试记录">
        <el-table-column label="测试类型" prop="name" width="120" />
        <el-table-column label="状态" prop="status" width="110" />
        <el-table-column label="结论" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.verdict === 'pass' ? 'success' : row.verdict ? 'danger' : 'info'">
              {{ row.verdict === "pass" ? "通过" : row.verdict ? "未通过" : "已完成" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="指标" prop="extra" min-width="150" />
        <el-table-column label="报告编号" min-width="110">
          <template #default="{ row }"><span class="mono">{{ row.report || "—" }}</span></template>
        </el-table-column>
      </el-table>
      <p class="muted">报告由测试平台出具；如需 PDF/Excel 原件，请联系对接的技术办理人。</p>
      <template #footer><el-button @click="reportOpen = false">关闭</el-button></template>
    </el-dialog>
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
