<script setup lang="ts">
import { onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { supplyChainApi, type ApprovalRow } from "../../../api/supplyChain";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";
import { formatDateTime } from "../../../domain/date";
import { useSessionStore } from "../../../stores/session";

const session = useSessionStore();
const rows = ref<ApprovalRow[]>([]);
const people = ref<WorkbenchPersonDTO[]>([]);
const loading = ref(false);
const error = ref("");
const dialogVisible = ref(false);
const current = ref<ApprovalRow>();
const testAssignee = ref(0);
const contractAssignee = ref(0);
const reason = ref("同意接入");
const busy = ref(false);
const simulatingId = ref(0);

const regionLabel = (region: string) => region === "OVERSEAS" ? "海外" : "国内";
const statusLabel = (status: string) => {
  const labels: Record<string, string> = {
    PENDING_TEST: "等待测试补齐",
    PENDING_APPROVAL: "待接入",
    PROCESSING: "办理中",
    APPROVED: "已通过",
    REJECTED: "不通过",
  };
  return labels[status] || status;
};
const formatTime = (value?: string | null) => formatDateTime(value, "—", false);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [list, staff] = await Promise.all([supplyChainApi.approvals(), workbenchApi.people()]);
    rows.value = list;
    people.value = staff;
  } catch (value) {
    error.value = value instanceof Error ? value.message : "待审批列表加载失败；请确认当前账号有审批权。";
  } finally {
    loading.value = false;
  }
}

function openReject(row: ApprovalRow) {
  current.value = row;
  reason.value = "";
  dialogVisible.value = true;
}

async function rejectCurrent() {
  if (!current.value || busy.value) return;
  if (!reason.value.trim()) { ElMessage.error("请填写驳回原因。"); return; }
  busy.value = true;
  try {
    await supplyChainApi.reject(current.value.id, Number(current.value.revision || 0), reason.value);
    ElMessage.success("已驳回，供应商可修改后重提");
    dialogVisible.value = false;
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "驳回失败，请重试。");
  } finally {
    busy.value = false;
  }
}

async function simulate(row: ApprovalRow) {
  if (simulatingId.value) return;
  simulatingId.value = row.id;
  try {
    const result = await supplyChainApi.simulate(row.id);
    ElMessage.success(`资源 ${result.resource_id} 已登记接入（${result.state}）`);
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "模拟测试/接入失败；请确认当前账号是测试办理人。");
  } finally {
    simulatingId.value = 0;
  }
}

onMounted(load);
</script>

<template>
  <div class="workbench-approvals">
    <div class="approval-toolbar">
      <span class="muted">供应商新增模型/价格变更的整批审批；通过后由办理人模拟测试并接入。</span>
      <el-button size="small" :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-table v-loading="loading" :data="rows" style="width: 100%">
      <el-table-column prop="supplier_name" label="供应商" min-width="150" />
      <el-table-column label="区域" width="80">
        <template #default="{ row }">{{ regionLabel(row.region) }}</template>
      </el-table-column>
      <el-table-column prop="row_count" label="模型数" width="80" />
      <el-table-column label="首个模型" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.first_model" type="warning" size="small">是</el-tag>
          <span v-else class="muted">否</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'PENDING_APPROVAL' ? 'warning' : 'success'" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="提交时间" width="170">
        <template #default="{ row }">{{ formatTime(row.submitted_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="170">
        <template #default="{ row }">
          <el-button v-if="row.status !== 'REJECTED'" type="danger" plain size="small" @click="openReject(row)">驳回</el-button>
          <span v-else class="muted">已驳回</span>
        </template>
      </el-table-column>
      <template #empty><el-empty description="暂无待审批/办理中的申报" :image-size="64" /></template>
    </el-table>

    <el-dialog v-model="dialogVisible" title="驳回申报" width="520px">
      <template v-if="current">
        <p class="dialog-summary">{{ current.supplier_name }} · {{ regionLabel(current.region) }} · {{ current.row_count }} 个模型</p>
        <el-form label-position="top">
          <el-form-item label="驳回原因 *">
            <el-input v-model="reason" type="textarea" :rows="3" maxlength="200" show-word-limit placeholder="例如：连通测试未通过，请检查测试地址后重试" />
          </el-form-item>
        </el-form>
        <p class="dialog-hint">驳回后申报状态为「不通过」，供应商可修改后重提。</p>
      </template>
      <template #footer>
        <el-button :disabled="busy" @click="dialogVisible = false">取消</el-button>
        <el-button type="danger" :loading="busy" @click="rejectCurrent">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.workbench-approvals { display: grid; gap: 10px; }
.approval-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.dialog-summary { margin: 0 0 14px; color: var(--app-text-secondary); }
.dialog-hint { margin: 8px 0 0; color: var(--app-text-muted); font-size: 12px; }
</style>
