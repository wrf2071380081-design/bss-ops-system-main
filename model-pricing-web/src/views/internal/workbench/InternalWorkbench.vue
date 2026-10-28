<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter, useRoute } from "vue-router";
import { ElMessage } from "element-plus";
import { formatDateTime } from "../../../domain/date";
import { ApiError } from "../../../domain/common";
import { useSessionStore } from "../../../stores/session";
import {
  workbenchApi,
  type InboxItemDTO,
  type InboxState,
  type OperationItemDTO,
  type WorkbenchPersonDTO,
} from "../../../modules/workbench/api/workbench";
import NoticeDetailDialog from "../resources/NoticeDetailDialog.vue";
import SubmissionFlowDialog from "./SubmissionFlowDialog.vue";

const router = useRouter();
const route = useRoute();
const session = useSessionStore();
const selectedFlow = ref<{ id: string; node: boolean }>();
watch(
  () => [route.query.submission_id, route.query.node_id],
  () => {
    const raw = route.query.node_id || route.query.submission_id;
    if (typeof raw === "string" && /^[1-9]\d*$/.test(raw)) {
      selectedFlow.value = { id: raw, node: !!route.query.node_id };
    }
  },
  { immediate: true },
);

const activeTab = ref<"todo" | "operations">("todo");
const selectedStates = ref<InboxState[]>(["TODO", "READ"]);
// 0 = 当前登录人（默认只看自己）；空数组 = 全部人员（一键全选）。
const selectedPeopleIds = ref<number[]>([0]);
const peopleKeyword = ref("");
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
const selectedSupplyNotice = ref<{ id: string; staffID?: string; readOnly: boolean }>();
const selectedOperation = ref<OperationItemDTO>();
let inboxGeneration = 0, operationsGeneration = 0;

// 勾选顺序与原型一致：待办、已办、待阅、已阅。
const stateTabs: { value: InboxState; label: string }[] = [
  { value: "TODO", label: "待办" },
  { value: "DONE", label: "已办" },
  { value: "READ", label: "待阅" },
  { value: "READ_DONE", label: "已阅" },
];

const NOTICE_LABELS: Record<string, string> = {
  ONLINE_OFFLINE_PLAN: "上下架计划公告",
  RESOURCE_CHANGE: "资源变动公告",
  RESOURCE_ONLINED: "资源上架公告",
  RESOURCE_OFFLINED: "资源下架公告",
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
  PRICE_CHANGE: "内部改价计划",
};
const ENTITY_LABELS: Record<string, string> = {
  standard_model: "标准模型",
  resource: "资源",
  supply_notice: "公告",
  supply_plan: "资源计划",
  formal_config: "正式配置",
};
const filteredPeople = computed(() => {
  const text = peopleKeyword.value.trim().toLocaleLowerCase();
  return text ? people.value.filter(person => person.name.toLocaleLowerCase().includes(text)) : people.value;
});
const selfName = computed(() => session.user?.name || "我（当前登录）");
const selectedPeopleLabel = computed(() => selectedPeopleIds.value.length
  ? selectedPeopleIds.value.map(id => id === 0 ? selfName.value : people.value.find(person => person.id === id)?.name || `人员 ${id}`).join("、")
  : "全部");
