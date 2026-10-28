<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { dimensionLabels, taskLabels } from "../../../api/standardModels.contract";
import { parsePriceUnit, priceUnitText } from "../../../api/standardModelEditor";
import type { StandardPrice } from "../../../api/standardModels.types";
const rows = defineModel<StandardPrice[]>({ required: true });
const props = withDefaults(defineProps<{ disabled: boolean; modelType?: string; showExtras?: boolean; detailsOnly?: boolean }>(), { showExtras: true, detailsOnly: false });
const units = ref<string[]>([]), created = new Set<string>();
watch(() => rows.value, value => { units.value = value.map(priceUnitText); }, { immediate: true, flush: "sync" });
const dirty = computed(() => rows.value.some((price, index) => units.value[index] !== priceUnitText(price)));
function getRows() { return rows.value.map((price, index) => ({ ...price, ...(units.value[index] === priceUnitText(price) ? {} : parsePriceUnit(units.value[index] || "")), dimensions: { ...price.dimensions } })); }
function add() {
  let index = 1; while (rows.value.some(row => row.item_key === `price_${index}`)) index++;
  const key = `price_${index}`; created.add(key);
  rows.value.push({ item_key: key, label: "", task_type: `${props.modelType || "TEXT"}_INPUT`, amount: "", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "", dimensions: {}, source_url: "" });
  units.value.push("百万 tokens");
}
function remove(index: number) { rows.value.splice(index, 1); units.value.splice(index, 1); }
function labelChanged(price: StandardPrice) {
  if (!created.has(price.item_key)) return;
  const type = props.modelType || "TEXT";
  price.task_type = /缓存.*存储/.test(price.label) ? "CACHE_STORAGE" : /缓存/.test(price.label) ? "CACHE_INPUT" : /输出/.test(price.label) ? `${type}_OUTPUT` : /生成/.test(price.label) && type !== "TEXT" ? `${type}_GENERATION` : `${type}_INPUT`;
}
function dimension(price: StandardPrice, key: string, value: string) { if (value.trim()) price.dimensions[key] = value; else delete price.dimensions[key]; }
defineExpose({ getRows, dirty });
</script>
<template>
  <section class="price-editor">
    <div v-if="!detailsOnly" class="price-table-scroll" role="region" aria-label="官方价格与计费条件，可横向滚动" tabindex="0"><table class="price-table"><thead><tr><th>计费项</th><th>官方单价</th><th>币种</th><th>单位</th><th>适用条件</th><th></th></tr></thead><tbody>
      <tr v-for="(price, index) in rows" :key="price.item_key"><td><el-input v-model="price.label" :aria-label="`第${index + 1}项计费项`" placeholder="输入／输出／缓存" :disabled="disabled" @input="labelChanged(price)" /></td><td><el-input v-model="price.amount" :aria-label="`第${index + 1}项官方单价`" inputmode="decimal" placeholder="可填写0" :disabled="disabled" /></td><td><el-input v-model="price.currency" :aria-label="`第${index + 1}项币种`" placeholder="USD／CNY" :disabled="disabled" /></td><td><el-input v-model="units[index]" :aria-label="`第${index + 1}项单位`" placeholder="百万 tokens" :disabled="disabled" /></td><td><el-input v-model="price.condition" :aria-label="`第${index + 1}项适用条件`" placeholder="规格、长度或质量条件" :disabled="disabled" /></td><td><el-button v-if="!disabled" size="small" :aria-label="`移除第${index + 1}项计费项`" @click="remove(index)">移除</el-button></td></tr>
      <tr v-if="!rows.length"><td colspan="6" class="empty">尚未填写官方价格</td></tr>
    </tbody></table></div>
    <el-button v-if="!disabled && !detailsOnly" class="add-price" @click="add">＋ 增加计费项</el-button>
    <details v-if="showExtras && rows.length" :open="detailsOnly" class="price-extra"><summary>补充计费信息</summary><div v-for="(price, index) in rows" :key="price.item_key" class="price-extra-row"><strong>{{ price.label || `计费项 ${index + 1}` }}</strong><label>计费任务<el-select v-model="price.task_type" :disabled="disabled" :aria-label="`第${index + 1}项计费任务`"><el-option v-for="(text, key) in taskLabels" :key="key" :label="text" :value="key" /></el-select></label><label class="full">官方价格来源<el-input v-model="price.source_url" :aria-label="`第${index + 1}项价格来源`" placeholder="https://…（选填）" :disabled="disabled" /></label><label v-for="(text, key) in dimensionLabels" :key="key">{{ text }}<el-input :model-value="price.dimensions[key] || ''" :aria-label="`第${index + 1}项${text}`" :disabled="disabled" @update:model-value="(value: string) => dimension(price, key, value)" /></label></div></details>
  </section>
</template>
<style scoped>
.price-editor{width:100%;min-width:0}.price-table-scroll{overflow:auto;border:1px solid #e3e6eb;border-radius:6px}.price-table{width:100%;min-width:800px;border-collapse:collapse;table-layout:fixed}.price-table th,.price-table td{padding:8px;text-align:left;border-bottom:1px solid #eceef1}.price-table th{background:#f8f9fb;font-size:12px;color:#727781;font-weight:500}.price-table th:nth-child(1){width:125px}.price-table th:nth-child(2){width:110px}.price-table th:nth-child(3){width:85px}.price-table th:nth-child(4){width:155px}.price-table th:nth-child(5){width:220px}.price-table th:last-child{width:70px}.price-table .el-select{width:100%}.add-price{margin:8px 0}.empty{font-size:12px;text-align:center!important;color:#8b9098;padding:16px!important}.price-extra{font-size:12px;color:#8b9098;margin:4px 0}.price-extra summary{cursor:pointer}.price-extra-row{display:grid;grid-template-columns:1fr 1fr;gap:10px;padding:12px 0;border-bottom:1px solid #eceef1}.price-extra-row>strong,.price-extra-row>.full{grid-column:1/-1}.price-extra-row label{display:grid;gap:5px}@media(max-width:600px){.price-extra-row{grid-template-columns:1fr}}
.price-table-scroll{border:0;border-radius:0}.price-table{min-width:1010px}.price-table th,.price-table td{padding:10px}.price-table th{height:40px;font-size:12px;background:#f7f8fa}.price-table th:nth-child(1),.price-table th:nth-child(2),.price-table th:nth-child(3),.price-table th:nth-child(4){width:150px}.price-table th:nth-child(5){width:235px}.price-table th:last-child{width:60px}.price-table :deep(.el-input__wrapper),.price-table :deep(.el-select__wrapper){min-height:34px}.add-price{display:flex;width:100%;height:30px;margin:8px 0 0;font-size:12px;font-weight:400;color:#263b57;border-color:#e0e5ed;border-radius:5px}
.price-table-scroll::-webkit-scrollbar{height:12px}.price-table-scroll::-webkit-scrollbar-track{background:#f1f2f4}.price-table-scroll::-webkit-scrollbar-thumb{border:2px solid #f1f2f4;border-radius:8px;background:#969696}
</style>
