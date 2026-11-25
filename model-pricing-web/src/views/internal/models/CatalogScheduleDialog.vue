<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { agentCatalog, catalogVendors, type Plan, type Source } from "../../../api/agentCatalog";
import { formatDateTime } from "../../../domain/date";

const emit = defineEmits<{ close: []; review: [sources: Source[]] }>();
const plans = ref<Plan[]>([]);
const canManage = ref(false), busy = ref(false), error = ref(""), notice = ref("");
const editing = ref(false);
const request = new AbortController();
const repeats = { daily: "每天", weekly: "每周", biweekly: "每双周", monthly: "每月", yearly: "每年", interval: "按间隔", once: "单次" };
const weekdays = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];
const form = reactive({ id: "", revision: "0", frequency: "daily", time: "09:00", weekday: "1", monthday: "1",
  minutes: "60", date: "", validity: "forever", validFrom: "", validUntil: "", active: "on" });
async function load() {
  try {
    const result = await agentCatalog.plans(request.signal);
    plans.value = result.plans;
    canManage.value = result.canManage;
  } catch (cause) {
    if (!request.signal.aborted) error.value = cause instanceof Error ? cause.message : "暂时无法读取计划。";
  }
}
function edit(plan?: Plan) {
  const [frequency = "daily", arg = ""] = plan?.recurrence.split(":") ?? [];
  Object.assign(form, { id: plan?.id ?? "", revision:String(plan?.revision??0), frequency, time: plan?.localTime ?? "09:00",
    weekday: ["weekly", "biweekly"].includes(frequency) ? arg : "1",
    monthday: frequency === "monthly" ? arg : "1", minutes: frequency === "interval" ? arg : "60",
    date: frequency === "yearly" ? "2000-" + arg : frequency === "once" ? arg : "",
    validity: plan?.validUntil ? "range" : "forever", validFrom: plan?.validFrom ?? "", validUntil: plan?.validUntil ?? "",
    active: plan?.active === false ? "off" : "on" });
  editing.value = true;
  error.value = notice.value = "";
}
function cadence(plan: Plan) {
  const [rule, value] = plan.recurrence.split(":");
  const label = repeats[rule as keyof typeof repeats] ?? rule;
  if (rule === "interval") return "每 " + value + " 分钟";
  const day = ["weekly", "biweekly"].includes(rule) ? weekdays[Number(value) - 1] : rule === "monthly" ? value + " 日" : value;
  return [label, day, plan.localTime].filter(Boolean).join(" ");
}
async function save() {
  if (busy.value || !canManage.value) return;
  error.value = notice.value = "";
  if (form.validity === "range" && (!form.validFrom || !form.validUntil || form.validFrom > form.validUntil)) {
    error.value = "请填写有效的开始和结束日期。";
    return;
  }
  busy.value = true;
  try {
    await agentCatalog.savePlan({ operation: "platform", ...form });
    editing.value = false;
    notice.value = "计划已保存。";
    await load();
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "暂时无法保存计划。";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => request.abort());
</script>

<template>
  <el-dialog :model-value="true" :title="editing ? (form.id ? '编辑抓取计划' : '新建抓取计划') : '官方模型定时抓取'" width="min(760px, 94vw)"
    :close-on-click-modal="!editing" :show-close="!busy" :close-on-press-escape="!busy" @close="emit('close')">
    <div class="schedule-head"><span>北京时间 · 每次抓取全部六家厂商</span><el-button v-if="canManage && !editing" @click="edit()">新建计划</el-button></div>
    <p v-if="editing && form.id" class="edit-summary">正在修改现有计划，保存后按新的时间安排执行。</p>
    <el-alert v-if="error || notice" :title="error || notice" :type="error ? 'error' : 'success'" :closable="false" show-icon />
    <form v-if="editing" class="plan-form" @submit.prevent="save"><fieldset :disabled="busy" class="plan-fields">
      <label>重复方式<select v-model="form.frequency"><option v-for="(name, key) in repeats" :key="key" :value="key">{{ name }}</option></select></label>
      <label v-if="form.frequency !== 'interval'">执行时间<input v-model="form.time" type="time" required /></label>
      <label v-if="['weekly','biweekly'].includes(form.frequency)">星期<select v-model="form.weekday"><option v-for="(day, i) in weekdays" :key="day" :value="String(i + 1)">{{ day }}</option></select></label>
      <label v-if="form.frequency === 'monthly'">每月日期<select v-model="form.monthday"><option v-for="day in 31" :key="day" :value="String(day)">{{ day }} 日</option></select></label>
      <label v-if="['once','yearly'].includes(form.frequency)">{{ form.frequency === 'yearly' ? '选择月日（按年重复）' : '执行日期' }}<input v-model="form.date" type="date" required /></label>
      <label v-if="form.frequency === 'interval'">每隔多少分钟<input v-model="form.minutes" type="number" min="1" max="9999" required /></label>
      <label>有效期<select v-model="form.validity"><option value="forever">长期有效</option><option value="range">指定日期范围</option></select></label>
      <template v-if="form.validity === 'range'"><label>开始日期<input v-model="form.validFrom" type="date" required /></label><label>结束日期<input v-model="form.validUntil" type="date" required /></label></template>
      <label class="plan-enabled"><input v-model="form.active" type="checkbox" true-value="on" false-value="off" />启用计划</label>
      </fieldset><div class="plan-actions"><el-button :disabled="busy" @click="editing = false">返回计划列表</el-button><el-button type="primary" native-type="submit" :loading="busy">{{ form.id ? '保存修改' : '创建计划' }}</el-button></div>
    </form>
    <template v-if="!editing"><p v-if="!plans.length && !error">暂无平台抓取计划。</p>
    <article v-for="plan in plans" :key="plan.id" class="plan-row">
      <div class="schedule-head"><div><strong>{{ cadence(plan) }}</strong><span> · {{ plan.active ? '已启用' : '已暂停' }}</span>
        <div>{{ plan.validUntil ? plan.validFrom + ' 至 ' + plan.validUntil : '长期有效' }}</div></div>
        <el-button v-if="canManage" link type="primary" @click="edit(plan)">编辑</el-button></div>
      <details v-if="plan.history.length"><summary>最近执行</summary>
        <div v-for="item in plan.history" :key="item.run" class="schedule-head run-row">
          <span>{{ catalogVendors[item.vendor] }} · {{ formatDateTime(item.at, '时间未知', false) }}</span>
          <el-button link type="primary" @click="emit('review', [{ vendor: item.vendor, runId: item.run }])">查看采集</el-button>
        </div>
      </details>
    </article>
    </template>
  </el-dialog>
</template>

<style scoped>
.plan-fields{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:16px;border:0;margin:0;padding:0;grid-column:1/-1;min-width:0}.plan-actions{grid-column:1/-1;display:flex;justify-content:flex-end;padding-top:12px;border-top:1px solid #e1e7ea}.edit-summary{color:#69757d;font-size:13px}
.schedule-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px}.plan-form{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;padding:16px;background:#f4f8f8;border-radius:8px;margin:12px 0}.plan-form label{display:grid;gap:6px}.plan-form input:not([type=checkbox]),.plan-form select{border:1px solid #d9e1e5;border-radius:5px;padding:8px;background:white;min-width:0}.plan-form .plan-enabled{display:flex;align-items:center}.plan-row{padding:14px 0;border-bottom:1px solid #e8edef}.run-row{margin:8px 0 0}summary{cursor:pointer}
</style>
