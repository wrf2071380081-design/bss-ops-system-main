<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO } from "../../../api/supplierEntities.contract";
import type { SupplierMergePreviewDTO, SupplierMergeReferencesDTO } from "../../../api/supplierMerge.contract";

const props = defineProps<{ modelValue: boolean; source?: SupplierEntityDTO }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();
const visible = computed({ get: () => props.modelValue, set: value => emit("update:modelValue", value) });
const sourceID = computed(() => props.source?.id || "");
const targetKeyword = ref("");
const targetID = ref("");
const targets = ref<SupplierEntityDTO[]>([]);
const searching = ref(false);
const loading = ref(false);
const error = ref("");
const preview = ref<SupplierMergePreviewDTO>();

const referenceRows: Array<{ key: keyof SupplierMergeReferencesDTO; label: string }> = [
  { key: "resources", label: "供应资源" },
  { key: "submissions", label: "申报记录" },
  { key: "pending_submissions", label: "待处理申报" },
  { key: "onboarding_flows", label: "办理流程" },
  { key: "contracts", label: "合同记录" },
  { key: "credit_lines", label: "授信记录" },
  { key: "formal_configs", label: "正式生产配置" },
  { key: "qualifications", label: "资质记录" },
  { key: "customer_relations", label: "关联客户" },
  { key: "test_credential_records", label: "测试凭据记录" },
  { key: "website_account_configured", label: "网站账号" },
  { key: "aliases", label: "曾用名" },
];
const regionText = (value: SupplierEntityDTO["region"]) => value === "CN" ? "国内" : "海外";
function referenceText(row: SupplierMergeReferencesDTO, key: keyof SupplierMergeReferencesDTO) {
  const value = row[key];
  if (key === "website_account_configured") return value ? "已配置" : "未配置";
  if (key === "aliases" && value === null) return "结构未交付";
  return String(value);
}

async function searchTargets() {
  searching.value = true;
  error.value = "";
  try {
    const result = await supplierEntitiesApi.list({ page: 1, size: 100, keyword: targetKeyword.value.trim() || undefined });
    targets.value = result.list.filter(item => item.id !== sourceID.value);
    if (!targets.value.some(item => item.id === targetID.value)) targetID.value = "";
  } catch (value) {
    error.value = value instanceof Error ? value.message : "目标供应商查询失败。";
    targets.value = [];
  } finally { searching.value = false; }
}

async function buildPreview() {
  if (!sourceID.value || !targetID.value || sourceID.value === targetID.value) return;
  loading.value = true;
  error.value = "";
  preview.value = undefined;
  try { preview.value = await supplierEntitiesApi.mergePreview(sourceID.value, targetID.value); }
  catch (value) { error.value = value instanceof Error ? value.message : "合并影响预览失败。"; }
  finally { loading.value = false; }
}

watch(() => props.modelValue, open => {
  if (!open) return;
  targetKeyword.value = "";
  targetID.value = "";
  preview.value = undefined;
  error.value = "";
  void searchTargets();
});
</script>

<template>
  <el-dialog v-model="visible" title="合并预览" width="min(980px, 96vw)" destroy-on-close>
    <el-alert title="只读影响预览，不会执行合并。跨区域、资源、办理、配置、合同授信及客户关系处置策略尚未确认。" type="warning" show-icon :closable="false" />
    <section class="merge-target-picker">
      <el-input v-model="targetKeyword" aria-label="目标供应商搜索" clearable placeholder="按供应商名称搜索" @keyup.enter="searchTargets" />
      <el-button :loading="searching" @click="searchTargets">查找目标</el-button>
      <el-select v-model="targetID" aria-label="目标供应商" filterable placeholder="选择保留的目标供应商" :disabled="searching || !targets.length">
        <el-option v-for="item in targets" :key="item.id" :label="`${item.name} · ID ${item.id}`" :value="item.id" />
      </el-select>
      <el-button type="primary" :loading="loading" :disabled="!targetID || targetID === sourceID" @click="buildPreview">生成预览</el-button>
    </section>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-empty v-if="!targets.length && !searching && !error" description="没有可选的其他供应商主体" :image-size="64" />
    <div v-if="preview" v-loading="loading" class="merge-preview-result">
      <div class="merge-parties">
        <article><span>来源主体</span><strong>{{ preview.source.name }}</strong><small>ID {{ preview.source.id }} · {{ regionText(preview.source.region) }} · 采购人 {{ preview.source.buyer_id }} · 修订 {{ preview.source.revision }}</small></article>
        <article><span>目标主体（保留）</span><strong>{{ preview.target.name }}</strong><small>ID {{ preview.target.id }} · {{ regionText(preview.target.region) }} · 采购人 {{ preview.target.buyer_id }} · 修订 {{ preview.target.revision }}</small></article>
      </div>
      <h3>关联记录数量</h3>
      <el-table :data="referenceRows" size="small" row-key="key">
        <el-table-column prop="label" label="业务记录" min-width="170" />
        <el-table-column label="来源主体" min-width="120"><template #default="{ row }">{{ referenceText(preview.source_references, row.key) }}</template></el-table-column>
        <el-table-column label="目标主体" min-width="120"><template #default="{ row }">{{ referenceText(preview.target_references, row.key) }}</template></el-table-column>
      </el-table>
      <el-alert v-if="preview.duplicate_resource_keys" :title="`发现 ${preview.duplicate_resource_keys} 组相同模型与区域的重复资源，需先确定保留规则。`" type="error" show-icon :closable="false" />
      <section class="merge-review-items">
        <h3>需要人工决策</h3>
        <el-empty v-if="!preview.review_items.length && !preview.duplicate_resource_keys" description="未发现自动识别的冲突；仍需按业务规则确认是否允许合并。" :image-size="48" />
        <ul v-else>
          <li v-for="item in preview.review_items" :key="item.code"><strong>{{ item.title }}</strong><span>来源 {{ item.source_count }} · 目标 {{ item.target_count }}</span></li>
        </ul>
      </section>
      <p class="merge-readonly-note">该预览不代表合并获准。当前没有合并执行入口，刷新后需要重新生成最新预览。</p>
    </div>
    <template #footer><el-button @click="visible = false">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.merge-target-picker{display:grid;grid-template-columns:minmax(180px,1fr) auto minmax(220px,1fr) auto;gap:10px;align-items:center;margin:18px 0}.merge-preview-result{margin-top:20px}.merge-parties{display:grid;grid-template-columns:1fr 1fr;gap:12px}.merge-parties article{display:grid;gap:6px;padding:14px;border:1px solid var(--el-border-color);border-radius:8px}.merge-parties span,.merge-parties small,.merge-readonly-note{color:var(--app-text-muted)}.merge-parties strong{font-size:16px}.merge-parties small{overflow-wrap:anywhere}.merge-preview-result h3{margin:20px 0 10px;font-size:15px}.merge-review-items ul{padding-left:20px}.merge-review-items li{display:flex;justify-content:space-between;gap:12px;padding:6px 0}.merge-review-items li span{color:var(--app-text-muted)}.merge-readonly-note{font-size:12px;margin-top:14px}@media(max-width:680px){.merge-target-picker{grid-template-columns:1fr 1fr}.merge-target-picker .el-select{grid-column:1/-1}.merge-parties{grid-template-columns:1fr}}
</style>
