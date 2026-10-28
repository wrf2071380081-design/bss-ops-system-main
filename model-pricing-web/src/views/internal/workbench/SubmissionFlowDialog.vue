<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue';
import {useRouter} from 'vue-router';
import {ElMessage} from 'element-plus';
import {submissionFlowsApi,type SubmissionFlowView} from '../../../api/submissionFlows';
import {workbenchApi,type WorkbenchPersonDTO} from '../../../modules/workbench/api/workbench';
import F12SubjectPanel from '../subjects/F12SubjectPanel.vue';
const props=defineProps<{id:string;node?:boolean}>();const emit=defineEmits<{close:[];changed:[]}>();const router=useRouter();
const data=ref<SubmissionFlowView>(),people=ref<WorkbenchPersonDTO[]>([]),loading=ref(false),busy=ref(false),error=ref('');
const test=ref(''),contract=ref(''),reason=ref(''),lifecycle=ref(crypto.randomUUID());let active=true,request:AbortController|undefined;
const contractReady=ref(false);
const selectedNode=computed(()=>data.value?.nodes?.find(n=>n.id===data.value?.selected_node_id));
const isContract=computed(()=>selectedNode.value?.key==='CONTRACT');
const title=computed(()=>isContract.value?'合同与授信办理':props.node?'测试与资源配置办理':'申报审批与分派');
const flowContext=computed(()=>{const v=data.value;if(!v?.flow_id||!v.flow_revision)return undefined;const revision=Number(v.flow_revision);return Number.isSafeInteger(revision)&&revision>0&&revision<=2147483647?{flowID:v.flow_id,moduleKey:'CONTRACT',expectedRevision:revision}:undefined;});
const nodeNames:Record<string,string>={CONFIG:'测试与资源配置',CONTRACT:'合同与授信'};
const nodeStates:Record<string,string>={IN_PROGRESS:'办理中',WAITING_PREDECESSOR:'等待前序办理',COMPLETED:'已完成'};
async function load(){request?.abort();const current=request=new AbortController();loading.value=true;error.value='';
 try{const [view,staff]=await Promise.all([submissionFlowsApi.get(props.id,props.node,current.signal),workbenchApi.people()]);if(active&&!current.signal.aborted){data.value=view;people.value=staff;}}
 catch(e){if(active&&!current.signal.aborted)error.value=e instanceof Error?e.message:'办理信息加载失败。';}finally{if(active&&!current.signal.aborted)loading.value=false;}}
