import { apiRequest } from "./http";
import { buildPlanExecution, validatePlanExecutionReceipt } from "./planExecution.contract";
import { buildPlanCancellation, buildPlanRevision, validateMaintainedPlan } from "./planMaintenance.contract";
import { buildPlanCreate, validateCreatedPlan, type PlanResource } from "./planCreate.contract";
import { buildPriceChangePlanCreate, buildPriceChangePlanRevision, validateCreatedPriceChangePlan, validateRevisedPriceChangePlan } from "./priceChangePlan.contract";
import type { ResourceRow } from "./resources.types";
import { validatePlanPage, validatePlanQuery, type PlanQuery } from "./planList.contract";
import { standardModelVersion } from "./standardModels.contract";
import { validateNoticePage, validateNoticeView, validatePlanSummary, validateReadReceipt } from "./supplyWorkbench.contract";
import type { NoticeQuery, PlanSummary } from "./supplyWorkbench.types";

export const supplyWorkbenchApi = {
  async executePlan(plan: PlanSummary, confirmed: boolean, completionNote: string, reason: string, lifecycle: string) {
    const body = buildPlanExecution(plan, confirmed, completionNote, reason);
    return validatePlanExecutionReceipt(await apiRequest<unknown>({ method: "POST", url: `/internal/plans/${plan.id}/execute`, data: body,
      idempotency: { scope: `supply-plan-execute:${plan.id}`, payload: body, lifecycle } }), plan);
  },
  async revisePlan(plan: PlanSummary, reason: string, dueAt: string, lifecycle: string) {
    const body = buildPlanRevision(plan, reason, dueAt);
    return validateMaintainedPlan(await apiRequest<unknown>({ method: "PUT", url: `/internal/plans/${plan.id}`, data: body, idempotency: { scope: `supply-plan-revise:${plan.id}`, payload: body, lifecycle } }), plan, body, "revise");
  },
  async cancelPlan(plan: PlanSummary, reason: string, lifecycle: string) {
    const body = buildPlanCancellation(plan, reason);
    return validateMaintainedPlan(await apiRequest<unknown>({ method: "POST", url: `/internal/plans/${plan.id}/cancel`, data: body, idempotency: { scope: `supply-plan-cancel:${plan.id}`, payload: body, lifecycle } }), plan, body, "cancel");
  },
  async createPlan(resource: PlanResource, reason: string, dueAt: string, lifecycle: string) {
    const body = buildPlanCreate(resource, reason, dueAt);
    return validateCreatedPlan(await apiRequest<unknown>({ method: "POST", url: "/internal/plans", data: body, idempotency: { scope: "supply-state-plan-create", payload: body, lifecycle } }), resource, body);
  },
  async createPriceChangePlan(resource: ResourceRow, supplierModelID: string, afterCoefficient: string, assigneeID: string, reason: string, dueAt: string, lifecycle: string) {
    const body = buildPriceChangePlanCreate(resource, supplierModelID, afterCoefficient, assigneeID, reason, dueAt);
    return validateCreatedPriceChangePlan(await apiRequest<unknown>({ method: "POST", url: "/internal/plans/price-change", data: body,
      idempotency: { scope: `supply-price-plan-create:${resource.id}`, payload: body, lifecycle } }), resource, body);
  },
  async revisePriceChangePlan(plan: PlanSummary, supplierModelID: string, afterCoefficient: string, assigneeID: string, reason: string, dueAt: string, lifecycle: string) {
    const body = buildPriceChangePlanRevision(plan, supplierModelID, afterCoefficient, assigneeID, reason, dueAt);
    return validateRevisedPriceChangePlan(await apiRequest<unknown>({ method: "PUT", url: `/internal/plans/${plan.id}/price-change`, data: body,
      idempotency: { scope: `supply-price-plan-revise:${plan.id}`, payload: body, lifecycle } }), plan, body);
  },
  async plans(query: PlanQuery, signal?: AbortSignal) {
    const params = validatePlanQuery(query);
    return validatePlanPage(await apiRequest<unknown>({ url: "/internal/plans", params, signal }), params);
  },
  async notices(query: NoticeQuery, signal?: AbortSignal) {
    if (query.resource_id) standardModelVersion(query.resource_id);
    return validateNoticePage(await apiRequest<unknown>({ url: "/internal/notices", params: query, signal }), query);
  },
  async notice(id: string, selectedStaffID?: string, signal?: AbortSignal) {
    standardModelVersion(id);
    if (selectedStaffID) standardModelVersion(selectedStaffID);
    return validateNoticeView(await apiRequest<unknown>({ url: `/internal/notices/${id}`, params: selectedStaffID ? { staff_id: selectedStaffID } : {}, signal }), id, selectedStaffID);
  },
  async markRead(id: string, expectedStaffID: string, signal?: AbortSignal) {
    standardModelVersion(id); standardModelVersion(expectedStaffID);
    // The person is only used to validate the response. The server chooses the actor.
    return validateReadReceipt(await apiRequest<unknown>({ url: `/internal/notices/${id}/read`, method: "POST", data: {}, signal,
      idempotency: { scope: `supply-notice-read:${id}`, payload: {} },
    }), id, expectedStaffID);
  },
  async plan(id: string, signal?: AbortSignal) {
    standardModelVersion(id);
    return validatePlanSummary(await apiRequest<unknown>({ url: `/internal/plans/${id}`, signal }), id);
  },
};
