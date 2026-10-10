import { http, HttpResponse } from "msw";
import type { FinanceAccountDetailDTO, FinanceAccountSummaryDTO, FxRateDTO } from "../../api/finance.types";
import { ApiError } from "../../domain/common";

const basePath = "/api/internal/finance";
const readableIdentities = new Set(["FINANCE_OP", "VIEWER"]);

// 汇率固定 USD/CNY（与后端 12-finance.md 一致）；2026-10 为「待锁定」占位展示。
const fxRates: FxRateDTO[] = [
  { id: "fx-202609-usd-cny", month: "2026-09", baseCurrency: "USD", quoteCurrency: "CNY", rate: "7.1", status: "LOCKED", lockedAt: "2026-09-01T09:05:00+08:00", lockedBy: "许宁", updatedAt: "2026-09-01T09:05:00+08:00" },
  { id: "2026-10", month: "2026-10", baseCurrency: "USD", quoteCurrency: "CNY", rate: "", status: "PENDING_LOCK" },
  { id: "fx-202608-usd-cny", month: "2026-08", baseCurrency: "USD", quoteCurrency: "CNY", rate: "6.9", status: "EXPIRED", lockedAt: "2026-08-01T09:00:00+08:00", lockedBy: "许宁", updatedAt: "2026-09-01T00:05:00+08:00" },
];

const accounts: FinanceAccountDetailDTO[] = [
  { customerId: "customer-blue", customerCode: "91310000MA1FL0XXXX", customerName: "蓝海电商", levelCode: "GOLD", status: "NORMAL", currency: "CNY", creditLimit: "2000000.00", usedCredit: "720000.00", availableCredit: "1280000.00", depositBalance: "300000.00", paymentTerms: "MONTHLY", updatedAt: "2026-09-10T14:30:00+08:00", billingEntity: "上海蓝海电子商务有限公司", invoiceTitle: "上海蓝海电子商务有限公司", taxNoMasked: "9131****XXXX", settlementCycle: "30 天", recentTransactions: [{ id: "C1", occurredAt: "2026-09-09T18:00:00+08:00", type: "CREDIT_USAGE", amount: "126000.00", currency: "CNY", referenceNo: "BILL:91", note: "" }, { id: "D1", occurredAt: "2026-09-02T10:20:00+08:00", type: "DEPOSIT_IN", amount: "100000.00", currency: "CNY", referenceNo: "", note: "押金补充" }] },
  { customerId: "customer-lighthouse", customerCode: "91110000MA06XXXXXX", customerName: "灯塔教育", levelCode: "SILVER", status: "WARNING", currency: "CNY", creditLimit: "800000.00", usedCredit: "690000.00", availableCredit: "110000.00", depositBalance: "80000.00", paymentTerms: "MONTHLY", updatedAt: "2026-09-10T09:15:00+08:00", billingEntity: "北京灯塔在线教育科技有限公司", invoiceTitle: "北京灯塔在线教育科技有限公司", taxNoMasked: "9111****XXXX", settlementCycle: "30 天", recentTransactions: [{ id: "C2", occurredAt: "2026-09-10T08:45:00+08:00", type: "CREDIT_USAGE", amount: "95000.00", currency: "CNY", referenceNo: "BILL:102", note: "" }] },
  { customerId: "customer-medical", customerCode: "91330100MA2XXXXXXX", customerName: "康澜医疗", levelCode: "STANDARD", status: "FROZEN", currency: "CNY", creditLimit: "500000.00", usedCredit: "500000.00", availableCredit: "0.00", depositBalance: "50000.00", paymentTerms: "PREPAID", updatedAt: "2026-09-08T10:20:00+08:00", billingEntity: "杭州康澜医疗科技有限公司", invoiceTitle: "杭州康澜医疗科技有限公司", taxNoMasked: "9133****XXXX", settlementCycle: "30 天", recentTransactions: [{ id: "C3", occurredAt: "2026-09-08T09:50:00+08:00", type: "CREDIT_USAGE", amount: "50000.00", currency: "CNY", referenceNo: "BILL:77", note: "" }] },
];

const copy = <T>(value: T): T => structuredClone(value);
const accountSummary = ({ billingEntity: _billingEntity, invoiceTitle: _invoiceTitle, taxNoMasked: _taxNoMasked, settlementCycle: _settlementCycle, recentTransactions: _recentTransactions, ...row }: FinanceAccountDetailDTO): FinanceAccountSummaryDTO => row;
const result = (data: unknown) => HttpResponse.json({ code: 0, message: "ok", data, requestId: `mock-${crypto.randomUUID()}` });
const failure = (error: unknown) => { const candidate = error as { httpStatus?: number; status?: number; message?: string }; const status = candidate.httpStatus || candidate.status || 500; return HttpResponse.json({ code: `HTTP_${status}`, message: candidate.message || "Mock 服务异常", requestId: `mock-${crypto.randomUUID()}` }, { status }); };
const ensureReadable = (request: Request) => { const identity = request.headers.get("X-Mock-Identity") || "VIEWER"; if (!readableIdentities.has(identity)) throw new ApiError(403, "当前身份无权查看财务信息"); };

export const financeHandlers = [
  http.get(`${basePath}/fx-rates`, ({ request }) => {
    try {
      ensureReadable(request); const url = new URL(request.url); const search = (url.searchParams.get("search") || "").trim().toLowerCase(); const month = url.searchParams.get("month") || ""; const status = url.searchParams.get("status") || ""; const page = Math.max(1, Number(url.searchParams.get("page")) || 1); const size = Math.min(100, Math.max(1, Number(url.searchParams.get("size")) || 10));
      const matched = fxRates.filter((rate) => (!search || `${rate.month} ${rate.lockedBy ?? ""}`.toLowerCase().includes(search)) && (!month || rate.month === month) && (!status || rate.status === status)); const offset = (page - 1) * size;
      return result({ list: copy(matched.slice(offset, offset + size)), total: matched.length, page, size });
    } catch (error) { return failure(error); }
  }),
  http.get(`${basePath}/accounts`, ({ request }) => {
    try {
      ensureReadable(request); const url = new URL(request.url); const search = (url.searchParams.get("search") || "").trim().toLowerCase(); const status = url.searchParams.get("status") || ""; const page = Math.max(1, Number(url.searchParams.get("page")) || 1); const size = Math.min(100, Math.max(1, Number(url.searchParams.get("size")) || 10));
      const matched = accounts.filter((account) => (!search || `${account.customerCode} ${account.customerName}`.toLowerCase().includes(search)) && (!status || account.status === status)); const offset = (page - 1) * size;
      return result({ list: matched.slice(offset, offset + size).map((item) => accountSummary(copy(item))), total: matched.length, page, size });
    } catch (error) { return failure(error); }
  }),
  http.get(`${basePath}/accounts/:customerId`, ({ request, params }) => {
    try { ensureReadable(request); const account = accounts.find((item) => item.customerId === String(params.customerId)); if (!account) throw new ApiError(404, "客户财务账户不存在或当前账号不可见"); return result(copy(account)); }
    catch (error) { return failure(error); }
  }),
];
