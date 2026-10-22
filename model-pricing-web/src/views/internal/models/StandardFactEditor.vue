<script setup lang="ts">
import type { StandardFact } from "../../../api/standardModels.types";
import { factLabels } from "../../../api/standardModels.contract";
const rows = defineModel<StandardFact[]>({ required: true });
defineProps<{ title: string; disabled: boolean }>();
</script>

<template>
  <section class="fact-editor">
    <div class="fact-heading"><h3>{{ title }}</h3><el-button v-if="!disabled" size="small" @click="rows.push({ key: '', disclosure: 'UNVERIFIED', value: '' })">添加资料项</el-button></div>
    <p v-if="!rows.length" class="hint">尚未补充。可先保存，补齐并人工核验后才能用于报价。</p>
    <div v-for="(row, index) in rows" :key="index" class="fact-row">
      <el-input v-model="row.key" :aria-label="`${title}第${index + 1}项标识`" placeholder="项目标识，如 context_window" :disabled="disabled" />
      <el-select v-model="row.disclosure" :aria-label="`${title}第${index + 1}项披露状态`" :disabled="disabled"><el-option v-for="(label, key) in factLabels" :key="key" :label="label" :value="key" /></el-select>
      <el-input v-model="row.value" :aria-label="`${title}第${index + 1}项内容`" placeholder="规格、单位或访问条件" :disabled="disabled" />
      <el-button v-if="!disabled" size="small" @click="rows.splice(index, 1)">移除</el-button>
    </div>
  </section>
</template>

<style scoped>
.fact-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.fact-heading h3{font-size:15px}.fact-row{display:grid;grid-template-columns:minmax(140px,1fr) 140px minmax(180px,2fr) auto;gap:10px;margin:10px 0}.hint{color:#8a9099;font-size:13px}@media(max-width:700px){.fact-row{grid-template-columns:1fr 1fr}.fact-row>:nth-child(3){grid-column:1/-1}}
</style>
