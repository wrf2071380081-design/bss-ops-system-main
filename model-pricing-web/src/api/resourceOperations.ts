import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import {
  buildResourceAdmission,
  buildResourceConfigBinding,
  validateFormalConfigOptions,
  validateResourceAdmissionReceipt,
  validateResourceConfigBindingReceipt,
  type FormalConfigOption,
  type ResourceAdmissionInput,
} from "./resourceOperations.contract";
import type { ResourceRow } from "./resources.types";
import { buildApprovedPrice, parseResourceWorkflow, type ApprovedPriceOption } from "./resourceWorkflow.contract";

export const resourceOperationsApi = {
  async refreshEvidence(resource: ResourceRow, reason: string, lifecycle: string) {
    if(!reason.trim())throw new Error('请填写同步原因。');
    const data={expected_revision:standardModelVersion(resource.revision),reason:reason.trim()};
    return apiRequest<{resource_id:string;revision:string;report_id:string;state:string}>({method:'POST',url:`/internal/resources/${encodeURIComponent(standardModelVersion(resource.id))}/refresh-test-evidence`,data,idempotency:{scope:`resource-evidence:${resource.id}`,payload:data,lifecycle}});
  },
  async workflow(resourceID: string, signal?: AbortSignal) {
    return parseResourceWorkflow(await apiRequest<unknown>({url:`/internal/resources/${encodeURIComponent(standardModelVersion(resourceID))}/workflow-actions`,signal}));
  },
  async applyApprovedPrice(resource: ResourceRow, source: ApprovedPriceOption, confirmed: boolean, note: string, reason: string, lifecycle: string) {
    const data=buildApprovedPrice(resource.revision,source,confirmed,note,reason);
    const result=await apiRequest<{resource_id:string;revision:string;coefficient:string;changed:boolean}>({method:'POST',url:`/internal/resources/${encodeURIComponent(standardModelVersion(resource.id))}/apply-approved-price`,data,idempotency:{scope:`approved-price:${resource.id}`,payload:data,lifecycle}});
    if(!result||result.resource_id!==resource.id||typeof result.changed!=='boolean'||result.revision!==(BigInt(data.expected_revision)+(result.changed?1n:0n)).toString()||Number(result.coefficient)!==Number(source.coefficient))throw new Error('改价回执不一致，请刷新资源后查看办理结果。');
    return result;
  },
  async formalConfigs(supplierID: string, region: string, signal?: AbortSignal): Promise<FormalConfigOption[]> {
    const result = await apiRequest<unknown>({ url: `/internal/suppliers/${encodeURIComponent(standardModelVersion(supplierID))}/formal-configs`, signal });
    return validateFormalConfigOptions(result, supplierID, region);
  },
  async createFormalConfig(supplierID: string, input: { scope: string; base_url: string; api_key: string; reason: string }, lifecycle: string) {
    supplierID = standardModelVersion(supplierID);
    const url = `/internal/suppliers/${encodeURIComponent(supplierID)}/formal-configs`;
    const data = { ...input, scope: input.scope.trim(), base_url: input.base_url.trim(), api_key: input.api_key.trim(), reason: input.reason.trim(), external_confirmed: true as const };
    const result = await apiRequest<unknown>({ method: "POST", url, data, idempotency: { scope: `formal-config:create:${supplierID}`, payload: data, lifecycle } });
    return validateFormalConfigOptions([result], supplierID, "")[0]!;
  },
  async updateFormalConfig(supplierID: string, config: FormalConfigOption, input: { scope: string; base_url: string; api_key: string; reason: string }, lifecycle: string) {
    supplierID = standardModelVersion(supplierID);
    const url = `/internal/suppliers/${encodeURIComponent(supplierID)}/formal-configs/${encodeURIComponent(standardModelVersion(config.id))}`;
    const data = { expected_revision: config.revision, scope: input.scope.trim(), base_url: input.base_url.trim(), ...(input.api_key.trim() ? { api_key: input.api_key.trim() } : {}), reason: input.reason.trim(), external_confirmed: true as const };
    const result = await apiRequest<unknown>({ method: "PUT", url, data, idempotency: { scope: `formal-config:update:${config.id}`, payload: data, lifecycle } });
    return validateFormalConfigOptions([result], supplierID, "")[0]!;
  },
  async disableFormalConfig(supplierID: string, config: FormalConfigOption, reason: string, lifecycle: string) {
    supplierID = standardModelVersion(supplierID);
    const url = `/internal/suppliers/${encodeURIComponent(supplierID)}/formal-configs/${encodeURIComponent(standardModelVersion(config.id))}`;
    const data = { expected_revision: config.revision, reason: reason.trim() };
    const result = await apiRequest<unknown>({ method: "DELETE", url, data, idempotency: { scope: `formal-config:disable:${config.id}`, payload: data, lifecycle } });
    return validateFormalConfigOptions([result], supplierID, "")[0]!;
  },
  async admit(resource: ResourceRow, input: ResourceAdmissionInput, lifecycle: string) {
    const body = buildResourceAdmission(resource, input);
    return validateResourceAdmissionReceipt(await apiRequest<unknown>({
      method: "POST", url: `/internal/resources/${resource.id}/admit`, data: body,
      idempotency: { scope: `resource-admit:${resource.id}`, payload: body, lifecycle },
    }), resource, body.expected_revision);
  },
  async bindFormalConfig(resource: ResourceRow, config: FormalConfigOption, reason: string, lifecycle: string) {
    const body = buildResourceConfigBinding(resource, config, reason);
    return validateResourceConfigBindingReceipt(await apiRequest<unknown>({
      method: "PUT", url: `/internal/resources/${resource.id}/formal-config`, data: body,
      idempotency: { scope: `resource-formal-config:${resource.id}`, payload: body, lifecycle },
    }), resource, body);
  },
};
