import { apiRequest } from "./http";
import {
  validateOperationLogPage,
  validateSupplierAccount,
  validateSupplierAccountUpdate,
} from "./supplierAccount.contract";
import type {
  CreateSupplierQualificationRequest,
  SupplierAccountDTO,
  SupplierLicenseDTO,
  SupplierOperationLogPage,
  SupplierPortalProfileDTO,
  SupplierQualificationSubmissionDTO,
  UpdateSupplierAccountRequest,
} from "./supplierAccount.types";

// 批次 7a：`GET /supplier/profile` 后端已实现（旧模型：supplier_profile + legal_subject，字段打码）。
// `PUT /supplier/profile` 与 `/supplier/qualifications` 属批次 7b，**后端尚未实现**（会 404）。
//
// 账户设置（本期）走新接口 `/supplier/account`：读 A/B 线共用的 `supplier` 表，明文、可写。
// 决策 D1：不改动 7a 的 /profile 语义，避免影响已对接调用方；新页面一律用 /account。
export const supplierAccountApi = {
  getProfile: () =>
    apiRequest<SupplierPortalProfileDTO>({ url: "/supplier/profile", method: "GET" }),
  listQualifications: () =>
    apiRequest<SupplierQualificationSubmissionDTO[]>({
      url: "/supplier/qualifications",
      method: "GET",
    }),
  submitQualification: (payload: CreateSupplierQualificationRequest) =>
    apiRequest<SupplierQualificationSubmissionDTO>({
      url: "/supplier/qualifications",
      method: "POST",
      data: payload,
      idempotency: { scope: "supplier:qualification:submit", payload },
    }),

  // ---- 账户设置（本期） ----
  // 所有响应都过一遍契约校验：regionLocked 缺失必须报错，不能静默渲染成可编辑。
  getAccount: async (): Promise<SupplierAccountDTO> =>
    validateSupplierAccount(await apiRequest<unknown>({ url: "/supplier/account", method: "GET" })),
  updateAccount: async (payload: UpdateSupplierAccountRequest): Promise<SupplierAccountDTO> => {
    const body = validateSupplierAccountUpdate(payload);
    return validateSupplierAccount(
      await apiRequest<unknown>({
        url: "/supplier/account",
        method: "PUT",
        data: body,
        // 写操作挂幂等（项目红线）；scope 固定，payload 变化由后端按幂等键拦截。
        idempotency: { scope: "supplier:account:update", payload: body },
      }),
    );
  },
  listOperationLogs: async (query: { page?: number; size?: number } = {}): Promise<SupplierOperationLogPage> => {
    const page = query.page ?? 1;
    const size = query.size ?? 20;
    return validateOperationLogPage(
      await apiRequest<unknown>({
        url: "/supplier/account/operation-logs",
        method: "GET",
        params: { page, size },
      }),
      page,
      size,
    );
  },
  /** 上传/替换企业证明。文件单独走一条接口（multipart），返回落库后的 fileRef。 */
  uploadLicense: async (file: File, operatorName?: string): Promise<SupplierLicenseDTO> => {
    const form = new FormData();
    form.append("file", file);
    if (operatorName?.trim()) form.append("operator_name", operatorName.trim());
    const result = (await apiRequest<unknown>({
      url: "/supplier/account/license",
      method: "POST",
      data: form,
    })) as Partial<SupplierLicenseDTO> | null;
    if (!result || typeof result.name !== "string" || typeof result.fileRef !== "string") {
      throw new Error("企业证明上传响应不符合约定，请重新上传。");
    }
    return {
      name: result.name,
      fileRef: result.fileRef,
      ...(result.uploadedAt ? { uploadedAt: result.uploadedAt } : {}),
    };
  },
};
