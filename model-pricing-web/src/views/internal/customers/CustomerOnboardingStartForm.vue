<script setup lang="ts">
/**
 * 发起开通的表单（B3b 第 1 项 Tab2 与第 2 项向导 Step2 **共用**）。
 *
 * 为什么抽成组件：同一张表单要在两个地方出现——客户详情「档案与办理」Tab（已有客户补发起）
 * 与建客户向导 Step2（新建客户紧接着发起）。复制一份 = 两处校验规则迟早不一致。
 *
 * presetModels：从「接入模型」的待配置行点「配置资源」进来时传入。
 * 原型里配来源只能配**已批准的模型**，所以这时候选来源按模型收窄，
 * 不允许顺手给一个没报过价的模型开客户。
 *
 * 幂等：这里只管收集与提交，`startOnboarding` 在 api 层按 scope + payload 生成幂等键，
 * 重复点击不会建出两条流程（后端还有 uk_flow_customer_processing 兜底）。
 */
import { computed, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { customerDetailApi, type OnboardingCandidate, type OnboardingFlowDTO } from "../../../api/customerDetail";
import { ApiError } from "../../../domain/common";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";

const props = defineProps<{
  customerId: string;
  region: string;
  /** 从「接入模型」待配置行进来时带上：逐模型选来源（原型行内「配置资源」）。 */
  presetModels?: Array<{ standard_model_id: number; model_name: string }>;
  /** 已有进行中的开通流程时传：本次只往里追加模型，不再新建流程。 */
  flowId?: number;
}>();
const emit = defineEmits<{ (event: "started", flow: OnboardingFlowDTO): void }>();

const message = (value: unknown) =>
  value instanceof ApiError ? value.message : value instanceof Error ? value.message : "操作失败，请稍后重试。";

const people = ref<WorkbenchPersonDTO[]>([]);
const firstOrder = ref(true);
const contractAssigneeId = ref("");
const configAssigneeId = ref("");
const reason = ref("");
const submitting = ref(false);
const error = ref("");

/**
 * 操作登记（可选，仅在「往进行中的流程追加来源」时出现）。
 *
 * 对齐原型「配置资源」弹窗：技术先去生产平台（网关）把来源配好，再回平台登记
 * 「什么时候、谁、凭什么」。我们这边 BSS 自己写绑定，但生产侧的操作人与时刻
 * 可能和点这个按钮的人不一样，所以留三个选填项原样留痕；不填就走系统口径。
 */
const opExecutedAt = ref("");
const opExecutedBy = ref("");
const opEvidence = ref("");
const opRecord = computed(() => {
  const executedBy = opExecutedBy.value.trim();
  const evidence = opEvidence.value.trim();
  if (!opExecutedAt.value && !executedBy && !evidence) return undefined;
  return {
    executed_at: opExecutedAt.value || undefined,
    executed_by: executedBy || undefined,
    evidence: evidence || undefined,
  };
});

const keyword = ref("");
const candidates = ref<OnboardingCandidate[]>([]);
const candidateLoading = ref(false);
const candidateError = ref("");
const selected = ref<OnboardingCandidate[]>([]);

const selectedKeys = computed(() => new Set(selected.value.map(item => String(item.resource_id))));

/** 从「接入模型」待配置行进来时带的模型范围（按标准模型 id 去重）。 */
const presetModels = computed(() => {
  const seen = new Set<number>();
  const out: Array<{ standard_model_id: number; model_name: string }> = [];
  for (const item of props.presetModels ?? []) {
    const id = Number(item?.standard_model_id);
    if (!Number.isSafeInteger(id) || id <= 0 || seen.has(id)) continue;
    seen.add(id);
    out.push({ standard_model_id: id, model_name: item.model_name || `标准模型 #${id}` });
  }
  return out;
});
const presetModelIds = computed(() => presetModels.value.map(item => item.standard_model_id));
const presetScoped = computed(() => presetModels.value.length > 0);
/** 逐模型选中的来源：modelId → resourceId（原型「配置资源」就是给每个模型挑一条来源）。 */
const presetSelection = ref<Record<number, string>>({});
/** 逐模型可选来源：modelId → 候选列表。 */
const sourcesByModel = computed(() => {
  const map = new Map<number, OnboardingCandidate[]>();
  for (const item of candidates.value) {
    const key = Number(item.standard_model_id);
    const list = map.get(key) ?? [];
    list.push(item);
    map.set(key, list);
  }
  return map;
});
const unselectedModels = computed(() => presetModels.value.filter(item => !presetSelection.value[item.standard_model_id]));
/** 追加模式：流程已存在，只需选来源，不再问首单与办理人（人已经在流程上了）。 */
const appending = computed(() => Boolean(props.flowId && props.flowId > 0));

async function loadPeople() {
  try {
    people.value = await workbenchApi.people();
  } catch {
    people.value = []; // 人员选择器拉不到时，用户仍可手填 ID（见下面的 el-input 兜底不提供，保持简单：给出错误提示）
  }
}

async function searchCandidates() {
  candidateLoading.value = true;
  candidateError.value = "";
  try {
    if (presetScoped.value) {
      // 走「按模型取候选」：一个模型一次查询（一次配源通常就 1–3 个模型，代价可忽略）。
      const groups = await Promise.all(
        presetModelIds.value.slice(0, 6).map(id => customerDetailApi.onboardingCandidatesForModel(props.region, id, keyword.value.trim(), 1, 50)),
      );
      const merged = new Map<string, OnboardingCandidate>();
      for (const group of groups) for (const item of group.list) merged.set(String(item.resource_id), item);
      candidates.value = [...merged.values()];
      rememberPresetSelection();
    } else {
      const result = await customerDetailApi.onboardingCandidates(props.region, keyword.value.trim(), 1, 50);
      candidates.value = result.list;
    }
  } catch (value) {
    candidates.value = [];
    candidateError.value = message(value);
  } finally {
    candidateLoading.value = false;
  }
}

function addCandidate(row: OnboardingCandidate) {
  if (selectedKeys.value.has(String(row.resource_id))) return;
  selected.value = [...selected.value, row];
}

function removeCandidate(resourceId: number) {
  selected.value = selected.value.filter(item => item.resource_id !== resourceId);
}

const positiveId = (value: string) => /^[1-9]\d*$/.test(value.trim()) && Number.isSafeInteger(Number(value));

/** 候选刷新后，把「已选来源」同步进 selected，保持与旧路径同一份提交数据结构。 */
function rememberPresetSelection() {
  const picked: OnboardingCandidate[] = [];
  for (const item of presetModels.value) {
    const resourceId = presetSelection.value[item.standard_model_id];
    if (!resourceId) continue;
    const hit = sourcesByModel.value.get(item.standard_model_id)?.find(source => String(source.resource_id) === String(resourceId));
    if (hit) picked.push(hit);
  }
  selected.value = picked;
}

function onPresetSourceChange() {
  rememberPresetSelection();
}

function validate() {
  if (!positiveId(props.customerId)) return "客户 ID 非法，请刷新后重试。";
  if (appending.value) {
    if (presetScoped.value && unselectedModels.value.length) {
      return `还有 ${unselectedModels.value.length} 个模型没选供应来源：${unselectedModels.value.map(item => item.model_name).join("、")}`;
    }
    if (!selected.value.length) return "请至少给一个模型选择供应来源。";
    return "";
  }
  if (!positiveId(configAssigneeId.value)) return "请选择资源配置办理人。";
  if (firstOrder.value && !positiveId(contractAssigneeId.value)) return "首单开通必须选择合同与授信办理人。";
  if (presetScoped.value && unselectedModels.value.length) {
    return `还有 ${unselectedModels.value.length} 个模型没选供应来源：${unselectedModels.value.map(item => item.model_name).join("、")}`;
  }
  if (!selected.value.length) return "请至少选择一个模型与供应资源。";
  return "";
}

async function submit() {
  const invalid = validate();
  if (invalid) {
    error.value = invalid;
    return;
  }
  if (submitting.value) return;
  error.value = "";
  submitting.value = true;
  try {
    if (appending.value) {
      const next = await customerDetailApi.addOnboardingModels(props.customerId, selected.value.map(item => ({
        standard_model_id: item.standard_model_id,
        resource_id: item.resource_id,
      })), opRecord.value);
      // 对齐原型「绑完即生效」：本次申请的模型一配齐，后端就自动完成「资源配置」节点，
      // 模型当场开通。没配齐才需要用户之后再去办（补齐剩下的模型，齐了同样会自动完成）。
      const configDone = next?.nodes?.some(node => node.key === "CONFIG" && node.status === "COMPLETED");
      ElMessage.success(configDone
        ? "本次申请的模型已全部配齐，资源配置已自动完成，模型已开通。"
        : "已加入本次开通；本次申请还有模型没配来源，全部配齐后会自动开通。");
      const refreshed = next?.nodes?.length ? next : await customerDetailApi.onboarding(props.customerId);
      emit("started", refreshed);
      return;
    }
    const flow = await customerDetailApi.startOnboarding(props.customerId, {
      first_order: firstOrder.value,
      contract_assignee_id: firstOrder.value ? Number(contractAssigneeId.value) : undefined,
      config_assignee_id: Number(configAssigneeId.value),
      models: selected.value.map(item => ({ standard_model_id: item.standard_model_id, resource_id: item.resource_id })),
      reason: reason.value.trim() || undefined,
    });
    ElMessage.success("开通流程已发起。");
    emit("started", flow);
  } catch (value) {
    error.value = message(value);
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  void loadPeople();
  void searchCandidates();
});
</script>

<template>
  <div class="start-form">
    <el-form label-position="top" :disabled="submitting">
      <el-alert
        v-if="appending"
        type="info"
        show-icon
        :closable="false"
        title="本次往进行中的开通流程里追加模型"
        description="选好来源后点「加入本次开通」；真正的开通以「开通流程 → 完成资源配置」为准。"
      />
      <div v-if="!appending" class="form-grid">
        <el-form-item label="是否首单">
          <el-switch v-model="firstOrder" active-text="首单（先合同授信后资源配置）" inactive-text="非首单（仅资源配置）" />
        </el-form-item>
        <el-form-item v-if="firstOrder" label="合同与授信办理人 *">
          <el-select v-model="contractAssigneeId" filterable placeholder="选择办理人" class="full">
            <el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" />
          </el-select>
        </el-form-item>
        <el-form-item label="资源配置办理人 *">
          <el-select v-model="configAssigneeId" filterable placeholder="选择办理人" class="full">
            <el-option v-for="person in people" :key="person.id" :label="person.name" :value="String(person.id)" />
          </el-select>
        </el-form-item>
        <el-form-item label="发起说明">
          <el-input v-model="reason" maxlength="200" show-word-limit placeholder="可选，落审计" />
        </el-form-item>
      </div>
    </el-form>

    <!-- 逐模型配源（原型行内「配置资源」）：一行一个模型，各选一条供应来源 -->
    <el-table
      v-if="presetScoped"
      v-loading="candidateLoading"
      :data="presetModels"
      size="small"
      class="candidate-table"
    >
      <el-table-column label="模型" min-width="180">
        <template #default="{ row }">
          <div class="cell-main">{{ row.model_name }}</div>
          <div class="cell-sub">标准模型 #{{ row.standard_model_id }}</div>
        </template>
      </el-table-column>
      <el-table-column label="供应来源" min-width="300">
        <template #default="{ row }">
          <el-select
            v-model="presetSelection[row.standard_model_id]"
            filterable
            clearable
            placeholder="选择供应来源"
            class="full"
            @change="onPresetSourceChange"
          >
            <el-option
              v-for="source in (sourcesByModel.get(row.standard_model_id) || [])"
              :key="source.resource_id"
              :label="`${source.supplier_name} · 系数 ${source.coefficient} · 资源 #${source.resource_id}`"
              :value="String(source.resource_id)"
            />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="节点" width="90">
        <template #default="{ row }">{{ (sourcesByModel.get(row.standard_model_id) || [])[0]?.region || region }}</template>
      </el-table-column>
      <template #empty>
        <el-empty description="这些模型没有可用的已准入来源（同部署节点、已准入才有）" :image-size="60" />
      </template>
    </el-table>

    <template v-if="!presetScoped">
    <div class="candidate-bar">
      <el-input v-model="keyword" placeholder="按模型名 / 供应商名搜索已准入来源" class="candidate-input" clearable @keyup.enter="searchCandidates" />
      <el-button :loading="candidateLoading" @click="searchCandidates">查询来源</el-button>
      <span class="hint">仅显示与本客户同部署节点（{{ region }}）且已准入的资源</span>
    </div>
    <el-alert v-if="candidateError" :title="candidateError" type="error" show-icon :closable="false" class="block-gap" />

    <el-table v-loading="candidateLoading" :data="candidates" size="small" class="candidate-table" empty-text="没有匹配的已准入来源">
      <el-table-column label="模型" min-width="180">
        <template #default="{ row }">
          <div class="cell-main">{{ row.model_name }}</div>
          <div class="cell-sub">标准模型 #{{ row.standard_model_id }}</div>
        </template>
      </el-table-column>
      <el-table-column label="供应商" min-width="150" prop="supplier_name" />
      <el-table-column label="节点" width="90" prop="region" />
      <el-table-column label="采购系数" width="110" align="right">
        <template #default="{ row }"><span class="mono">{{ row.coefficient }}</span></template>
      </el-table-column>
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :disabled="selectedKeys.has(String(row.resource_id))" @click="addCandidate(row)">加入</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="selected-block">
      <div class="selected-title">已选来源（{{ selected.length }}）</div>
      <div v-if="!selected.length" class="hint">还没有选择来源</div>
      <el-tag
        v-for="item in selected"
        :key="item.resource_id"
        class="selected-tag"
        closable
        @close="removeCandidate(item.resource_id)"
      >
        {{ item.model_name }} · {{ item.supplier_name }} · {{ item.region }}
      </el-tag>
    </div>
    </template>

    <!-- 操作登记（可选）：原型「配置资源」里那三个生产侧字段，只有追加来源时才需要 -->
    <el-collapse v-if="appending" class="op-record">
      <el-collapse-item name="op">
        <template #title>
          <span class="op-title">操作登记（可选）</span>
          <span class="hint">生产平台上真正的操作时间 / 人员 / 依据，填了会写进流程节点说明</span>
        </template>
        <el-form label-position="top">
          <div class="form-grid">
            <el-form-item label="实际操作时间">
              <el-date-picker
                v-model="opExecutedAt"
                type="datetime"
                placeholder="在生产平台上动手的时间"
                value-format="YYYY-MM-DD HH:mm"
                class="full"
              />
            </el-form-item>
            <el-form-item label="实际操作人员">
              <el-input v-model="opExecutedBy" maxlength="64" placeholder="去生产平台操作的人，可不等于登录人" />
            </el-form-item>
          </div>
          <el-form-item label="完成依据">
            <el-input
              v-model="opEvidence"
              type="textarea"
              :rows="2"
              maxlength="500"
              show-word-limit
              placeholder="工单号 / 变更单号 / 操作截图链接"
            />
          </el-form-item>
        </el-form>
      </el-collapse-item>
    </el-collapse>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="block-gap" />
    <div class="actions">
      <el-button type="primary" :loading="submitting" @click="submit">{{ appending ? "加入本次开通" : "发起开通" }}</el-button>
    </div>
  </div>
</template>

<style scoped>
.start-form { display: flex; flex-direction: column; gap: 12px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 20px; }
.full { width: 100%; }
.candidate-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.candidate-input { width: 320px; }
.candidate-table { width: 100%; }
.selected-block { border: 1px dashed var(--el-border-color); border-radius: 8px; padding: 12px 14px; }
.selected-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.selected-tag { margin: 0 8px 8px 0; }
.cell-main { font-weight: 600; }
.cell-sub { color: var(--app-text-muted); font-size: 12px; margin-top: 2px; }
.hint { color: var(--app-text-muted); font-size: 12px; }
.block-gap { margin-top: 4px; }
.op-record { border: 1px solid var(--el-border-color-lighter); border-radius: 8px; padding: 0 12px; }
.op-title { font-size: 13px; font-weight: 500; margin-right: 8px; }
.actions { display: flex; justify-content: flex-end; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
