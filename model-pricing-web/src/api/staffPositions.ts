import { apiRequest } from "./http";

/**
 * 内部人员业务岗位（000070）。
 *
 * 只用于展示与职责沟通：多岗位**不**授予超管、不改变审批链与办理人规则，
 * 真实权限仍由角色/权限点与指派关系决定。
 */
export type StaffPosition = "OPS" | "BIZ" | "SALES";

export const STAFF_POSITION_LABELS: Record<StaffPosition, string> = {
  OPS: "运营",
  BIZ: "商务",
  SALES: "销售",
};

interface WirePositionRow {
  staff_id: string;
  positions: string[];
}

export const staffPositionsApi = {
  list: async (): Promise<Record<string, StaffPosition[]>> => {
    const data = await apiRequest<{ list: WirePositionRow[] }>({ url: "/internal/staff-positions", method: "GET" });
    const out: Record<string, StaffPosition[]> = {};
    for (const row of data.list ?? []) {
      out[row.staff_id] = (row.positions ?? []).filter((item): item is StaffPosition => item === "OPS" || item === "BIZ" || item === "SALES");
    }
    return out;
  },

  save: async (staffId: string, positions: StaffPosition[], idempotencyKey: string): Promise<StaffPosition[]> => {
    const data = await apiRequest<{ staff_id: string; positions: string[] }>({
      url: `/internal/staff/${staffId}/positions`,
      method: "PUT",
      data: { positions },
      idempotency: { scope: `staff-positions:${idempotencyKey}`, payload: { staffId, positions } },
    });
    return (data.positions ?? []).filter((item): item is StaffPosition => item === "OPS" || item === "BIZ" || item === "SALES");
  },
};
