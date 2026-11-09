<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { apiRequest } from "../../../api/http";
import { standardModelVersion } from "../../../api/standardModels.contract";
import { validateSourceOperationOptions, validateSourceOperations, sourceOperationLocation, type SourceOperation, type SourceOperationOptions } from "../../../api/sourceOperations.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import { formatDateTime } from "../../../domain/date";
const props = defineProps<{ modelID: string }>();
const router = useRouter();
const rows = ref<SourceOperation[]>([]), page = ref(1), total = ref(0), loading = ref(false), error = ref("");
const planActions: Record<string, string> = { CREATE: "创建上下架计划", REVISE: "修改上下架计划", CANCEL: "取消上下架计划", EXECUTE: "登记计划实际完成", BLOCKED: "计划执行阻断" };
const actionLabels: Record<string, string> = {
  CREATE: "新建", UPDATE: "维护", UPDATE_NOOP: "维护（无变更）", DISABLE: "停用", DISABLE_NOOP: "停用（无变更）",
  UPLOAD: "上传", ARCHIVE: "归档", REVISE: "修订", CANCEL: "取消", EXECUTE: "登记实际完成", BLOCK: "执行阻断",
  ADMIT: "登记接入", BIND_FORMAL_CONFIG: "绑定正式配置", ONLINE: "上架", OFFLINE: "下架",
  CHANGE_COEFFICIENT: "批准改价", CHANGE_COEFFICIENT_NOOP: "同价确认", APPLY_APPROVED_PRICE: "应用批准改价",
  CONFIG_COMPLETE: "登记配置完成", CANCEL_ACCESS: "取消接入", RESOURCE_REVISION: "资源变更", PLAN_REVISION: "计划变更",
};
// 原型「操作记录」的三个筛选：操作人、操作类型、时间区间。
const filters = ref({ operator: "", action: "", range: [] as string[] });
const applied = ref({ operator: "", action: "", range: [] as string[] });
const options = ref<SourceOperationOptions>({ operators: [], actions: [] }), optionError = ref("");
let controller: AbortController | undefined, optionController: AbortController | undefined;

function rangeParams(range: string[]) {
  const params: Record<string, string> = {};
  if (range[0]) params.from = new Date(`${range[0]}T00:00:00`).toISOString();
  if (range[1]) {
    const end = new Date(`${range[1]}T00:00:00`);
    end.setDate(end.getDate() + 1); // 左闭右开：结束日整天都要包含
    params.to = end.toISOString();
  }
  return params;
}
async function load() {
  controller?.abort(); const request = controller = new AbortController();
  rows.value = []; total.value = 0; loading.value = true; error.value = "";
  const current = applied.value;
  try {
    const result = validateSourceOperations(await apiRequest<unknown>({
      url: `/internal/standard-models/${standardModelVersion(props.modelID)}/resource-operations`,
      params: { page: page.value, size: 20, ...(current.operator ? { operator: current.operator } : {}), ...(current.action ? { action: current.action } : {}), ...rangeParams(current.range) },
      signal: request.signal,
    }), page.value, 20);
    if (!request.signal.aborted) { rows.value = result.list; total.value = result.total; }
  } catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "操作记录暂时无法加载。"; }
  finally { if (!request.signal.aborted) loading.value = false; }
}
async function loadOptions() {
  optionController?.abort(); const request = optionController = new AbortController();
  try {
    options.value = validateSourceOperationOptions(await apiRequest<unknown>({ url: `/internal/standard-models/${standardModelVersion(props.modelID)}/resource-operations/options`, signal: request.signal }));
    optionError.value = "";
  } catch (value) { if (!request.signal.aborted) optionError.value = value instanceof Error ? value.message : "筛选项加载失败，可直接输入。"; }
}
function apply() { applied.value = { ...filters.value, range: [...filters.value.range] }; page.value = 1; void load(); }
function reset() { filters.value = { operator: "", action: "", range: [] }; apply(); }
const actionLabel = (action: string) => actionLabels[action] || action;
watch(() => props.modelID, () => { page.value = 1; void load(); void loadOptions(); }, { immediate: true });
onBeforeUnmount(() => { controller?.abort(); optionController?.abort(); });
</script>
<template>
  <section v-loading="loading" class="model-source-records">
    <p class="scope-note">汇总本模型全部供应资源及上下架计划的操作记录，不受来源列表筛选影响。模型资料维护记录在标准模型库查看。</p>
    <form class="record-filters" @submit.prevent="apply">
      <label>操作人<el-select v-model="filters.operator" clearable filterable allow-create placeholder="全部" aria-label="操作人" style="width:150px" @change="apply"><el-option v-for="name in options.operators" :key="name" :label="name" :value="name" /></el-select></label>
      <label>操作类型<el-select v-model="filters.action" clearable filterable placeholder="全部" aria-label="操作类型" style="width:170px" @change="apply"><el-option v-for="action in options.actions" :key="action" :label="actionLabel(action)" :value="action" /></el-select></label>
      <label>操作时间<el-date-picker v-model="filters.range" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始" end-placeholder="结束" aria-label="操作时间" style="width:250px" @change="apply" /></label>
      <el-button @click="reset">重置</el-button>
    </form>
    <el-alert v-if="optionError" :title="optionError" type="warning" :closable="false" />
    <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text @click="load">重新加载操作记录</el-button></el-alert>
    <el-table v-else :data="rows" empty-text="暂无供应资源操作记录" aria-label="模型供应资源操作记录">
      <el-table-column label="操作" min-width="160"><template #default="{ row }">{{ row.entity_type === 'plan' ? planActions[row.action] || row.action : actionLabel(row.action) }}</template></el-table-column>
      <el-table-column label="业务对象" min-width="210"><template #default="{ row }"><strong>{{ row.supplier_name }}</strong><div class="scope-note">{{ resourceRegionLabels[row.region] }} · {{ row.supplier_model_id }}</div></template></el-table-column>
      <el-table-column prop="operator_name" label="操作人" min-width="110"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column>
      <el-table-column label="时间" min-width="165"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      <el-table-column prop="reason" label="操作说明" min-width="180" />
      <el-table-column label="详情" width="100" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="router.push(sourceOperationLocation(row))">{{ row.entity_type === 'plan' ? '查看计划' : '查看资源' }}</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="!error && total" v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
  </section>
</template>
<style scoped>.model-source-records{min-height:180px;padding:0 18px 18px}.scope-note{font-size:12px;line-height:1.8;color:#8a9099}.record-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding:4px 0 16px}.record-filters label{display:flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}.el-pagination{margin-top:20px;justify-content:flex-end}@media(max-width:700px){.record-filters label{flex:1;min-width:170px}}</style>