const displayTitle = (item: InboxItemDTO) => {
  if (item.biz_type === "NOTICE") return NOTICE_LABELS[item.title] || item.title;
  if (item.biz_type === "ONBOARDING_NODE") return NODE_LABELS[item.title] || item.title;
  if (item.biz_type === "PLAN") return PLAN_LABELS[item.title] || item.title;
  return item.title;
};
const stateLabel = (state: InboxState) => stateTabs.find(item => item.value === state)?.label || state;
const operationObject = (item: OperationItemDTO) => `${ENTITY_LABELS[item.entity_type] || item.entity_type} #${item.entity_id}`;
const stateTagType = (state: InboxState) => state === "TODO" ? "warning" : state === "DONE" ? "success" : "info";
const formatTime = (value?: string | null) => formatDateTime(value, "—", true);
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
  const generation = ++inboxGeneration;
  loading.value = true;
  list.value = [];
  total.value = 0;
  error.value = "";
  try {
    const todoStates = selectedStates.value.filter(state => state === "TODO" || state === "DONE");
    const readStates = selectedStates.value.filter(state => state === "READ" || state === "READ_DONE");
    // 待办/已办按人员筛选（可多选、可全选）；待阅/已阅是全员广播，固定查当前登录人，
    // 各自点各自的已阅，避免全选时同一条公告按人重复出现。
    // 「全部人员」依赖人员列表，列表还没回来时先补一次，否则会退化成只看自己。
    if (!selectedPeopleIds.value.length && !people.value.length) await loadPeople();
    const targets = selectedPeopleIds.value.length ? [...new Set(selectedPeopleIds.value)] : people.value.map(person => person.id);
    if (todoStates.length && !targets.length) targets.push(0);
    const combinations = [
      ...todoStates.flatMap(state => targets.map(assigneeId => ({ state, assigneeId }))),
      ...readStates.map(state => ({ state, assigneeId: 0 })),
    ];
    if (!combinations.length) {
      total.value = 0;
      return;
    }
    const wanted = page.value * size.value;
    const chunkSize = Math.min(100, wanted);
    const results = await Promise.all(combinations.map(async ({ state, assigneeId }) => {
      const rows: InboxItemDTO[] = [];
      let combinationTotal = 0;
      let requestedPage = 1;
      do {
        const result = await workbenchApi.inbox({
          state,
          assigneeId: assigneeId || undefined,
          keyword: keyword.value || undefined,
          page: requestedPage,
          size: chunkSize,
        });
        if (requestedPage === 1) combinationTotal = result.total;
        rows.push(...result.list);
        if (!result.list.length) break;
        requestedPage += 1;
      } while (rows.length < Math.min(wanted, combinationTotal));
      return { rows, total: combinationTotal };
    }));
    if (generation !== inboxGeneration) return;
    const combined = results.flatMap(result => result.rows).sort((a, b) => {
      const byTime = Date.parse(b.occurred_at) - Date.parse(a.occurred_at);
      return byTime || a.biz_type.localeCompare(b.biz_type) || a.biz_id - b.biz_id;
    });
    list.value = combined.slice((page.value - 1) * size.value, page.value * size.value);
    total.value = results.reduce((sum, result) => sum + result.total, 0);
  } catch (value) {
    if (generation !== inboxGeneration) return;
    list.value = [];
    total.value = 0;
    error.value = errorMessage(value);
  } finally {
    if (generation === inboxGeneration) loading.value = false;
  }
}

async function loadOperations() {
  const generation = ++operationsGeneration;
  opLoading.value = true;
  error.value = "";
  try {
    const res = await workbenchApi.operations({ page: opPage.value, size: opSize.value });
    if (generation !== operationsGeneration) return;
    operations.value = res.list;
    opTotal.value = res.total;
  } catch (value) {
    if (generation !== operationsGeneration) return;
    operations.value = [];
    opTotal.value = 0;
    error.value = errorMessage(value);
  } finally {
    if (generation === operationsGeneration) opLoading.value = false;
  }
}

function resetAndLoad() {
  page.value = 1;
  void loadInbox();
}

function toggleState(state: InboxState, checked: boolean) {
  selectedStates.value = checked
    ? [...new Set([...selectedStates.value, state])]
    : selectedStates.value.filter(value => value !== state);
  resetAndLoad();
}

function toggleStateFromEvent(state: InboxState, event: Event) {
  toggleState(state, event.target instanceof HTMLInputElement && event.target.checked);
}

function togglePerson(id: number, checked: boolean) {
  selectedPeopleIds.value = checked
    ? [...new Set([...selectedPeopleIds.value, id])]
    : selectedPeopleIds.value.filter(value => value !== id);
  changePeople();
}

function togglePersonFromEvent(id: number, event: Event) {
  togglePerson(id, event.target instanceof HTMLInputElement && event.target.checked);
}

function selectAllPeople() {
  selectedPeopleIds.value = [];
  peopleKeyword.value = "";
  changePeople();
}

