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
  type OperationOptionsDTO,
  type WorkbenchPersonDTO,
} from "../../../modules/workbench/api/workbench";
import { OPERATION_ACTION_LABELS, OPERATION_ENTITY_LABELS, OPERATION_ENTITY_CATEGORY, OPERATION_ENTITY_CATEGORY_ORDER } from "../../../domain/labels";
import NoticeDetailDialog from "../resources/NoticeDetailDialog.vue";
import SubmissionFlowDialog from "./SubmissionFlowDialog.vue";
import { submissionFlowsApi } from "../../../api/submissionFlows";

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

// 待办表排序（原型要求五列都能升降序）。数据是「状态 × 人员」多路合并后在本地排的，
// 所以排序也在本地做，不回后台翻页。
type InboxSortKey = "title" | "biz_object" | "assignee_name" | "occurred_at" | "state";
const sortKey = ref<InboxSortKey>("occurred_at");
const sortOrder = ref<"asc" | "desc">("desc");
const mergedRows = ref<InboxItemDTO[]>([]);

// 操作记录筛选条（原型：操作人 / 操作类型 / 对象类型 / 操作时间 / 搜索 / 重置）。
// 三个下拉都是**多选**（原型的勾选下拉），选项由后端返回库里真实出现过的值。
const opOperatorIds = ref<number[]>([]);
const opActions = ref<string[]>([]);
const opEntityCategories = ref<string[]>([]);
const opDateRange = ref<[string, string] | null>(null);
const opKeyword = ref("");
const opOptions = ref<OperationOptionsDTO>({ actions: [], entity_types: [], operators: [] });
// 对象类型按原型固定成 8 类（标准模型库/供应商/客户/模型/报价/申报/请求/系统记录）。
// 库里没有数据的类别置灰而不是隐藏——原型列的就是这八项，隐藏会看不出差别。
const opEntityCategoryOptions = computed(() => {
  const byCategory = new Map<string, string[]>();
  for (const raw of opOptions.value.entity_types) {
    const category = OPERATION_ENTITY_CATEGORY[raw] || "系统记录";
    byCategory.set(category, [...(byCategory.get(category) ?? []), raw]);
  }
  return OPERATION_ENTITY_CATEGORY_ORDER.map(category => ({
    label: category,
    types: byCategory.get(category) ?? [],
    disabled: !byCategory.has(category),
  }));
});
const opEntityTypes = computed(() =>
  opEntityCategories.value.flatMap(category => opEntityCategoryOptions.value.find(option => option.label === category)?.types ?? []),
);

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
const operationLabel = (action: string) => OPERATION_ACTION_LABELS[action] || action;
const operationEntityLabel = (entityType: string) => OPERATION_ENTITY_LABELS[entityType] || entityType;
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
// 对象列：优先用后端补的业务对象名（供应商 / 模型、供应商 · 申报单号），取不到再退回「类型 #id」。
const operationObject = (item: OperationItemDTO) => {
  if (item.object_name) return item.object_name;
  const label = operationEntityLabel(item.entity_type);
  return label ? `${label} #${item.entity_id}` : "系统记录";
};
const sortColumnValue = (row: InboxItemDTO) => {
  switch (sortKey.value) {
    case "title": return displayTitle(row);
    case "biz_object": return row.biz_object || "";
    case "assignee_name": return row.assignee_name || "";
    case "state": return stateLabel(row.state);
    case "occurred_at": default: return row.occurred_at;
  }
};
function compareInboxRows(a: InboxItemDTO, b: InboxItemDTO) {
  let result = sortKey.value === "occurred_at"
    ? Date.parse(a.occurred_at) - Date.parse(b.occurred_at)
    : String(sortColumnValue(a)).localeCompare(String(sortColumnValue(b)), "zh-Hans-CN");
  // 同值时按业务 id 兜底，保证翻页/重排稳定。
  if (!result) result = a.biz_id - b.biz_id;
  return sortOrder.value === "asc" ? result : -result;
}
function applyInboxView() {
  const sorted = [...mergedRows.value].sort(compareInboxRows);
  list.value = sorted.slice((page.value - 1) * size.value, page.value * size.value);
}
function onInboxSortChange({ prop, order }: { prop?: string; order?: string | null }) {
  if (!order) {
    sortKey.value = "occurred_at";
    sortOrder.value = "desc";
  } else {
    sortKey.value = (prop as InboxSortKey) || "occurred_at";
    sortOrder.value = order === "ascending" ? "asc" : "desc";
  }
  applyInboxView();
}
const defaultInboxSort = computed(() => ({ prop: sortKey.value, order: sortOrder.value === "asc" ? "ascending" : "descending" }));
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
    // 待办/已办与待阅/已阅都按人员筛选：待阅是全员广播但已读状态是各自的，
    // 一条公告对应每人一条（notice + notice_read），切换人员即可看到别人的已读。
    // 「全部人员」依赖人员列表，列表还没回来时先补一次，否则会退化成只看自己。
    if (!selectedPeopleIds.value.length && !people.value.length) await loadPeople();
    const targets = selectedPeopleIds.value.length ? [...new Set(selectedPeopleIds.value)] : people.value.map(person => person.id);
    if ((todoStates.length || readStates.length) && !targets.length) targets.push(0);
    const combinations = [
      ...todoStates.flatMap(state => targets.map(assigneeId => ({ state, assigneeId }))),
      ...readStates.flatMap(state => targets.map(assigneeId => ({ state, assigneeId }))),
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
    mergedRows.value = results.flatMap(result => result.rows);
    total.value = results.reduce((sum, result) => sum + result.total, 0);
    applyInboxView();
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
    const [dateFrom, dateTo] = opDateRange.value ?? [];
    const res = await workbenchApi.operations({
      operatorIds: opOperatorIds.value,
      actions: opActions.value,
      entityTypes: opEntityTypes.value,
      keyword: opKeyword.value.trim() || undefined,
      dateFrom: dateFrom || undefined,
      dateTo: dateTo || undefined,
      page: opPage.value,
      size: opSize.value,
    });
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

async function loadOperationOptions() {
  try {
    opOptions.value = await workbenchApi.operationOptions();
  } catch {
    // 筛选条是辅助能力，取不到就退化成只有搜索框，不影响列表。
    opOptions.value = { actions: [], entity_types: [], operators: [] };
  }
}

function searchOperations() {
  opPage.value = 1;
  void loadOperations();
}

// 原型在日期区间右侧有独立的「清空」，只清时间、不动其它筛选。
function clearOperationDates() {
  opDateRange.value = null;
  searchOperations();
}

function resetOperationFilters() {
  opOperatorIds.value = [];
  opActions.value = [];
  opEntityCategories.value = [];
  opDateRange.value = null;
  opKeyword.value = "";
  searchOperations();
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

// 查看：待办直接跳到具体业务页面；公告弹详情；其余按事项类型跳转。
async function openItem(item: InboxItemDTO) {
  if (item.biz_type === "SUBMISSION_HANDLING") {
    await jumpToHandling(item);
    return;
  }
  // 申报审批按原型走独立页（可筛选模型、逐模型摆开看、审批完回供应商记录）。
  if (item.biz_type === "SUBMISSION_APPROVAL") {
    await router.push({ path: `/internal/submissions/${item.biz_id}` });
    return;
  }
  // 合同与授信按原型走「供应商档案页」：详情 → 档案与办理 → 页面底部「当前办理范围」→ 办理。
  if (item.biz_type === "SUBMISSION_CONTRACT") {
    await jumpToContract(item);
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

// 合同与授信：先解析出这条节点待办属于哪个供应商，再把用户送到该供应商的「档案与办理」页。
// 原型 20261104 的路径是 工作台详情 → 供应商档案页 → 底部「当前办理范围」→ 办理原商务事项，
// 落地页能同时看到基础资料、合同与授信档案和本批进度，比弹窗更有上下文。
// 解析失败（流程已撤、接口异常）时退回原办理弹窗，由弹窗展示错误，避免点了没反应。
// 注意：`?node_id=` 深链仍走弹窗——办理弹窗里「查看事项」切换节点要靠它，
// 换成整页跳转会让弹窗内的节点切换把自己顶掉。
async function jumpToContract(item: InboxItemDTO) {
  try {
    const view = await submissionFlowsApi.get(String(item.biz_id), true);
    if (view.supplier_id) {
      await router.push({
        path: `/internal/suppliers/${view.supplier_id}/models`,
        query: { tab: "profile", section: "contract", contract_flow: String(item.biz_id) },
      });
      return;
    }
  } catch {
    // 取不到节点视图时退回办理弹窗，由弹窗展示错误。
  }
  selectedFlow.value = { id: String(item.biz_id), node: true };
}

// 供应资源接入：原型是从供应商详情的「接入模型」进入（能看到同批所有模型与进度），
// 不是单条资源的详情抽屉——抽屉里看不到其他模型，也没法对照办理进度。
async function jumpToHandling(item: InboxItemDTO) {
  try {
    const view = await submissionFlowsApi.get(String(item.biz_id), true);
    const targets = (view.rows || []).filter(row => row.resource_id && row.status !== "CANCELED");
    if (view.supplier_id) {
      await router.push({
        path: `/internal/suppliers/${view.supplier_id}/models`,
        query: { tab: "models", ...(targets.length === 1 ? { highlight: targets[0].resource_id } : {}) },
      });
      return;
    }
  } catch {
    // 取不到节点视图时退回办理详情弹窗，由弹窗展示错误。
  }
  selectedFlow.value = { id: String(item.biz_id), node: true };
}

watch(activeTab, (tab) => {
  if (tab !== "operations") return;
  void loadOperationOptions();
  if (operations.value.length === 0) void loadOperations();
});

onMounted(() => {
  void loadPeople();
  void loadInbox();
  void loadOperationOptions();
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

        <el-table
          v-loading="loading"
          :data="list"
          :default-sort="defaultInboxSort"
          style="width: 100%"
          @sort-change="onInboxSortChange"
        >
          <el-table-column prop="title" label="事项" min-width="180" sortable="custom">
            <template #default="{ row }">{{ displayTitle(row) }}</template>
          </el-table-column>
          <el-table-column prop="biz_object" label="业务对象" min-width="220" sortable="custom" />
          <el-table-column prop="assignee_name" label="责任人" width="140" sortable="custom">
            <template #default="{ row }">
              <div>{{ row.assignee_name || "—" }}</div>
              <div class="wb-sub">{{ row.assignee_role }}</div>
            </template>
          </el-table-column>
          <el-table-column prop="occurred_at" label="时间" width="200" sortable="custom">
            <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
          </el-table-column>
          <el-table-column prop="state" label="状态" width="110" sortable="custom">
            <template #default="{ row }">
              <el-tag :type="stateTagType(row.state)" size="small">{{ stateLabel(row.state) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }"><el-button link type="primary" @click="openItem(row)">详情</el-button></template>
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
        <div class="wb-toolbar">
          <el-select
            v-model="opOperatorIds"
            class="wb-filter"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            clearable
            placeholder="操作人"
            @change="searchOperations"
          >
            <el-option v-for="person in opOptions.operators" :key="person.id" :label="person.name" :value="person.id" />
          </el-select>
          <el-select
            v-model="opActions"
            class="wb-filter"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            clearable
            placeholder="操作类型"
            @change="searchOperations"
          >
            <el-option v-for="action in opOptions.actions" :key="action" :label="operationLabel(action)" :value="action" />
          </el-select>
          <el-select
            v-model="opEntityCategories"
            class="wb-filter"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            clearable
            placeholder="对象类型"
            @change="searchOperations"
          >
            <el-option
              v-for="option in opEntityCategoryOptions"
              :key="option.label"
              :label="option.label"
              :value="option.label"
              :disabled="option.disabled"
            />
          </el-select>
          <el-date-picker
            v-model="opDateRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="—"
            start-placeholder="yyyy/mm/dd"
            end-placeholder="yyyy/mm/dd"
            class="wb-date-range"
            @change="searchOperations"
          />
          <el-button text :disabled="!opDateRange" @click="clearOperationDates">清空</el-button>
          <el-input
            v-model="opKeyword"
            class="wb-search"
            placeholder="搜索操作 / 业务对象"
            clearable
            @keyup.enter="searchOperations"
            @clear="searchOperations"
          />
          <el-button @click="resetOperationFilters">重置</el-button>
        </div>

        <el-table v-loading="opLoading" :data="operations" style="width: 100%">
          <el-table-column label="操作" width="200">
            <template #default="{ row }">
              <span :title="row.action">{{ operationLabel(row.action) }}</span>
            </template>
          </el-table-column>
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
      <el-descriptions-item label="操作">
        {{ operationLabel(selectedOperation.action) }}
        <span class="wb-raw">{{ selectedOperation.action }}</span>
      </el-descriptions-item>
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
.wb-filter { width: 150px; }
.wb-date-range { width: 260px; }
.wb-raw { margin-left: 8px; color: var(--app-text-muted); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
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
