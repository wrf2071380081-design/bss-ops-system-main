import { apiRequest } from "./http";
import { assertContractId } from "../domain/contractId";

/**
 * 客户报价单审批（原型「提交审批 → 超管逐模型批准/驳回」，迁移 000057 + 000063）。
 *
 * 与「特价审批」区分：
 *  - 特价审批（`/internal/customer-workspace/approvals`）批的是「允许破线」，两步角色链；
 *  - 这里批的是「这张报价单这批模型可以对外」，一步、M9:A、逐模型、批完即派发。
 *
 * 000063 对齐原型后：
 *  - 结论有第三种 PARTIAL（部分批准）：批准的先办、驳回的可重报；
 *  - 提交结论时必填「处理意见」，有批准模型时必填「技术办理人」，客户合同未归档时必填「商务办理人」；
 *  - 撤回必须填原因。
 */

/** 一条价目快照（原型审批详情右侧「报价明细」的一行）。 */
export interface ApprovalPriceItemDTO {
  key: string;
  price: string;
  currency: string;
  /** 官方价（与 price 同计量口径）；老数据为空串。 */
  amount?: string;
  unit_code?: string;
  unit_quantity?: string;
  /** 报价系数 = 客户单价 ÷ 官方价，服务端按 decimal 算好（4 位）；算不出时为空串。 */
  coefficient?: string;
}

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
  /** 提交时的价目快照；老数据为空数组。 */
  price_detail: ApprovalPriceItemDTO[] | null;
}

/** 审批草稿（原型弹窗的「保存进度」）。允许不完整。 */
export interface ApprovalDraftDTO {
  rows: ApprovalDecisionInput[];
  note: string;
  ops_assignee_id: number;
  business_assignee_id: number;
  saved_by: number;
  saved_at: string | null;
}

/** 审批单（含逐模型行）。`status` 取值 PENDING / APPROVED / PARTIAL / REJECTED / WITHDRAWN。 */
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
  customer_name: string;
  quote_version: number;
  quote_status: string;
  /** 技术办理人（原型「有批准模型时的技术办理人」）。 */
  ops_assignee_id: number | null;
  /** 商务办理人（原型「必要商务办理人」）。 */
  business_assignee_id: number | null;
  /** 撤回原因（status=WITHDRAWN 时有值）。 */
  withdraw_reason: string;
  /** 审批草稿：审批人「保存进度」留下的填写状态，提交结论后消失。 */
  draft?: ApprovalDraftDTO | null;
  /** 重报来源审批 id；普通首次提交为 0。 */
  previous_approval_id: number;
  rows: QuoteApprovalRowDTO[];
}

export interface QuoteApprovalSummaryDTO {
  id: number;
  quote_id: number;
  customer_id: number;
  customer_name: string;
  status: string;
  model_count: number;
  submitted_by: number;
  submitted_name: string;
  submitted_at: string;
  decided_by: number | null;
  decided_name: string;
  decided_at: string | null;
}

export interface ApprovalDecisionInput {
  row_id: number;
  result: "APPROVED" | "REJECTED";
  reason?: string;
}

/** 逐模型审批入参：结论 + 处理意见 + 两个办理人。 */
export interface ApprovalDecideInput {
  rows: ApprovalDecisionInput[];
  note: string;
  ops_assignee_id: number;
  business_assignee_id: number;
}

/** 审批结论的中文名（对齐原型状态词：已分派 = 全部批准且已派人）。 */
export const APPROVAL_STATUS_LABELS: Record<string, string> = {
  PENDING: "待审批",
  APPROVED: "已分派",
  PARTIAL: "部分批准",
  REJECTED: "已驳回",
  WITHDRAWN: "已撤回",
};

export const APPROVAL_STATUS_TYPES: Record<string, "success" | "danger" | "info" | "warning"> = {
  PENDING: "warning",
  APPROVED: "success",
  PARTIAL: "warning",
  REJECTED: "danger",
  WITHDRAWN: "info",
};

