import assert from "node:assert/strict";
import test from "node:test";
import { buildSupplierEntitySave, normalizeSupplierEntity, normalizeSupplierEntityPage, normalizeSupplierQualification, normalizeSupplierQualificationPage } from "../src/api/supplierEntities.contract.ts";

/** 9 个可选自由文本字段（迁移 000083）的默认值：不填就是空串，原型示例不入库。 */
const freeTextDefaults = {
  contact_title: "", contact_email: "", default_settlement: "", default_payment: "", invoice_preference: "",
  min_consumption: "", data_training_policy: "", log_retention_policy: "", default_sla: "",
};

test("supplier entity save converts percent to ratio and omits buyer assignment", () => {
  const body = buildSupplierEntitySave({
    name: "示例供应商", region: "CN", registration_no: "", contact: "联系人", phone: "",
    tax_rate_percent: "6.25", settlement_currency: "CNY", remark: "", ...freeTextDefaults,
  }, "供应商建档");
  assert.equal(body.tax_rate, "0.06250000");
  assert.equal("buyer_id" in body, false);
  assert.equal("established" in body, false);
  assert.equal(body.reason, "供应商建档");
});

test("supplier entity response preserves canonical bigint strings and edit authority", () => {
  const profile = normalizeSupplierEntity({
    id: "9007199254740993", buyer_id: "7", name: "示例供应商", region: "CN", registration_no: "",
    aliases_supported: true, contact: "", phone: "", tax_rate: "0.06000000", settlement_currency: "CNY", remark: "", established: false,
    status: "ACTIVE", revision: "1", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  });
  assert.equal(profile.id, "9007199254740993");
  assert.equal(profile.can_edit, true);
  assert.throws(() => normalizeSupplierEntity({ id: 9007199254740993 }), /格式不正确|安全范围/);
});

test("supplier tax percent refuses precision lost by the v36 native column", () => {
  assert.throws(() => buildSupplierEntitySave({
    name: "示例供应商", region: "CN", registration_no: "", contact: "", phone: "",
    tax_rate_percent: "6.251", settlement_currency: "CNY", remark: "", ...freeTextDefaults,
  }, "建档"), /0.01/);
});

test("supplier tax rate may be left blank without becoming zero", () => {
  const body = buildSupplierEntitySave({
    name: "税率待补供应商", region: "CN", registration_no: "", contact: "", phone: "",
    tax_rate_percent: "", settlement_currency: "CNY", remark: "", ...freeTextDefaults,
  }, "资料待补");
  assert.equal(body.tax_rate, null);
});

test("supplier zero tax rate remains distinct from a missing rate", () => {
  const body = buildSupplierEntitySave({
    name: "零税率供应商", region: "CN", registration_no: "", contact: "", phone: "",
    tax_rate_percent: "0", settlement_currency: "CNY", remark: "", ...freeTextDefaults,
  }, "确认零税率");
  assert.equal(body.tax_rate, "0.00000000");
  assert.equal(normalizeSupplierEntity({
    id: "8", buyer_id: "7", name: "税率待补供应商", aliases_supported: true, region: "CN", registration_no: "",
    contact: "", phone: "", tax_rate: null, settlement_currency: "CNY", remark: "", established: false,
    status: "ACTIVE", revision: "1", created_at: "2026-10-22T00:00:00Z",
    updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  }).tax_rate, null);
});

test("supplier entity normalizes former names for search and rejects malformed aliases", () => {
  const profile = normalizeSupplierEntity({
    id: "9", buyer_id: "7", name: "新名称", aliases: ["旧名称"], aliases_supported: true, region: "CN", registration_no: "",
    contact: "", phone: "", tax_rate: null, settlement_currency: "CNY", remark: "", established: false,
    status: "ACTIVE", revision: "2", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  });
  assert.deepEqual(profile.aliases, ["旧名称"]);
  assert.throws(() => normalizeSupplierEntity({
    id: "9", buyer_id: "7", name: "新名称", aliases: [null], aliases_supported: true, region: "CN", registration_no: "",
    contact: "", phone: "", tax_rate: null, settlement_currency: "CNY", remark: "", established: false,
    status: "ACTIVE", revision: "2", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  }), /响应不符合字段契约/);
});

test("supplier alias capability is explicit and consistent across the entity page", () => {
  const row = {
    id: "9", buyer_id: "7", name: "新名称", aliases: [], aliases_supported: false, region: "CN", registration_no: "",
    contact: "", phone: "", tax_rate: null, settlement_currency: "CNY", remark: "", established: false,
    status: "ACTIVE", revision: "2", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:00Z", can_edit: true,
  };
  const page = normalizeSupplierEntityPage({ list: [row], total: 1, page: 1, size: 20, aliases_supported: false }, 1, 20);
  assert.equal(page.aliases_supported, false);
  assert.equal(page.list[0].aliases_supported, false);
  assert.throws(() => normalizeSupplierEntityPage({ list: [row], total: 1, page: 1, size: 20 }, 1, 20), /分页信息/);
  assert.throws(() => normalizeSupplierEntityPage({ list: [row], total: 1, page: 1, size: 20, aliases_supported: true }, 1, 20), /状态不一致/);
});

