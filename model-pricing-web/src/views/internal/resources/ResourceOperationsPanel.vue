<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { resourceOperationsApi } from "../../../api/resourceOperations";
import { canAdmitResource, canBindFormalConfig, type FormalConfigOption } from "../../../api/resourceOperations.contract";
import { resourceRegionLabels } from "../../../api/resources.contract";
import type { ResourceRow } from "../../../api/resources.types";
import type { ResourceWorkflowActions } from "../../../api/resourceWorkflow.contract";

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
const workflow = ref<ResourceWorkflowActions>(), workflowError = ref(''), workflowBusy = ref(false);
const priceForm=reactive({source:'',confirmed:false,note:'',reason:''}), priceBusy=ref(false), priceError=ref(''), priceLifecycle=ref(crypto.randomUUID());
const evidenceReason=ref(''), evidenceBusy=ref(false), evidenceError=ref(''), evidenceLifecycle=ref(crypto.randomUUID());
async function refreshEvidence(){
 if(evidenceBusy.value||!workflow.value?.can_refresh_tests)return;
 evidenceBusy.value=true;evidenceError.value='';
 try{await resourceOperationsApi.refreshEvidence(props.resource,evidenceReason.value,evidenceLifecycle.value);if(active){evidenceLifecycle.value=crypto.randomUUID();evidenceReason.value='';ElMessage.success('已同步当前申报行的测试结果');emit('updated');}}
 catch(error){if(active)evidenceError.value=error instanceof Error?error.message:'测试同步失败，请重试。';}
 finally{if(active)evidenceBusy.value=false;}
}
let workflowRequest: AbortController | undefined;
async function loadWorkflow(){
 workflowRequest?.abort();const request=workflowRequest=new AbortController();workflowBusy.value=true;workflowError.value='';workflow.value=undefined;
 try {const result=await resourceOperationsApi.workflow(props.resource.id,request.signal);if(active&&!request.signal.aborted){workflow.value=result;if(!result.approved_prices.some(p=>p.source_row_id===priceForm.source))priceForm.source=result.approved_prices[0]?.source_row_id||'';}}
 catch(error){if(active&&!request.signal.aborted)workflowError.value=error instanceof Error?error.message:'当前办理信息不可用。';}
 finally{if(active&&!request.signal.aborted)workflowBusy.value=false;}
}
async function applyPrice(){
 if(priceBusy.value)return;
 const source=workflow.value?.approved_prices.find(p=>p.source_row_id===priceForm.source);if(!source){priceError.value='请刷新并选择已批准、分派给本人的改价申报。';return;}
 priceBusy.value=true;priceError.value='';
 try{await resourceOperationsApi.applyApprovedPrice(props.resource,source,priceForm.confirmed,priceForm.note,priceForm.reason,priceLifecycle.value);if(!active)return;ElMessage.success('批准改价已登记完成');Object.assign(priceForm,{source:'',confirmed:false,note:'',reason:''});priceLifecycle.value=crypto.randomUUID();emit('updated');void loadWorkflow();}
 catch(error){if(active)priceError.value=error instanceof Error?error.message:'改价应用失败，输入已保留。';}
 finally{if(active)priceBusy.value=false;}
}
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
  if (!workflow.value?.can_bind) {bindError.value='当前没有可办理的配置绑定任务，请刷新办理信息。';return;}
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
  if (!workflow.value?.can_admit) {admissionError.value='当前没有可办理的接入任务，请刷新办理信息。';return;}
  admissionBusy.value = true; admissionError.value = ""; admissionMessage.value = "";
  try {
    await resourceOperationsApi.admit(props.resource, { externalConfirmed: externalConfirmed.value, completionNote: completionNote.value, reason: admissionReason.value }, admissionLifecycle.value);
    if (!active) return;
    admissionMessage.value = "资源接入已登记"; externalConfirmed.value = false; completionNote.value = ""; admissionReason.value = ""; admissionLifecycle.value = crypto.randomUUID();
    emit("updated");
  } catch (error) { if (active) admissionError.value = error instanceof Error ? error.message : "资源接入登记失败，请重试。"; }
  finally { if (active) admissionBusy.value = false; }
}

