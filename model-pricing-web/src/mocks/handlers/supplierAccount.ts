import { http, HttpResponse } from "msw";
import type {
  CreateSupplierQualificationRequest,
  SupplierAccountDTO,
  SupplierOperationLogDTO,
  SupplierPortalProfileDTO,
  SupplierQualificationSubmissionDTO,
  UpdateSupplierAccountRequest,
} from "../../api/supplierAccount.types";
import { ApiError } from "../../domain/common";
import {
  accountFixtures,
  accountOperationLogs,
  type AccountVariant,
} from "../data/supplierAccount";

const basePath = "/api/supplier";
// 批次 7a：字段已按后端真实来源收敛（删掉库里没有列的无源字段）。
const profile: SupplierPortalProfileDTO = {
  id: "1",
  legalName: "供应商甲",
  registrationNoMasked: "9131************3X",
  status: "ACTIVE",
  qualificationStatus: "VALID",
  contactName: "甲操作员",
  contactPhoneMasked: "138****0001",
  settlementCurrency: "CNY",
  paymentTerms: "预付",
  updatedAt: "2026-09-09T16:20:00+08:00",
};
import { qualifications } from "../data/supplierReviews";
const idempotencyResults = new Map<string, { signature: string; value: unknown }>();

const copy = <T>(value: T): T => structuredClone(value);
const requestId = () => `mock-${crypto.randomUUID()}`;
const result = (data: unknown) => HttpResponse.json({ code: 0, message: "ok", data, requestId: requestId() });
function failure(error: unknown) { const candidate = error as { httpStatus?: number; status?: number; message?: string }; const status = candidate.httpStatus || candidate.status || 500; return HttpResponse.json({ code: status === 409 ? "STATE_CONFLICT" : `HTTP_${status}`, message: candidate.message || "Mock 服务异常", requestId: requestId() }, { status }); }
function ensureSupplier(request: Request) { if ((request.headers.get("X-Mock-Identity") || "") !== "SUPPLIER") throw new ApiError(403, "当前身份无权访问供应商账户资料"); }
function requireIdempotency(request: Request, signature: string) { const key = request.headers.get("Idempotency-Key"); if (!key) throw new ApiError(400, "缺少 Idempotency-Key"); const cached = idempotencyResults.get(key); if (cached && cached.signature !== signature) throw new ApiError(409, "幂等键已用于其他操作"); return { key, cached }; }

// ---- 账户设置（本期）----
// 两个演示主体按 localStorage 切换：默认 locked（有上架历史 → 主体属性锁定），
// 开发态页面底部提供切换入口。真实后端上线后这套状态切换随 mock 一起下线。
const VARIANT_KEY = "msw:account-variant";
function currentVariant(): AccountVariant {
  try { return localStorage.getItem(VARIANT_KEY) === "unlocked" ? "unlocked" : "locked"; }
  catch { return "locked"; }
}
const accounts: Record<AccountVariant, SupplierAccountDTO> = {
  locked: copy(accountFixtures.locked),
  unlocked: copy(accountFixtures.unlocked),
};
const operationLogs: Record<AccountVariant, SupplierOperationLogDTO[]> = {
  locked: copy(accountOperationLogs.locked),
  unlocked: copy(accountOperationLogs.unlocked),
};

