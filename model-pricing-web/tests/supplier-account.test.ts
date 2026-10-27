import test from "node:test";
import assert from "node:assert/strict";
import {
  isSupplierRegion,
  registrationLabelOf,
  regionFieldLabelOf,
  validateOperationLogPage,
  validateSupplierAccount,
  validateSupplierAccountUpdate,
} from "../src/api/supplierAccount.contract.ts";

const account = {
  id: "1001",
  name: "澄海智算（演示）",
  registrationNo: "DEMO-COMPANY-0000",
  contact: "顾宁",
  phone: "13800000001",
  region: "CN",
  regionLocked: true,
  regionLockReason: "有上架历史，已锁定",
  license: { name: "澄海智算企业证明.txt", fileRef: "supplier-license/3/1.txt", uploadedAt: "2026-09-22T14:20:00+08:00" },
  updatedAt: "2026-10-27T09:30:09+08:00",
};

const logPage = {
  list: [
    { id: "5", action: "维护企业资料", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-10-27T10:47:17+08:00", payload: { changed: ["contact"] } },
    { id: "4", action: "更换企业证明", object: "澄海智算（演示）", operator: "顾宁", operatedAt: "2026-10-27T10:46:00+08:00" },
  ],
  total: 2,
  page: 1,
  size: 20,
};

test("account contract keeps lock flag and projects only known fields", () => {
  const dto = validateSupplierAccount({ ...account, internalCost: "1.00", creditLine: "999" });
  assert.equal(dto.regionLocked, true);
  assert.equal(dto.regionLockReason, "有上架历史，已锁定");
  assert.equal((dto as unknown as Record<string, unknown>).internalCost, undefined);
  assert.equal(dto.license?.name, "澄海智算企业证明.txt");
});

test("account contract rejects a response without the lock flag", () => {
  // 锁定标志是后端算的，前端绝不能猜：缺字段必须报错，否则锁定态会被渲染成可编辑。
  const { regionLocked, ...withoutFlag } = account;
  assert.throws(() => validateSupplierAccount(withoutFlag), /缺少锁定标志/);
});

test("account contract rejects unknown region and bad timestamps", () => {
  assert.throws(() => validateSupplierAccount({ ...account, region: "MARS" }), /主体属性取值异常/);
  assert.throws(() => validateSupplierAccount({ ...account, updatedAt: "not-a-date" }), /更新时间格式异常/);
  assert.throws(() => validateSupplierAccount({ ...account, license: { name: "", fileRef: "" } }), /企业证明字段缺失/);
});

test("registration label follows the saved region, not the pending select", () => {
  assert.equal(registrationLabelOf("CN"), "统一社会信用代码");
  assert.equal(registrationLabelOf("OVERSEAS"), "当地企业注册编号");
  assert.equal(regionFieldLabelOf({ regionLocked: false }), "主体属性");
  assert.equal(
    regionFieldLabelOf({ regionLocked: true, regionLockReason: "有上架历史，已锁定" }),
    "主体属性（有上架历史，已锁定）",
  );
  assert.equal(regionFieldLabelOf({ regionLocked: true }), "主体属性（已锁定）");
});

test("update contract trims fields and blocks empty name or bad region", () => {
  const body = validateSupplierAccountUpdate({ name: "  初禾资源  ", registrationNo: " DEMO ", contact: " 吴安 ", region: "OVERSEAS" });
  assert.deepEqual(body, { name: "初禾资源", registrationNo: "DEMO", contact: "吴安", region: "OVERSEAS" });
  assert.throws(() => validateSupplierAccountUpdate({ name: "   ", registrationNo: "", contact: "", region: "CN" }), /企业资料必填/);
  assert.throws(() => validateSupplierAccountUpdate({ name: "甲", registrationNo: "", contact: "", region: "MARS" as never }), /主体属性取值不合法/);
  assert.throws(() => validateSupplierAccountUpdate({ name: "长".repeat(129), registrationNo: "", contact: "", region: "CN" }), /不能超过 128/);
});

test("region guard only accepts the two server-side values", () => {
  assert.equal(isSupplierRegion("CN"), true);
  assert.equal(isSupplierRegion("OVERSEAS"), true);
  assert.equal(isSupplierRegion("cn"), false);
  assert.equal(isSupplierRegion(undefined), false);
});

test("operation log page echoes pagination and rejects mismatches", () => {
  const page = validateOperationLogPage(logPage, 1, 20);
  assert.equal(page.total, 2);
  assert.deepEqual(page.list[0].payload, { changed: ["contact"] });
  // page/size 与请求不一致 = 后端换了分页口径，宁可报错也不显示错页数据。
  assert.throws(() => validateOperationLogPage(logPage, 2, 20), /分页与请求不一致/);
  assert.throws(() => validateOperationLogPage({ ...logPage, total: 1 }, 1, 20), /总数小于本页条数/);
  assert.throws(() => validateOperationLogPage({ ...logPage, list: [logPage.list[0], logPage.list[0]] }, 1, 20), /记录重复/);
  assert.throws(() => validateOperationLogPage({ ...logPage, list: [{ ...logPage.list[0], operatedAt: "x" }] }, 1, 20), /时间格式异常/);
});

test("empty operator falls back to a dash instead of failing the whole list", () => {
  // 2026-10-27 实测：上传企业证明时对接负责人还没填，operator 落成空串，
  // 旧实现把整页操作记录判为不合法（红条 + 暂无记录）。展示字段应兜底，不该阻断列表。
  const rows = [
    { id: "1", action: "更换企业证明", object: "云桥算力", operator: "", operatedAt: "2026-10-27T06:32:08+08:00" },
    { id: "2", action: "维护企业资料", object: "云桥算力", operator: "   ", operatedAt: "2026-10-27T06:32:25+08:00" },
    { id: "3", action: "维护企业资料", object: "云桥算力", operator: "陈经理", operatedAt: "2026-10-27T06:33:48+08:00" },
  ];
  const page = validateOperationLogPage({ list: rows, total: 3, page: 1, size: 20 }, 1, 20);
  assert.deepEqual(page.list.map((row) => row.operator), ["—", "—", "陈经理"]);
  // action / object 仍必须非空（这两个字段空了说明数据真的坏了）。
  assert.throws(
    () => validateOperationLogPage({ ...logPage, list: [{ ...logPage.list[0], action: "" }] }, 1, 20),
    /action 字段缺失/,
  );
});