async function approve(){if(!data.value||busy.value)return;busy.value=true;error.value='';try{await submissionFlowsApi.approve(data.value,test.value,contract.value,reason.value,lifecycle.value);if(!active)return;ElMessage.success('审批通过，已分派办理任务');lifecycle.value=crypto.randomUUID();emit('changed');await load();}catch(e){if(active)error.value=e instanceof Error?e.message:'审批失败，输入已保留，请刷新后核对。';}finally{if(active)busy.value=false;}}
async function reject(){if(!data.value||busy.value)return;busy.value=true;error.value='';try{await submissionFlowsApi.reject(data.value,reason.value,lifecycle.value);if(!active)return;ElMessage.success('已驳回，供应商可修改后重新提交');lifecycle.value=crypto.randomUUID();emit('changed');await load();}catch(e){if(active)error.value=e instanceof Error?e.message:'驳回失败，原因已保留，请刷新核对。';}finally{if(active)busy.value=false;}}
async function completeContract(){if(!data.value||busy.value)return;busy.value=true;error.value='';try{await submissionFlowsApi.completeContract(data.value,reason.value,lifecycle.value);if(!active)return;ElMessage.success('合同授信已完成，待办已更新');lifecycle.value=crypto.randomUUID();emit('changed');await load();}catch(e){if(active)error.value=e instanceof Error?e.message:'资料尚未齐备或流程已变化，完成说明已保留。';}finally{if(active)busy.value=false;}}
function openNode(id:string){void router.push({path:'/internal/workbench',query:{node_id:id}});emit('close');}
function openResource(row:SubmissionFlowView['rows'][number]){if(!row.resource_id||!data.value)return;void router.push({path:`/internal/model-plaza/${row.resource_id}`,query:{region:data.value.region,tab:'operations'}});emit('close');}
watch(()=>[props.id,props.node],()=>{contractReady.value=false;void load();},{immediate:true});onBeforeUnmount(()=>{active=false;request?.abort();});
</script>
<template>
 <el-dialog :model-value="true" :title="title" :width="isContract?'min(1120px,94vw)':'min(780px,94vw)'" :close-on-click-modal="false" :show-close="!busy" @close="!busy&&emit('close')">
  <div v-loading="loading"><el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text :disabled="busy" @click="load">刷新办理信息</el-button></el-alert>
   <template v-if="data"><p>{{ data.supplier_name }} · 申报 {{ data.id }} · 修订 {{ data.revision }} · {{ data.status }}</p>
    <el-table :data="data.rows"><el-table-column prop="model_name" label="标准模型" min-width="150"/><el-table-column prop="interface_model_id" label="供应商接口模型 ID" min-width="160"/><el-table-column prop="coefficient" label="采购系数" width="100"/><el-table-column label="办理状态" width="140"><template #default="{row}">{{row.status==='CANCELED'?'本次办理已取消':row.status==='CONFIGURED'?'配置已完成':row.status==='ADMITTED'?'已接入':row.status==='COMPLETED'?'已完成':'待办理'}}</template></el-table-column><el-table-column label="资源" width="130"><template #default="{row}"><el-button v-if="row.resource_id" link type="primary" @click="openResource(row)">{{ data.can_handle&&row.status!=='CANCELED'?'办理资源':'查看资源' }}</el-button><span v-else>批准后进入办理</span></template></el-table-column></el-table>
    <el-table v-if="data.nodes?.length" :data="data.nodes" class="flow-progress"><el-table-column label="办理事项"><template #default="{row}">{{ nodeNames[row.key] || row.key }}</template></el-table-column><el-table-column label="办理人"><template #default="{row}">{{ row.assignee_name || '未分派' }}</template></el-table-column><el-table-column label="进度"><template #default="{row}">{{ nodeStates[row.status] || row.status }}</template></el-table-column><el-table-column label="详情" width="100"><template #default="{row}"><el-button v-if="row.id!==data.selected_node_id" link @click="openNode(row.id)">查看事项</el-button></template></el-table-column></el-table>
    <el-form v-if="data.can_approve" label-position="top" :disabled="busy" class="assignment-form">
     <el-form-item label="测试与资源配置办理人"><el-select v-model="test" aria-label="测试与资源配置办理人" placeholder="选择一个办理人"><el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name"/></el-select></el-form-item>
     <el-form-item v-if="data.first_model" label="合同与授信办理人"><el-select v-model="contract" aria-label="合同与授信办理人" placeholder="首次模型申报需选择"><el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name"/></el-select></el-form-item>
     <el-form-item label="审批意见 / 驳回原因"><el-input v-model="reason" aria-label="审批意见" type="textarea" maxlength="500" placeholder="驳回时请填写明确原因；无需选择后续办理人"/></el-form-item>
    </el-form>
    <el-alert v-else-if="!isContract" type="info" :closable="false" :title="data.can_handle?'请逐个打开资源，核对自身测试与正式配置后登记完成。':'当前账号可查看；操作资格由当前审批能力及指定办理人决定。'"/>
    <template v-if="isContract">
     <el-alert v-if="!data.can_handle_contract" type="info" :closable="false" title="当前为只读查看，只有此节点指定办理人可以维护首次合同授信资料。"/>
     <el-alert v-else-if="selectedNode?.status==='WAITING_PREDECESSOR'" type="info" :closable="false" title="可以先准备合同与授信资料；资源配置完成后再登记商务完成。"/>
     <el-alert v-else-if="!data.can_complete_contract" type="warning" :closable="false" title="合同授信齐备查询尚未装配，或当前结构尚未就绪；请先完成资料接入，系统不会跳过核验结束流程。"/>
     <F12SubjectPanel v-if="flowContext" subject-type="SUPPLIER" :subject-id="data.supplier_id" :can-manage="false" :flow-context="flowContext" :flow-can-manage="data.can_handle_contract" @readiness-change="contractReady=$event" @refresh-flow-context="load" @changed="load"/>
     <el-form-item v-if="data.can_handle_contract" label="完成说明"><el-input v-model="reason" aria-label="合同授信完成说明" type="textarea" maxlength="500"/></el-form-item>
    </template>
   </template>
  </div>
  <template #footer><el-button :disabled="busy" @click="emit('close')">关闭办理详情</el-button><template v-if="data?.can_approve"><el-button type="danger" :disabled="busy||!reason.trim()" @click="reject">驳回申报</el-button><el-button type="primary" :loading="busy" :disabled="!test||!reason.trim()||(data.first_model&&!contract)" @click="approve">审批通过并分派</el-button></template><el-button v-if="isContract&&data?.can_handle_contract" type="primary" :loading="busy" :disabled="!data.can_complete_contract||!contractReady||!reason.trim()" @click="completeContract">登记合同授信完成</el-button></template>
 </el-dialog>
</template>
<style scoped>.assignment-form{margin-top:18px}.el-select{width:100%}.el-alert{margin:12px 0}</style>
