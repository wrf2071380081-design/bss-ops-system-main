import { resourceOperationsApi } from "./resourceOperations";
import { resourcesApi } from "./resources";
import type { FormalConfigOption } from "./resourceOperations.contract";
import type { ResourceRow } from "./resources.types";

/**
 * 一步完成资源接入：建或复用正式配置 → 关联 → 挂申报阶段的测试证据（尽力而为）→ 登记接入。
 *
 * 对应原型 b3CompleteResource：用户只提供 Base URL 与 API Key，其余动作由系统串起来，
 * 四步各自留审计。测试不是接入前置条件，证据挂不上只影响「能否报价」，不影响「是否已接入」。
 */
export interface ResourceAccessInput {
  baseURL: string;
  apiKey?: string;
  /** 最近一次读到的资源修订号；不传则用 resource.revision。 */
  revision?: string;
}

export interface ResourceAccessResult {
  config: FormalConfigOption;
  revision: string;
}

export function accessReasonFor(resource: ResourceRow, baseURL: string) {
  const subject = resource.supplier_model_id || resource.model_name;
  return `接口配置登记：${subject} → ${baseURL}`;
}

/** 只允许「未接入过 + 待测试/待接入」的资源走这次登记。 */
export function canCompleteAccess(resource: ResourceRow): boolean {
  return !resource.ever_admitted && ["PENDING_TEST", "PENDING_ACCESS"].includes(resource.state);
}

export async function completeResourceAccess(
  resource: ResourceRow,
  input: ResourceAccessInput
): Promise<ResourceAccessResult> {
  if (!canCompleteAccess(resource)) throw new Error("只有待接入的资源可以登记接入。");
  const baseURL = input.baseURL.trim();
  const apiKey = (input.apiKey || "").trim();
  if (!baseURL) throw new Error("请填写 Base URL。");
  try {
    const parsed = new URL(baseURL);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") throw new Error("bad scheme");
  } catch {
    throw new Error("请输入有效的 HTTP 或 HTTPS 地址。");
  }

  const reason = accessReasonFor(resource, baseURL);
  const configs = await resourceOperationsApi.formalConfigs(resource.supplier_id, resource.region);
  // 复用只能落在**同一准入配置组**里：不同组即使 Base URL 相同也必须各配一次
  // （2026-11-20 业务确认；之前这里只看 URL，GLM 会直接沿用 ds 组的配置）。
  let groupID = "";
  try { groupID = (await resourceOperationsApi.configGroup(resource.id)).config_group_id || ""; } catch { groupID = ""; }
  // 2026-11-23 业务裁决：**每个模型单独配置**，不再跨资源复用同组配置。
  // 之前按"同组复用"实现，但测试阶段两组用同一 URL+Key，导致永远分不清是该复用还是该新建，
  // 反复修不好。现在只认"这个资源自己已经绑定过的配置"，其余一律要求填写并新建。
  const bound = resource.formal_config_id
    ? configs.find(item => item.id === resource.formal_config_id)
    : undefined;
  let config = bound;
  if (!config) {
    if (!apiKey) throw new Error("首次登记该地址需要填写生产 API Key。");
    config = await resourceOperationsApi.createFormalConfig(
      resource.supplier_id,
      { scope: `${resource.model_name} 生产配置`, base_url: baseURL, api_key: apiKey, reason, config_group_id: groupID || undefined },
      crypto.randomUUID()
    );
  }

  let revision = input.revision || resource.revision;
  if (resource.formal_config_id !== config.id || resource.formal_config_revision !== config.revision) {
    const bound = await resourceOperationsApi.bindFormalConfig(resource, config, reason, crypto.randomUUID());
    revision = bound.resource_revision;
  }

  try {
    const evidence = await resourceOperationsApi.refreshEvidence(
      { ...resource, revision, state: "PENDING_ACCESS" },
      "接入登记时同步申报测试结果",
      crypto.randomUUID()
    );
    revision = evidence.revision;
  } catch {
    // 这个接口历史上有「返回 500 但数据已写成功」的缺陷，别拿旧修订号去撞 409。
    const fresh = await resourcesApi.get(resource.id, resource.region);
    revision = fresh.revision;
  }

  await resourceOperationsApi.admit(
    {
      ...resource, revision, state: "PENDING_ACCESS", ever_admitted: false,
      formal_config_id: config.id, formal_config_revision: config.revision,
    },
    {
      externalConfirmed: true,
      completionNote: `已核对 ${resource.model_name} 的生产配置：Base URL ${baseURL}，生产 API Key 已登记。`,
      reason,
    },
    crypto.randomUUID()
  );
  return { config, revision };
}