/** 价项键 → 展示名（原型「输入 / 输出 / 缓存输入」）。未知键原样显示，不编造。 */
export const PRICE_ITEM_LABELS: Record<string, string> = {
  // 库里现存两种命名：官方价项用 input/output，历史 SKU 口径用 text_input/text_output。
  input: "输入",
  output: "输出",
  cache_input: "缓存输入",
  cache_write: "缓存写入",
  cache_storage: "缓存存储",
  image_input: "图片输入",
  image_output: "图片输出",
  image_cache_input: "图片缓存输入",
  audio_input: "音频输入",
  audio_output: "音频输出",
  text_input: "输入",
  text_output: "输出",
  text_cache_input: "缓存输入",
  text_cache_write: "缓存写入",
  text_cache_storage: "缓存存储",
};

const base = "/internal";

function quoteId(value: string | number) {
  const text = String(value);
  assertContractId(text);
  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error("报价单 ID 超出前端安全范围或非法，请刷新后重试。");
  return parsed;
}

export interface QuoteApprovalListQuery {
  /** 客户 id；不传或 0 = 跨客户（报价单页签的全局审批队列）。 */
  customerID?: string | number;
  status?: string;
  /** 销售（提交人）员工 id。 */
  submittedBy?: string | number;
  /** 报价编号 / 客户名关键字。 */
  keyword?: string;
  /** 时间区间，按 stage 决定筛「提交时间」还是「处理时间」。 */
  dateFrom?: string;
  dateTo?: string;
  stage?: "pending" | "history";
}

export const quoteApprovalApi = {
  /**
   * 提交审批：DRAFT → PENDING，按标准模型生成待决行。
   *
   * `previousApprovalId` 是关联重报（原型「关联驳回模型新申请」）：后端会只把
   * 上一张审批里被驳回的模型纳入本次审批，其余模型（已批准的）不再重复送审。
   */
  submit: (quoteID: string | number, lifecycle: string, previousApprovalId?: number) => {
    const payload = previousApprovalId && previousApprovalId > 0
      ? { lifecycle, previous_approval_id: previousApprovalId }
      : { lifecycle };
    return apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quotes/${quoteId(quoteID)}/submit-approval`,
      method: "POST",
      // lifecycle 必须进 body：服务端的幂等键是 sha256(body)+op+path，
      // 空 body 会让「驳回后重新提交」命中上一次的成功结果（返回 200 但什么也没写）。
      data: payload,
      idempotency: { scope: `customer:quote:${quoteID}:submit-approval`, payload, lifecycle },
    });
  },

  get: (approvalID: string | number, signal?: AbortSignal) =>
    apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}`,
      method: "GET",
      signal,
    }),

  /**
   * 逐模型审批。必须覆盖当前全部待决模型（后端逐行校验，漏一个直接 400）。
   * 驳回必填原因、处理意见必填、有批准必须选技术办理人——前端先挡，服务端再挡。
   */
  decide: (approvalID: string | number, input: ApprovalDecideInput, lifecycle: string) => {
    const payload = { ...input, lifecycle };
    return apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}/decision`,
      method: "POST",
      data: payload,
      idempotency: { scope: `customer:quote-approval:${approvalID}:decision`, payload, lifecycle },
    });
  },

  /** 保存进度（草稿）：允许不完整，不改变审批状态；提交结论后服务端自动清空。 */
  saveDraft: (approvalID: string | number, input: ApprovalDecideInput, lifecycle: string) => {
    const payload = { ...input, lifecycle };
    return apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}/draft`,
      method: "POST",
      data: payload,
      idempotency: { scope: `customer:quote-approval:${approvalID}:draft`, payload, lifecycle },
    });
  },

  /** 撤回：原因必填（对齐原型撤回弹窗）。 */
  withdraw: (approvalID: string | number, reason: string, lifecycle: string) => {
    const payload = { reason, lifecycle };
    return apiRequest<QuoteApprovalDTO>({
      url: `${base}/customer-quote-approvals/${quoteId(approvalID)}/withdraw`,
      method: "POST",
      data: payload,
      idempotency: { scope: `customer:quote-approval:${approvalID}:withdraw`, payload, lifecycle },
    });
  },

  /**
   * 审批单列表。不传 customerID = 跨客户（客户报价单页签的全局审批队列），
   * 服务端限 200 条；传客户 id 则是客户详情「报价审批」用的客户维度列表。
   * 筛选对齐原型：销售 / 状态 / 时间 / 关键字。
   */
  list: async (query: QuoteApprovalListQuery = {}, signal?: AbortSignal): Promise<QuoteApprovalSummaryDTO[]> => {
    const scoped = Number(query.customerID) > 0;
    const submittedBy = Number(query.submittedBy) > 0 ? Number(query.submittedBy) : 0;
    const data = await apiRequest<{ list: QuoteApprovalSummaryDTO[] }>({
      url: `${base}/customer-workspace/quote-approvals`,
      method: "GET",
      params: {
        ...(scoped ? { customer_id: quoteId(query.customerID as string | number) } : {}),
        ...(query.status ? { status: query.status } : {}),
        ...(submittedBy ? { submitted_by: submittedBy } : {}),
        ...(query.keyword?.trim() ? { keyword: query.keyword.trim() } : {}),
        ...(query.dateFrom ? { date_from: query.dateFrom } : {}),
        ...(query.dateTo ? { date_to: query.dateTo } : {}),
        ...(query.stage ? { stage: query.stage } : {}),
      },
      signal,
    });
    return data?.list ?? [];
  },
};

