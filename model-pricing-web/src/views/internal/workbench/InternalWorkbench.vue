<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { formatDateTime } from "../../../domain/date";
import { ApiError } from "../../../domain/common";
import {
  workbenchApi,
  type InboxItemDTO,
  type InboxState,
  type OperationItemDTO,
  type WorkbenchPersonDTO,
} from "../../../modules/workbench/api/workbench";

const router = useRouter();

const activeTab = ref<"todo" | "operations">("todo");
const stateTab = ref<InboxState>("TODO");
const assigneeId = ref(0);
const region = ref("");
const keyword = ref("");
const page = ref(1);
const size = ref(20);
const total = ref(0);
const list = ref<InboxItemDTO[]>([]);
const people = ref<WorkbenchPersonDTO[]>([]);
const loading = ref(false);
const error = ref("");

const opPage = ref(1);
const opSize = ref(20);
const opTotal = ref(0);
const operations = ref<OperationItemDTO[]>([]);
const opLoading = ref(false);

const stateTabs: { value: InboxState; label: string }[] = [
  { value: "TODO", label: "待办" },
  { value: "READ", label: "待阅" },
  { value: "DONE", label: "已办" },
  { value: "READ_DONE", label: "已阅" },
];

const NOTICE_LABELS: Record<string, string> = {
  ONLINE_OFFLINE_PLAN: "上下架计划公告",
  RESOURCE_CHANGE: "资源变动公告",
  COST_CHANGE: "采购成本变化公告",
};
const NODE_LABELS: Record<string, string> = {
  PREP: "账户与接口准备",
  CONFIG: "资源配置",
  TEST: "模型测试",
  CONTRACT: "合同签署",
  CREDIT: "授信开具",
  ACCOUNT: "账户开通",
  EXECUTE: "执行操作",
};
const PLAN_LABELS: Record<string, string> = {
  ONLINE: "上架计划",
  OFFLINE: "下架计划",
  PRICE_CHANGE: "价格变更计划",
};
const isViewingOther = computed(() => assigneeId.value !== 0);
const displayTitle = (item: InboxItemDTO) => {
  if (item.biz_type === "NOTICE") return NOTICE_LABELS[item.title] || item.title;
  if (item.biz_type === "ONBOARDING_NODE") return NODE_LABELS[item.title] || item.title;
  if (item.biz_type === "PLAN") return PLAN_LABELS[item.title] || item.title;
  return item.title;
};
const stateLabel = (state: InboxState) => stateTabs.find(item => item.value === state)?.label || state;
const stateTagType = (state: InboxState) => state === "TODO" ? "warning" : state === "DONE" ? "success" : "info";
const formatTime = (value?: string | null) => formatDateTime(value, "—", false);
const errorMessage = (value: unknown) => value instanceof ApiError ? value.message : value instanceof Error ? value.message : "工作台加载失败，请稍后重试。";

async function loadPeople() {
  try {
    people.value = await workbenchApi.people();
  } catch {
    // 人员选择器是辅助能力，失败不影响本人收件箱。
    people.value = [];
  }
}

async function loadInbox() {
  loading.value = true;
  error.value = "";
  try {
    const res = await workbenchApi.inbox({
      state: stateTab.value,
      assigneeId: assigneeId.value || undefined,
      region: region.value || undefined,
      keyword: keyword.value || undefined,
      page: page.value,
      size: size.value,
    });
    list.value = res.list;
    total.value = res.total;
  } catch (value) {
    list.value = [];
    total.value = 0;
    error.value = errorMessage(value);
  } finally {
    loading.value = false;
  }
}

async function loadOperations() {
  opLoading.value = true;
  error.value = "";
  try {
    const res = await workbenchApi.operations({ page: opPage.value, size: opSize.value });
    operations.value = res.list;
    opTotal.value = res.total;
  } catch (value) {
    operations.value = [];
    opTotal.value = 0;
    error.value = errorMessage(value);
  } finally {
    opLoading.value = false;
  }
}

function resetAndLoad() {
  page.value = 1;
  void loadInbox();
}

function changeState(state: InboxState) {
  stateTab.value = state;
  resetAndLoad();
}

function changeAssignee() {
  page.value = 1;
  void loadInbox();
}

function changeRegion() {
  page.value = 1;
  void loadInbox();
}

function search() {
  page.value = 1;
  void loadInbox();
}

function refresh() {
  if (activeTab.value === "todo") void loadInbox();
  else void loadOperations();
}

async function markRead(item: InboxItemDTO) {
  if (!item.readable) return;
  try {
    await workbenchApi.markRead(item.biz_id);
    ElMessage.success("已标记为已阅");
    void loadInbox();
  } catch (value) {
    ElMessage.error(errorMessage(value));
  }
}

