<script setup lang="ts">
/**
 * 发起开通的表单（B3b 第 1 项 Tab2 与第 2 项向导 Step2 **共用**）。
 *
 * 为什么抽成组件：同一张表单要在两个地方出现——客户详情「档案与办理」Tab（已有客户补发起）
 * 与建客户向导 Step2（新建客户紧接着发起）。复制一份 = 两处校验规则迟早不一致。
 *
 * 幂等：这里只管收集与提交，`startOnboarding` 在 api 层按 scope + payload 生成幂等键，
 * 重复点击不会建出两条流程（后端还有 uk_flow_customer_processing 兜底）。
 */
import { computed, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { customerDetailApi, type OnboardingCandidate, type OnboardingFlowDTO } from "../../../api/customerDetail";
import { ApiError } from "../../../domain/common";
import { workbenchApi, type WorkbenchPersonDTO } from "../../../modules/workbench/api/workbench";

const props = defineProps<{ customerId: string; region: string }>();
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

const keyword = ref("");
const candidates = ref<OnboardingCandidate[]>([]);
const candidateLoading = ref(false);
const candidateError = ref("");
const selected = ref<OnboardingCandidate[]>([]);

const selectedKeys = computed(() => new Set(selected.value.map(item => String(item.resource_id))));

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
    const result = await customerDetailApi.onboardingCandidates(props.region, keyword.value.trim(), 1, 50);
    candidates.value = result.list;
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

function validate() {
  if (!positiveId(props.customerId)) return "客户 ID 非法，请刷新后重试。";
  if (!positiveId(configAssigneeId.value)) return "请选择资源配置办理人。";
  if (firstOrder.value && !positiveId(contractAssigneeId.value)) return "首单开通必须选择合同与授信办理人。";
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
      <div class="form-grid">
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

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="block-gap" />
    <div class="actions">
      <el-button type="primary" :loading="submitting" @click="submit">发起开通</el-button>
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
.actions { display: flex; justify-content: flex-end; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
