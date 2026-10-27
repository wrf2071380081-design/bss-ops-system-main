<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierTestConfigDTO } from "../../../api/supplierEntities.contract";

const props = defineProps<{ supplierID: string; canEdit: boolean }>();
const config = ref<SupplierTestConfigDTO>();
const baseURL = ref("");
const apiKey = ref("");
const reason = ref("维护供应商测试配置");
const loading = ref(true), saving = ref(false), available = ref(false), error = ref("");
let request: AbortController | undefined;
let sequence = 0;
let saveSequence = 0;

function lifecycle() {
  return `supplier-test-config:${props.supplierID}:${globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`}`;
}

async function load() {
  request?.abort();
  const current = request = new AbortController();
  const currentSequence = ++sequence;
  loading.value = true; available.value = false; error.value = ""; apiKey.value = "";
  try {
    const value = await supplierEntitiesApi.testConfig(props.supplierID, current.signal);
    if (currentSequence !== sequence || current.signal.aborted) return;
    config.value = value; baseURL.value = value.base_url; apiKey.value = ""; available.value = true;
  } catch (value) {
    if (currentSequence === sequence && !current.signal.aborted) error.value = value instanceof Error ? value.message : "Inspector供应商测试配置暂不可用。";
  } finally { if (currentSequence === sequence && !current.signal.aborted) loading.value = false; }
}

async function save() {
  if (!available.value || !config.value || !props.canEdit || saving.value) return;
  if (!baseURL.value.trim() || !reason.value.trim()) { error.value = "请填写测试URL和操作说明。"; return; }
  if (!config.value.has_api_key && !apiKey.value) { error.value = "首次配置必须填写供应商测试Key。"; return; }
  const supplierID = props.supplierID;
  const currentSequence = sequence;
  const currentSaveSequence = ++saveSequence;
  saving.value = true; error.value = "";
  try {
    const saved = await supplierEntitiesApi.saveTestConfig(supplierID, {
      baseURL: baseURL.value, apiKey: apiKey.value, expectedRevision: config.value.revision, reason: reason.value,
    }, lifecycle());
    if (currentSequence !== sequence || supplierID !== props.supplierID) return;
    config.value = saved;
    baseURL.value = config.value.base_url; apiKey.value = ""; reason.value = "维护供应商测试配置";
  } catch (value) {
    if (currentSequence !== sequence || supplierID !== props.supplierID) return;
    error.value = value instanceof Error ? value.message : "配置未保存，请保留当前输入并重试。";
  } finally {
    if (currentSaveSequence === saveSequence && supplierID === props.supplierID) saving.value = false;
  }
}

onMounted(() => void load());
watch(() => props.supplierID, () => {
  ++saveSequence;
  saving.value = false;
  config.value = undefined;
  apiKey.value = "";
  void load();
});
onBeforeUnmount(() => { ++sequence; ++saveSequence; request?.abort(); apiKey.value = ""; });
</script>

<template>
  <section class="supplier-test-config" data-testid="supplier-test-config">
    <div class="config-heading"><div><h3>供应商测试配置</h3><p>供应商自测使用供应商自己的 URL、Key、池子与额度；不会用于内部平台测试。</p></div><el-button size="small" :loading="loading" :disabled="saving" @click="load">刷新</el-button></div>
    <el-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <el-skeleton v-if="loading && !available" :rows="2" animated />
    <template v-else-if="available && config">
      <el-alert :type="config.has_api_key ? 'success' : 'warning'" :title="config.has_api_key ? `已关联 Inspector 配置 · 版本 ${config.revision}` : '尚未保存供应商测试配置'" :closable="false" />
      <el-form label-position="top" class="config-form">
        <el-form-item label="供应商测试 URL"><el-input v-model="baseURL" :disabled="!canEdit || saving" maxlength="2048" placeholder="https://provider.example/v1" /></el-form-item>
        <el-form-item label="供应商测试 Key">
          <el-input v-model="apiKey" type="password" show-password autocomplete="new-password" :disabled="!canEdit || saving" maxlength="4096" :placeholder="config.has_api_key ? '留空表示保留 Inspector 已保存的 Key' : '首次配置请输入 Key'" />
        </el-form-item>
        <el-form-item label="操作说明"><el-input v-model="reason" :disabled="!canEdit || saving" maxlength="500" /></el-form-item>
        <div class="config-footer"><span class="muted">Key 不会从接口读取；保存后仅显示配置状态。内部测试使用平台独立配置。</span><el-button v-if="canEdit" type="primary" :loading="saving" :disabled="loading" @click="save">保存测试配置</el-button></div>
      </el-form>
    </template>
    <el-alert v-else-if="!loading && !error" type="info" title="尚未取得测试配置。页面不会保存或发送 Key，请刷新重试或确认 Inspector 提供方状态。" :closable="false" />
  </section>
</template>

<style scoped>
.supplier-test-config{margin-top:20px;padding-top:16px;border-top:1px solid var(--app-border,#e5e7eb)}.config-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:12px}.config-heading h3{margin:0 0 4px;font-size:15px}.config-heading p,.muted{margin:0;color:var(--app-text-muted,#6b7280);font-size:12px;line-height:1.5}.config-form{margin-top:12px;max-width:720px}.config-footer{display:flex;align-items:center;justify-content:space-between;gap:16px}
</style>