function openItem(item: InboxItemDTO) {
  if (item.deeplink) {
    void router.push(item.deeplink);
    return;
  }
  ElMessage.info(item.biz_type === "NOTICE" ? "公告详情页建设中；待阅可点「标记已读」" : "详情页建设中");
}

watch(activeTab, (tab) => {
  if (tab === "operations" && operations.value.length === 0) void loadOperations();
});

onMounted(() => {
  void loadPeople();
  void loadInbox();
});
</script>

<template>
  <section class="page-heading">
    <div>
      <div class="eyebrow">INTERNAL WORKBENCH</div>
      <h1>工作台</h1>
      <p>汇总分配给我的办理事项与阅读公告，点击进入原业务模块。</p>
    </div>
    <el-button :loading="loading || opLoading" @click="refresh">刷新</el-button>
  </section>

  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

  <section class="panel wb-panel">
    <div class="wb-toolbar">
      <el-select v-model="assigneeId" style="width: 200px" @change="changeAssignee">
        <el-option :value="0" label="我（当前登录）" />
        <el-option v-for="person in people" :key="person.id" :value="person.id" :label="person.name" />
      </el-select>
      <el-select v-model="region" style="width: 140px" @change="changeRegion">
        <el-option value="" label="全部平台" />
        <el-option value="CN" label="国内平台" />
        <el-option value="OVERSEAS" label="海外平台" />
      </el-select>
      <el-input v-model="keyword" placeholder="搜索事项 / 业务对象" clearable style="width: 240px" @keyup.enter="search" />
      <el-button @click="search">搜索</el-button>
      <span v-if="isViewingOther" class="wb-readonly">只读查看他人收件箱；操作请切换回本人</span>
    </div>

    <el-tabs v-model="activeTab">
      <el-tab-pane label="我的待办" name="todo">
        <div class="wb-state-tabs">
          <button
            v-for="tab in stateTabs"
            :key="tab.value"
            type="button"
            class="wb-state"
            :class="{ active: stateTab === tab.value }"
            @click="changeState(tab.value)"
          >
            {{ tab.label }}
          </button>
        </div>

        <el-table v-loading="loading" :data="list" style="width: 100%" @row-click="openItem">
          <el-table-column label="事项" min-width="200">
            <template #default="{ row }">{{ displayTitle(row) }}</template>
          </el-table-column>
          <el-table-column prop="biz_object" label="业务对象" min-width="240" />
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="stateTagType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button v-if="row.readable" text type="primary" @click.stop="markRead(row)">标记已读</el-button>
              <span v-else class="wb-muted">—</span>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="当前没有事项" :image-size="64" />
          </template>
        </el-table>

        <div class="wb-pager">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="size"
            :total="total"
            :page-sizes="[20, 50]"
            layout="total, sizes, prev, pager, next"
            @current-change="loadInbox"
            @size-change="resetAndLoad"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane label="操作记录" name="operations">
        <el-table v-loading="opLoading" :data="operations" style="width: 100%">
          <el-table-column prop="action" label="操作" width="180" />
          <el-table-column label="对象" min-width="240">
            <template #default="{ row }">{{ row.entity_type }} #{{ row.entity_id }}</template>
          </el-table-column>
          <el-table-column prop="operator_name" label="操作人" width="140" />
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="reason" label="说明" min-width="180" show-overflow-tooltip />
          <template #empty>
            <el-empty description="暂无操作记录" :image-size="64" />
          </template>
        </el-table>

        <div class="wb-pager">
          <el-pagination
            v-model:current-page="opPage"
            v-model:page-size="opSize"
            :total="opTotal"
            :page-sizes="[20, 50]"
            layout="total, sizes, prev, pager, next"
            @current-change="loadOperations"
            @size-change="loadOperations"
          />
        </div>
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<style scoped>
.wb-panel { padding: 20px 22px; }
.wb-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
.wb-readonly { color: var(--app-warning); font-size: 12px; }
.wb-state-tabs { display: flex; gap: 6px; margin: 4px 0 14px; }
.wb-state { padding: 6px 14px; border: 1px solid var(--app-border); border-radius: 999px; background: var(--app-surface); color: var(--app-text-primary); cursor: pointer; }
.wb-state:hover { background: var(--app-surface-muted); }
.wb-state.active { border-color: var(--app-accent); background: var(--app-hover-bg); color: var(--app-accent); }
.wb-pager { display: flex; justify-content: flex-end; margin-top: 14px; }
.wb-muted { color: var(--app-text-muted); }
</style>
