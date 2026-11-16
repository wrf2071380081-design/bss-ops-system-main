import { apiRequest } from "./http";

/**
 * 供应商「生产平台服务商」关联（000069）。
 *
 * 口径：供应商是统一主体，它在国内/海外平台上"是不是服务商"由这张表的人工登记决定，
 * 与 supplier.region 无关，也不代表模型接入。
 */
export type PlatformServicePlatform = "CN" | "OVERSEAS";
export type PlatformServiceStatus = "PENDING" | "CONFIRMED";

export interface PlatformServiceDTO {
  id: string;
  supplier_id: string;
  platform: PlatformServicePlatform;
  service_name: string;
  identifier: string;
  status: PlatformServiceStatus;
  created_at?: string;
  confirmed_at?: string;
}

interface WireRow {
  id: string;
  supplier_id: string;
  platform: string;
  service_name: string;
  identifier: string;
  status: string;
  created_at?: string;
  confirmed_at?: string;
}

function normalize(row: WireRow): PlatformServiceDTO {
  return {
    id: row.id,
    supplier_id: row.supplier_id,
    platform: row.platform === "OVERSEAS" ? "OVERSEAS" : "CN",
    service_name: row.service_name,
    identifier: row.identifier,
    status: row.status === "CONFIRMED" ? "CONFIRMED" : "PENDING",
    created_at: row.created_at,
    confirmed_at: row.confirmed_at,
  };
}

export const supplierPlatformServicesApi = {
  list: async (supplierId: string): Promise<PlatformServiceDTO[]> => {
    const data = await apiRequest<{ list: WireRow[] }>({
      url: `/internal/suppliers/${supplierId}/platform-services`,
      method: "GET",
    });
    return (data.list ?? []).map(normalize);
  },

  create: async (
    supplierId: string,
    payload: { platform: PlatformServicePlatform; service_name: string; identifier: string },
    idempotencyKey: string,
  ): Promise<PlatformServiceDTO> => {
    const data = await apiRequest<WireRow>({
      url: `/internal/suppliers/${supplierId}/platform-services`,
      method: "POST",
      data: payload,
      idempotency: { scope: `supplier-platform-service:create:${idempotencyKey}`, payload },
    });
    return normalize(data);
  },

  confirm: async (supplierId: string, serviceId: string, idempotencyKey: string): Promise<PlatformServiceDTO> => {
    const data = await apiRequest<WireRow>({
      url: `/internal/suppliers/${supplierId}/platform-services/${serviceId}/confirm`,
      method: "POST",
      data: {},
      idempotency: { scope: `supplier-platform-service:confirm:${idempotencyKey}`, payload: { supplierId, serviceId } },
    });
    return normalize(data);
  },

  remove: async (supplierId: string, serviceId: string): Promise<void> => {
    await apiRequest<{ id: string }>({
      url: `/internal/suppliers/${supplierId}/platform-services/${serviceId}`,
      method: "DELETE",
    });
  },
};
