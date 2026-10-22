import { standardModelVersion } from "./standardModels.contract";
import type { ResourceRow } from "./resources.types";

export interface FormalConfigOption {
  id: string;
  supplier_id: string;
  region: string;
  scope: string;
  base_url: string;
  has_api_key: boolean;
  status: "ACTIVE" | "INACTIVE";
  revision: string;
  confirmed_revision: string;
}

export interface ResourceAdmissionInput {
  externalConfirmed: boolean;
  completionNote: string;
  reason: string;
}

export function canBindFormalConfig(resource: ResourceRow): boolean {
  return !resource.ever_admitted && ["PENDING_TEST", "PENDING_ACCESS"].includes(resource.state);
}

export function canAdmitResource(resource: ResourceRow): boolean {
  return !resource.ever_admitted && resource.state === "PENDING_ACCESS" &&
    resource.test.status === "COMPLETED" && resource.test.valid &&
    resource.test.connectivity_passed && resource.test.pressure_passed &&
    Boolean(resource.formal_config_id) && BigInt(standardModelVersion(resource.formal_config_revision)) > 0n;
}

export function buildResourceAdmission(resource: ResourceRow, input: ResourceAdmissionInput) {
  if (!canAdmitResource(resource)) throw new Error("资源自身的连通性和压力测试、正式配置尚未就绪，暂不能登记接入。");
  const completionNote = input.completionNote.trim(), reason = input.reason.trim();
  if (input.externalConfirmed !== true || !completionNote || !reason) throw new Error("请确认外部实际配置并填写完成依据和操作原因。");
  return {
    expected_revision: standardModelVersion(resource.revision),
    external_confirmed: true as const,
    completion_note: completionNote,
    reason,
  };
}

export function buildResourceConfigBinding(resource: ResourceRow, config: FormalConfigOption, reasonInput: string) {
  if (!canBindFormalConfig(resource)) throw new Error("已接入资源不能重新绑定首次正式配置。");
  if (config.supplier_id !== resource.supplier_id || config.region !== resource.region ||
      config.status !== "ACTIVE" || !config.has_api_key || config.revision !== config.confirmed_revision) {
    throw new Error("只能绑定同一供应商和区域中已确认且包含正式凭据的有效配置。");
  }
  const reason = reasonInput.trim();
  if (!reason) throw new Error("请填写配置关联原因。");
  return {
    expected_revision: standardModelVersion(resource.revision),
    formal_config_id: standardModelVersion(config.id),
    expected_config_revision: standardModelVersion(config.revision),
    reason,
  };
}

export function validateFormalConfigOptions(value: unknown, supplierID: string, region: string): FormalConfigOption[] {
  standardModelVersion(supplierID);
  if (!Array.isArray(value)) throw new Error("正式配置列表响应不符合约定，请重新加载。");
  return value.map((candidate) => {
    if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) throw new Error("正式配置列表响应不符合约定，请重新加载。");
    const item = candidate as Record<string, unknown>;
    if (["api_key", "api_key_enc", "secret", "ciphertext"].some(key => key in item)) throw new Error("正式配置响应包含不应返回的凭据字段。");
    const id = standardModelVersion(item.id);
    if (item.supplier_id !== supplierID || (item.region !== "CN" && item.region !== "OVERSEAS") ||
        (region && item.region !== region) || typeof item.scope !== "string" || !item.scope.trim() ||
        typeof item.base_url !== "string" || !item.base_url.trim() || typeof item.has_api_key !== "boolean" ||
        !["ACTIVE", "INACTIVE"].includes(String(item.status)) || typeof item.revision !== "string" ||
        typeof item.confirmed_revision !== "string") throw new Error("正式配置与供应商或区域不匹配，请重新加载。");
    standardModelVersion(item.revision);
    if (item.confirmed_revision !== "0") standardModelVersion(item.confirmed_revision);
    return {
      id, supplier_id: supplierID, region: item.region, scope: item.scope, base_url: item.base_url,
      has_api_key: item.has_api_key, status: item.status as FormalConfigOption["status"], revision: item.revision,
      confirmed_revision: item.confirmed_revision,
    };
  });
}

export interface ResourceAdmissionReceipt {
  resource_id: string;
  state: "ADMITTED";
  ever_admitted: true;
  revision: string;
  updated_at: string;
}

export function validateResourceAdmissionReceipt(value: unknown, resource: ResourceRow, expectedRevision: string): ResourceAdmissionReceipt {
  const result = value as ResourceAdmissionReceipt | null;
  if (!result || result.resource_id !== resource.id || result.state !== "ADMITTED" || result.ever_admitted !== true ||
      result.revision !== (BigInt(standardModelVersion(expectedRevision)) + 1n).toString() || !Number.isFinite(Date.parse(result.updated_at))) {
    throw new Error("接入回执与当前资源或修订号不一致，请重新加载核对。");
  }
  return { ...result };
}

export interface ResourceConfigBindingReceipt {
  resource_id: string;
  formal_config_id: string;
  formal_config_revision: string;
  resource_revision: string;
  changed: boolean;
  updated_at: string;
}

export function validateResourceConfigBindingReceipt(value: unknown, resource: ResourceRow, body: ReturnType<typeof buildResourceConfigBinding>): ResourceConfigBindingReceipt {
  const result = value as ResourceConfigBindingReceipt | null;
  const expectedResourceRevision = BigInt(standardModelVersion(body.expected_revision)) + (result?.changed === true ? 1n : 0n);
  if (!result || result.resource_id !== resource.id || result.formal_config_id !== body.formal_config_id ||
      result.formal_config_revision !== body.expected_config_revision || result.changed !== (expectedResourceRevision > BigInt(standardModelVersion(body.expected_revision))) ||
      result.resource_revision !== expectedResourceRevision.toString() || !Number.isFinite(Date.parse(result.updated_at))) {
    throw new Error("配置关联回执与当前资源或版本不一致，请重新加载核对。");
  }
  return { ...result };
}