function changePeople() {
  selectedSupplyNotice.value = undefined;
  page.value = 1;
  void loadInbox();
}

function search() {
  page.value = 1;
  void loadInbox();
}

function resetFilters() {
  selectedPeopleIds.value = [0];
  selectedStates.value = ["TODO", "READ"];
  peopleKeyword.value = "";
  keyword.value = "";
  page.value = 1;
  selectedSupplyNotice.value = undefined;
  void loadInbox();
}

// 查看：按事项类型跳到对应业务界面，或打开办理 / 公告弹窗。
function openItem(item: InboxItemDTO) {
  if (["SUBMISSION_APPROVAL", "SUBMISSION_HANDLING", "SUBMISSION_CONTRACT"].includes(item.biz_type)) {
    selectedFlow.value = { id: String(item.biz_id), node: item.biz_type !== "SUBMISSION_APPROVAL" };
    return;
  }
  if (item.biz_type === "NOTICE") {
    // 本人公告可标记已读；看他人收件箱时只读。
    const mine = session.user?.staffId ? item.assignee_id === session.user.staffId : item.readable;
    selectedSupplyNotice.value = { id: String(item.biz_id), staffID: item.assignee_id ? String(item.assignee_id) : undefined, readOnly: !mine };
    return;
  }
  if (item.deeplink) {
    void router.push(item.deeplink);
    return;
  }
  ElMessage.info("该事项暂无关联页面");
}

watch(activeTab, (tab) => {
  if (tab === "operations" && operations.value.length === 0) void loadOperations();
});

onMounted(() => {
  void loadPeople();
  void loadInbox();
});
onBeforeUnmount(() => { ++inboxGeneration; ++operationsGeneration; });
</script>

<template>
  <SubmissionFlowDialog v-if="selectedFlow" :key="`${selectedFlow.id}:${selectedFlow.node}`" :id="selectedFlow.id" :node="selectedFlow.node" @close="selectedFlow=undefined" @changed="loadInbox" />
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

  <section class="panel wb-panel">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="待办事项" name="todo">
        <div class="wb-toolbar">
          <div class="wb-state-options" role="group" aria-label="事项状态筛选">
            <label v-for="tab in stateTabs" :key="tab.value">
              <input type="checkbox" :checked="selectedStates.includes(tab.value)" @change="toggleStateFromEvent(tab.value, $event)">
              {{ tab.label }}
            </label>
          </div>
          <details class="wb-people-filter">
            <summary><span class="wb-people-label">人员</span><strong>{{ selectedPeopleLabel }}</strong><span aria-hidden="true">⌄</span></summary>
            <div class="wb-people-menu">
              <el-input v-model="peopleKeyword" size="small" clearable placeholder="输入匹配已有人员" aria-label="搜索人员" />
              <button class="wb-all-people" type="button" @click="selectAllPeople">全部人员</button>
              <label class="wb-person-option">
                <input type="checkbox" :checked="selectedPeopleIds.includes(0)" @change="togglePersonFromEvent(0, $event)">
                我（{{ selfName }}）
              </label>
              <label v-for="person in filteredPeople" :key="person.id" class="wb-person-option">
                <input type="checkbox" :checked="selectedPeopleIds.includes(person.id)" @change="togglePersonFromEvent(person.id, $event)">
                {{ person.name }}
              </label>
              <p v-if="!filteredPeople.length" class="wb-empty-people">没有匹配人员</p>
            </div>
          </details>
          <el-input v-model="keyword" class="wb-search" placeholder="搜索事项 / 业务对象" clearable @keyup.enter="search" />
          <el-button @click="resetFilters">重置</el-button>
        </div>

        <el-table v-loading="loading" :data="list" style="width: 100%">
          <el-table-column label="事项" min-width="180">
            <template #default="{ row }">{{ displayTitle(row) }}</template>
          </el-table-column>
          <el-table-column prop="biz_object" label="业务对象" min-width="220" />
          <el-table-column label="责任人" width="140">
            <template #default="{ row }">
              <div>{{ row.assignee_name || "—" }}</div>
              <div class="wb-sub">{{ row.assignee_role }}</div>
            </template>
          </el-table-column>
          <el-table-column label="时间" width="200">
            <template #default="{ row }">创建 {{ formatTime(row.occurred_at) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="stateTagType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }"><el-button link type="primary" @click="openItem(row)">查看</el-button></template>
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
            <template #default="{ row }">{{ operationObject(row) }}</template>
          </el-table-column>
          <el-table-column prop="operator_name" label="操作人" width="140" />
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ formatDateTime(row.created_at, "—", false) }}</template>
          </el-table-column>
          <el-table-column label="详情" width="90">
            <template #default="{ row }"><el-button link type="primary" @click="selectedOperation = row">查看</el-button></template>
          </el-table-column>
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

  <NoticeDetailDialog
    v-if="selectedSupplyNotice"
    :key="`${selectedSupplyNotice.id}:${selectedSupplyNotice.staffID}`"
    :id="selectedSupplyNotice.id"
    :selected-staff-i-d="selectedSupplyNotice.staffID"
    :read-only="selectedSupplyNotice.readOnly"
    @close="selectedSupplyNotice = undefined"
    @read="loadInbox"
  />
  <el-dialog :model-value="Boolean(selectedOperation)" title="操作记录详情" width="min(620px, 94vw)" @close="selectedOperation = undefined">
    <el-descriptions v-if="selectedOperation" :column="1" border>
      <el-descriptions-item label="操作">{{ selectedOperation.action }}</el-descriptions-item>
      <el-descriptions-item label="对象">{{ operationObject(selectedOperation) }}</el-descriptions-item>
      <el-descriptions-item label="操作人">{{ selectedOperation.operator_name }}</el-descriptions-item>
      <el-descriptions-item label="时间">{{ formatTime(selectedOperation.created_at) }}</el-descriptions-item>
      <el-descriptions-item label="说明">{{ selectedOperation.reason || "—" }}</el-descriptions-item>
    </el-descriptions>
  </el-dialog>
