export interface SupplierModelResourceDTO {
  id: string;
  standard_model_id: string;
  model_name: string;
  official_id: string;
  /** 模型公司（原型表格「模型公司」列） */
  company: string;
  /** 模型类型枚举 TEXT/IMAGE/VIDEO/AUDIO（原型表格「模型类型」列） */
  model_type: string;
  supplier_model_id: string;
  region: "CN" | "OVERSEAS";
  coefficient: string;
  state: string;
  ever_admitted: boolean;
  revision: string;
  test: {
    report_id?: string;
    status: string;
    connectivity_tested: boolean;
    connectivity_passed: boolean;
    stress_tested: boolean;
    stress_passed: boolean;
    valid: boolean;
    connection_ms?: string | null;
    rps?: string | null;
    tps?: string | null;
  };
  quote_readiness: { eligible: boolean; blocked_reasons: string[] };
}

export interface SupplierModelPageDTO {
  list: SupplierModelResourceDTO[];
  total: number;
  page: number;
  size: number;
}

export interface SupplierModelListParams {
  page: number;
  size: number;
  keyword?: string;
  region?: "CN" | "OVERSEAS" | "";
  state?: string;
}
