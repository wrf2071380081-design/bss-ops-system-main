import { assertContractId } from "../domain/contractId";
import { apiFileRequest, apiRequest } from "./http";
import type { CustomerQuoteContextDTO, CustomerQuotePreviewItemDTO, FloorViolationDTO, GenerateCustomerQuoteDraft, GeneratedCustomerQuoteDTO, RefreshQuoteResultDTO, SpecialPriceResultDTO } from "./customerQuotes.types";

// Stage 9a real-backend write endpoint. Later customer-quote operations remain unimplemented.
const basePath = "/internal/customer-quotes";

function requestId(value: string) {
  assertContractId(value);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) throw new Error("ID超出前端安全范围，请后端支持字符串ID请求。");
  return parsed;
}

function responseId(value: unknown) {
  assertContractId(value);
  return String(value);
}

interface GeneratedQuoteWireDTO {
  id: string | number;
  customer_id: string | number;
  version_no: number;
  status: "DRAFT";
  quote_type: GeneratedCustomerQuoteDTO["quoteType"];
  item_count: number;
  below_floor_count: number;
  floor_violations?: Array<{
    sku_id?: string | number; sku_code?: string;
    resource_id?: string | number; price_item_key?: string; model_name?: string; supplier_name?: string;
    unit_price: string; floor_price: string;
  }>;
  price_book_version: number;
  valid_until?: string;
  owner_sales_operator_id: string | number;
  created_at: string;
}

interface QuotePreviewItemWireDTO {
  sku_id: string | number;
  sku_code: string;
  currency: string;
  unit_price: string;
}

const mapPreviewItem = (item: QuotePreviewItemWireDTO): CustomerQuotePreviewItemDTO => ({
  skuId: responseId(item.sku_id), skuCode: item.sku_code, currency: item.currency, unitPrice: item.unit_price,
});

/** 「继续编辑」回填：一条价项（官方价 + 已存单价）。 */
export interface QuoteForEditPriceItemDTO {
  item_key: string;
  label: string;
  amount: string;
  currency: string;
  unit_code: string;
  unit_quantity: string;
  unit_price: string;
  floor_price: string;
}

/** 「继续编辑」回填：一行 = 一个模型 + 一个供应来源。 */
export interface QuoteForEditLineDTO {
  resource_id: number;
  standard_model_id: number;
  model_name: string;
  official_id: string;
  company: string;
  model_type: string;
  supplier_id: number;
  supplier_name: string;
  supplier_model: string;
  region: string;
  /** 采购系数（成本折扣），不是客户报价系数。 */
  coefficient: string;
  /**
   * 对外报价系数（销售当初填的原值）。历史单据没有记录，为空串——
   * 那种情况下前端只能用「单价 ÷ 官方价」兜一个编辑初值，但绝不用于导出。
   */
  quotation_coefficient: string;
  /** 来源最近一次实测：连通(ms) / RPS / TPS / 缓存命中率(%)；没有报告的来源为空串。 */
  test_connection_ms: string;
  test_rps: string;
  test_tps: string;
  test_cache_hit_rate: string;
  price_items: QuoteForEditPriceItemDTO[];
}

/** 「继续编辑」回填的整张单。 */
export interface QuoteForEditDTO {
  id: number;
  customer_id: number;
  customer_name: string;
  version_no: number;
  status: string;
  our_company: string;
  valid_until: string | null;
  lines: QuoteForEditLineDTO[];
  /** SKU 口径的老明细条数：资源口径编辑器表达不了，未带出。 */
  skipped_items: number;
}

