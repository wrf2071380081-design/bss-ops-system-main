import type { IsoDateTime } from "../../../domain/common";
import { apiRequest } from "../../../api/http";

// ------------------------------------------------------------
// 旧工作台（指标卡 + 任务 + 提醒）——保留给 MSW mock 与兼容，
// 新页面统一走下面的 inbox/operations。
// ------------------------------------------------------------
export interface WorkbenchMetricDTO {
  id: string;
  label: string;
  value: string;
  suffix?: string;
  hint: string;
  tone: "NORMAL" | "SUCCESS" | "WARNING" | "DANGER";
}

export interface WorkbenchTaskDTO {
  id: string;
  title: string;
  module: string;
  description: string;
  count: number;
  level: "NORMAL" | "WARNING" | "URGENT";
  routeKey: string;
  routePath: string;
}

export interface WorkbenchNoticeDTO {
  id: string;
  title: string;
  description: string;
  occurredAt: IsoDateTime;
}

export interface WorkbenchDTO {
  metrics: WorkbenchMetricDTO[];
  tasks: WorkbenchTaskDTO[];
  notices: WorkbenchNoticeDTO[];
  generatedAt: IsoDateTime;
}

interface TodoWireDTO {
  id: string | number;
  biz_type: string;
  biz_id: string | number;
  title: string;
  priority: "LOW" | "MID" | "HIGH";
  status: "OPEN" | "DONE";
  created_at: IsoDateTime;
  deeplink: string;
  assignee_name: string;
}

interface MetricWireDTO {
  key: string;
  title: string;
  value: string;
  unit: string;
  trend: "UP" | "DOWN" | "FLAT" | "NA";
  deeplink: string;
}

function internalDeeplink(path: string) {
  if (path.startsWith("/quotes") || path.startsWith("/supplier/quotes")) return "/internal/supplier-quotes";
  if (path.startsWith("/customer-quotes") || path.startsWith("/customers")) return "/internal/customers";
  if (path.startsWith("/price-books")) return "/internal/price-books";
  if (path.startsWith("/pricing")) return "/internal/pricing/policies";
  if (path.startsWith("/suppliers")) return "/internal/suppliers";
  if (path.startsWith("/fx")) return "/internal/finance";
  return "/internal/workbench";
}

// ------------------------------------------------------------
// 新工作台：统一收件箱（F0，v0.22）
// ------------------------------------------------------------
export type InboxState = "TODO" | "READ" | "DONE" | "READ_DONE";

export interface InboxItemDTO {
  biz_type: string;
  biz_id: number;
  item_type: "TODO" | "READ";
  state: InboxState;
  title: string;
  biz_object: string;
  region?: string;
  priority: string;
  occurred_at: IsoDateTime;
  /** 责任人：原型「责任人」列（姓名 + 关系副标题）。 */
  assignee_id: number;
  assignee_name: string;
  /** 阅读接收人 / 审批人 / 办理人 / 发起人 / 处理人 */
  assignee_role: string;
  deeplink?: string;
  readable: boolean;
}

export interface InboxPageDTO {
  list: InboxItemDTO[];
  total: number;
  page: number;
  size: number;
}

export interface WorkbenchPersonDTO {
  id: number;
  name: string;
}

export interface InboxQuery {
  state: InboxState;
  assigneeId?: number;
  bizType?: string;
  region?: string;
  keyword?: string;
  page?: number;
  size?: number;
}

export interface OperationItemDTO {
  id: number;
  action: string;
  entity_type: string;
  entity_id: number;
  operator_name: string;
  reason?: string;
  created_at: IsoDateTime;
}

export interface OperationPageDTO {
  list: OperationItemDTO[];
  total: number;
  page: number;
  size: number;
}

export interface OperationQuery {
  operatorId?: number;
  entityType?: string;
  entityId?: number;
  page?: number;
  size?: number;
}

export const workbenchApi = {
  // 旧接口（保留兼容；新页面不再使用）
  get: async (): Promise<WorkbenchDTO> => {
    const [todos, metrics] = await Promise.all([
      apiRequest<{ list: TodoWireDTO[] }>({ url: "/internal/workbench/todos", method: "GET", params: { page: 1, size: 20, status: "OPEN" } }),
      apiRequest<{ cards: MetricWireDTO[] }>({ url: "/internal/workbench/metrics", method: "GET" }),
    ]);
    return {
      metrics: metrics.cards.map(card => ({
        id: card.key,
        label: card.title,
        value: card.value,
        suffix: card.unit,
        hint: card.trend === "NA" ? "实时统计" : `趋势 ${card.trend}`,
        tone: card.trend === "UP" ? "WARNING" : card.trend === "DOWN" ? "SUCCESS" : "NORMAL",
      })),
      tasks: todos.list.map(todo => ({
        id: String(todo.id),
        title: todo.title,
        module: todo.biz_type,
        description: `${todo.assignee_name || "待分派"} · ${todo.status}`,
        count: 1,
        level: todo.priority === "HIGH" ? "URGENT" : todo.priority === "MID" ? "WARNING" : "NORMAL",
        routeKey: "",
        routePath: internalDeeplink(todo.deeplink),
      })),
      notices: [],
      generatedAt: new Date().toISOString(),
    };
  },

  inbox: async (query: InboxQuery): Promise<InboxPageDTO> =>
    apiRequest<InboxPageDTO>({
      url: "/internal/workbench/inbox",
      method: "GET",
      params: {
        state: query.state,
        assignee_id: query.assigneeId || undefined,
        biz_type: query.bizType || undefined,
        region: query.region || undefined,
        keyword: query.keyword || undefined,
        page: query.page || 1,
        size: query.size || 20,
      },
    }),

  people: async (): Promise<WorkbenchPersonDTO[]> => {
    const data = await apiRequest<{ list: WorkbenchPersonDTO[] }>({ url: "/internal/workbench/people", method: "GET" });
    return data.list;
  },

  operations: async (query: OperationQuery): Promise<OperationPageDTO> =>
    apiRequest<OperationPageDTO>({
      url: "/internal/workbench/operations",
      method: "GET",
      params: {
        operator_id: query.operatorId || undefined,
        entity_type: query.entityType || undefined,
        entity_id: query.entityId || undefined,
        page: query.page || 1,
        size: query.size || 20,
      },
    }),

  markRead: async (noticeId: number): Promise<void> => {
    await apiRequest<{ notice_id: number }>({
      url: `/internal/workbench/notices/${noticeId}/read`,
      method: "POST",
      data: {},
      idempotency: { scope: `workbench:notice-read:${noticeId}`, payload: { noticeId } },
    });
  },
};
