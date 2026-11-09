<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue';
import {useRouter} from 'vue-router';
import {ElMessage} from 'element-plus';
import {submissionFlowsApi,type SubmissionFlowView} from '../../../api/submissionFlows';
import {workbenchApi,type WorkbenchPersonDTO} from '../../../modules/workbench/api/workbench';
import F12SubjectPanel from '../subjects/F12SubjectPanel.vue';
import ResourceReportsPanel from '../resources/ResourceReportsPanel.vue';
const props=defineProps<{id:string;node?:boolean}>();const emit=defineEmits<{close:[];changed:[]}>();const router=useRouter();
const data=ref<SubmissionFlowView>(),people=ref<WorkbenchPersonDTO[]>([]),loading=ref(false),busy=ref(false),error=ref('');
const test=ref(''),contract=ref(''),reason=ref(''),lifecycle=ref(crypto.randomUUID());let active=true,request:AbortController|undefined;
const contractReady=ref(false);
// 测试与报告：原型是在审批弹窗里直接展开测试历史，这里用嵌套弹窗复用资源报告面板。
const reportsRow=ref<SubmissionFlowView['rows'][number]>(),reportsVisible=ref(false);
function openReports(row:SubmissionFlowView['rows'][number]){if(!row.resource_id)return;reportsRow.value=row;reportsVisible.value=true;}
// 逐模型决定（原型：勾=通过、不勾=不通过）。默认不勾，避免"没看就全放行"。
const picked=ref<Record<string,boolean>>({});
const approvedCount=computed(()=>data.value?.rows.filter(row=>picked.value[row.id]).length||0);
const rejectedCount=computed(()=>(data.value?.rows.length||0)-approvedCount.value);
const selectedNode=computed(()=>data.value?.nodes?.find(n=>n.id===data.value?.selected_node_id));
const isContract=computed(()=>selectedNode.value?.key==='CONTRACT');
const title=computed(()=>isContract.value?'合同与授信办理':props.node?'测试与资源配置办理':'申报审批与分派');
const flowContext=computed(()=>{const v=data.value;if(!v?.flow_id||!v.flow_revision)return undefined;const revision=Number(v.flow_revision);return Number.isSafeInteger(revision)&&revision>0&&revision<=2147483647?{flowID:v.flow_id,moduleKey:'CONTRACT',expectedRevision:revision}:undefined;});
const nodeNames:Record<string,string>={CONFIG:'测试与资源配置',CONTRACT:'合同与授信'};
const nodeStates:Record<string,string>={IN_PROGRESS:'办理中',WAITING_PREDECESSOR:'等待前序办理',COMPLETED:'已完成'};
const decisionLabels:Record<string,string>={PENDING:'待决策',APPROVED:'通过',REJECTED:'不通过'};
// 状态码直接印在弹窗抬头会露出 PENDING_APPROVAL 这种英文枚举，按原型翻成人话。
const submissionStatusLabels:Record<string,string>={DRAFT:'草稿',PENDING_APPROVAL:'待审批',APPROVED:'已批准',REJECTED:'已驳回',PROCESSING:'办理中',COMPLETED:'已完成',WITHDRAWN:'已撤回'};
function resetPicked(view:SubmissionFlowView){const next:Record<string,boolean>={};for(const row of view.rows){next[row.id]=row.decision==='APPROVED';}picked.value=next;}
async function load(){request?.abort();const current=request=new AbortController();loading.value=true;error.value='';
 try{const [view,staff]=await Promise.all([submissionFlowsApi.get(props.id,props.node,current.signal),workbenchApi.people()]);if(active&&!current.signal.aborted){data.value=view;people.value=staff;if(view.can_approve)resetPicked(view);}}
 catch(e){if(active&&!current.signal.aborted)error.value=e instanceof Error?e.message:'办理信息加载失败。';}finally{if(active&&!current.signal.aborted)loading.value=false;}}
