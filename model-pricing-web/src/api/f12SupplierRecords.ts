import { apiRequest } from "./http";
import { normalizeF12Contracts, normalizeF12Credits } from "./f12SupplierRecords.contract";

const page = 1, size = 100;
export const f12SupplierRecordsApi = {
  async contracts(supplierID: string, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ url: "/internal/contracts", params: { subject_type: "SUPPLIER", subject_id: supplierID, page, size }, signal });
    return normalizeF12Contracts(value, page, size);
  },
  async credits(supplierID: string, signal?: AbortSignal) {
    const value = await apiRequest<unknown>({ url: "/internal/credits", params: { subject_type: "SUPPLIER", subject_id: supplierID, page, size }, signal });
    return normalizeF12Credits(value, page, size);
  },
};
