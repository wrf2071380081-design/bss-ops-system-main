<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { resourceOperationsApi } from "../../../api/resourceOperations";
import { canAdmitResource, canBindFormalConfig, type FormalConfigOption } from "../../../api/resourceOperations.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import type { ResourceRow } from "../../../api/resources.types";

const props = defineProps<{ resource: ResourceRow }>();
const emit = defineEmits<{ updated: [] }>();
const configs = ref<FormalConfigOption[]>([]), selectedConfigID = ref(""), loadingConfigs = ref(false), configError = ref("");
const bindReason = ref(""), bindBusy = ref(false), bindMessage = ref(""), bindError = ref("");
const externalConfirmed = ref(false), completionNote = ref(""), admissionReason = ref(""), admissionBusy = ref(false), admissionMessage = ref(""), admissionError = ref("");
const bindLifecycle = ref(crypto.randomUUID()), admissionLifecycle = ref(crypto.randomUUID());
const configDialogOpen = ref(false), configBusy = ref(false);
const editingConfig = ref<FormalConfigOption>();
const configLifecycle = ref(crypto.randomUUID());
const configForm = reactive({ scope: "", base_url: "", api_key: "", reason: "", externalConfirmed: false });
const bindable = computed(() => canBindFormalConfig(props.resource));
const admissible = computed(() => canAdmitResource(props.resource));
const eligibleConfigs = computed(() => configs.value.filter(config => config.supplier_id === props.resource.supplier_id && config.region === props.resource.region && config.status === "ACTIVE" && config.has_api_key && config.revision === config.confirmed_revision));
let active = true, configRequest: AbortController | undefined;

async function loadConfigs() {
  configRequest?.abort(); const request = configRequest = new AbortController();
  loadingConfigs.value = true; configError.value = "";
  try {
    const result = await resourceOperationsApi.formalConfigs(props.resource.supplier_id, props.resource.region, request.signal);
    if (!active || request.signal.aborted) return;
    configs.value = result;
    if (!eligibleConfigs.value.some(config => config.id === selectedConfigID.value)) selectedConfigID.value = eligibleConfigs.value[0]?.id || "";
  } catch (error) {
    if (active && !request.signal.aborted) { configs.value = []; selectedConfigID.value = ""; configError.value = error instanceof Error ? error.message : "正式配置暂时无法加载。"; }
  } finally { if (active && !request.signal.aborted) loadingConfigs.value = false; }
}

async function bindConfig() {
  if (bindBusy.value) return;
  const config = eligibleConfigs.value.find(item => item.id === selectedConfigID.value);
  if (!config) { bindError.value = "请选择同一供应商和区域中已核验的正式配置。"; return; }
  bindBusy.value = true; bindError.value = ""; bindMessage.value = "";
  try {
    await resourceOperationsApi.bindFormalConfig(props.resource, config, bindReason.value, bindLifecycle.value);
    if (!active) return;
    bindMessage.value = "正式配置已关联"; bindReason.value = ""; bindLifecycle.value = crypto.randomUUID();
    emit("updated");
  } catch (error) { if (active) bindError.value = error instanceof Error ? error.message : "配置关联失败，请重试。"; }
  finally { if (active) bindBusy.value = false; }
}

function openCreateConfig() {
  editingConfig.value = undefined;
  Object.assign(configForm, { scope: "", base_url: "", api_key: "", reason: "", externalConfirmed: false });
  configLifecycle.value = crypto.randomUUID();
  configError.value = "";
  configDialogOpen.value = true;
}
function openEditConfig(config: FormalConfigOption) {
  editingConfig.value = config;
  Object.assign(configForm, { scope: config.scope, base_url: config.base_url, api_key: "", reason: "", externalConfirmed: false });
  configLifecycle.value = crypto.randomUUID();
  configError.value = "";
  configDialogOpen.value = true;
}
async function saveFormalConfig() {
  if (configBusy.value) return;
  const scope = configForm.scope.trim(), baseURL = configForm.base_url.trim(), key = configForm.api_key.trim(), reason = configForm.reason.trim();
  if (!scope || !baseURL || !reason || !configForm.externalConfirmed || (!editingConfig.value && !key)) {
    configError.value = "请填写配置名称、URL、原因，并确认已完成外部平台操作；新建时还需填写 API Key。";
    return;
  }
  try {
    const url = new URL(baseURL);
    if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("invalid URL scheme");
  } catch { configError.value = "请输入有效的 HTTP 或 HTTPS 地址。"; return; }
  configBusy.value = true;
  configError.value = "";
  try {
    const input = { scope, base_url: baseURL, api_key: key, reason };
    const saved = editingConfig.value
      ? await resourceOperationsApi.updateFormalConfig(props.resource.supplier_id, editingConfig.value, input, configLifecycle.value)
      : await resourceOperationsApi.createFormalConfig(props.resource.supplier_id, input, configLifecycle.value);
    if (!active) return;
    selectedConfigID.value = saved.id;
    configDialogOpen.value = false;
    configForm.api_key = "";
    ElMessage.success(editingConfig.value ? "正式配置已更新" : "正式配置已创建");
    await loadConfigs();
  } catch (error) { if (active) configError.value = error instanceof Error ? error.message : "正式配置保存失败，请重试。"; }
  finally { if (active) configBusy.value = false; }
}
async function disableConfig(config: FormalConfigOption) {
  try {
    const { value } = await ElMessageBox.prompt("停用后不能再绑定新资源；已被资源引用时服务端会拒绝停用。", "停用正式配置", { inputPlaceholder: "操作原因", inputValidator: value => Boolean(value?.trim()) || "请填写停用原因", confirmButtonText: "确认停用", cancelButtonText: "取消", type: "warning" });
    await resourceOperationsApi.disableFormalConfig(props.resource.supplier_id, config, value, crypto.randomUUID());
    ElMessage.success("正式配置已停用");
    await loadConfigs();
  } catch (error) {
    if (error === "cancel" || error === "close") return;
    configError.value = error instanceof Error ? error.message : "正式配置停用失败，请重试。";
  }
}

