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
const statusLabel = (status: string) => status === "PENDING_APPROVAL" ? "待审批" : status === "PROCESSING" ? "办理中" : status;
const formatTime = (value?: string | null) => formatDateTime(value, "—", false);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [list, staff] = await Promise.all([supplyChainApi.approvals(), workbenchApi.people()]);
    rows.value = list;
    people.value = staff;
  } catch (value) {
    error.value = value instanceof Error ? value.message : "审批列表加载失败，请确认当前账号有审批权。";
  } finally {
    loading.value = false;
  }
}

function openApprove(row: ApprovalRow) {
  current.value = row;
  const self = session.user?.staffId || people.value[0]?.id || 0;
  testAssignee.value = self;
  contractAssignee.value = self;
  reason.value = "同意接入";
  dialogVisible.value = true;
}

async function approve() {
  if (!current.value || busy.value) return;
  if (!testAssignee.value) { ElMessage.error("请选择测试与资源配置办理人。"); return; }
  busy.value = true;
  try {
    await supplyChainApi.approve(current.value.id, {
      test_assignee_id: testAssignee.value,
      contract_assignee_id: contractAssignee.value || undefined,
      reason: reason.value.trim() || "同意接入",
    });
    ElMessage.success("已通过，办理人可以在审批中心执行模拟测试与接入");
    dialogVisible.value = false;
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "审批失败，请重试。");
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
  <section class="page-heading">
    <div>
      <div class="eyebrow">INTERNAL / APPROVALS</div>
      <h1>审批中心</h1>
      <p>供应商申报的整批审批；任一领导通过即可，办理人随后完成测试与登记接入。</p>
    </div>
    <el-button :loading="loading" @click="load">刷新</el-button>
  </section>

  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

  <section class="panel approval-panel">
    <el-table v-loading="loading" :data="rows" style="width: 100%">
      <el-table-column prop="supplier_name" label="供应商" min-width="160" />
      <el-table-column label="区域" width="90">
        <template #default="{ row }">{{ regionLabel(row.region) }}</template>
      </el-table-column>
      <el-table-column prop="row_count" label="模型数" width="90" />
      <el-table-column label="首个模型" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.first_model" type="warning" size="small">是</el-tag>
          <span v-else class="muted">否</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'PENDING_APPROVAL' ? 'warning' : 'success'" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="提交时间" width="180">
        <template #default="{ row }">{{ formatTime(row.submitted_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button v-if="row.status === 'PENDING_APPROVAL'" type="primary" size="small" @click="openApprove(row)">审批</el-button>
          <el-button v-else-if="row.status === 'PROCESSING'" size="small" :loading="simulatingId === row.id" @click="simulate(row)">模拟测试并接入</el-button>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <template #empty><el-empty description="暂无待审批/办理中的申报" :image-size="64" /></template>
    </el-table>
  </section>

  <el-dialog v-model="dialogVisible" title="通过申报并分配办理人" width="520px">
    <template v-if="current">
      <p class="dialog-summary">{{ current.supplier_name }} · {{ regionLabel(current.region) }} · {{ current.row_count }} 个模型</p>
      <el-form label-position="top">
        <el-form-item label="测试与资源配置办理人 *">
          <el-select v-model="testAssignee" filterable style="width: 100%">
            <el-option v-for="person in people" :key="person.id" :value="person.id" :label="person.name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="current.first_model" label="合同与授信办理人">
          <el-select v-model="contractAssignee" filterable style="width: 100%">
            <el-option :value="0" label="暂不分配" />
            <el-option v-for="person in people" :key="person.id" :value="person.id" :label="person.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="审批意见">
          <el-input v-model="reason" type="textarea" :rows="3" maxlength="200" show-word-limit />
        </el-form-item>
      </el-form>
      <p class="dialog-hint">通过后由办理人在列表点"模拟测试并接入"；测试为 mock，仅用于打通链路。</p>
    </template>
    <template #footer>
      <el-button :disabled="busy" @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="busy" @click="approve">通过并分配</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.approval-panel { padding: 18px 20px; }
.muted { color: var(--app-text-muted); font-size: 12px; }
.dialog-summary { margin: 0 0 14px; color: var(--app-text-secondary); }
.dialog-hint { margin: 8px 0 0; color: var(--app-text-muted); font-size: 12px; }
</style>
