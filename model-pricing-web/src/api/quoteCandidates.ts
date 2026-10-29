import { apiRequest } from "./http";

// 客户报价的资源口径：候选来自供应域已接入且可报价的资源。
// 成本口径 = 官方价项金额 × 采购系数；服务端据此算 floor，低于 floor 会被拒（409）。

export interface ResourceQuotePriceItem {
  item_key: string;
  label: string;
  task_type: string;
  amount: string;
  currency: string;
  unit_code: string;
  unit_quantity: string;
  unit_cost: string;
}

export interface ResourceQuoteCandidate {
  resource_id: number;
  model_name: string;
  official_id: string;
  company: string;
  model_type: string;
  supplier_id: number;
  supplier_name: string;
  region: string;
  supplier_model_id: string;
  coefficient: string;
  blocked_reasons?: string[];
  price_items: ResourceQuotePriceItem[];
}

export interface ResourceQuoteCandidatePage {
  list: ResourceQuoteCandidate[];
  total: number;
  page: number;
  size: number;
}

export interface ResourceQuoteItemInput {
  resource_id: number;
  price_item_key: string;
  unit_price: string;
}

export interface AffectedQuote {
  quote_id: number;
  version_no: number;
  status: string;
  quote_type: string;
  valid_until?: string;
  price_item_key: string;
  unit_price: string;
  currency: string;
}

export interface AffectedCustomer {
  customer_id: number;
  customer_name: string;
  level_code: string;
  owner_sales_operator_id: number;
  quotes: AffectedQuote[];
}

export interface AffectedCustomerResult {
  resource_id: number;
  list: AffectedCustomer[];
  total: number;
}

function num(value: unknown): number {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}

function validateCandidate(value: unknown): ResourceQuoteCandidate {
  const row = (value ?? {}) as Record<string, unknown>;
  const items = Array.isArray(row.price_items) ? row.price_items : [];
  return {
    resource_id: num(row.resource_id),
    model_name: String(row.model_name ?? ""),
    official_id: String(row.official_id ?? ""),
    company: String(row.company ?? ""),
    model_type: String(row.model_type ?? ""),
    supplier_id: num(row.supplier_id),
    supplier_name: String(row.supplier_name ?? ""),
    region: String(row.region ?? ""),
    supplier_model_id: String(row.supplier_model_id ?? ""),
    coefficient: String(row.coefficient ?? ""),
    blocked_reasons: Array.isArray(row.blocked_reasons) ? row.blocked_reasons.map(String) : [],
    price_items: items.map(item => {
      const price = (item ?? {}) as Record<string, unknown>;
      return {
        item_key: String(price.item_key ?? ""),
        label: String(price.label ?? ""),
        task_type: String(price.task_type ?? ""),
        amount: String(price.amount ?? ""),
        currency: String(price.currency ?? ""),
        unit_code: String(price.unit_code ?? ""),
        unit_quantity: String(price.unit_quantity ?? ""),
        unit_cost: String(price.unit_cost ?? ""),
      };
    }),
  };
}

export const quoteCandidatesApi = {
  // 候选资源（可报价）：供应域已判定 ADMITTED + 正式配置已确认 + 测试证据有效 + 模型就绪。
  list: (keyword = "", page = 1, size = 20) => apiRequest<ResourceQuoteCandidatePage>({
    url: "/internal/quote-candidates", method: "GET",
    params: { keyword: keyword || undefined, page, size },
  }).then(data => ({
    list: (data?.list ?? []).map(validateCandidate),
    total: Number(data?.total ?? 0),
    page: Number(data?.page ?? page),
    size: Number(data?.size ?? size),
  })),

  // 这条资源影响哪些客户（改价/上下架前的影响面）。
  affectedCustomers: (resourceId: number) => apiRequest<AffectedCustomerResult>({
    url: `/internal/resources/${resourceId}/affected-customers`, method: "GET",
  }).then(data => ({
    resource_id: Number(data?.resource_id ?? resourceId),
    total: Number(data?.total ?? 0),
    list: (data?.list ?? []).map(customer => ({
      customer_id: num(customer.customer_id),
      customer_name: String(customer.customer_name ?? ""),
      level_code: String(customer.level_code ?? ""),
      owner_sales_operator_id: num(customer.owner_sales_operator_id),
      quotes: (customer.quotes ?? []).map(quote => ({
        quote_id: num(quote.quote_id),
        version_no: Number(quote.version_no ?? 0),
        status: String(quote.status ?? ""),
        quote_type: String(quote.quote_type ?? ""),
        valid_until: quote.valid_until ? String(quote.valid_until) : undefined,
        price_item_key: String(quote.price_item_key ?? ""),
        unit_price: String(quote.unit_price ?? ""),
        currency: String(quote.currency ?? ""),
      })),
    })),
  })),
};
