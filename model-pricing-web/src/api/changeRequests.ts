import { assertContractId } from "../domain/contractId";
import { apiRequest } from "./http";
import type {
  ApprovalStepDTO,
  ChangeRequestDetailDTO,
  ChangeRequestItemDTO,
  ChangeRequestListParams,
  ChangeRequestPageDTO,
  DecideChangeRequestRequest,
  DecideChangeRequestResultDTO,
} from "./changeRequests.types";

interface ChangeRequestWireDTO {
  id: string | number;
  change_type: ChangeRequestItemDTO["changeType"];
  status: ChangeRequestItemDTO["status"];
  sku_id: string | number | null;
  risk_level: string;
  steps_total: number;
  steps_approved: number;
  pending_step_no: number | null;
  pending_role: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

interface ChangeRequestDetailWireDTO extends ChangeRequestWireDTO {
  payload?: unknown;
  margin_preview?: unknown;
  request_id?: string | null;
  updated_by?: string | null;
  steps: Array<{ step_no: number; required_role: string; decision: ApprovalStepDTO["decision"];
    approver_id: string | number | null; comment: string | null; decided_at: string | null }>;
}

function id(value: string | number) { assertContractId(value); return String(value); }
function item(row: ChangeRequestWireDTO): ChangeRequestItemDTO {
  return { id: id(row.id), changeType: row.change_type, status: row.status,
    skuId: row.sku_id === null ? null : id(row.sku_id), riskLevel: row.risk_level,
    stepsTotal: row.steps_total, stepsApproved: row.steps_approved,
    pendingStepNo: row.pending_step_no, pendingRole: row.pending_role,
    createdBy: row.created_by, createdAt: row.created_at, updatedAt: row.updated_at };
}

export const changeRequestsApi = {
  list: (params: ChangeRequestListParams) => apiRequest<{ list: ChangeRequestWireDTO[]; total: number; page: number; size: number }>({
    url: "/internal/change-requests", method: "GET", params,
  }).then((data): ChangeRequestPageDTO => ({ ...data, list: data.list.map(item) })),
  get: (requestId: string) => apiRequest<ChangeRequestDetailWireDTO>({
    url: `/internal/change-requests/${encodeURIComponent(requestId)}`, method: "GET",
  }).then((data): ChangeRequestDetailDTO => ({ ...item(data), payload: data.payload, marginPreview: data.margin_preview,
    requestId: data.request_id, updatedBy: data.updated_by,
    steps: data.steps.map(step => ({ stepNo: step.step_no, requiredRole: step.required_role, decision: step.decision,
      approverId: step.approver_id === null ? null : id(step.approver_id), comment: step.comment, decidedAt: step.decided_at })) })),
  // 通用审批入口：官方价变更单 / 价目表发布 / 特价审批共用。
  // 后端角色匹配 required_role，且禁止同一人连审两步——前端只做「显示与禁用」，
  // 真正的越权判定在服务层（403 / 400）。
  decide: (changeRequestId: string, decision: DecideChangeRequestRequest) => {
    const target = id(changeRequestId.trim());
    const payload = { step_no: decision.stepNo, decision: decision.decision, comment: decision.comment };
    return apiRequest<{ change_request_id: string | number; step_no: number; decision: "APPROVED" | "REJECTED"; final_status: DecideChangeRequestResultDTO["finalStatus"] }>({
      url: `/internal/approvals/${encodeURIComponent(target)}/decision`, method: "POST", data: payload,
      idempotency: { scope: `change-request:decision:${target}:step:${decision.stepNo}`, payload },
    }).then((data): DecideChangeRequestResultDTO => ({
      changeRequestId: id(data.change_request_id), stepNo: data.step_no,
      decision: data.decision, finalStatus: data.final_status,
    }));
  },
};