/** 计量单位：把 unit_code + unit_quantity 说成人话（「/ 百万 tokens」）。 */
export function priceUnitText(item: ApprovalPriceItemDTO): string {
  const code = (item.unit_code || "").toUpperCase();
  if (!code) return "";
  const quantity = Number(item.unit_quantity || 0);
  const names: Record<string, string> = {
    TOKEN: "tokens",
    CHARACTER: "字符",
    IMAGE: "张图",
    SECOND: "秒",
    MINUTE: "分钟",
    HOUR: "小时",
    CALL: "次调用",
    PIECE: "个",
    GB: "GB",
  };
  const name = names[code] || code.toLowerCase();
  if (code === "TOKEN") {
    if (quantity === 1000000) return "百万 tokens";
    if (quantity === 1000) return "千 tokens";
    if (quantity > 1) return `${quantity} tokens`;
    return "tokens";
  }
  return quantity > 1 ? `${quantity} ${name}` : name;
}

/** 单条价目的展示文：`输入：USD 3 / 百万 tokens`。 */
export function priceItemText(item: ApprovalPriceItemDTO): string {
  const label = PRICE_ITEM_LABELS[item.key] || item.key || "价项";
  const symbol = item.currency === "USD" ? "USD " : item.currency === "CNY" ? "¥" : "";
  const unit = priceUnitText(item);
  return `${label}：${symbol}${item.price}${unit ? ` / ${unit}` : ""}`;
}

/**
 * 模型行的报价系数：同一模型的各价项按同一系数折算，正常只有一个值；
 * 销售对不同价项给了不同折扣时按「最低 ~ 最高」显示，不藏信息。
 */
export function approvalCoefficientText(row: QuoteApprovalRowDTO): string {
  const values = [...new Set((row.price_detail ?? []).map(item => (item.coefficient || "").trim()).filter(Boolean))];
  if (!values.length) return "—";
  if (values.length === 1) return values[0];
  const sorted = values.map(Number).filter(value => Number.isFinite(value)).sort((a, b) => a - b);
  if (sorted.length < 2) return values[0];
  return `${sorted[0]} ~ ${sorted[sorted.length - 1]}`;
}
