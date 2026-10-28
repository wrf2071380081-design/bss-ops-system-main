<script setup lang="ts">
import {ref,watch} from 'vue';
import {apiRequest} from '../../../api/http';
import {resourceOperationsApi} from '../../../api/resourceOperations';
import type {FormalConfigOption} from '../../../api/resourceOperations.contract';
import type {ResourceRow} from '../../../api/resources.types';
const props=defineProps<{supplierId:string;selected:ResourceRow[]}>();const emit=defineEmits<{changed:[]}>();
type Action='CONFIGURE'|'CONFIRM'|'CANCEL';
type Result={resource_id:string;status:number;message?:string;receipt?:{stage:string;revision:string}};
const open=ref(false),action=ref<Action>('CONFIGURE'),busy=ref(false),loading=ref(false),error=ref(''),reason=ref(''),note=ref(''),confirmed=ref(false),lifecycle=ref('');
const chosen=ref<ResourceRow[]>([]),configs=ref<FormalConfigOption[]>([]),configIDs=ref<Record<string,string>>({}),results=ref<Result[]>([]);
const labels:Record<Action,string>={CONFIGURE:'登记配置完成',CONFIRM:'确认已开通',CANCEL:'取消所选接入'};
const stages:Record<string,string>={CONFIGURED:'配置已完成',ADMITTED:'已登记开通',CANCELED:'已取消本次办理'};
let epoch=0;
watch(()=>props.supplierId,()=>{++epoch;open.value=false;busy.value=false;loading.value=false;results.value=[];});
async function show(value:Action){
 const current=++epoch;loading.value=false;
 action.value=value;chosen.value=[...props.selected];reason.value='';note.value='';confirmed.value=false;results.value=[];error.value='';lifecycle.value=crypto.randomUUID();configIDs.value={};open.value=true;
 if(value!=='CONFIGURE')return;loading.value=true;
 try{const options=await Promise.all([...new Set(chosen.value.map(r=>r.region))].map(region=>resourceOperationsApi.formalConfigs(props.supplierId,region)));if(current!==epoch)return;configs.value=options.flat().filter((item,i,all)=>all.findIndex(x=>x.id===item.id)===i);for(const row of chosen.value)configIDs.value[row.id]=configs.value.find(c=>c.id===row.formal_config_id)?.id||'';}
 catch(e){if(current===epoch)error.value=e instanceof Error?e.message:'配置列表加载失败';}finally{if(current===epoch)loading.value=false;}
}
async function submit(){
 if(!reason.value.trim()){error.value='请填写操作原因';return}
 if(action.value!=='CANCEL'&&(!confirmed.value||!note.value.trim())){error.value='请确认外部实际完成并填写依据';return}
 const supplier=props.supplierId,current=epoch;
 const items=chosen.value.map(row=>{const cfg=configs.value.find(c=>c.id===configIDs.value[row.id]);return {resource_id:row.id,expected_revision:row.revision,...(action.value==='CONFIGURE'?{formal_config_id:cfg?.id||'',expected_config_revision:cfg?.revision||''}:{})}});
 if(action.value==='CONFIGURE'&&items.some(i=>!i.formal_config_id)){error.value='请逐条选择有效正式配置';return}
 const body={action:action.value,items,reason:reason.value.trim(),external_confirmed:confirmed.value,completion_note:note.value.trim()};busy.value=true;error.value='';
 try{const result=await apiRequest<{results:Result[]}>({method:'POST',url:`/internal/supplier-entities/${supplier}/resource-actions`,data:body,idempotency:{scope:`supplier-access:${supplier}`,payload:body,lifecycle:lifecycle.value}});if(current!==epoch)return;results.value=result.results;emit('changed');}
 catch(e){if(current===epoch)error.value=e instanceof Error?e.message:'办理失败，输入已保留';}finally{if(current===epoch)busy.value=false;}
}
</script>
<template>
 <div class="batch-access-actions"><span>已选 {{selected.length}} 个模型</span><el-button v-for="value in (['CONFIGURE','CONFIRM','CANCEL'] as const)" :key="value" :type="value==='CANCEL'?'danger':'primary'" :disabled="!selected.length||busy" @click="show(value)">{{labels[value]}}</el-button></div>
 <el-dialog v-model="open" :title="labels[action]" width="min(860px,94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
  <el-alert v-if="action==='CANCEL'" type="warning" :closable="false" title="仅取消尚未接入模型的本次办理；已接入模型请创建下架计划。"/>
  <el-alert v-else type="info" :closable="false" title="本系统登记外部实际操作结果。逐条校验人员、版本及前置条件，未通过的模型会单独显示失败原因。"/>
  <el-alert v-if="error" type="error" :title="error" :closable="false"/>
  <el-table :data="chosen" v-loading="loading"><el-table-column prop="model_name" label="模型" min-width="160"/><el-table-column prop="supplier_model_id" label="接口模型ID" min-width="160"/>
   <el-table-column v-if="action==='CONFIGURE'" label="正式配置" min-width="230"><template #default="{row}"><el-select v-model="configIDs[row.id]" :aria-label="`模型 ${row.id} 正式配置`" :disabled="busy||!!results.length"><el-option v-for="cfg in configs.filter(c=>c.region===row.region&&c.status==='ACTIVE')" :key="cfg.id" :value="cfg.id" :label="`${cfg.scope} · ${cfg.base_url}`"/></el-select></template></el-table-column>
   <el-table-column v-if="results.length" label="办理结果" min-width="200"><template #default="{row}"><span v-if="results.find(r=>r.resource_id===row.id)?.status===200">{{stages[results.find(r=>r.resource_id===row.id)?.receipt?.stage||'']||'已完成'}}</span><span v-else>{{results.find(r=>r.resource_id===row.id)?.message}}</span></template></el-table-column>
  </el-table>
  <el-form label-position="top" :disabled="busy||!!results.length">
   <el-checkbox v-if="action!=='CANCEL'" v-model="confirmed">已在外部平台完成所选模型的实际操作</el-checkbox>
   <el-form-item v-if="action!=='CANCEL'" label="完成依据"><el-input v-model="note" type="textarea" aria-label="批量接入完成依据" maxlength="500"/></el-form-item>
   <el-form-item label="操作原因"><el-input v-model="reason" aria-label="批量接入操作原因" maxlength="500"/></el-form-item>
  </el-form>
  <template #footer><el-button :disabled="busy" @click="open=false">{{results.length?'关闭结果':'取消'}}</el-button><el-button type="primary" :loading="busy" :disabled="loading||!!results.length" @click="submit">{{labels[action]}}</el-button></template>
 </el-dialog>
</template>
<style scoped>.batch-access-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:10px 0 16px}.batch-access-actions span{font-size:13px;color:var(--app-text-muted);margin-right:auto}.el-form{margin-top:18px}.el-alert{margin-bottom:12px}</style>