export const supplierAccountHandlers = [
  http.get(`${basePath}/profile`, ({ request }) => { try { ensureSupplier(request); return result(copy(profile)); } catch (error) { return failure(error); } }),
  http.get(`${basePath}/qualifications`, ({ request }) => { try { ensureSupplier(request); return result(copy(qualifications)); } catch (error) { return failure(error); } }),
  http.post(`${basePath}/qualifications`, async ({ request }) => {
    try {
      ensureSupplier(request);
      const body = (await request.json()) as CreateSupplierQualificationRequest;
      if (!body.qualificationType?.trim() || !body.qualificationName?.trim() || !body.documentNo?.trim()) throw new ApiError(400, "请填写资质类型、名称和证件编号");
      if (!body.attachmentName?.trim()) throw new ApiError(400, "请选择资质附件");
      if (body.validTo && Number.isNaN(Date.parse(body.validTo))) throw new ApiError(400, "资质有效期格式不正确");
      const signature = JSON.stringify(body);
      const { key, cached } = requireIdempotency(request, signature);
      if (cached) return result(copy(cached.value));
      const documentNo = body.documentNo.trim();
      const item: SupplierQualificationSubmissionDTO = { id: `supplier-qual-${crypto.randomUUID()}`, qualificationType: body.qualificationType.trim(), qualificationName: body.qualificationName.trim(), documentNoMasked: documentNo.length > 6 ? `${documentNo.slice(0, 3)}****${documentNo.slice(-3)}` : "******", validFrom: body.validFrom, validTo: body.validTo, status: "SUBMITTED", attachmentName: body.attachmentName.trim(), submittedAt: new Date().toISOString() };
      qualifications.unshift(item);
      idempotencyResults.set(key, { signature, value: copy(item) });
      return result(copy(item));
    } catch (error) { return failure(error); }
  }),

  // 账户设置：读。字段明文（决策 D2），regionLocked 由后端算（决策 D3），此处由 fixture 模拟。
  http.get(`${basePath}/account`, ({ request }) => {
    try { ensureSupplier(request); return result(copy(accounts[currentVariant()])); }
    catch (error) { return failure(error); }
  }),

  // 账户设置：写。校验顺序对齐原型：必填 → 取值 → 锁定 → 幂等。
  http.put(`${basePath}/account`, async ({ request }) => {
    try {
      ensureSupplier(request);
      const variant = currentVariant();
      const account = accounts[variant];
      const body = (await request.json()) as UpdateSupplierAccountRequest;
      const name = (body.name || "").trim();
      // 原型实测：清空企业名称 → 统一 toast「企业资料必填」（不是行内红字）。
      if (!name) throw new ApiError(400, "企业资料必填");
      if (body.region !== "CN" && body.region !== "OVERSEAS") throw new ApiError(400, "主体属性取值不合法");
      // 后端兜底：已上架/有资源/有申报时不允许改 region（决策 D3）。
      if (body.region !== account.region && account.regionLocked) throw new ApiError(409, "主体属性变更前须无资源、无申报");
      const signature = JSON.stringify(body);
      const { key, cached } = requireIdempotency(request, signature);
      if (cached) return result(copy(cached.value));

      const registrationNo = (body.registrationNo || "").trim();
      const contact = (body.contact || "").trim();
      const changed: string[] = [];
      if (name !== account.name) changed.push("name");
      if (registrationNo !== account.registrationNo) changed.push("registrationNo");
      if (contact !== account.contact) changed.push("contact");
      if (body.region !== account.region) changed.push("region");

      const updated: SupplierAccountDTO = {
        ...account,
        name,
        registrationNo,
        contact,
        region: body.region,
        // 企业证明不走这个接口：文件在 POST /account/license 里单独登记，这里保持原值。
        license: account.license,
        updatedAt: new Date().toISOString(),
      };
      accounts[variant] = updated;
      // 保存即留痕（原型：操作记录首行出现「维护企业资料」）。
      operationLogs[variant].unshift({
        id: `log-${crypto.randomUUID()}`,
        action: "维护企业资料",
        object: updated.name,
        // 与后端一致：操作人取本次保存后的对接负责人（供应商侧没有 staff 外键，只落名字快照）。
        operator: updated.contact || account.contact,
        operatedAt: updated.updatedAt,
        payload: { changed },
      });
      idempotencyResults.set(key, { signature, value: copy(updated) });
      return result(copy(updated));
    } catch (error) { return failure(error); }
  }),

  // 企业证明上传：mock 不落盘，只登记文件名并挂到当前主体上。
  // 真实后端是 multipart → 落盘 → 写 supplier_qualification（见 POST /api/supplier/account/license）。
  http.post(`${basePath}/account/license`, async ({ request }) => {
    try {
      ensureSupplier(request);
      const form = await request.formData();
      const file = form.get("file");
      if (!(file instanceof File) || !file.name) throw new ApiError(400, "请选择企业证明文件");
      if (file.size > 10 * 1024 * 1024) throw new ApiError(400, "企业证明不能超过 10 MB");
      const ext = (file.name.match(/\.[^.]+$/) || [""])[0].toLowerCase();
      if (![".pdf", ".png", ".jpg", ".jpeg", ".txt"].includes(ext)) {
        throw new ApiError(400, "企业证明仅支持 pdf / png / jpg / jpeg / txt");
      }
      const variant = currentVariant();
      const account = accounts[variant];
      const license = {
        name: file.name,
        fileRef: `supplier-license/${account.id}/${Date.now()}${ext}`,
        uploadedAt: new Date().toISOString(),
      };
      accounts[variant] = { ...account, license, updatedAt: license.uploadedAt };
      operationLogs[variant].unshift({
        id: `log-${crypto.randomUUID()}`,
        action: "更换企业证明",
        object: account.name,
        operator: account.contact || "—",
        operatedAt: license.uploadedAt,
        payload: { licenseName: file.name, fileRef: license.fileRef },
      });
      return result(copy(license));
    } catch (error) { return failure(error); }
  }),

  // 账户操作留痕：分页返回，排序在前端做（一期数据量小）。
  http.get(`${basePath}/account/operation-logs`, ({ request }) => {
    try {
      ensureSupplier(request);
      const url = new URL(request.url);
      const page = Math.max(1, Number(url.searchParams.get("page") || 1));
      const size = Math.min(100, Math.max(1, Number(url.searchParams.get("size") || 20)));
      const all = operationLogs[currentVariant()];
      const start = (page - 1) * size;
      return result({ list: copy(all.slice(start, start + size)), total: all.length, page, size });
    } catch (error) { return failure(error); }
  }),
];
