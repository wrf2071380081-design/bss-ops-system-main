import {apiRequest} from './http';
import {standardModelVersion} from './standardModels.contract';
export interface SubmissionFlowView {
 id:string;supplier_id:string;supplier_name:string;revision:string;status:string;kind:string;region:string;first_model:boolean;can_approve:boolean;can_handle:boolean;
 rows:Array<{id:string;model_id:string;model_name:string;interface_model_id:string;coefficient:string;status:string;resource_id:string}>;
 nodes:Array<{id:string;key:string;status:string;assignee_id:string;assignee_name:string}>;
 flow_id:string;flow_revision:string;selected_node_id:string;can_handle_contract:boolean;can_complete_contract:boolean;
 created_at:string;submitted_at:string|null;
}
export interface SupplierProgressNode {resource_id:string;submission_id:string;node_id:string;key:string;status:string;assignee_id:string;assignee_name:string;row_status:string}
export const submissionFlowsApi={
 async supplierProgress(id:string,signal?:AbortSignal){return apiRequest<{supplier_id:string;list:SupplierProgressNode[]}>({url:`/internal/flows/suppliers/${standardModelVersion(id)}/progress`,signal});},
 async supplierHistory(id:string,page=1,size=20,signal?:AbortSignal){return apiRequest<{list:SubmissionFlowView[];total:number;page:number;size:number}>({url:`/internal/flows/suppliers/${standardModelVersion(id)}/submissions`,params:{page,size},signal});},
 async get(id:string,node=false,signal?:AbortSignal){
  const result=await apiRequest<SubmissionFlowView>({url:`/internal/flows/${node?'nodes':'submissions'}/${standardModelVersion(id)}`,signal});
  standardModelVersion(result.id);standardModelVersion(result.revision);
  if(typeof result.can_approve!=='boolean'||typeof result.can_handle!=='boolean'||!Array.isArray(result.rows))throw new Error('办理信息不完整，请刷新。');return result;
 },
 async approve(current:SubmissionFlowView,test:string,contract:string,reason:string,lifecycle:string){
  if(!current.can_approve||!reason.trim()||(current.first_model&&!contract))throw new Error('请选择办理人并填写审批意见。');
  const data={expected_revision:standardModelVersion(current.revision),test_assignee_id:standardModelVersion(test),...(contract?{contract_assignee_id:standardModelVersion(contract)}:{}),reason:reason.trim()};
  return apiRequest<{submission_id:string;revision:string;status:string}>({method:'POST',url:`/internal/flows/submissions/${standardModelVersion(current.id)}/approve`,data,idempotency:{scope:`submission-approve:${current.id}`,payload:data,lifecycle}});
 },
 async reject(current:SubmissionFlowView,reason:string,lifecycle:string){
  if(!current.can_approve||!reason.trim())throw new Error('请填写驳回原因。');
  const data={expected_revision:standardModelVersion(current.revision),reason:reason.trim()};
  return apiRequest<{submission_id:string;revision:string;status:string}>({method:'POST',url:`/internal/flows/submissions/${standardModelVersion(current.id)}/reject`,data,idempotency:{scope:`submission-reject:${current.id}`,payload:data,lifecycle}});
 },
 async completeContract(current:SubmissionFlowView,reason:string,lifecycle:string){
  if(!current.can_handle_contract||!reason.trim())throw new Error('只有当前合同授信办理人可以登记，请填写完成说明。');
  const data={expected_flow_revision:standardModelVersion(current.flow_revision),reason:reason.trim()};
  return apiRequest<{node_id:string;status:string}>({method:'POST',url:`/internal/flows/nodes/${standardModelVersion(current.selected_node_id)}/complete-contract`,data,idempotency:{scope:`contract-complete:${current.selected_node_id}`,payload:data,lifecycle}});
 }
};
