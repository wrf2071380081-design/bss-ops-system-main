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

export const resourceOperationsApi = {
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
