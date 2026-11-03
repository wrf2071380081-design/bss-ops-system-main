import { apiRequest } from "./http";
import { assertContractId } from "../domain/contractId";
import { resourcesApi } from "./resources";

/**
 * 客户详情页（B3b 第 1 项）与建客户向导（第 2 项）用到的接口。
 *
 * 字段风格与 `customerWorkspace.ts` 保持一致：**后端 JSON 直接透传 snake_case，不做 mapper**。
 * 理由：这几个接口的字段集合还在演进（B1/B2 刚加过字段），中间加一层 mapper 只会让
 * "后端加了字段、前端忘了映射"变成常态；类型声明写全即可。
 */

const basePath = "/internal/customers";

function customerIdParam(value: string) {
  assertContractId(value);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error("客户 ID 超出前端安全范围或非法，请刷新后重试。");
  return parsed;
}

function idParam(value: string | number, label: string) {
  const text = String(value);
  assertContractId(text);
  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error(`${label}超出前端安全范围或非法，请刷新后重试。`);
  return parsed;
}

/** 客户基础档案（GET /internal/customers/:id）。 */
export interface CustomerProfileDTO {
  id: number;
  subject_id: number;
  legal_name: string;
  uscc: string;
  subject_type: string;
  region: string;
  settlement_currency: string;
  /** 未填写时后端返回空串（不是 null）。 */
  tax_rate: string;
  contract_status: string;
  owner_sales_operator_id: number;
  owner_sales_name: string;
  payment_type: string;
  billing_cycle: number;
  credit_limit: string;
  credit_used: string;
  status: string;
  created_at: string;
  updated_at: string;
}

/**
 * 接入模型投影行（GET /internal/customers/:id/resource-links）。
 *
 * `service_state` 取值：服务中 / 已禁用 / 来源待处理 / 无可用来源。
 * 后端本轮**不会**产出「接入中」（那要等开通流程投影），前端不要为它写特判。
 */
export interface CustomerModelServiceRow {
  standard_model_id: number;
  model_name: string;
  region: string;
  service_state: string;
  bound_count: number;
  available_count: number;
  risk_flags: string[];
}

export interface OnboardingNodeDTO {
  id: number;
  key: string;
  status: string;
  assignee_id: number;
  assignee_name: string;
  completed_at?: string;
  note?: string;
}

export interface OnboardingModelRowDTO {
  standard_model_id: number;
  model_name: string;
  resource_id: number;
  supplier_name: string;
  region: string;
}

export interface OnboardingLinkDTO {
  id: number;
  standard_model_id: number;
  resource_id: number;
  state: string;
  archived: boolean;
  activation_note?: string;
  activated_at?: string;
}

export interface OnboardingFlowDTO {
  id: number;
  customer_profile_id: number;
  status: string;
  current_node: string;
  created_at: string;
  updated_at: string;
  nodes: OnboardingNodeDTO[];
  models: OnboardingModelRowDTO[];
  links: OnboardingLinkDTO[];
  /** 后端已拼好的跳转链接（本页自己就是落点，B3b 不用它拼路由，但保留字段以便其它端复用）。 */
  deeplink: string;
}

export interface CustomerOperationLogDTO {
  id: number;
  action: string;
  /** 服务端已翻译好的中文文案，前端直接用，不要自建映射。 */
  action_label: string;
  entity_type: string;
  entity_id: number;
  operator_name: string;
  reason: string;
  /** jsonb 取出后的 **JSON 字符串**（不是对象）；解析失败按原样展示。 */
  before: string;
  after: string;
  request_id: string;
  created_at: string;
}

export interface CreateCustomerPayload {
  legal_name: string;
  subject_type?: string;
  uscc?: string;
  region: string;
  settlement_currency: string;
  /** 字符串小数（如 "0.06"）；不要传 number。 */
  tax_rate?: string;
  owner_sales_operator_id: number;
  payment_type?: string;
  /** 不填就不传（传 0 会被后端判 400）。 */
  billing_cycle?: number;
  reason?: string;
}

export interface OnboardingModelInput {
  standard_model_id: number;
  resource_id: number;
}