async function approve(){if(!data.value||busy.value)return;busy.value=true;error.value='';try{await submissionFlowsApi.approve(data.value,test.value,contract.value,reason.value,lifecycle.value,picked.value);if(!active)return;ElMessage.success(approvedCount.value===data.value.rows.length?'审批通过，已分派办理任务':'已按勾选提交：通过的行进入办理，未勾选的行不通过');lifecycle.value=crypto.randomUUID();emit('changed');await load();}catch(e){if(active)error.value=e instanceof Error?e.message:'审批失败，输入已保留，请刷新后核对。';}finally{if(active)busy.value=false;}}
async function reject(){if(!data.value||busy.value)return;busy.value=true;error.value='';try{await submissionFlowsApi.reject(data.value,reason.value,lifecycle.value);if(!active)return;ElMessage.success('已驳回，供应商可修改后重新提交');lifecycle.value=crypto.randomUUID();emit('changed');await load();}catch(e){if(active)error.value=e instanceof Error?e.message:'驳回失败，原因已保留，请刷新核对。';}finally{if(active)busy.value=false;}}
async function completeContract(){
  if(!data.value||busy.value)return;
  // 按钮不再置灰：资料没齐、说明没填都改成点击时明确告知，避免"点了没反应、以为已办完"。
  if(!data.value.can_complete_contract||!contractReady.value){ElMessage.warning('合同与授信资料尚未齐备：请先上传合同与对应币种的授信资料。');return;}
  if(!reason.value.trim()){ElMessage.warning('请先填写完成说明，再登记合同授信完成。');return;}
  busy.value=true;error.value='';
  try{
    await submissionFlowsApi.completeContract(data.value,reason.value,lifecycle.value);
    if(!active)return;
    ElMessage.success('合同授信已完成，工作台待办已结束。');
    lifecycle.value=crypto.randomUUID();
    emit('changed');emit('close');
  }catch(e){if(active)error.value=e instanceof Error?e.message:'资料尚未齐备或流程已变化，完成说明已保留。';}
  finally{if(active)busy.value=false;}
}
function openNode(id:string){void router.push({path:'/internal/workbench',query:{node_id:id}});emit('close');}
function openResource(row:SubmissionFlowView['rows'][number]){if(!row.resource_id||!data.value)return;void router.push({path:`/internal/model-plaza/${row.resource_id}`,query:{region:data.value.region,tab:'operations'}});emit('close');}
// 原型审批页的「对比」：跳到该标准模型的来源对比页，横向比较各供应商系数与测试结果。
function openCompare(row:SubmissionFlowView['rows'][number]){if(!data.value)return;void router.push({path:`/internal/model-detail/${row.model_id}`,query:{from_submission:data.value.id}});emit('close');}
watch(()=>[props.id,props.node],()=>{contractReady.value=false;void load();},{immediate:true});onBeforeUnmount(()=>{active=false;request?.abort();});
</script>
<template>
 <el-dialog class="flow-dialog" :model-value="true" :title="title" :width="isContract?'min(1120px,94vw)':'min(980px,94vw)'" :close-on-click-modal="false" :show-close="!busy" @close="!busy&&emit('close')">
  <div v-loading="loading"><el-alert v-if="error" :title="error" type="error" :closable="false"><el-button text :disabled="busy" @click="load">刷新办理信息</el-button></el-alert>
   <template v-if="data"><p>{{ data.supplier_name }} · 申报 {{ data.id }} · 修订 {{ data.revision }} · {{ submissionStatusLabels[data.status] || data.status }}</p>
    <p v-if="data.can_approve" class="decision-summary">通过 {{ approvedCount }} 个 · 不通过 {{ rejectedCount }} 个。勾选为通过，未勾选为不通过。</p>
    <el-table :data="data.rows">
     <el-table-column v-if="data.can_approve" label="决定" width="70"><template #default="{row}"><input type="checkbox" :checked="!!picked[row.id]" :aria-label="`通过模型 ${row.model_name}`" :disabled="busy" @change="picked={...picked,[row.id]:($event.target as HTMLInputElement).checked}"></template></el-table-column>
     <el-table-column v-else label="审批结果" width="100"><template #default="{row}">{{ decisionLabels[row.decision||'PENDING'] }}</template></el-table-column>
     <el-table-column prop="model_name" label="标准模型" min-width="140"/>
     <el-table-column prop="interface_model_id" label="供应商接口模型 ID" min-width="150"/>
     <!-- 价格变更：原系数与申报值并排（口径同整页审批页），新增单只有申报值。 -->
     <el-table-column :label="data.kind==='PRICE_CHANGE'?'采购系数（原 → 申报）':'采购系数'" width="170">
      <template #default="{row}">
       <span v-if="data.kind==='PRICE_CHANGE'" class="mono">{{ row.current_coefficient || "—" }}<span class="muted"> → </span><strong>{{ row.coefficient }}</strong></span>
       <span v-else>{{ row.coefficient }}</span>
      </template>
     </el-table-column>
     <el-table-column label="办理状态" width="120"><template #default="{row}">{{row.status==='CANCELED'?'本次办理已取消':row.status==='CONFIGURED'?'配置已完成':row.status==='ADMITTED'?'已接入':row.status==='COMPLETED'?'已完成':'待办理'}}</template></el-table-column>
     <el-table-column label="测试与报告" width="110"><template #default="{row}"><el-button v-if="row.resource_id" link type="primary" :disabled="busy" @click="openReports(row)">测试与报告</el-button><span v-else class="muted">未接入</span></template></el-table-column>
     <el-table-column label="对比" width="80"><template #default="{row}"><el-button link type="primary" :disabled="busy" @click="openCompare(row)">对比</el-button></template></el-table-column>
     <el-table-column label="资源" width="110"><template #default="{row}"><el-button v-if="row.resource_id" link type="primary" @click="openResource(row)">{{ data.can_handle&&row.status!=='CANCELED'?'办理资源':'查看资源' }}</el-button><span v-else>{{ data.can_approve?'批准后进入办理':'未接入' }}</span></template></el-table-column>
    </el-table>
    <el-table v-if="data.nodes?.length" :data="data.nodes" class="flow-progress"><el-table-column label="办理事项"><template #default="{row}">{{ nodeNames[row.key] || row.key }}</template></el-table-column><el-table-column label="办理人"><template #default="{row}">{{ row.assignee_name || '未分派' }}</template></el-table-column><el-table-column label="进度"><template #default="{row}">{{ nodeStates[row.status] || row.status }}</template></el-table-column><el-table-column label="详情" width="100"><template #default="{row}"><el-button v-if="row.id!==data.selected_node_id" link @click="openNode(row.id)">查看事项</el-button></template></el-table-column></el-table>
    <el-form v-if="data.can_approve" label-position="top" :disabled="busy" class="assignment-form">
     <el-form-item label="测试与资源配置办理人"><el-select v-model="test" aria-label="测试与资源配置办理人" placeholder="选择一个办理人"><el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name"/></el-select></el-form-item>
     <el-form-item v-if="data.first_model" label="合同与授信办理人"><el-select v-model="contract" aria-label="合同与授信办理人" placeholder="首次模型申报需选择"><el-option v-for="person in people" :key="person.id" :value="String(person.id)" :label="person.name"/></el-select></el-form-item>
     <el-form-item label="审批意见（选填）"><el-input v-model="reason" aria-label="审批意见" type="textarea" maxlength="500" placeholder="选填；驳回时建议写明原因"/></el-form-item>
    </el-form>
    <el-alert v-else-if="!isContract" type="info" :closable="false" :title="data.can_handle?'请逐个打开资源，核对自身测试与正式配置后登记完成。':'当前账号可查看；操作资格由当前审批能力及指定办理人决定。'"/>
    <template v-if="isContract">
     <el-alert v-if="!data.can_handle_contract" type="info" :closable="false" title="当前为只读查看，只有此节点指定办理人可以维护首次合同授信资料。"/>
     <el-alert v-else-if="selectedNode?.status==='WAITING_PREDECESSOR'" type="info" :closable="false" title="可以先准备合同与授信资料；资源配置完成后再登记商务完成。"/>
     <el-alert v-else-if="!data.can_complete_contract" type="warning" :closable="false" title="合同授信资料尚未齐备：请先补齐合同与对应币种的授信记录，系统不会跳过核验结束流程。"/>
     <F12SubjectPanel v-if="flowContext" subject-type="SUPPLIER" :subject-id="data.supplier_id" :can-manage="false" :flow-context="flowContext" :flow-can-manage="data.can_handle_contract" :currency="data.region === 'OVERSEAS' ? 'USD' : 'CNY'" @readiness-change="contractReady=$event" @refresh-flow-context="load" @changed="load"/>
    </template>
   </template>
  </div>
  <!-- 合同授信的「完成」动作放页脚常驻：原来完成说明在弹窗最底部、按钮还是灰的，
       用户传完资料以为已经办完，回到工作台仍是待办。现在说明与按钮同屏，点按钮会给明确原因。 -->
  <template #footer>
   <div v-if="isContract && data?.can_handle_contract" class="complete-bar">
    <p class="complete-hint" :class="{ ready: data.can_complete_contract && contractReady }">
     {{ data.can_complete_contract && contractReady
        ? '合同与授信资料已齐备：填写完成说明后点「登记合同授信完成」，这条待办才会结束。'
        : '先把合同与对应币种的授信资料传齐，再登记完成；系统不会跳过核验。' }}
    </p>
    <el-input v-model="reason" type="textarea" :rows="2" maxlength="500" aria-label="合同授信完成说明" placeholder="完成说明（必填）：例如合同已用印、授信额度已确认"/>
    <div class="complete-actions">
     <el-button :disabled="busy" @click="emit('close')">关闭办理详情</el-button>
     <el-button type="primary" :loading="busy" @click="completeContract">登记合同授信完成</el-button>
    </div>
   </div>
   <template v-else><el-button :disabled="busy" @click="emit('close')">关闭办理详情</el-button><template v-if="data?.can_approve"><el-button type="danger" :disabled="busy" @click="reject">驳回申报</el-button><el-button type="primary" :loading="busy" :disabled="!test||approvedCount===0||(data.first_model&&!contract)" :title="approvedCount===0?'未勾选任何模型：整单驳回请用左侧「驳回申报」':''" @click="approve">核对并提交审批</el-button></template></template>
  </template>
 </el-dialog>
  <el-dialog v-model="reportsVisible" title="测试与报告" width="min(1180px,96vw)" append-to-body align-center>
    <p class="reports-head">{{ reportsRow?.model_name }} · {{ reportsRow?.interface_model_id }}</p>
    <div class="reports-body">
      <ResourceReportsPanel v-if="reportsRow?.resource_id" :resource-i-d="reportsRow.resource_id" />
    </div>
    <template #footer><el-button @click="reportsVisible=false">关闭测试报告</el-button></template>
  </el-dialog>
