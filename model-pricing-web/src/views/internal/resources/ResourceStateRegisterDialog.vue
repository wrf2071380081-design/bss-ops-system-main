<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { supplyWorkbenchApi } from "../../../api/supplyWorkbench";
import { canCreateStatePlan } from "../../../api/planCreate.contract";
import { quoteCandidatesApi } from "../../../api/quoteCandidates";
import { resourceRegionLabels } from "../../../api/resources.contract";
import type { ResourceRow } from "../../../api/resources.types";

// 原型（TokenOps 2026-11-04）模型广场详情页的「下架 / 上架」不是一个开关，
// 而是一张「确认下架登记」单：外部生产平台先操作，内部只登记结果并留痕。
// 平台底层的上下架仍是「计划 + 到点登记」两步，这里把两步合成一次点击：
// 建计划（预计时间=现在）→ 立即登记完成，界面上就是原型那一个弹窗。
const props = defineProps<{ rows: ResourceRow[]; kind: "down" | "up" }>();
const emit = defineEmits<{ close: []; done: [] }>();

const note = ref(""), confirmed = ref(false), busy = ref(false), error = ref("");
const affected = ref<Record<string, string[]>>({}), affectedLoading = ref(false);
const lifecycle = crypto.randomUUID();
let active = true;

const stateText = (state: string) =>
  state === "ADMITTED" ? "已接入" : state === "OFFLINE" ? "已下架" : state === "PENDING_ACCESS" ? "待接入" : state === "PENDING_TEST" ? "待测试" : state;
const title = props.kind === "down" ? "确认下架登记" : "确认上架登记";
const targetState = props.kind === "down" ? "已下架" : "已接入";
// 资源写权限是「归属采购人」制（后端 can_edit = buyer_id == 操作人）：
// 原型形态上没有这一层，所以这里不隐藏按钮，而是在弹窗里把原因说清楚。
const denied = computed(() => props.rows.filter(row => !canCreateStatePlan(row)));
const deniedText = computed(() => denied.value.map(row => row.supplier_name).join("、"));

async function loadAffected() {
  if (props.kind !== "down" || !props.rows.length) return;
  affectedLoading.value = true;
  try {
    const entries = await Promise.all(props.rows.map(async row => {
      try {
        const result = await quoteCandidatesApi.affectedCustomers(Number(row.id));
        return [row.id, result.list.map(customer => customer.customer_name)] as const;
      } catch { return [row.id, [] as string[]] as const; }
    }));
    if (active) affected.value = Object.fromEntries(entries);
  } finally { if (active) affectedLoading.value = false; }
}

async function submit() {
  if (!confirmed.value || busy.value) return;
  busy.value = true; error.value = "";
  // 备注照原型是选填；后端计划要求 reason 与完成依据必填，用同一句兜底，语义不丢。
  const fallback = props.kind === "down" ? "内部登记：外部平台已完成下架" : "内部登记：外部平台已完成上架";
  const reason = note.value.trim() || fallback;
  const failed: string[] = [];
  let done = 0;
  try {
    for (const row of props.rows) {
      if (!canCreateStatePlan(row)) { failed.push(`${row.supplier_name}：当前状态或权限不允许登记`); continue; }
      try {
        const created = await supplyWorkbenchApi.createPlan(row, reason, new Date().toISOString(), crypto.randomUUID());
        const plan = await supplyWorkbenchApi.plan(created.id);
        await supplyWorkbenchApi.executePlan(plan, true, reason, reason, crypto.randomUUID());
        done += 1;
      } catch (value) {
        failed.push(`${row.supplier_name}：${value instanceof Error ? value.message : "登记失败"}`);
      }
    }
  } finally { busy.value = false; }
  if (!active) return;
  if (failed.length) {
    error.value = `${done} 条已登记，${failed.length} 条未完成：${failed.join("；")}`;
    if (done) emit("done");
    return;
  }
  ElMessage.success(`${done} 条${props.kind === "down" ? "下架" : "上架"}登记完成`);
  emit("done");
}

function close() { if (!busy.value) emit("close"); }
onMounted(() => { void loadAffected(); });
onBeforeUnmount(() => { active = false; });
</script>

<template>
  <el-dialog :model-value="true" :title="title" width="min(720px, 94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="close">
    <p class="register-hint">内部登记：请先完成生产平台操作。本次确认仅记录内部结果，不操作真实生产平台；登记时间为系统时间，不代表外部生产操作时间。</p>
    <ul class="register-rows">
      <li v-for="row in rows" :key="row.id">
        <strong>{{ row.supplier_name }} / {{ row.model_name }}</strong>
        <span class="muted"> · {{ row.supplier_model_id }} · {{ resourceRegionLabels[row.region] }} · {{ stateText(row.state) }} → {{ targetState }}</span>
        <p v-if="kind === 'down'" v-loading="affectedLoading" class="affected">
          仍关联客户：{{ affected[row.id]?.length ? affected[row.id]!.join("、") : affectedLoading ? '读取中…' : '无' }}
        </p>
      </li>
    </ul>
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="本次登记备注"><el-input v-model="note" type="textarea" :rows="3" :disabled="busy" aria-label="本次登记备注" placeholder="可选：登记依据、工单号或其他可追溯信息" /></el-form-item>
    </el-form>
    <el-checkbox v-model="confirmed" :disabled="busy">确认已在对应生产平台完成本次操作，并登记内部结果</el-checkbox>
    <el-alert v-if="denied.length" class="register-error" type="warning" :closable="false" :title="`以下资源当前账号不是归属采购人，无法登记：${deniedText}`" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" class="register-error" />
    <template #footer>
      <el-button :disabled="busy" @click="close">关闭</el-button>
      <el-button type="primary" :loading="busy" :disabled="!confirmed || denied.length === rows.length" @click="submit">确认登记</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.register-hint { margin: 0 0 14px; line-height: 1.8; font-size: 13px; color: #8a9099; }
.register-rows { margin: 0 0 18px; padding: 0; list-style: none; }
.register-rows li { padding: 12px 14px; border: 1px solid #e6e8ed; border-radius: 6px; background: #f8f9fb; font-size: 13px; }
.register-rows li + li { margin-top: 8px; }
.affected { margin: 6px 0 0; font-size: 12px; color: #b87327; }
.register-error { margin-top: 12px; }
.muted { color: #8a9099; }
</style>
