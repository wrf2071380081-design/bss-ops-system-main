import type { IsoDateTime, PageResult } from "../domain/common";

export type ChangeRequestType = "DEPRECATE" | "PRICE_UP" | "PRICE_DOWN" | "PRICE_BOOK_PUBLISH" | "PRICE_BOOK_ROLLBACK" | "SPECIAL_PRICE";
// 取值对齐设计文档 §2.6「官方价变更单」状态字典（待处理/人工确认/审批中/已批准/已生效/已拒绝）。
// 原类型只有 PENDING/APPROVED/REJECTED，漏了 CONFIRMING/APPROVING/EFFECTIVE——
// 后端返回这些值时前端类型是假的，审批入口的「可否审批」判断会漏判。
export type ChangeRequestStatus = "PENDING" | "CONFIRMING" | "APPROVING" | "APPROVED" | "EFFECTIVE" | "REJECTED";

/** 可发起审批决策的状态（其余为已终结或尚未进入审批）。 */
export const CHANGE_REQUEST_DECIDABLE: ChangeRequestStatus[] = ["PENDING", "CONFIRMING", "APPROVING"];
export const isChangeRequestDecidable = (status: ChangeRequestStatus) => CHANGE_REQUEST_DECIDABLE.includes(status);

export interface DecideChangeRequestRequest {
  stepNo: number;
  decision: "APPROVED" | "REJECTED";
  comment?: string;
}

export interface DecideChangeRequestResultDTO {
  changeRequestId: string;
  stepNo: number;
  decision: "APPROVED" | "REJECTED";
  finalStatus: ChangeRequestStatus;
}

export interface ChangeRequestListParams {
  type?: ChangeRequestType;
  status?: ChangeRequestStatus;
  sku_id?: string;
  page: number;
  size: number;
}

export interface ChangeRequestItemDTO {
  id: string;
  changeType: ChangeRequestType;
  status: ChangeRequestStatus;
  skuId: string | null;
  riskLevel: string;
  stepsTotal: number;
  stepsApproved: number;
  pendingStepNo: number | null;
  pendingRole: string | null;
  createdBy: string;
  createdAt: IsoDateTime;
  updatedAt: IsoDateTime;
}

export interface ApprovalStepDTO {
  stepNo: number;
  requiredRole: string;
  decision: "APPROVED" | "REJECTED" | null;
  approverId: string | null;
  comment: string | null;
  decidedAt: IsoDateTime | null;
}

export interface ChangeRequestDetailDTO extends ChangeRequestItemDTO {
  payload?: unknown;
  marginPreview?: unknown;
  requestId?: string | null;
  updatedBy?: string | null;
  steps: ApprovalStepDTO[];
}

export type ChangeRequestPageDTO = PageResult<ChangeRequestItemDTO>;
