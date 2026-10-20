export interface StandardFact { key: string; disclosure: string; value: string }
export interface StandardAbility { key: string; state: string; description: string }
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
}
export interface StandardModelPage { list: StandardModelDTO[]; total: number; page: number; size: number }
export interface StandardModelQuery {
  page: number; size: number; keyword?: string; company?: string; model_type?: string;
  status?: string; verify_status?: string; sort_by?: string; order?: string;
}
