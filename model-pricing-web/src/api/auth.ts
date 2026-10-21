import { apiRequest } from "./http";
import type { PortalType } from "../stores/session";

export interface LoginResult {
  token: string;
  snapshot: {
    account_id: number | string;
    portal_type: string;
    operator_id: number | string;
    can_approve_supplier?: boolean;
    supplier_name?: string;
    supplier_region?: string;
    roles: { code: string }[] | null;
    perms: string[] | null;
  };
}

export const login = (portal: PortalType, loginID: string, password: string) =>
  apiRequest<LoginResult>({ url: `/${portal}/auth/login`, method: "POST",
    data: { login_id: loginID, password }, anonymous: true });

export const logout = (portal: PortalType) =>
  apiRequest<void>({ url: `/${portal}/auth/logout`, method: "POST", noContent: true });

// 供应商：手机号 + 验证码（当前阶段验证码固定 1234）。
export const loginSupplierMobile = (mobile: string, code: string) =>
  apiRequest<LoginResult>({
    url: "/supplier/auth/login-mobile",
    method: "POST",
    data: { mobile, code },
    anonymous: true,
  });

export interface SupplierRegisterPayload {
  mobile: string;
  code: string;
  company_name: string;
  region: "CN" | "OVERSEAS";
  credit_code: string;
  contact: string;
  license_name: string;
  agreement: boolean;
}

export const registerSupplier = (payload: SupplierRegisterPayload) =>
  apiRequest<LoginResult>({
    url: "/supplier/auth/register",
    method: "POST",
    data: payload,
    anonymous: true,
  });
