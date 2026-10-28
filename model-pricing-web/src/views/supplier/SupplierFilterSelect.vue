<script setup lang="ts">
import { computed, ref } from "vue";

/**
 * 申报记录的筛选项（原型 vDropdown 的移植）。
 *
 * 原型是一个 details 折叠面板：标题写「维度名 + 全部/N 项 + ⌄」，
 * 展开后是带搜索的勾选框列表，**勾一下就立刻重查**，没有「查询」按钮。
 * Element Plus 的 el-select multiple 只显示对勾、不带勾选框，视觉上和原型差一截，
 * 所以这里用 popover + checkbox 自己拼一个。
 */
export interface FilterOption { value: string; label: string }

const props = defineProps<{
  label: string;
  options: FilterOption[];
  modelValue: string[];
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string[]] }>();

const search = ref("");
const visibleOptions = computed(() => {
  const keyword = search.value.trim().toLowerCase();
  if (!keyword) return props.options;
  return props.options.filter(item => item.label.toLowerCase().includes(keyword) || item.value.toLowerCase().includes(keyword));
});
const summary = computed(() => (props.modelValue.length ? `${props.modelValue.length} 项` : "全部"));

function toggle(value: string, checked: boolean | string | number) {
  const next = checked
    ? [...props.modelValue, value]
    : props.modelValue.filter(item => item !== value);
  emit("update:modelValue", Array.from(new Set(next)));
}
</script>

<template>
  <el-popover placement="bottom-start" :width="230" trigger="click" :disabled="disabled" popper-class="filter-popover">
    <template #reference>
      <button type="button" class="filter-trigger" :class="{ 'is-active': modelValue.length > 0, 'is-disabled': disabled }" :disabled="disabled">
        <span class="filter-trigger__label">{{ label }}</span>
        <strong class="filter-trigger__value">{{ summary }}</strong>
        <span class="filter-trigger__caret">⌄</span>
      </button>
    </template>
    <div class="filter-menu">
      <el-input v-model="search" size="small" placeholder="输入匹配已有选项" clearable />
      <div class="filter-options">
        <el-checkbox
          v-for="item in visibleOptions"
          :key="item.value"
          :model-value="modelValue.includes(item.value)"
          @change="toggle(item.value, $event)"
        >{{ item.label }}</el-checkbox>
      </div>
      <p v-if="!visibleOptions.length" class="filter-empty">没有匹配项</p>
    </div>
  </el-popover>
</template>

<style scoped>
.filter-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 132px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: #fff;
  color: var(--app-text);
  font-size: 12px;
  cursor: pointer;
}
.filter-trigger:hover { border-color: var(--app-primary, #409eff); }
.filter-trigger.is-active { border-color: var(--app-primary, #409eff); color: var(--app-primary, #409eff); }
.filter-trigger.is-disabled { cursor: not-allowed; opacity: .55; }
.filter-trigger__label { flex: 1; text-align: left; white-space: nowrap; }
.filter-trigger__value { font-weight: 600; }
.filter-trigger__caret { color: var(--app-text-muted); }
.filter-menu { display: grid; gap: 8px; }
.filter-options { display: grid; gap: 2px; max-height: 220px; overflow-y: auto; }
.filter-options :deep(.el-checkbox) { height: 26px; margin-right: 0; }
.filter-empty { margin: 0; color: var(--app-text-muted); font-size: 12px; text-align: center; }
</style>
