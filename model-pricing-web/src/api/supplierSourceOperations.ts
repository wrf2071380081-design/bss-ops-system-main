import { apiRequest } from "./http";
import { standardModelVersion } from "./standardModels.contract";
import { validateSourceOperations } from "./sourceOperations.contract";

export const supplierSourceOperationsApi = {
  async list(supplierID: string, page: number, size = 20, signal?: AbortSignal) {
    const id = standardModelVersion(supplierID);
    const result = await apiRequest<unknown>({ url: `/internal/suppliers/${id}/operations`, params: { page, size }, signal });
    return validateSourceOperations(result, page, size, id);
  },
};
