import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import { validateNoticePage, validateNoticeView, validatePlanSummary, validateReadReceipt } from "./supplyWorkbench.contract";
import type { NoticeQuery } from "./supplyWorkbench.types";

export const supplyWorkbenchApi = {
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
