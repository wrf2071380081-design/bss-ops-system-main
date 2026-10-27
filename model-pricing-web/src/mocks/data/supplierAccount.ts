import type {
  SupplierAccountDTO,
  SupplierOperationLogDTO,
} from "../../api/supplierAccount.types";

// 演示数据：两个主体覆盖原型实测的两种状态。
//   locked   —— 澄海智算（国内，有上架历史 → 主体属性锁定，label 带「（有上架历史，已锁定）」）
//   unlocked —— 初禾资源（海外，新注册、无资源无申报 → 主体属性可改）
// 切换方式见 handlers/supplierAccount.ts 的 mockAccountVariant()：
//   开发态页面底部有「切换演示主体」入口，对应 localStorage 键 `msw:account-variant`。
export type AccountVariant = "locked" | "unlocked";

export const accountFixtures: Record<AccountVariant, SupplierAccountDTO> = {
  locked: {
    id: "1001",
    name: "澄海智算（演示）",
    registrationNo: "DEMO-COMPANY-0000",
    contact: "顾宁",
    phone: "13800000001",
    region: "CN",
    regionLocked: true,
    regionLockReason: "有上架历史，已锁定",
    license: {
      name: "澄海智算企业证明.txt",
      fileRef: "supplier/1001/license/chenghai.txt",
      uploadedAt: "2026-09-22T14:20:00+08:00",
    },
    updatedAt: "2026-10-26T14:48:00+08:00",
  },
  unlocked: {
    id: "1002",
    name: "初禾资源（新注册演示）",
    registrationNo: "DEMO-COMPANY-NEW",
    contact: "吴安",
    phone: "13800000002",
    region: "OVERSEAS",
    regionLocked: false,
    license: {
      name: "初禾企业证明.txt",
      fileRef: "supplier/1002/license/chuhe.txt",
      uploadedAt: "2026-10-08T14:20:00+08:00",
    },
    updatedAt: "2026-10-27T09:29:00+08:00",
  },
};

/** 操作留痕演示数据：一期只收本主体账户相关事件（决策 D4）。 */
export const accountOperationLogs: Record<AccountVariant, SupplierOperationLogDTO[]> = {
  locked: [
    { id: "log-1001-07", action: "供应商登录", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-10-27T09:28:08+08:00", payload: { ip: "10.20.3.18", channel: "PORTAL" } },
    { id: "log-1001-06", action: "维护企业资料", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-10-26T14:48:00+08:00", payload: { changed: ["contact"], contact: { from: "顾宁", to: "顾宁" } } },
    { id: "log-1001-05", action: "合同／授信资料新增", object: "澄海智算（演示）", operator: "陈乐", operatedAt: "2026-10-09T17:08:00+08:00", payload: { document: "年度框架协议-2026.pdf" } },
    { id: "log-1001-04", action: "合同／授信资料新增", object: "澄海智算（演示）", operator: "陈乐", operatedAt: "2026-10-09T16:07:00+08:00", payload: { document: "授信额度确认函.pdf" } },
    { id: "log-1001-03", action: "供应商登录", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-10-08T09:31:12+08:00", payload: { ip: "10.20.3.18", channel: "PORTAL" } },
    { id: "log-1001-02", action: "维护企业资料", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-09-30T10:12:00+08:00", payload: { changed: ["registrationNo"] } },
    { id: "log-1001-01", action: "供应商注册", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-09-22T14:20:00+08:00", payload: { region: "CN", channel: "SELF_SERVICE" } },
  ],
  unlocked: [
    { id: "log-1002-04", action: "维护企业资料", object: "初禾资源（新注册演示）", operator: "吴安", operatedAt: "2026-10-27T09:30:09+08:00", payload: { changed: ["contact"], contact: { from: "吴安", to: "吴安改" } } },
    { id: "log-1002-03", action: "供应商登录", object: "初禾资源（新注册演示）", operator: "吴安", operatedAt: "2026-10-27T09:29:59+08:00", payload: { ip: "10.20.3.24", channel: "PORTAL" } },
    { id: "log-1002-02", action: "供应商登录", object: "初禾资源（新注册演示）", operator: "吴安", operatedAt: "2026-10-27T09:29:03+08:00", payload: { ip: "10.20.3.24", channel: "PORTAL" } },
    { id: "log-1002-01", action: "供应商注册", object: "初禾资源（新注册演示）", operator: "吴安", operatedAt: "2026-10-08T14:20:00+08:00", payload: { region: "OVERSEAS", channel: "SELF_SERVICE" } },
  ],
};
