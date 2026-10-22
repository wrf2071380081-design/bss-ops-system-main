<script setup lang="ts">
import { dimensionLabels, taskLabels, unitLabels } from "../../../api/standardModels.contract";
import type { StandardPrice } from "../../../api/standardModels.types";
const rows = defineModel<StandardPrice[]>({ required: true });
defineProps<{ disabled: boolean }>();
function add() { rows.value.push({ item_key: '', label: '', task_type: 'TEXT_INPUT', amount: '', currency: 'USD', unit_code: 'TOKEN', unit_quantity: '', condition: '', dimensions: {}, source_url: '' }); }
function dimension(price: StandardPrice, key: string, value: string) { if (value.trim()) price.dimensions[key] = value; else delete price.dimensions[key]; }
</script>

<template>
  <section class="price-editor">
    <header><div><h3>官方价格 / 计费单位</h3><p>价格必须有币种、单位数量和适用条件；0 表示有效零价，留空表示尚未填写。</p></div><el-button v-if="!disabled" size="small" @click="add">添加价格项</el-button></header>
    <p v-if="!rows.length" class="hint">尚未填写官方价格。</p>
    <fieldset v-for="(price, index) in rows" :key="index" :disabled="disabled" class="price-row">
      <legend>价格项 {{ index + 1 }}</legend>
      <div class="price-grid">
        <label>价格项标识<el-input v-model="price.item_key" placeholder="如 text_input_standard" :disabled="disabled" /></label>
        <label>名称<el-input v-model="price.label" placeholder="例如：标准文本输入" :disabled="disabled" /></label>
        <label>计费任务<el-select v-model="price.task_type" :disabled="disabled"><el-option v-for="(text, key) in taskLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <label>金额<el-input v-model="price.amount" inputmode="decimal" placeholder="最多8位小数" :disabled="disabled" /></label>
        <label>币种<el-input v-model="price.currency" placeholder="USD / CNY" :disabled="disabled" /></label>
        <label>单位数量<el-input v-model="price.unit_quantity" inputmode="decimal" placeholder="例如 1000000" :disabled="disabled" /></label>
        <label>计费单位<el-select v-model="price.unit_code" :disabled="disabled"><el-option v-for="(text, key) in unitLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <label>适用条件<el-input v-model="price.condition" placeholder="例如：标准 API 请求" :disabled="disabled" /></label>
        <label class="wide">官方价格来源<el-input v-model="price.source_url" placeholder="https://…（可选）" :disabled="disabled" /></label>
      </div>
      <details><summary>补充计费维度（按适用条件选填）</summary><div class="price-grid"><label v-for="(text, key) in dimensionLabels" :key="key">{{ text }}<el-input :model-value="price.dimensions[key] || ''" :disabled="disabled" @update:model-value="(value: string) => dimension(price, key, value)" /></label></div></details>
      <el-button v-if="!disabled" size="small" @click="rows.splice(index, 1)">移除此价格项</el-button>
    </fieldset>
  </section>
</template>

<style scoped>
header{display:flex;justify-content:space-between;align-items:center;gap:16px}h3{font-size:15px}header p,.hint{color:#8a9099;font-size:13px}.price-row{border:1px solid #e6e8ed;border-radius:8px;padding:15px;margin:16px 0}.price-row legend{padding:0 6px;color:#32565b}.price-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px;margin:12px 0}.price-grid label{display:grid;gap:7px;font-size:13px;color:#526970}.wide{grid-column:1/-1}summary{cursor:pointer;color:#315ee8;font-size:13px;padding:10px 0}@media(max-width:760px){.price-grid{grid-template-columns:1fr 1fr}}@media(max-width:500px){.price-grid{grid-template-columns:1fr}}
</style>