async function admit() {
  if (admissionBusy.value) return;
  admissionBusy.value = true; admissionError.value = ""; admissionMessage.value = "";
  try {
    await resourceOperationsApi.admit(props.resource, { externalConfirmed: externalConfirmed.value, completionNote: completionNote.value, reason: admissionReason.value }, admissionLifecycle.value);
    if (!active) return;
    admissionMessage.value = "资源接入已登记"; externalConfirmed.value = false; completionNote.value = ""; admissionReason.value = ""; admissionLifecycle.value = crypto.randomUUID();
    emit("updated");
  } catch (error) { if (active) admissionError.value = error instanceof Error ? error.message : "资源接入登记失败，请重试。"; }
  finally { if (active) admissionBusy.value = false; }
}

onMounted(() => { if (bindable.value) void loadConfigs(); });
onBeforeUnmount(() => { active = false; configRequest?.abort(); });
</script>

<template>
  <section class="resource-operations">
    <p class="operation-context">{{ resource.supplier_name }} · {{ resource.model_name }} · {{ resourceRegionLabels[resource.region] }}</p>
    <el-alert type="info" :closable="false" title="服务端将按当前流程节点校验办理人；此页面不指定办理人。" />

    <section v-if="bindable" class="operation-card">
      <header class="config-heading"><h3>正式配置</h3><el-button size="small" @click="openCreateConfig">新建正式配置</el-button></header>
      <p class="muted">正式凭据与供应商申报测试 Key 分开维护；列表只显示安全摘要。</p>
      <el-alert v-if="configError" type="error" :closable="false" :title="configError"><el-button text @click="loadConfigs">重新加载配置</el-button></el-alert>
      <el-table :data="configs" row-key="id" empty-text="暂无正式配置" size="small">
        <el-table-column prop="scope" label="配置名称" min-width="120" />
        <el-table-column prop="base_url" label="服务地址" min-width="220" />
        <el-table-column label="状态 / 版本" min-width="130"><template #default="{ row }"><el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'">{{ row.status === 'ACTIVE' ? '有效' : '已停用' }}</el-tag><div class="muted">修订 {{ row.revision }}</div></template></el-table-column>
        <el-table-column label="凭据" width="90"><template #default="{ row }">{{ row.has_api_key ? '已设置' : '未设置' }}</template></el-table-column>
        <el-table-column label="操作" width="130"><template #default="{ row }"><el-button link type="primary" :disabled="row.status !== 'ACTIVE'" @click="openEditConfig(row)">修改</el-button><el-button link type="danger" :disabled="row.status !== 'ACTIVE'" @click="disableConfig(row)">停用</el-button></template></el-table-column>
      </el-table>
      <h3>关联正式配置</h3>
      <p class="muted">选择此供应商和区域下已人工确认、包含正式凭据的配置。测试凭据不会写入此关联。</p>
      <el-alert v-if="!loadingConfigs && !eligibleConfigs.length && !configError" type="warning" :closable="false" title="没有可关联的已确认配置，请先由有权限人员维护并确认正式配置。" />
      <el-form label-position="top" @submit.prevent="bindConfig">
        <el-form-item label="正式配置">
          <el-select v-model="selectedConfigID" aria-label="正式配置" placeholder="选择已确认的正式配置" :loading="loadingConfigs" :disabled="loadingConfigs || !eligibleConfigs.length || bindBusy">
            <el-option v-for="config in eligibleConfigs" :key="config.id" :label="`${config.scope} · ${config.base_url} · 修订 ${config.revision}`" :value="config.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="操作原因"><el-input v-model="bindReason" aria-label="操作原因" maxlength="300" show-word-limit :disabled="bindBusy" /></el-form-item>
      </el-form>
      <el-alert v-if="bindMessage" type="success" :closable="false" :title="bindMessage" />
      <el-alert v-if="bindError" type="error" :closable="false" :title="bindError"><el-button text @click="emit('updated')">刷新资源后重试</el-button></el-alert>
      <el-button type="primary" :loading="bindBusy" :disabled="loadingConfigs || !eligibleConfigs.length || !selectedConfigID || !bindReason.trim()" @click="bindConfig">关联正式配置</el-button>
    </section>

    <section v-if="admissible" class="operation-card">
      <h3>登记资源接入</h3>
      <p class="muted">仅当本资源自己的连通性和压力测试均通过、正式配置已关联时开放。页面记录外部实际操作依据，办理资格由服务端核验。</p>
      <el-checkbox v-model="externalConfirmed" :disabled="admissionBusy">已在{{ resourceRegionLabels[resource.region] }} B 端完成该模型实际配置</el-checkbox>
      <el-form label-position="top" @submit.prevent="admit">
        <el-form-item label="完成依据"><el-input v-model="completionNote" aria-label="完成依据" type="textarea" :rows="3" maxlength="500" show-word-limit :disabled="admissionBusy" /></el-form-item>
        <el-form-item label="登记原因"><el-input v-model="admissionReason" aria-label="登记原因" maxlength="300" show-word-limit :disabled="admissionBusy" /></el-form-item>
      </el-form>
      <el-alert v-if="admissionMessage" type="success" :closable="false" :title="admissionMessage" />
      <el-alert v-if="admissionError" type="error" :closable="false" :title="admissionError"><el-button text @click="emit('updated')">刷新资源后重试</el-button></el-alert>
      <el-button type="primary" :loading="admissionBusy" :disabled="admissionBusy || !externalConfirmed || !completionNote.trim() || !admissionReason.trim()" @click="admit">登记接入</el-button>
    </section>

    <el-alert v-if="!bindable && !admissible" type="info" :closable="false" title="当前资源状态没有待执行的配置关联或接入登记操作。" />
  </section>

  <el-dialog v-model="configDialogOpen" append-to-body :title="editingConfig ? '修改正式配置' : '新建正式配置'" width="min(560px, 94vw)" :close-on-click-modal="false">
    <el-alert title="此处提交的是正式环境凭据；测试凭据不会自动复制。页面不会回显已保存的 Key。" type="warning" :closable="false" />
    <el-alert v-if="configError" :title="configError" type="error" :closable="false" />
    <el-form label-position="top" :disabled="configBusy">
      <el-form-item label="配置名称 *"><el-input v-model="configForm.scope" maxlength="255" /></el-form-item>
      <el-form-item label="服务地址 *"><el-input v-model="configForm.base_url" placeholder="https://api.example.com" /></el-form-item>
      <el-form-item :label="editingConfig ? '轮换 API Key（留空保留）' : 'API Key *'"><el-input v-model="configForm.api_key" type="password" show-password autocomplete="new-password" /></el-form-item>
      <el-form-item label="操作原因 *"><el-input v-model="configForm.reason" type="textarea" :rows="2" maxlength="300" show-word-limit /></el-form-item>
      <el-checkbox v-model="configForm.externalConfirmed">已在外部平台完成对应配置</el-checkbox>
    </el-form>
    <template #footer><el-button :disabled="configBusy" @click="configDialogOpen = false">取消</el-button><el-button type="primary" :loading="configBusy" @click="saveFormalConfig">保存正式配置</el-button></template>
  </el-dialog>
</template>

<style scoped>
.resource-operations{display:grid;gap:18px;color:#1c1d1f;--el-color-primary:#315ee8}.operation-context,.muted{font-size:13px;line-height:1.75;color:#8a9099;margin:0}.operation-card{border:1px solid #e6e8ed;border-radius:8px;padding:18px}.operation-card h3{font-size:16px;margin:0 0 8px}.operation-card .el-form{margin-top:14px}.operation-card .el-select{width:100%}.operation-card .el-alert{margin:10px 0}.operation-card .el-checkbox{margin:10px 0}.operation-card>.el-button{margin-top:6px}
.config-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.config-heading h3{margin:0}
</style>