</template>

<style scoped>
.wb-panel { padding: 20px 22px; }
.wb-toolbar { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; margin: 4px 0 14px; }
.wb-state-options { display: inline-flex; gap: 18px; flex-wrap: wrap; }
.wb-state-options label { display: inline-flex; align-items: center; gap: 6px; color: var(--app-text-primary); font-size: 13px; cursor: pointer; }
.wb-search { width: 240px; }
.wb-people-filter { position: relative; width: 200px; max-width: 100%; }
.wb-people-filter summary { display: flex; align-items: center; gap: 8px; min-height: 32px; padding: 0 11px; border: 1px solid var(--app-border); border-radius: 4px; background: var(--app-surface); cursor: pointer; font-size: 13px; list-style: none; }
.wb-people-filter summary::-webkit-details-marker { display: none; }
.wb-people-label { color: var(--app-text-muted); }
.wb-people-filter summary strong { flex: 1; overflow: hidden; color: var(--app-text-primary); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.wb-people-menu { position: absolute; z-index: 20; top: calc(100% + 6px); left: 0; width: 280px; max-height: 340px; overflow: auto; padding: 10px; border: 1px solid var(--app-border); border-radius: 6px; background: var(--app-surface); box-shadow: 0 8px 24px rgb(31 45 61 / 14%); }
.wb-all-people { width: 100%; margin: 8px 0 4px; padding: 7px 8px; border: 0; border-radius: 4px; background: var(--app-hover-bg); color: var(--app-accent); text-align: left; cursor: pointer; }
.wb-person-option { display: flex; align-items: center; gap: 8px; padding: 7px 8px; color: var(--app-text-primary); font-size: 13px; cursor: pointer; }
.wb-person-option:hover { background: var(--app-surface-muted); }
.wb-empty-people { padding: 4px 8px; color: var(--app-text-muted); font-size: 12px; }
.wb-sub { color: var(--app-text-muted); font-size: 12px; line-height: 1.6; }
.wb-pager { display: flex; justify-content: flex-end; margin-top: 14px; }
</style>
