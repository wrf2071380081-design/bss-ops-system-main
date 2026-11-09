import { apiRequest } from "./http";

const base = "/supplier/test-config-groups";

/**
 * 供应商准入测试**配置组**（000052，原型对齐）。
 *
 * 原型申报页是卡片式多组：每组一套 URL/Key，模型表格里每行挂一组，
 * 提交门槛是「每组至少一个连通成功 + 整批至少一次有效压测」。
 *
 * Key 只进不出：服务端只回 has_api_key，前端永远拿不到明文；
 * 编辑卡片时不回显 Key，提交时**不传 api_key 表示保持不变**（传空串才是清空）。
 */
export interface SupplierTestConfigGroupDTO {
  id: string;
  name: string;
  base_url: string;
  has_api_key: boolean;
  enabled: boolean;
  sort_order: number;
  updated_at?: string;
}

export interface SupplierTestConfigGroupInput {
  name: string;
  base_url: string;
  /** 不传 = 不设/不改；传空串 = 清空；传值 = 覆盖。 */
  api_key?: string;
}

const assertGroup = (value: unknown): SupplierTestConfigGroupDTO => {
  const group = value as SupplierTestConfigGroupDTO | null;
  if (!group || typeof group.id !== "string" || typeof group.name !== "string" ||
      typeof group.base_url !== "string" || typeof group.has_api_key !== "boolean" ||
      typeof group.enabled !== "boolean") {
    throw new Error("配置组响应不符合约定，请重新加载。");
  }
  return group;
};

export const supplierTestConfigGroupsApi = {
  async list(signal?: AbortSignal) {
    const result = await apiRequest<{ list: SupplierTestConfigGroupDTO[] }>({ url: base, signal });
    const list = Array.isArray(result?.list) ? result.list : [];
    return list.map(assertGroup).sort((a, b) => a.sort_order - b.sort_order || Number(a.id) - Number(b.id));
  },

  async create(input: SupplierTestConfigGroupInput, lifecycle: string) {
    return assertGroup(await apiRequest<SupplierTestConfigGroupDTO>({
      method: "POST", url: base, data: input,
      idempotency: { scope: `${base}:create`, payload: input, lifecycle },
    }));
  },

  /** 局部更新：只传要改的字段；`api_key` 不传＝保留已保存的 Key。 */
  async update(id: string, input: Partial<SupplierTestConfigGroupInput> & { enabled?: boolean }, lifecycle: string) {
    return assertGroup(await apiRequest<SupplierTestConfigGroupDTO>({
      method: "PATCH", url: `${base}/${id}`, data: input,
      idempotency: { scope: `${base}:update:${id}`, payload: input, lifecycle },
    }));
  },

  async disable(id: string, lifecycle: string) {
    return assertGroup(await apiRequest<SupplierTestConfigGroupDTO>({
      method: "POST", url: `${base}/${id}/disable`, data: {},
      idempotency: { scope: `${base}:disable:${id}`, payload: {}, lifecycle },
    }));
  },
};