</template>
<style scoped>.assignment-form{margin-top:18px}.el-select{width:100%}.el-alert{margin:12px 0}.decision-summary{font-size:12px;color:#6b7280;margin:0 0 10px}.reports-head{margin:0 0 10px;font-size:12px;color:#6b7280}.reports-body{max-height:62vh;overflow:auto}
/* 价格变更的「原 → 申报」：数字等宽，箭头弱化。 */
.mono{font-variant-numeric:tabular-nums}.muted{color:var(--app-text-muted)}
/* 合同授信页脚的完成条：说明与按钮同屏，避免用户传完资料就以为办完了。 */
.complete-bar{text-align:left}
.complete-hint{margin:0 0 8px;font-size:12px;color:#8a6a2f;background:#fffaf0;border:1px solid #f1e5c7;border-radius:6px;padding:8px 10px}
.complete-hint.ready{color:#1f6448;background:#eef7f2;border-color:#cfe7dc}
.complete-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:8px}</style>

<style>
/* 弹窗由 Element Plus 渲染在 body 上，scoped 选择器抓不到它的内部节点，这里用全局类名兜住。
   目的：主体区滚动，页脚的「完成说明 + 登记合同授信完成」始终可见。 */
.flow-dialog{margin-top:6vh!important;margin-bottom:6vh}
.flow-dialog .el-dialog__body{max-height:calc(88vh - 210px);overflow:auto}
.flow-dialog .el-dialog__footer{border-top:1px solid var(--el-border-color-lighter);padding-top:12px}
</style>
