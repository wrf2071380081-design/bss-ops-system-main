export interface SupplyNotice {
  id: string;
  resource_id: string;
  resource_name: string;
  region: string;
  kind: string;
  title: string;
  content: string;
  reason: string;
  source_event_id?: string;
  affected_status: "PENDING" | "READY";
  affected_customer_count?: string;
  published_at: string;
}
export interface NoticeView {
  notice: SupplyNotice;
  staff_id: string;
  staff_name: string;
  read_status: "UNREAD" | "READ";
  read_at: string | null;
  permissions: { can_mark_read: boolean };
}
export interface NoticeQuery {
  resource_id?: string;
  region?: string;
  read_status?: string;
  page: number;
  size: number;
}
export interface NoticePage { list: NoticeView[]; total: number; page: number; size: number }
export interface NoticeReadReceipt { notice_id: string; staff_id: string; read_at: string; created: boolean }
export interface PlanSummary {
  permissions: { can_revise: boolean; can_cancel: boolean; can_execute: boolean };
  id: string;
  resource_id: string;
  region: string;
  kind: string;
  status: string;
  created_by: string;
  assignee_id?: string;
  supplier_model_id?: string;
  before_supplier_model_id?: string;
  after_supplier_model_id?: string;
  creator_name: string;
  revision: string;
  expected_resource_revision: string;
  before_coefficient?: string;
  after_coefficient?: string;
  current_resource_revision: string;
  due_at: string;
  executed_at: string | null;
  executed_by?: string;
  completion_note?: string;
  created_at: string;
  reason: string;
  is_initiator: boolean;
  read_only: true;
  precondition_matches: boolean;
  blocked_reasons: string[];
}