onMounted(() => { void loadWorkflow(); if (bindable.value) void loadConfigs(); });
watch(() => props.resource.revision, () => { void loadWorkflow(); if (bindable.value) void loadConfigs(); });
onBeforeUnmount(() => { active = false; configRequest?.abort(); workflowRequest?.abort(); });
</script>

<template>
  <section class="resource-operations">
    <p class="operation-context">{{ resource.supplier_name }} · {{ resource.model_name }} · {{ resourceRegionLabels[resource.region] }}</p>
    <el-alert type="info" :closable="false" title="服务端将按当前流程节点校验办理人；此页面不指定办理人。" />
    <el-alert v-if="workflowError" type="warning" :closable="false" :title="workflowError"><el-button text :loading="workflowBusy" @click="loadWorkflow">刷新办理信息</el-button></el-alert>
    <section v-if="workflow?.can_refresh_tests" class="operation-card">
      <h3>同步办理测试结果</h3><p class="muted">读取此申报行指定的连通性、压力任务及报告。两项均通过且目标一致后，才可登记接入。</p>
      <el-input v-model="evidenceReason" aria-label="同步测试原因" placeholder="填写同步原因" maxlength="300" :disabled="evidenceBusy" />
      <el-alert v-if="evidenceError" type="error" :closable="false" :title="evidenceError" />
      <el-button :loading="evidenceBusy" :disabled="!evidenceReason.trim()" @click="refreshEvidence">同步测试结果</el-button>
    </section>

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
      <el-button type="primary" :loading="bindBusy" :disabled="!workflow?.can_bind || loadingConfigs || !eligibleConfigs.length || !selectedConfigID || !bindReason.trim()" @click="bindConfig">关联正式配置</el-button>
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
      <el-button type="primary" :loading="admissionBusy" :disabled="!workflow?.can_admit || admissionBusy || !externalConfirmed || !completionNote.trim() || !admissionReason.trim()" @click="admit">登记接入</el-button>
    </section>

    <section v-if="workflow?.approved_prices.length" class="operation-card">
      <h3>供应商批准改价</h3>
      <p class="muted">仅显示已批准并分派给本人的申报行；采购系数由批准内容确定。</p>
      <el-form label-position="top" :disabled="priceBusy" @submit.prevent="applyPrice">
        <el-form-item label="批准改价申报"><el-select v-model="priceForm.source" aria-label="批准改价申报"><el-option v-for="source in workflow.approved_prices" :key="source.source_row_id" :value="source.source_row_id" :label="`申报行 ${source.source_row_id} · 修订 ${source.source_revision} · 采购系数 ${source.coefficient}`" /></el-select></el-form-item>
        <el-checkbox v-model="priceForm.confirmed">已在外部平台完成批准的采购系数调整</el-checkbox>
        <el-form-item label="改价完成依据"><el-input v-model="priceForm.note" aria-label="改价完成依据" type="textarea" maxlength="500" /></el-form-item>
        <el-form-item label="改价登记原因"><el-input v-model="priceForm.reason" aria-label="改价登记原因" maxlength="300" /></el-form-item>
      </el-form>
      <el-alert v-if="priceError" type="error" :closable="false" :title="priceError"><el-button text @click="loadWorkflow();emit('updated')">刷新办理信息与资源</el-button></el-alert>
      <el-button type="primary" :loading="priceBusy" :disabled="!priceForm.source || !priceForm.confirmed || !priceForm.note.trim() || !priceForm.reason.trim()" @click="applyPrice">登记批准改价完成</el-button>
    </section>

    <el-alert v-if="!bindable && !admissible && workflow && !workflow.approved_prices.length" type="info" :closable="false" title="当前账号没有此资源待执行的配置关联、接入或批准改价任务。" />
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
