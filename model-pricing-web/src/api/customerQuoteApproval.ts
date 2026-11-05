import { apiRequest } from "./http";
import { assertContractId } from "../domain/contractId";

/**
 * 客户报价单审批（原型「提交审批 → 超管逐模型批准/驳回」，迁移 000057）。
 *
 * 与「特价审批」区分：
 *  - 特价审批（`/internal/customer-workspace/approvals`）批的是「允许破线」，两步角色链；
 *  - 这里批的是「这张报价单这批模型可以对外」，一步、M9:A、逐模型。
 */

/** 逐模型决定行。`result` 取值 PENDING / APPROVED / REJECTED。 */
export interface QuoteApprovalRowDTO {
  id: number;
  standard_model_id: number;
  model_name: string;
  official_id: string;
  item_count: number;
  price_min: string;
  price_max: string;
  result: string;
  reason: string;
  decided_by: number | null;
  decided_at: string | null;
}

/** 审批单（含逐模型行）。`status` 取值 PENDING / APPROVED / REJECTED / WITHDRAWN。 */
export interface QuoteApprovalDTO {
  id: number;
  quote_id: number;
  status: string;
  model_count: number;
  note: string;
  submitted_by: number;
  submitted_at: string;
  decided_by: number | null;
  decided_at: string | null;
  customer_id: number;
  quote_version: number;
  quote_status: string;
  rows: QuoteApprovalRowDTO[];
}

export interface QuoteApprovalSummaryDTO {
  id: number;
  quote_id: number;
  customer_id: number;
  status: string;
  model_count: number;
  submitted_by: number;
  submitted_at: string;
  decided_by: number | null;
  decided_at: string | null;
}

export interface ApprovalDecisionInput {
  row_id: number;
  result: "APPROVED" | "REJECTED";
  reason?: string;
}

const base = "/internal";

function quoteId(value: string | number) {
  const text = String(value);
  assertContractId(text);
  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error("报价单 ID 超出前端安全范围或非法，请刷新后重试。");
  return parsed;
}

export const quoteApprovalApi = {
  /** 提交审批：DRAFT → PENDING，按标准模型生成待决行。 */
  submit: (quoteID: string | number, lifecycle: string) =>
    apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quotes/${quoteId(quoteID)}/submit-approval`,
      method: "POST",
      // lifecycle 必须进 body：服务端的幂等键是 sha256(body)+op+path，
      // 空 body 会让「驳回后重新提交」命中上一次的成功结果（返回 200 但什么也没写）。
      data: { lifecycle },
      idempotency: { scope: `customer:quote:${quoteID}:submit-approval`, payload: { lifecycle }, lifecycle },
    }),

  get: (approvalID: string | number, signal?: AbortSignal) =>
    apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}`,
      method: "GET",
      signal,
    }),

  /**
   * 逐模型审批。必须覆盖当前全部待决模型（后端逐行校验，漏一个直接 400）。
   * 驳回必填原因，前端先挡一次，服务端再挡一次。
   */
  decide: (approvalID: string | number, rows: ApprovalDecisionInput[], lifecycle: string) => {
    const payload = { rows, lifecycle };
    return apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}/decision`,
      method: "POST",
      data: payload,
      idempotency: { scope: `customer:quote-approval:${approvalID}:decision`, payload, lifecycle },
    });
  },

  withdraw: (approvalID: string | number, lifecycle: string) =>
    apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}/withdraw`,
      method: "POST",
      data: { lifecycle },
      idempotency: { scope: `customer:quote-approval:${approvalID}:withdraw`, payload: { lifecycle }, lifecycle },
    }),

  list: async (customerID: string | number, status = "", signal?: AbortSignal): Promise<QuoteApprovalSummaryDTO[]> => {
    const data = await apiRequest<{ list: QuoteApprovalSummaryDTO[] }>({
      url: `${base}/customer-workspace/quote-approvals`,
      method: "GET",
      params: { customer_id: quoteId(customerID), ...(status ? { status } : {}) },
      signal,
    });
    return data?.list ?? [];
  },
};
