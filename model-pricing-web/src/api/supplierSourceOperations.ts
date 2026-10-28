import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import { validateSourceOperations, type SourceOperationType } from "./sourceOperations.contract";

export const supplierSourceOperationsApi = {
  async list(supplierID: string, page: number, size = 20, signal?: AbortSignal, entityType?: SourceOperationType) {
    const id = standardModelVersion(supplierID);
    const result = await apiRequest<unknown>({ url: `/internal/suppliers/${id}/operations`, params: { page, size, ...(entityType ? { entity_type: entityType } : {}) }, signal });
    const projected = validateSourceOperations(result, page, size, id);
    if (entityType && projected.list.some(row => row.entity_type !== entityType)) throw new Error("操作记录分类与当前筛选不匹配。");
    return projected;
  },
};