export interface StartOnboardingPayload {
  first_order: boolean;
  contract_assignee_id?: number;
  config_assignee_id: number;
  models: OnboardingModelInput[];
  reason?: string;
}

export interface CompleteNodePayload {
  /** 必填：CONTRACT 节点是「合同与授信已归档」的确认依据，CONFIG 是资源配置说明。 */
  reason: string;
  /** 可选：合同号 / F12 记录号，落审计。 */
  f12_reference?: string;
}

/** 发起开通时可选的「标准模型 × 供应资源」候选。 */
export interface OnboardingCandidate {
  standard_model_id: number;
  model_name: string;
  resource_id: number;
  supplier_name: string;
  region: string;
  coefficient: string;
}

export const customerDetailApi = {
  profile: (customerId: string) =>
    apiRequest<CustomerProfileDTO>({ url: `${basePath}/${customerIdParam(customerId)}`, method: "GET" }),

  resourceLinks: (customerId: string, signal?: AbortSignal) =>
    apiRequest<{ list: CustomerModelServiceRow[] }>({
      url: `${basePath}/${customerIdParam(customerId)}/resource-links`, method: "GET", signal,
    }).then(data => ({ list: data?.list ?? [] })),

  /** 没有流程时后端返回 404（不是空对象），调用方按「尚未发起开通」处理。 */
  onboarding: (customerId: string, signal?: AbortSignal) =>
    apiRequest<OnboardingFlowDTO>({
      url: `${basePath}/${customerIdParam(customerId)}/onboarding`, method: "GET", signal,
    }),

  operationLogs: (customerId: string, page = 1, size = 20, signal?: AbortSignal) =>
    apiRequest<{ list: CustomerOperationLogDTO[]; total: number; page: number; size: number }>({
      url: `${basePath}/${customerIdParam(customerId)}/operation-logs`, method: "GET",
      params: { page, size }, signal,
    }),

  createCustomer: (payload: CreateCustomerPayload) =>
    apiRequest<CustomerProfileDTO>({
      url: basePath, method: "POST", data: payload,
      idempotency: { scope: "customer:create", payload },
    }),

  startOnboarding: (customerId: string, payload: StartOnboardingPayload) =>
    apiRequest<OnboardingFlowDTO>({
      url: `${basePath}/${customerIdParam(customerId)}/onboarding`, method: "POST", data: payload,
      idempotency: { scope: `customer:onboarding:${customerId}`, payload },
    }),

  completeNode: (nodeId: string | number, payload: CompleteNodePayload) =>
    apiRequest<OnboardingFlowDTO>({
      url: `/internal/customer-nodes/${idParam(nodeId, "节点 ID ")}/complete`, method: "POST", data: payload,
      idempotency: { scope: `customer:node:${nodeId}:complete`, payload },
    }),

  /**
   * 发起开通的候选来源：**已准入（ADMITTED）且与客户同部署节点**的资源。
   *
   * 数据源刻意用 `GET /internal/resources`（供应域只要求「内部员工」，不挂功能权限点），
   * 而不是 `/internal/quote-candidates`：
   *   - 后者要求 M9:V，且只返回「可报价」的资源（比 ADMITTED 更严），
   *     用它会让「已准入但尚未配正式配置」的资源选不到，反而卡住开通；
   *   - 前者字段里有 `standard_model_id`，正好是开通接口需要的；`quote-candidates` 没有这个字段。
   * 过滤在后端做（`state=ADMITTED` + `region`），前端只做展示。
   */
  async onboardingCandidates(region: string, keyword = "", page = 1, size = 20, signal?: AbortSignal) {
    const result = await resourcesApi.rows({
      view: "flat", page, size, sort_by: "coefficient", order: "asc",
      region: region || undefined, state: "ADMITTED", keyword: keyword || undefined,
    }, signal);
    return {
      list: result.list.map((row): OnboardingCandidate => ({
        standard_model_id: Number(row.standard_model_id),
        model_name: row.model_name,
        resource_id: Number(row.id),
        supplier_name: row.supplier_name,
        region: row.region,
        coefficient: row.coefficient,
      })),
      total: result.total,
      page: result.page,
      size: result.size,
    };
  },
};
