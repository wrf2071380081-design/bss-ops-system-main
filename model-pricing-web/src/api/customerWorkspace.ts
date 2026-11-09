import { apiRequest } from "./http";

// 客户管理合并页（客户列表 / 报价 / 审批记录）的只读接口。
// 后端实现：internal/api/customer_workspace.go（纯只读 SQL，不写库）。

export interface CustomerWorkspaceRow {
  id: number;
  name: string;
  level_code: string;
  /** customer_profile 无来源列，后端恒返回空串，前端显示「—」 */
  source: string;
  owner_name: string;
  owner_sales_operator_id: number;
  region: "CN" | "OVERSEAS" | string;
  settlement_currency: "CNY" | "USD" | string;
  /** NULL 时后端返回空串，前端显示「未填写」 */
  tax_rate: string;
  /** 000037 落库：待记录 / 办理中 / 已归档 */
  contract_status: string;
  /** 派生：服务中 / 接入中 / 潜在客户 */
  service_status: string;
  /** 派生：已有 / 潜在 */
  cooperation_stage: string;
  handling_status: string;
  status: string;
  credit_limit: string;
  credit_used: string;
  active_quotes: number;
  pending_quotes: number;
  /**
   * 已开通模型数：后端按 standard_model_id 去重、且只算当前生效（state='ACTIVE'）的绑定。
   * 口径 = 原型「已开通模型数」：禁用/待处理/停供不计入（"现在能用"而不是"曾经有过关系"）。
   * 无绑定是合法值 `0`（不是 null），展示时不要用 `?? "—"`。
   */
  opened_model_count: number;
}

export interface CustomerQuoteRow {
  id: number;
  customer_id: number;
  customer_name: string;
  version_no: number;
  quote_type: string;
  status: string;
  price_book_version: number;
  valid_until: string;
  owner_name: string;
  item_count: number;
  /** 我方签约主体：报价时手填的那个，用于「继续编辑」回填（老单可能为空串）。 */
  our_company?: string;
  created_at: string;
}

export interface CustomerApprovalRow {
  id: number;
  quote_id: number;
  customer_name: string;
  status: string;
  reason: string;
  created_by: string;
  created_at: string;
}

export interface CustomerWorkspaceQuery {
  keyword?: string;
  page?: number;
  size?: number;
}

export const customerWorkspaceApi = {
  customers: async (query: CustomerWorkspaceQuery = {}): Promise<{ list: CustomerWorkspaceRow[]; total: number }> => {
    const data = await apiRequest<{ list: CustomerWorkspaceRow[]; total: number }>({
      url: "/internal/customer-workspace/customers",
      method: "GET",
      params: {
        keyword: query.keyword || undefined,
        page: query.page ?? 1,
        size: query.size ?? 20,
      },
    });
    return { list: data.list, total: data.total };
  },

  quotes: async (query: CustomerWorkspaceQuery & { customer_id?: number; status?: string } = {}): Promise<{ list: CustomerQuoteRow[]; total: number }> => {
    const data = await apiRequest<{ list: CustomerQuoteRow[]; total: number }>({
      url: "/internal/customer-workspace/quotes",
      method: "GET",
      params: {
        keyword: query.keyword || undefined,
        customer_id: query.customer_id || undefined,
        status: query.status || undefined,
        page: query.page ?? 1,
        size: query.size ?? 20,
      },
    });
    return { list: data.list, total: data.total };
  },

  approvals: async (): Promise<CustomerApprovalRow[]> => {
    const data = await apiRequest<{ list: CustomerApprovalRow[] }>({
      url: "/internal/customer-workspace/approvals",
      method: "GET",
    });
    return data.list;
  },
};
