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
  /** 平台判定：pass / pass_with_warnings / fail；压力测试无判定时为空 */
  verdict?: string;
  /** 测试目标变化后旧结果失效 */
  stale?: boolean;
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
  test_api_key_configured?: boolean;
  row_status: string;
  /** 压力测试勾选（原型「压力测试」列） */
  participate_test?: boolean;
  /** 已提交但该行仍未通过，前端显示「待补」 */
  supplement_required?: boolean;
  connectivity_tested: boolean;
  connectivity_passed: boolean;
  stress_tested: boolean;
  stress_passed: boolean;
  connectivity_job?: SupplierSubmissionTestDTO;
  stress_job?: SupplierSubmissionTestDTO;
}

export interface SupplierSubmissionDTO {
  id: string;
  /** 对外内部编号，形如 S00000003 */
  submission_no?: string;
  submitter_name?: string;
  region?: string;
  row_count?: number;
  model_names?: string[];
  /** 提交后仍未通过的行数 */
  pending_row_count?: number;
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
  participate_test?: boolean;
  interface_model_id: string;
  standard_model_id: string;
  coefficient: string;
  /**
   * @deprecated 000046 起测试凭据上移主体（`/supplier/test-credential`），
   * 行级 URL/Key 已废弃；服务端收到也会忽略，仅旧客户端兼容保留字段。
   */
  test_base_url?: string;
  /** @deprecated 同上：改走主体凭据接口，Key 不再随申报提交。 */
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
  company?: string;
  model_type?: string;
  region?: string;
  submitter?: string;
}

/** 列表筛选项（按供应商已有数据收窄） */
export interface SupplierSubmissionFilterOptionsDTO {
  companies: string[];
  model_types: string[];
  regions: string[];
  submitters: string[];
  statuses: string[];
  kinds: string[];
}

/** 批量测试的逐项动作 */
export type SupplierTestAction = "STARTED" | "SKIPPED" | "RETRIED" | "NOT_SELECTED" | "FAILED";

export interface SupplierTestItemDTO {
  row_id: string;
  category: SupplierTestCategory;
  action: SupplierTestAction;
  job_id?: string;
  reason?: string;
}

export interface SupplierBatchTestResultDTO {
  categories: SupplierTestCategory[];
  items: SupplierTestItemDTO[];
  started: number;
  skipped: number;
  retried: number;
  not_selected: number;
  failed: number;
}

/** Excel 导入的逐行结果 */
export interface SupplierImportRowDTO {
  line: number;
  ok: boolean;
  errors: string[];
  standard_model: string;
  interface_model_id: string;
  coefficient: string;
  participate_test: boolean;
  standard_model_id?: number;
  standard_model_name?: string;
  official_id?: string;
  company?: string;
  model_type?: string;
}

export interface SupplierImportPreviewDTO {
  total: number;
  ok_count: number;
  error_count: number;
  rows: SupplierImportRowDTO[];
}

export interface SupplierTestJobDTO {
  job_id: string;
  category: SupplierTestCategory;
  status: string;
  tested: boolean;
  passed: boolean;
  /** 平台判定：pass / pass_with_warnings / fail；压力测试无判定时为空 */
  verdict?: string;
  /** 测试目标变化后旧结果失效 */
  stale?: boolean;
  connection_ms?: string | null;
  rps?: string | null;
  tps?: string | null;
  report_id?: string;
}
