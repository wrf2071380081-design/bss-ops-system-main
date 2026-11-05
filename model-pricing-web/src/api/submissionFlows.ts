import {apiRequest} from './http';
import {standardModelVersion} from './standardModels.contract';
export interface SubmissionFlowView {
 id:string;supplier_id:string;supplier_name:string;revision:string;status:string;kind:string;region:string;first_model:boolean;can_approve:boolean;can_handle:boolean;
 rows:Array<{id:string;model_id:string;model_name:string;interface_model_id:string;coefficient:string;status:string;resource_id:string;decision?:'PENDING'|'APPROVED'|'REJECTED';company?:string;model_type?:string;
  /** 该行供应资源最近一次测试（原型审批页四列指标）；无报告时 test_status=NOT_TESTED。 */
  test_status?:string;connection_ms?:string;rps?:string;tps?:string;cache_hit_rate?:string;cache_available?:boolean}>;
 nodes:Array<{id:string;key:string;status:string;assignee_id:string;assignee_name:string}>;
 flow_id:string;flow_revision:string;selected_node_id:string;can_handle_contract:boolean;can_complete_contract:boolean;
 created_at:string;submitted_at:string|null;
  /** 申报人（供应商侧联系人）；历史单据可能为空。 */
  submitter?:string;
  /** 审批结论时间与审批人：仅供应商申报历史返回（取 APPROVE/REJECT 操作记录）。 */
  approved_at?:string|null;approver?:string;
  /** 逐模型决定的派生结论：通过 / 部分通过 / 不通过；未决策时为空。 */
  approval_result?:string;
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
 /**
  * decisions 是原型「逐模型决定」：key=申报行 id，true=通过、false=不通过。
  * 未出现在 map 里的行后端按「不勾=不通过」处理。
  */
 async approve(current:SubmissionFlowView,test:string,contract:string,reason:string,lifecycle:string,decisions:Record<string,boolean>){
  if(!current.can_approve||(current.first_model&&!contract))throw new Error('请选择办理人。');
  const rows=current.rows.map(row=>({id:standardModelVersion(row.id),decision:decisions[row.id]?'APPROVED':'REJECTED'}));
  if(rows.length&&!rows.some(row=>row.decision==='APPROVED'))throw new Error('请至少勾选一个模型通过，否则请用「驳回申报」。');
  const data={expected_revision:standardModelVersion(current.revision),test_assignee_id:standardModelVersion(test),...(contract?{contract_assignee_id:standardModelVersion(contract)}:{}),...(reason.trim()?{reason:reason.trim()}:{}),rows};
  return apiRequest<{submission_id:string;revision:string;status:string}>({method:'POST',url:`/internal/flows/submissions/${standardModelVersion(current.id)}/approve`,data,idempotency:{scope:`submission-approve:${current.id}`,payload:data,lifecycle}});
 },
 async reject(current:SubmissionFlowView,reason:string,lifecycle:string){
  if(!current.can_approve)throw new Error('当前账号没有审批权限。');
  const data={expected_revision:standardModelVersion(current.revision),...(reason.trim()?{reason:reason.trim()}:{})};
  return apiRequest<{submission_id:string;revision:string;status:string}>({method:'POST',url:`/internal/flows/submissions/${standardModelVersion(current.id)}/reject`,data,idempotency:{scope:`submission-reject:${current.id}`,payload:data,lifecycle}});
 },
 async completeContract(current:SubmissionFlowView,reason:string,lifecycle:string){
  if(!current.can_handle_contract||!reason.trim())throw new Error('只有当前合同授信办理人可以登记，请填写完成说明。');
  const data={expected_flow_revision:standardModelVersion(current.flow_revision),reason:reason.trim()};
  return apiRequest<{node_id:string;status:string}>({method:'POST',url:`/internal/flows/nodes/${standardModelVersion(current.selected_node_id)}/complete-contract`,data,idempotency:{scope:`contract-complete:${current.selected_node_id}`,payload:data,lifecycle}});
 }
};
