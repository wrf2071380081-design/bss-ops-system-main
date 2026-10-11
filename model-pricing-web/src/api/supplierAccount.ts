import { apiRequest } from "./http";
import type {
  CreateSupplierQualificationRequest,
  SupplierPortalProfileDTO,
  SupplierQualificationSubmissionDTO,
} from "./supplierAccount.types";

// 批次 7a：`GET /supplier/profile` 后端已实现。
// `PUT /supplier/profile` 与 `/supplier/qualifications` 属批次 7b，**后端尚未实现**（会 404）。
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
};
