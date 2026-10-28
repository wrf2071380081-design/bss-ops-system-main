<script setup lang="ts">
import { computed } from "vue";
import { resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import { modelTypeLabels } from "../../../api/standardModels.contract";
import type { ResourceRow, ResourceFilterOptions } from "../../../api/resources.types";
import { rangeFields, rangeLabels, type SourceFilters } from "../../../domain/modelPlaza";
const filters = defineModel<SourceFilters>({ required: true });
const props = defineProps<{ rows: ResourceRow[]; metrics?: boolean; loading?: boolean; options?: ResourceFilterOptions }>();
const emit = defineEmits<{ apply: []; reset: []; search: [keyword: string] }>();
const companies = computed(() => (props.options?.companies || [...new Set(props.rows.map(row => row.company))]).filter(Boolean).sort());
const suppliers = computed(() => (props.options?.suppliers || [...new Map(props.rows.map(row => [row.supplier_id, { id: row.supplier_id, name: row.supplier_name, region: row.region }])).values()]).sort((a, b) => a.name.localeCompare(b.name, "zh-CN")));
const rangeText = (key: typeof rangeFields[number]) => filters.value[`${key}_min`] || filters.value[`${key}_max`] ? `${filters.value[`${key}_min`] || '不限'} ～ ${filters.value[`${key}_max`] || '不限'}` : "全部";
</script>
<template>
  <form class="source-filter-bar" @submit.prevent="emit('apply')">
    <label>供应商名称<el-select v-model="filters.supplier_ids" multiple filterable :remote="!!options" :remote-method="(value: string) => emit('search', value)" @visible-change="(visible: boolean) => visible && emit('search', '')" clearable collapse-tags collapse-tags-tooltip aria-label="供应商名称" placeholder="全部" @change="emit('apply')"><el-option v-for="supplier in suppliers" :key="supplier.id" :value="supplier.id" :label="`${supplier.name} · ${resourceRegionLabels[supplier.region]}`" /></el-select></label>
    <template v-if="!metrics">
      <label>模型公司<el-select v-model="filters.companies" multiple filterable :remote="!!options" :remote-method="(value: string) => emit('search', value)" @visible-change="(visible: boolean) => visible && emit('search', '')" clearable collapse-tags collapse-tags-tooltip aria-label="模型公司" placeholder="全部" @change="emit('apply')"><el-option v-for="company in companies" :key="company" :value="company" :label="company" /></el-select></label>
      <label>模型类型<el-select v-model="filters.model_types" multiple filterable :remote="!!options" :remote-method="(value: string) => emit('search', value)" @visible-change="(visible: boolean) => visible && emit('search', '')" clearable collapse-tags collapse-tags-tooltip aria-label="模型类型" placeholder="全部" @change="emit('apply')"><el-option v-for="(name, key) in modelTypeLabels" :key="key" :value="key" :label="name" /></el-select></label>
    </template>
    <label v-if="!metrics">部署节点<el-select v-model="filters.regions" multiple clearable collapse-tags aria-label="部署节点" placeholder="全部" @change="emit('apply')"><el-option v-for="(name, key) in resourceRegionLabels" :key="key" :value="key" :label="name" /></el-select></label>
    <template v-if="metrics">
      <el-popover v-for="key in rangeFields" :key="key" placement="bottom-start" :width="260" trigger="click">
        <template #reference><el-button class="range-button"><span>{{ rangeLabels[key] }}</span><strong>{{ rangeText(key) }}</strong><span>⌄</span></el-button></template>
        <div class="range-editor"><strong>{{ rangeLabels[key] }}</strong><div><el-input v-model="filters[`${key}_min`]" :aria-label="`${rangeLabels[key]}下限`" placeholder="下限" inputmode="decimal" /><span>～</span><el-input v-model="filters[`${key}_max`]" :aria-label="`${rangeLabels[key]}上限`" placeholder="上限" inputmode="decimal" /></div><el-button type="primary" size="small" @click="emit('apply')">应用区间</el-button></div>
      </el-popover>
    </template>
    <label v-if="metrics || filters.states.length">状态<el-select v-model="filters.states" multiple clearable collapse-tags aria-label="资源状态" placeholder="全部" @change="emit('apply')"><el-option v-for="(name, key) in resourceStateLabels" :key="key" :label="name" :value="key" /></el-select></label>
    <el-input v-model="filters.keyword" class="filter-search" :aria-label="metrics ? '搜索供应来源' : '搜索模型'" :placeholder="metrics ? '供应商名称 / 接口模型 ID' : '搜索模型名称 / 官方 ID / 供应商'" clearable @input="emit('apply')" />
    <el-button native-type="submit" :loading="loading">查询</el-button><el-button @click="emit('reset')">重置</el-button>
  </form>
</template>
<style scoped>
.source-filter-bar{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:14px 0}.source-filter-bar label{display:flex;align-items:center;gap:8px;white-space:nowrap;font-size:12px}.source-filter-bar .el-select{width:155px}.filter-search{width:250px;flex:1;min-width:210px}.range-button{height:32px;margin:0;gap:8px}.range-button strong{max-width:120px;overflow:hidden;text-overflow:ellipsis;font-weight:500;color:var(--el-text-color-regular)}.range-button:deep(>span){gap:8px}.range-editor{display:grid;gap:12px}.range-editor>div{display:flex;align-items:center;gap:8px}.range-editor>.el-button{justify-self:end}@media(max-width:700px){.source-filter-bar label{flex:1;min-width:170px}.source-filter-bar .el-select{flex:1;min-width:0}.filter-search{flex-basis:100%}}
</style>