export const customerQuotesApi = {
  context: (customerId: string, page = 1, size = 100) => apiRequest<{
    customer_id: string | number;
    level_code: string;
    price_book: null | { id: string | number; level_code: string; version_no: number; items: QuotePreviewItemWireDTO[] };
    history: { list: Array<{ id: string | number; version_no: number; status: string; quote_type: string; valid_until?: string;
      price_book_version: number; created_at: string; items: QuotePreviewItemWireDTO[] }>; total: number; page: number; size: number };
  }>({ url: `/internal/customers/${requestId(customerId)}/quote-context`, method: "GET", params: { page, size } }).then((data): CustomerQuoteContextDTO => ({
    customerId: responseId(data.customer_id), levelCode: data.level_code,
    priceBook: data.price_book ? { id: responseId(data.price_book.id), levelCode: data.price_book.level_code,
      versionNo: data.price_book.version_no, items: data.price_book.items.map(mapPreviewItem) } : undefined,
    history: { ...data.history, list: data.history.list.map(item => ({ id: responseId(item.id), versionNo: item.version_no,
      status: item.status, quoteType: item.quote_type, validUntil: item.valid_until, priceBookVersion: item.price_book_version,
      createdAt: item.created_at, items: item.items.map(mapPreviewItem) })) },
  })),
  generate: (draft: GenerateCustomerQuoteDraft) => {
    const payload = {
      customer_id: requestId(draft.customerId), quote_type: draft.quoteType,
      ...(draft.sourceQuoteId ? { source_quote_id: requestId(draft.sourceQuoteId) } : {}),
      ...(draft.validTo ? { valid_to: draft.validTo } : {}),
      ...(draft.items?.length ? { items: draft.items.map(item => ({ sku_id: requestId(item.skuId), unit_price: item.unitPrice })) } : {}),
    };
    return apiRequest<GeneratedQuoteWireDTO>({ url: basePath, method: "POST", data: payload,
      idempotency: { scope: "customer-quote:generate", payload } }).then((data): GeneratedCustomerQuoteDTO => ({
        id: responseId(data.id), customerId: responseId(data.customer_id), versionNo: data.version_no,
        status: data.status, quoteType: data.quote_type, itemCount: data.item_count, belowFloorCount: data.below_floor_count,
        floorViolations: (data.floor_violations || []).map((item): FloorViolationDTO => ({
          ...(item.sku_id !== undefined ? { skuId: responseId(item.sku_id) } : {}),
          ...(item.sku_code ? { skuCode: item.sku_code } : {}),
          ...(item.resource_id !== undefined ? { resourceId: responseId(item.resource_id) } : {}),
          ...(item.model_name ? { resourceLabel: `${item.model_name}${item.price_item_key ? " / " + item.price_item_key : ""}` } : {}),
          unitPrice: item.unit_price, floorPrice: item.floor_price })),
        priceBookVersion: data.price_book_version, validUntil: data.valid_until,
        ownerSalesOperatorId: responseId(data.owner_sales_operator_id), createdAt: data.created_at,
      }));
  },
  // 资源口径出单（quote_type=RESOURCE）：候选与成本口径来自供应域资源。
  // ourCompany 是我方签约主体：随单落库，导出与「继续编辑」回填都靠它。
  /** 读一张报价单用于继续编辑：头 + 按来源分组的明细（含官方价与已存单价）。 */
  getForEdit: (quoteID: string | number) =>
    apiRequest<QuoteForEditDTO>({ url: `${basePath}/${requestId(String(quoteID))}`, method: "GET" }),

  generateResource: (draft: { customerId: string; validTo?: string; ourCompany?: string; items: Array<{ resourceId: string; priceItemKey: string; unitPrice: string; coefficient: string }> }) => {
    const payload = {
      customer_id: requestId(draft.customerId),
      ...(draft.validTo ? { valid_to: draft.validTo } : {}),
      ...(draft.ourCompany ? { our_company: draft.ourCompany } : {}),
      items: draft.items.map(item => ({
        resource_id: requestId(item.resourceId), price_item_key: item.priceItemKey,
        // 系数是报价单主数据，单价只是它乘官方价的结果；两者都要传，后端落库并校验自洽。
        unit_price: item.unitPrice, coefficient: item.coefficient,
      })),
    };
    return apiRequest<GeneratedQuoteWireDTO>({ url: `${basePath}/resource`, method: "POST", data: payload,
      idempotency: { scope: "customer-quote:generate-resource", payload } }).then((data): GeneratedCustomerQuoteDTO => ({
        id: responseId(data.id), customerId: responseId(data.customer_id), versionNo: data.version_no,
        status: data.status, quoteType: data.quote_type, itemCount: data.item_count, belowFloorCount: data.below_floor_count,
        floorViolations: (data.floor_violations || []).map((item): FloorViolationDTO => ({
          ...(item.sku_id !== undefined ? { skuId: responseId(item.sku_id) } : {}),
          ...(item.sku_code ? { skuCode: item.sku_code } : {}),
          ...(item.resource_id !== undefined ? { resourceId: responseId(item.resource_id) } : {}),
          ...(item.model_name ? { resourceLabel: `${item.model_name}${item.price_item_key ? " / " + item.price_item_key : ""}` } : {}),
          unitPrice: item.unit_price, floorPrice: item.floor_price })),
        priceBookVersion: data.price_book_version, validUntil: data.valid_until,
        ownerSalesOperatorId: responseId(data.owner_sales_operator_id), createdAt: data.created_at,
      }));
  },
  requestSpecialPrice: (id: string, reason: string, expectedMargin: string) => {
    const payload = { reason: reason.trim(), expected_margin: expectedMargin.trim() };
    return apiRequest<{ quote_id: string | number; change_request_id: string | number; step_count: number; status: string;
      margin_impact?: { current_price: string; current_margin: string; target_price: string; target_margin: string; delta_gap_distance: string } }>(
      { url: `${basePath}/${requestId(id)}/special-price`, method: "POST", data: payload,
        idempotency: { scope: `customer-quote:special-price:${id}`, payload } },
    ).then((data): SpecialPriceResultDTO => ({ quoteId: responseId(data.quote_id), changeRequestId: responseId(data.change_request_id),
      stepCount: data.step_count, status: data.status, marginImpact: data.margin_impact ? { currentPrice: data.margin_impact.current_price,
        currentMargin: data.margin_impact.current_margin, targetPrice: data.margin_impact.target_price,
        targetMargin: data.margin_impact.target_margin, deltaGapDistance: data.margin_impact.delta_gap_distance } : undefined }));
  },
  refresh: (id: string, reason: string) => {
    const payload = { reason: reason.trim() };
    return apiRequest<{ quote_id: string | number; old_version_no: number; new_version_no: number; unchanged: boolean; status: string;
      changed_items: Array<{ sku_id: string | number; old_floor_price: string; new_floor_price: string }> }>(
      { url: `${basePath}/${requestId(id)}/refresh`, method: "POST", data: payload,
        idempotency: { scope: `customer-quote:refresh:${id}`, payload } },
    ).then((data): RefreshQuoteResultDTO => ({ quoteId: responseId(data.quote_id), oldVersionNo: data.old_version_no,
      newVersionNo: data.new_version_no, unchanged: data.unchanged, status: data.status,
      changedItems: data.changed_items.map(item => ({ skuId: responseId(item.sku_id), oldFloorPrice: item.old_floor_price, newFloorPrice: item.new_floor_price })) }));
  },
  exportXlsx: (id: string) => apiFileRequest({ url: `${basePath}/${requestId(id)}/export`, method: "GET", params: { format: "xlsx" } }),
};
