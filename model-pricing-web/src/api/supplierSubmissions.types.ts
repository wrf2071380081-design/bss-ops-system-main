export type SupplierSubmissionKind = "MODEL_NEW" | "PRICE_CHANGE";
export type SupplierTestCategory = "CONNECTIVITY" | "STRESS";

export interface SupplierModelOptionDTO {
  id: string;
  name: string;
  official_id: string;
  company: string;
}

export interface SupplierSubmissionTestDTO {
  category: SupplierTestCategory;
  status: string;
  tested: boolean;
  passed: boolean;
  job_id?: string;
  report_id?: string;
  connection_ms?: string | null;
  rps?: string | null;
  tps?: string | null;
}

export interface SupplierSubmissionRowDTO {
  id: string;
  interface_model_id: string;
  standard_model_id: string;
  standard_model_name: string;
  official_id: string;
  coefficient: string;
  test_base_url: string;
  row_status: string;
  connectivity_tested: boolean;
  connectivity_passed: boolean;
  stress_tested: boolean;
  stress_passed: boolean;
  connectivity_job?: SupplierSubmissionTestDTO;
  stress_job?: SupplierSubmissionTestDTO;
}

export interface SupplierSubmissionDTO {
  id: string;
  kind: SupplierSubmissionKind;
  status: string;
  revision: string;
  reason: string;
  rows: SupplierSubmissionRowDTO[];
  permissions: { can_edit: boolean; can_submit: boolean; can_withdraw: boolean };
  created_at: string;
  updated_at: string;
  submitted_at?: string | null;
}

export interface SupplierSubmissionPageDTO {
  list: SupplierSubmissionDTO[];
  total: number;
  page: number;
  size: number;
}

export interface SupplierSubmissionWriteRow {
  id?: string;
  interface_model_id: string;
  standard_model_id: string;
  coefficient: string;
  test_base_url: string;
  test_api_key?: string;
}

export interface SupplierSubmissionWrite {
  kind: SupplierSubmissionKind;
  expected_revision?: string;
  reason: string;
  rows: SupplierSubmissionWriteRow[];
}

export interface SupplierSubmissionListParams {
  page: number;
  size: number;
  status?: string;
  kind?: SupplierSubmissionKind;
}

export interface SupplierTestJobDTO {
  job_id: string;
  category: SupplierTestCategory;
  status: string;
  tested: boolean;
  passed: boolean;
  connection_ms?: string | null;
  rps?: string | null;
  tps?: string | null;
  report_id?: string;
}
