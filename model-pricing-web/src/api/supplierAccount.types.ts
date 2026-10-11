import type { IsoDateTime } from "../domain/common";
import type {
  QualificationStatus,
  QualificationSubmissionStatus,
  SupplierStatus,
} from "../domain/status";

/** 供应商门户「主体档案」（GET /api/supplier/profile，批次 7a 后端已实现）。
 * ⚠️ 已删除的无源字段（库里没有对应列，后端不返回假字段，也不要再加回来）：
 *   `code`（供应商编码）、`shortName`（简称）、`contactEmail`（联系邮箱）、
 *   `registeredAddressMasked`（登记地址）、`serviceRegions`（服务区域）。
 *   三张来源表 supplier_profile / legal_subject / subject_operator 都没有这些列，
 *   全库也无其他来源。需要的话必须先加列（新迁移），不能靠后端编。
 * `paymentTerms` 不是独立字段：由 settle_type + billing_cycle 推导
 *   （PREPAID→「预付」、MONTHLY→「月结 N 天」）。 */
export interface SupplierPortalProfileDTO {
  id: string;
  legalName: string;
  registrationNoMasked: string;
  status: SupplierStatus;
  qualificationStatus: QualificationStatus;
  contactName: string;
  contactPhoneMasked: string;
  settlementCurrency: string;
  paymentTerms: string;
  updatedAt: IsoDateTime;
}

export interface SupplierQualificationSubmissionDTO {
  id: string;
  qualificationType: string;
  qualificationName: string;
  documentNoMasked: string;
  validFrom?: IsoDateTime;
  validTo?: IsoDateTime;
  status: QualificationSubmissionStatus;
  attachmentName: string;
  submittedAt: IsoDateTime;
  reviewedAt?: IsoDateTime;
  reviewComment?: string;
}

export interface CreateSupplierQualificationRequest {
  qualificationType: string;
  qualificationName: string;
  documentNo: string;
  validFrom?: IsoDateTime;
  validTo?: IsoDateTime;
  attachmentName: string;
  remark?: string;
}
