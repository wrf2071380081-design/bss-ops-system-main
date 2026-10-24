import type { StandardPrice } from "./standardModels.types";
export interface ResourceQuery {
  view: string; page: number; size: number; sort_by: string; order: string;
  keyword?: string; company?: string; model_type?: string; supplier_id?: string; standard_model_id?: string; region?: string; state?: string;
}
export interface ResourceGroup {
  id: string; name: string; official_id: string; company: string; model_type: string;
  supplier_count: number; resource_count: number; available_count: number;
  coefficient_min: string; coefficient_max: string; regions: string[]; state_counts: Record<string, number>;
}
export interface ResourceCache {
  status: string; report_id?: string; hit_tokens?: string; input_tokens?: string; hit_rate?: string; hit_percent?: string; scene?: string;
}
export interface ResourceRow {
  id: string; supplier_id: string; supplier_name: string; standard_model_id: string; model_name: string; official_id: string; company: string; model_type: string;
  region: string; supplier_model_id: string; coefficient: string; buyer_id: string; state: string; ever_admitted: boolean;
  revision: string; formal_config_id?: string; formal_config_revision: string;
  test: { report_id?: string; status: string; connectivity_passed: boolean; pressure_passed: boolean; valid: boolean; cache_stats: ResourceCache; metrics: { connection_ms: string | null; rps: string | null; tps: string | null } };
  quote_readiness: { eligible: boolean; blocked_reasons: string[]; resource_revision: string; model_version_no: string; formal_config_revision: string; test_evidence_ref?: string };
  official_price_items: StandardPrice[]; permissions: { can_edit: boolean }; created_at: string; updated_at: string; admitted_at?: string | null;
}
export interface ResourcePage<T> { view: string; list: T[]; total: number; page: number; size: number }
export interface ResourceFilterOptions { companies: string[]; suppliers: { id: string; name: string; region: string }[]; has_more: boolean }
