<script setup lang="ts">
import { computed, ref } from "vue";
const selected = defineModel<string[]>({ required: true });
const props = defineProps<{ label: string; options: { label: string; value: string }[]; loading?: boolean; remote?: boolean }>();
const emit = defineEmits<{ change: []; search: [keyword: string] }>();
const search = ref("");
const options = computed(() => props.remote ? props.options : props.options.filter(item => item.label.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())));
function input(value: string) { if (props.remote) emit("search", value); }
</script>
<template>
  <el-popover placement="bottom-start" :width="250" trigger="click" @show="search = ''; remote && emit('search', '')">
    <template #reference><button type="button" class="catalog-select" :class="{ selected: selected.length }" :aria-label="label"><span>{{ label }}</span><strong>{{ selected.length ? `${selected.length} 项` : '全部' }}</strong><span class="caret">⌄</span></button></template>
    <div class="catalog-select-menu" v-loading="loading"><el-input v-model="search" :aria-label="`搜索${label}`" placeholder="输入匹配已有选项" clearable @input="input" /><el-checkbox-group v-model="selected" :aria-label="label" @change="emit('change')"><el-checkbox v-for="item in options" :key="item.value" :value="item.value">{{ item.label }}</el-checkbox></el-checkbox-group><p v-if="!loading && !options.length">没有匹配项</p></div>
  </el-popover>
</template>
<style scoped>
.catalog-select{display:flex;align-items:center;gap:9px;justify-content:space-between;width:158px;height:34px;padding:7px 10px;background:white;border:1px solid #e3e6eb;border-radius:6px;color:#5c6169;font-size:12px;cursor:pointer}.catalog-select strong{font-size:11px;font-weight:400;color:#92979f;margin-left:auto}.catalog-select.selected{border-color:#bfcdf6;background:#f4f7ff}.catalog-select.selected strong{color:#315ee8}.catalog-select:focus-visible{outline:2px solid #315ee8;outline-offset:2px}.caret{font-size:11px;color:#9a9ea5}.catalog-select-menu .el-checkbox-group{display:flex;flex-direction:column;max-height:230px;overflow:auto;margin-top:6px}.catalog-select-menu .el-checkbox{margin:0;padding:0 8px;min-height:32px;flex-shrink:0}.catalog-select-menu .el-checkbox:hover{background:#f8f9fb}.catalog-select-menu p{font-size:12px;color:#92979f;padding:8px}
</style>
