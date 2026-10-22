export interface SupplierModelResourceDTO {
  id: string;
  standard_model_id: string;
  model_name: string;
  official_id: string;
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