test("supplier qualification response is paged, subject-bound, and excludes legacy storage paths", () => {
  const result = normalizeSupplierQualificationPage({
    list: [{ id: "81", supplier_id: "21", name: "营业执照", has_attachment: true, can_download: true, file_name: "license.pdf", uploaded_at: "2026-10-24T00:00:00Z" }],
    total: 1, page: 1, size: 20, metadata_supported: false,
  }, "21", 1, 20);
  assert.equal(result.metadata_supported, false);
  assert.equal(result.list[0].has_attachment, true);
  assert.equal(result.list[0].can_download, true);
  assert.equal(result.list[0].file_name, "license.pdf");
  assert.throws(() => normalizeSupplierQualificationPage({
    list: [{ id: "81", supplier_id: "22", name: "营业执照", has_attachment: true, uploaded_at: "2026-10-24T00:00:00Z" }],
    total: 1, page: 1, size: 20,
  }, "21", 1, 20), /不符合字段契约/);
  assert.throws(() => normalizeSupplierQualificationPage({
    list: [{ id: "81", supplier_id: "21", name: "营业执照", file_ref: "C:\\private\\license.pdf", has_attachment: true, uploaded_at: "2026-10-24T00:00:00Z" }],
    total: 1, page: 1, size: 20, metadata_supported: false,
  }, "21", 1, 20), /不符合字段契约/);
  assert.throws(() => normalizeSupplierQualificationPage({ list: [], total: 0, page: 1, size: 20 }, "21", 1, 20), /结构能力状态/);
  assert.throws(() => normalizeSupplierQualificationPage({ list: [], total: 0, page: 1, size: 20, metadata_supported: "false" }, "21", 1, 20), /结构能力状态/);
});

test("supplier qualification write response is a single row and does not require page capability metadata", () => {
  const row = normalizeSupplierQualification({
    id: "82", supplier_id: "21", name: "新资质", has_attachment: true, can_download: true,
    file_name: "license.pdf", uploaded_at: "2026-10-26T00:00:00Z",
  }, "21");
  assert.equal(row.id, "82");
  assert.equal(row.supplier_id, "21");
  assert.equal(row.can_download, true);
  assert.throws(() => normalizeSupplierQualification({
    id: "82", supplier_id: "22", name: "新资质", has_attachment: true, can_download: true,
    file_name: "license.pdf", uploaded_at: "2026-10-26T00:00:00Z",
  }, "21"), /不符合字段契约/);
});

test("commercial defaults and contact text are trimmed free text, never validated as enums", () => {
  const body = buildSupplierEntitySave({
    name: "示例供应商", region: "CN", registration_no: "", contact: "", phone: "",
    tax_rate_percent: "", settlement_currency: "CNY", remark: "",
    contact_title: " 商务负责人 ", contact_email: " not-an-email ",
    default_settlement: " 月结 30 天 ", default_payment: " 银行转账 ", invoice_preference: " 专用发票 ",
    min_consumption: " 1000 CNY / 月 ", data_training_policy: " 不用于训练 ",
    log_retention_policy: " 留存 90 天 ", default_sla: " 99.9% ",
  }, "补条款");
  assert.equal(body.contact_title, "商务负责人");
  // 邮箱暂定自由文本：后端不校验格式，前端也绝不能套正则。
  assert.equal(body.contact_email, "not-an-email");
  assert.equal(body.default_settlement, "月结 30 天");
  assert.equal(body.default_payment, "银行转账");
  assert.equal(body.invoice_preference, "专用发票");
  assert.equal(body.min_consumption, "1000 CNY / 月");
  assert.equal(body.data_training_policy, "不用于训练");
  assert.equal(body.log_retention_policy, "留存 90 天");
  assert.equal(body.default_sla, "99.9%");
});

test("commercial defaults reject values longer than the column width", () => {
  const base = {
    name: "示例供应商", region: "CN" as const, registration_no: "", contact: "", phone: "",
    tax_rate_percent: "", settlement_currency: "CNY" as const, remark: "", ...freeTextDefaults,
  };
  assert.throws(() => buildSupplierEntitySave({ ...base, contact_title: "职".repeat(65) }, "超长"), /联系人职务不能超过 64/);
  assert.throws(() => buildSupplierEntitySave({ ...base, contact_email: "a".repeat(129) }, "超长"), /联系人邮箱不能超过 128/);
  assert.throws(() => buildSupplierEntitySave({ ...base, default_sla: "S".repeat(129) }, "超长"), /默认 SLA ?不能超过 128/);
});
