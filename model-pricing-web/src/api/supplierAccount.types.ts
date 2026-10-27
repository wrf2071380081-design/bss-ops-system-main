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

/** 供应商主体地域（对齐 `supplier.region`）。原型文案：国内 / 海外。 */
export type SupplierRegion = "CN" | "OVERSEAS";

/** 企业证明（一份，取最新）。落库 `supplier_qualification(name, file_ref)`。 */
export interface SupplierLicenseDTO {
  name: string;
  fileRef: string;
  uploadedAt?: IsoDateTime;
}

/** 供应商门户「账户设置」（GET /api/supplier/account）。
 *
 * 与批次 7a 的 `SupplierPortalProfileDTO` 的区别：
 *   - 读的是 A/B 线共用的 `supplier` 表（name / region / registration_no / contact / phone），
 *     不是旧的 supplier_profile + legal_subject；
 *   - 字段为**明文**（本主体看自己，经安全确认，决策 D2）；
 *   - 多了 `regionLocked`：由后端按「有资源 / 有申报 / 曾上架」判定（决策 D3），
 *     前端只按该标志渲染，**不得自行推断**。 */
export interface SupplierAccountDTO {
  id: string;
  name: string;
  registrationNo: string;
  contact: string;
  phone: string;
  region: SupplierRegion;
  regionLocked: boolean;
  /** 锁定原因，用于 label 后缀，如「有上架历史，已锁定」。 */
  regionLockReason?: string;
  license: SupplierLicenseDTO | null;
  updatedAt: IsoDateTime;
}

/** PUT /api/supplier/account 请求体。
 *
 * 刻意不含 supplierId / phone：身份只从登录态解析，手机号不可写。
 * 也**不含**企业证明：文件走独立接口 `POST /supplier/account/license`（multipart），
 * 先上传拿到 fileRef，再单独保存资料（两个动作、两条留痕）。 */
export interface UpdateSupplierAccountRequest {
  name: string;
  registrationNo: string;
  contact: string;
  region: SupplierRegion;
}

/** 账户操作留痕（GET /api/supplier/account/operation-logs）。
 * 一期只收本主体账户相关事件（决策 D4）。 */
export interface SupplierOperationLogDTO {
  id: string;
  action: string;
  object: string;
  operator: string;
  operatedAt: IsoDateTime;
  payload?: Record<string, unknown>;
}

export interface SupplierOperationLogPage {
  list: SupplierOperationLogDTO[];
  total: number;
  page: number;
  size: number;
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
