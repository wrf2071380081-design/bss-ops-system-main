import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
export type RetestCategory = "baseline" | "stress";
export interface RetestOptions { can_start: boolean; reason: string; categories: RetestCategory[] }
const path=(id:string)=>`/internal/resources/${standardModelVersion(id)}/tests`;
export const resourceRetestsApi={
 async options(resourceID:string,signal?:AbortSignal):Promise<RetestOptions>{
  const value=await apiRequest<RetestOptions>({url:`${path(resourceID)}/options`,signal});
  if(!value||typeof value.can_start!=="boolean"||typeof value.reason!=="string"||!Array.isArray(value.categories)||value.categories.length!==2||new Set(value.categories).size!==2||value.categories.some(c=>!["baseline","stress"].includes(c)))throw new Error("补测能力响应不完整，请刷新。");
  return {can_start:value.can_start,reason:value.reason,categories:[...value.categories]};
 },
 async start(resourceID:string,category:RetestCategory,lifecycle:string){
  if(!["baseline","stress"].includes(category))throw new Error("请选择连通性或压力测试。");
  const url=path(resourceID),data={category};
  const value=await apiRequest<{job_id:string;external_job_id:string;status:string}>({method:"POST",url,data,idempotency:{scope:url,payload:data,lifecycle}});
  standardModelVersion(value?.job_id);if(typeof value.external_job_id!=="string"||!value.external_job_id||typeof value.status!=="string")throw new Error("测试受理响应不完整，请刷新报告确认任务。");
  return {job_id:value.job_id,status:value.status};
 },
};
