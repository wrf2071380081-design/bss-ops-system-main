export interface StandardFactContext { label?: string; unit?: string; semantics?: string; condition?: string; scope?: string; source_url?: string; quote?: string }
export interface StandardFact extends StandardFactContext { key: string; disclosure: string; value: string }
export interface StandardAbility extends StandardFactContext { key: string; state: string; description: string }
export interface StandardSource { label: string; url: string; source_kind: string }
export interface StandardPrice {
  item_key: string; label: string; task_type: string; amount: string; currency: string;
  unit_code: string; unit_quantity: string; condition: string; dimensions: Record<string, string>; source_url: string;
}
export interface StandardModelData {
  company: string; name: string; official_id: string; model_type: string;
  input_modalities: string[]; output_modalities: string[];
  capacity: StandardFact[]; abilities: StandardAbility[]; price_items: StandardPrice[];
  api_access: StandardFact[]; lifecycle: StandardFact[]; sources: StandardSource[]; checked_at: string;
}
export interface StandardModelDTO extends StandardModelData {
  id: string; maintainer_id: string; revision: string; version_no: string; current_version_no?: string;
  status: string; verify_status: string; checked_by: string; created_at: string; updated_at: string;
  missing_fields: string[]; quote_readiness: { ready: boolean; reasons: string[]; standard_model_version_no: string };
  permissions: { can_edit: boolean; can_verify: boolean }; changed?: boolean;
  /**
   * 非空表示这一行后端读不出来、已降级成占位行：只保证 id/名称/公司/类型/状态可展示，
   * 其余字段不可信。列表把这一行渲染成「资料异常」，不让整页打不开。
   */
  load_error?: string;
  /**
   * 能读出来、但资料不符合当前规则（如价格口径冲突、快照与表不一致）。非空时详情页提示
   * 「哪一项要改」，改完保存即修复——写回仍是严格校验，所以放宽的只是读取。
   */
  data_warnings?: { path: string; message: string }[];
}
export interface StandardModelPage { list: StandardModelDTO[]; total: number; page: number; size: number }
export interface StandardModelQuery {
  companies?: string[]; model_types?: string[]; supplier_ids?: string[]; regions?: string[];
  page: number; size: number; keyword?: string; company?: string; model_type?: string;
  status?: string; verify_status?: string; sort_by?: string; order?: string;
}
export interface ModelFilterOptions { companies: string[]; suppliers: { id: string; name: string; region: string }[]; has_more: boolean }
