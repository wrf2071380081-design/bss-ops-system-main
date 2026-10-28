import { apiRequest } from "./http";

const base = "/supplier/test-credential";

/**
 * 主体级准入测试凭据（000046，原型对齐）。
 *
 * 原型把「测试接口 URL / 测试 API Key」放在申报页顶部，一句
 * 「仅供本主体准入测试、供申报模型共用」——所以它挂在主体上，
 * 不属于任何一张申报单。
 *
 * Key 只进不出：服务端只回 has_api_key，前端永远拿不到明文。
 */
export interface SupplierTestCredentialDTO {
  base_url: string;
  has_api_key: boolean;
  configured: boolean;
  updated_at?: string;
}

export interface SupplierTestCredentialInput {
  base_url: string;
  /** 不传 = 留空保留已保存值；传空串 = 显式清空。 */
  api_key?: string;
}

export const supplierTestCredentialApi = {
  async get(signal?: AbortSignal) {
    const result = await apiRequest<SupplierTestCredentialDTO>({ url: base, signal });
    if (!result || typeof result.base_url !== "string" || typeof result.has_api_key !== "boolean") {
      throw new Error("测试凭据响应不符合约定，请重新加载。");
    }
    return result;
  },
  async save(input: SupplierTestCredentialInput, lifecycle: string) {
    const result = await apiRequest<SupplierTestCredentialDTO>({
      method: "PUT",
      url: base,
      data: input,
      idempotency: { scope: base, payload: input, lifecycle },
    });
    if (!result || typeof result.base_url !== "string") throw new Error("保存测试凭据的响应不符合约定。");
    return result;
  },
};
