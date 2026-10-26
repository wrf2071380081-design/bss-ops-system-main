<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { supplierSubmissionsApi } from "../../api/supplierSubmissions";
import type { SupplierSubmissionDTO } from "../../api/supplierSubmissions.types";
import { useSessionStore } from "../../stores/session";

const router = useRouter();
const route = useRoute();
const session = useSessionStore();
const activeTab = ref(route.query.tab === "history" ? "history" : "start");
const submissions = ref<SupplierSubmissionDTO[]>([]);
const loading = ref(false);
const error = ref("");
const supplierName = computed(() => session.user?.supplierName || "供应商");
const regionLabel = computed(() => session.user?.supplierRegion === "OVERSEAS" ? "海外主体" : "国内主体");
const drafts = computed(() => submissions.value.filter(item => item.status === "DRAFT"));
const history = computed(() => submissions.value.filter(item => item.status !== "DRAFT"));
let active = true;
let request: AbortController | undefined;

function newModel() { void router.push({ path: "/supplier/submissions/new", query: { kind: "MODEL_NEW" } }); }
function editDraft(item: SupplierSubmissionDTO) { void router.push({ path: "/supplier/submissions/new", query: { kind: item.kind, id: item.id } }); }
function onTabChange(tabName: string | number) { if (tabName !== "start") void load(); }
function statusLabel(value: string) {
  const labels: Record<string, string> = {
    DRAFT: "草稿",
    PENDING_TEST: "等待测试补齐",
    PENDING_APPROVAL: "待接入",
    PROCESSING: "办理中",
    APPROVED: "通过",
    REJECTED: "不通过",
    WITHDRAWN: "已撤回",
  };
  return labels[value] || value;
}
async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const result = await supplierSubmissionsApi.list({ page: 1, size: 100 }, current.signal);
    if (active && !current.signal.aborted) submissions.value = result.list;
  } catch (value) {
    if (active && !current.signal.aborted) { submissions.value = []; error.value = value instanceof Error ? value.message : "申报记录暂时无法加载。"; }
  } finally { if (active && !current.signal.aborted) loading.value = false; }
}
async function withdrawSubmission(item: SupplierSubmissionDTO) {
  if (!item.permissions.can_withdraw) return;
  try {
    const { value } = await ElMessageBox.prompt("撤回后将按申报流程记录状态；请输入撤回原因。", "撤回申报", { inputPlaceholder: "撤回原因", inputValidator: value => Boolean(value?.trim()) || "请填写撤回原因", confirmButtonText: "确认撤回", cancelButtonText: "返回", type: "warning" });
    await supplierSubmissionsApi.withdraw(item.id, item.revision, value, crypto.randomUUID());
    ElMessage.success("申报已撤回");
    await load();
  } catch (value) {
    if (value === "cancel" || value === "close") return;
    error.value = value instanceof Error ? value.message : "撤回未完成，请刷新申报记录后重试。";
  }
}
watch(() => route.query.tab, value => { activeTab.value = value === "history" ? "history" : value === "drafts" ? "drafts" : "start"; if (activeTab.value !== "start") void load(); });
onMounted(() => { if (activeTab.value !== "start") void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">TOKEN OPS / PORTAL</div>
    <h1>供应商报价系统</h1>
    <p>{{ supplierName }} · {{ regionLabel }}</p>
  </section>

  <el-tabs v-model="activeTab" class="supplier-tabs" @tab-change="onTabChange">
    <el-tab-pane label="发起申报" name="start">
      <section class="panel supplier-start">
        <div class="supplier-start__head"><h2>发起申报</h2></div>
        <div class="supplier-start__body">
          <p>一份模型新增申报可包含多个模型；价格变更请从“模型管理”选择对应资源发起。</p>
          <div class="supplier-start__actions"><el-button type="primary" @click="newModel">模型新增</el-button><el-button @click="activeTab = 'drafts'">查看草稿</el-button></div>
        </div>
      </section>
    </el-tab-pane>

    <el-tab-pane label="草稿" name="drafts">
      <section class="panel supplier-list-panel">
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
        <el-table v-loading="loading" :data="drafts" row-key="id" empty-text="暂无草稿">
          <el-table-column label="申报内容" min-width="240"><template #default="{ row }"><strong>{{ row.rows.length }} 个模型</strong><div class="muted">{{ row.reason || '尚未填写说明' }}</div></template></el-table-column>
          <el-table-column label="最近保存" prop="updated_at" min-width="170" />
          <el-table-column label="操作" width="120"><template #default="{ row }"><el-button link type="primary" :disabled="!row.permissions.can_edit" @click="editDraft(row)">继续编辑</el-button></template></el-table-column>
        </el-table>
      </section>
    </el-tab-pane>

    <el-tab-pane label="申报记录" name="history">
      <section class="panel supplier-list-panel">
        <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
        <el-table v-loading="loading" :data="history" row-key="id" empty-text="暂无申报记录">
          <el-table-column label="申报内容" min-width="280">
            <template #default="{ row }">
              <strong>{{ (row.model_names?.length ? row.model_names.join('、') : `${row.rows.length} 个模型`) }}</strong>
              <div class="muted">共 {{ row.row_count || row.rows.length }} 个模型 · {{ row.kind === 'PRICE_CHANGE' ? '价格变更' : '模型新增' }} · {{ row.submission_no || row.id }}</div>
            </template>
          </el-table-column>
          <el-table-column label="提交人" width="120"><template #default="{ row }">{{ row.submitter_name || '—' }}</template></el-table-column>
          <el-table-column label="提交时间" prop="submitted_at" min-width="170"><template #default="{ row }">{{ row.submitted_at || row.updated_at }}</template></el-table-column>
          <el-table-column label="当前状态" width="140">
            <template #default="{ row }">
              <el-tag :type="row.status === 'REJECTED' ? 'danger' : row.status === 'APPROVED' ? 'success' : row.status === 'PENDING_TEST' ? 'warning' : undefined">{{ statusLabel(row.status) }}</el-tag>
              <div v-if="row.pending_row_count" class="muted">待补 {{ row.pending_row_count }} 个</div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150">
            <template #default="{ row }">
              <el-button v-if="row.permissions.can_edit" link type="primary" @click="editDraft(row)">{{ row.status === 'REJECTED' ? '修改重提' : '继续编辑' }}</el-button>
              <el-button v-if="row.permissions.can_withdraw" link type="danger" @click="withdrawSubmission(row)">撤回</el-button>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </el-tab-pane>
  </el-tabs>

  <p class="supplier-footnote">Token Ops · 供应商申报门户</p>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-tabs :deep(.el-tabs__header) { margin-bottom: 16px; }
.supplier-start { padding: 0; overflow: hidden; }
.supplier-start__head { padding: 18px 22px; border-bottom: 1px solid var(--app-border); }
.supplier-start__head h2 { margin: 0; font-size: 17px; }
.supplier-start__body { padding: 20px 22px 22px; }
.supplier-start__body p { margin: 0 0 16px; color: var(--app-text-muted); font-size: 13px; }
.supplier-start__actions { display: flex; gap: 10px; }
.supplier-list-panel { padding: 18px; }
.muted { margin-top: 4px; color: var(--app-text-muted); font-size: 12px; }
.supplier-footnote { margin: 26px 0 0; color: var(--app-text-muted); font-size: 11px; }
</style>
