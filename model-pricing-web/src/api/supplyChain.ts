import { apiRequest } from "./http";

export interface SupplierModelOption {
  id: string;
  name: string;
  company: string;
  model_type: string;
  official_id: string;
}

export interface ApprovalRow {
  id: number;
  supplier_id: number;
  supplier_name: string;
  region: string;
  status: string;
  row_count: number;
  first_model: boolean;
  submitted_at: string;
  submitter: string;
  kind: string;
}

export interface SupplierWorkspaceRow {
  id: number;
  name: string;
  region: string;
  phone: string;
  resource_count: number;
  credit_amount: string;
  settlement_currency: string;
  tax_rate: string;
  contract_status: string;
  cooperation_stage: string;
  handling_status: string;
}

export interface ApprovalHistoryRow {
  id: number;
  supplier_id: number;
  supplier_name: string;
  kind: string;
  status: string;
  region: string;
  row_count: number;
  approver: string;
  approved_at: string;
  result: string;
  note: string;
}

export interface CreateSubmissionPayload {
  model_id: number;
  supplier_model_name: string;
  coefficient: string;
}

export interface ApprovePayload {
  test_assignee_id: number;
  contract_assignee_id?: number;
  reason: string;
}

export const supplyChainApi = {
  standardModels: async (): Promise<SupplierModelOption[]> => {
    const data = await apiRequest<{ list: SupplierModelOption[] }>({ url: "/supplier/standard-models", method: "GET" });
    return data.list;
  },

  createSubmission: async (payload: CreateSubmissionPayload): Promise<number> => {
    const data = await apiRequest<{ submission_id: number }>({
      url: "/supplier/submissions",
      method: "POST",
      data: payload,
      idempotency: { scope: "supplier:submission", payload },
    });
    return data.submission_id;
  },

  suppliers: async (): Promise<SupplierWorkspaceRow[]> => {
    const data = await apiRequest<{ list: SupplierWorkspaceRow[] }>({ url: "/internal/supply/suppliers", method: "GET" });
    return data.list;
  },

  approvalHistory: async (): Promise<ApprovalHistoryRow[]> => {
    const data = await apiRequest<{ list: ApprovalHistoryRow[] }>({ url: "/internal/approvals/history", method: "GET" });
    return data.list;
  },

  approvals: async (): Promise<ApprovalRow[]> => {
    const data = await apiRequest<{ list: ApprovalRow[] }>({ url: "/internal/approvals/submissions", method: "GET" });
    return data.list;
  },

  approve: async (id: number, payload: ApprovePayload): Promise<void> => {
    await apiRequest({
      url: `/internal/approvals/submissions/${id}/approve`,
      method: "POST",
      data: payload,
      idempotency: { scope: `approval:approve:${id}`, payload },
    });
  },

  simulate: async (id: number): Promise<{ resource_id: string; state: string; revision: number }> => {
    const data = await apiRequest<{ resource_id: string; state: string; revision: number }>({
      url: `/internal/approvals/submissions/${id}/simulate`,
      method: "POST",
      data: {},
      idempotency: { scope: `approval:simulate:${id}`, payload: { id } },
    });
    return data;
  },
};
