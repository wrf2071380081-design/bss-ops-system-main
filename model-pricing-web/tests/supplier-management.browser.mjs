import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const live = process.env.BSS_REAL_API === "1";
process.env.VITE_AUTH_ENABLED = live ? "true" : "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";
let supplierID = live ? "" : "42";
let supplierName = live ? `QA-S02-${Date.now()}` : "供应商甲";
let supplierSearchName = live ? supplierName : "供应商甲曾用名";
if (live && (!process.env.BSS_TEST_LOGIN_ID || !process.env.BSS_TEST_PASSWORD)) {
  throw new Error("真实联调需设置 BSS_TEST_LOGIN_ID 和 BSS_TEST_PASSWORD。");
}

const supplier = {
  id: 42,
  name: "供应商甲",
  aliases: ["供应商甲曾用名"],
  region: "CN",
  phone: "13800000000",
  resource_count: 0,
  credit_amount: "",
  credit_status: "NOT_RECORDED",
  settlement_currency: "CNY",
  tax_rate: "",
  contract_status: "未登记",
  cooperation_stage: "潜在",
  handling_status: "无未完成事项",
};
const establishedSupplier = {
  ...supplier,
  tax_rate: "0.0600",
  id: 43,
  name: "供应商乙",
  aliases: [],
  credit_amount: "1200.00",
  credit_status: "AVAILABLE",
  settlement_currency: "USD",
  contract_status: "已归档",
  cooperation_stage: "已有",
  handling_status: "有未完成事项",
};
const entity = {
  id: "42", buyer_id: "7", name: "供应商甲", aliases: [], aliases_supported: true, region: "CN", registration_no: "91310000TEST001",
  contact: "联系人甲", phone: "13800000000", tax_rate: null, settlement_currency: "CNY",
  established: false, status: "ACTIVE", revision: "1", created_at: "2026-10-20T01:00:00Z",
  updated_at: "2026-10-20T01:00:00Z", can_edit: true,
};
const readonlyEntity = { ...entity, id: "44", buyer_id: "8", name: "供应商只读", aliases: [], can_edit: false };
const envelope = data => ({ code: 0, message: "ok", data, requestId: "supplier-management-browser" });
const emptyPage = (page = 1, size = 100) => ({ list: [], total: 0, page, size });
const requests = [];
const pageErrors = [];
let createdSupplierBody;
let lastSupplierUpdateBody;
let qualificationUploadBody;
let qualificationRows = [{ id: "601", supplier_id: "42", name: "营业执照", has_attachment: true, can_download: true, file_name: "license.pdf", uploaded_at: "2026-10-20T01:00:00Z" }];
const qualificationPdf = Buffer.from("%PDF-1.7\nqualification fixture");
const f12ContractRows = Array.from({ length: 22 }, (_, index) => ({
  id: String(701 + index), subject_type: "SUPPLIER", subject_id: "42", status: index === 0 ? "ARCHIVED" : "PROCESSING",
  version: index + 1, has_attachment: false, signed_at: null,
  created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z",
}));
const f12CreditRows = Array.from({ length: 22 }, (_, index) => ({
  id: String(801 + index), subject_type: "SUPPLIER", subject_id: "42", direction: "SUPPLIER_TO_US",
  amount: `${100 + index}.00`, currency: "CNY", version: index + 1, effective_at: null, created_at: "2026-10-20T01:00:00Z",
}));
let forceNextSupplierConflict = false;
let fixtureEntity = { ...entity };
let fixtureConflictEntity;
let server, browser;

async function availablePort() {
  const probe = createPortServer();
  await new Promise((resolve, reject) => probe.listen(0, "127.0.0.1", error => error ? reject(error) : resolve()));
  const port = probe.address().port;
  await new Promise((resolve, reject) => probe.close(error => error ? reject(error) : resolve()));
  return port;
}

try {
  const proxy = live && process.env.BSS_TEST_API_ORIGIN
    ? { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } }
    : undefined;
  server = await createServer({ server: { host: "127.0.0.1", port: await availablePort(), strictPort: true, proxy }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(7000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("request", request => {
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/internal/")) return;
    requests.push({ method: request.method(), path: url.pathname, query: Object.fromEntries(url.searchParams) });
    if (request.method() === "PUT" && /\/api\/internal\/supplier-entities\/\d+$/.test(url.pathname)) lastSupplierUpdateBody = request.postDataJSON();
  });
  if (!live) await page.route("**/api/internal/**", async route => {
    const request = route.request();
    const url = new URL(request.url());
    const send = data => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope(data)) });
    if (url.pathname === "/api/internal/supplier-entities" && request.method() === "GET") {
      const taxRateBlank = createdSupplierBody
        ? { ...entity, id: "43", name: createdSupplierBody.name, tax_rate: createdSupplierBody.tax_rate, revision: "1", aliases: [] }
        : { ...entity, name: "税率待补供应商", tax_rate: null, aliases: ["税率待补供应商曾用名"] };
      const keyword = (url.searchParams.get("keyword") || "").trim().toLowerCase();
      const candidates = [taxRateBlank, readonlyEntity];
      const list = candidates.filter(row => !keyword || [row.name, ...row.aliases].some(value => value.toLowerCase().includes(keyword)));
      return send({ list, total: list.length, page: Number(url.searchParams.get("page") || 1), size: Number(url.searchParams.get("size") || 20), aliases_supported: true });
    }
    if (/^\/api\/internal\/supplier-entities\/\d+\/merge-preview$/.test(url.pathname) && request.method() === "POST") {
      const body = request.postDataJSON();
      return send({
        source: { id: "42", name: "税率待补供应商", region: "CN", buyer_id: "7", revision: "1" },
        target: { id: body.target_supplier_id, name: "供应商只读", region: "OVERSEAS", buyer_id: "8", revision: "3" },
        source_references: { resources: 1, submissions: 2, pending_submissions: 1, onboarding_flows: 1, contracts: 1, credit_lines: 1, formal_configs: 1, qualifications: 1, customer_relations: 2, test_credential_records: 1, website_account_configured: true, aliases: null },
        target_references: { resources: 2, submissions: 0, pending_submissions: 0, onboarding_flows: 0, contracts: 0, credit_lines: 1, formal_configs: 0, qualifications: 0, customer_relations: 0, test_credential_records: 0, website_account_configured: false, aliases: null },
        duplicate_resource_keys: 1,
        review_items: [{ code: "REGION_MISMATCH", title: "主体区域不同", source_count: 1, target_count: 1 }],
      });
    }
    if (url.pathname === "/api/internal/supplier-entities" && request.method() === "POST") {
      createdSupplierBody = request.postDataJSON();
      return send({ ...entity, id: "43", name: createdSupplierBody.name, tax_rate: createdSupplierBody.tax_rate, revision: "1" });
    }
    const qualificationMatch = url.pathname.match(/^\/api\/internal\/supplier-entities\/(\d+)\/qualifications$/);
    if (qualificationMatch && request.method() === "GET") {
      const supplierId = qualificationMatch[1];
      const page = Number(url.searchParams.get("page") || 1);
      const size = Number(url.searchParams.get("size") || 20);
      return send({ list: qualificationRows.map(row => ({ ...row, supplier_id: supplierId })), total: qualificationRows.length, page, size,metadata_supported:false });
    }
    if (qualificationMatch && request.method() === "POST") {
      qualificationUploadBody = request.postDataJSON();
      const row = { id: "602", supplier_id: qualificationMatch[1], name: qualificationUploadBody.name, has_attachment: true, can_download: true, file_name: qualificationUploadBody.file_name, uploaded_at: "2026-10-24T01:00:00Z" };
      qualificationRows = [row, ...qualificationRows];
      return send(row);
    }
    const qualificationFileMatch = url.pathname.match(/^\/api\/internal\/supplier-entities\/(\d+)\/qualifications\/(\d+)\/file$/);
    if (qualificationFileMatch && request.method() === "GET") {
      return route.fulfill({ status: 200, contentType: "application/pdf", headers: { "content-disposition": 'attachment; filename="license.pdf"', "x-content-type-options": "nosniff" }, body: qualificationPdf });
    }
    if (url.pathname === "/api/internal/supply/suppliers") {
      const keyword = (url.searchParams.get("keyword") || "").trim().toLowerCase();
      const region = url.searchParams.get("region") || "";
      const contract = url.searchParams.get("contract_status") || "";
      const stage = url.searchParams.get("cooperation_stage") || "";
      const handling = url.searchParams.get("handling_status") || "";
      const rows = [supplier, establishedSupplier].filter(row =>
        (!keyword || [row.name, ...(row.aliases || [])].some(value => value.toLowerCase().includes(keyword))) &&
        (!region || row.region === region) &&
        (!contract || row.contract_status === contract) &&
        (!stage || row.cooperation_stage === stage) &&
        (!handling || row.handling_status === handling));
      const page = Number(url.searchParams.get("page") || 1);
      const size = Number(url.searchParams.get("size") || 20);
      return send({ list: rows.slice((page - 1) * size, page * size), total: rows.length, page, size });
    }
    if (url.pathname === "/api/internal/workbench/inbox") return send({ list: [], total: 0, page: Number(url.searchParams.get("page") || 1), size: Number(url.searchParams.get("size") || 20) });
    if (url.pathname === "/api/internal/workbench/people") return send({ list: [{ id: 7, name: "采购甲" }] });
    if (url.pathname === "/api/internal/resources") return send({ view: "flat", ...emptyPage(Number(url.searchParams.get("page") || 1), Number(url.searchParams.get("size") || 100)) });
    if (url.pathname === "/api/internal/supplier-entities/42" && request.method() === "GET") return send(fixtureEntity);
    if (url.pathname === "/api/internal/supplier-entities/42" && request.method() === "PUT") {
      const body = request.postDataJSON();
      fixtureEntity = { ...fixtureEntity, ...body, revision: String(Number(fixtureEntity.revision) + 1), updated_at: "2026-10-24T00:00:00Z" };
      return send(fixtureEntity);
    }
    if (/^\/api\/internal\/supplier-entities\/43$/.test(url.pathname) && request.method() === "PUT" && forceNextSupplierConflict) {
      forceNextSupplierConflict = false;
      return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ code: 409, message: "供应商资料已更新，请刷新后重试。", requestId: "supplier-management-browser" }) });
    }
    if (url.pathname === "/api/internal/supplier-entities/43" && request.method() === "GET") {
      fixtureConflictEntity = { ...entity, id: "43", name: "服务端并发更新名称", contact: "并发更新联系人", revision: "2" };
      return send(fixtureConflictEntity);
    }
    if (url.pathname === "/api/internal/supplier-entities/43" && request.method() === "PUT") {
      const body = request.postDataJSON();
      createdSupplierBody = { ...createdSupplierBody, ...body };
      return send({ ...fixtureConflictEntity, ...body, revision: "3", updated_at: "2026-10-24T01:00:00Z" });
    }
    if (url.pathname === "/api/internal/contracts") {
      const page = Number(url.searchParams.get("page") || 1), size = Number(url.searchParams.get("size") || 20);
      return send({ list: f12ContractRows.slice((page - 1) * size, page * size), total: f12ContractRows.length, page, size });
    }
    if (url.pathname === "/api/internal/credits") {
      const page = Number(url.searchParams.get("page") || 1), size = Number(url.searchParams.get("size") || 20);
      return send({ list: f12CreditRows.slice((page - 1) * size, page * size), total: f12CreditRows.length, page, size,
        current_credit: [{ direction: "SUPPLIER_TO_US", currency: "CNY", status: "AVAILABLE", record: f12CreditRows.at(-1) }],
      });
    }
    if (url.pathname === "/api/internal/suppliers/42/operations") return send(emptyPage(Number(url.searchParams.get("page") || 1), Number(url.searchParams.get("size") || 20)));
    if (/^\/api\/internal\/flows\/suppliers\/\d+\/progress$/.test(url.pathname))return send({supplier_id:url.pathname.split('/')[5],list:[]});
    if (/^\/api\/internal\/supplier-entities\/\d+\/test-config$/.test(url.pathname))return send({supplier_id:url.pathname.split('/')[4],configured:false,base_url:'',revision:'0',has_api_key:false});
    if (/^\/api\/internal\/flows\/suppliers\/\d+\/submissions$/.test(url.pathname))return send(emptyPage(Number(url.searchParams.get('page')||1),Number(url.searchParams.get('size')||20)));
    throw new Error(`Unexpected API request ${request.method()} ${url.pathname}`);
  });

  const base = `http://127.0.0.1:${server.httpServer.address().port}`;
  if (live) {
    await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
    await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
    await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
    await page.getByRole("button", { name: "新建供应商", exact: true }).click();
    const formItems = page.locator(".supplier-entity-form .entity-form-grid .el-form-item");
    await formItems.nth(0).locator("input").fill(supplierName);
    assert.equal(await formItems.nth(5).locator("input").inputValue(), "", "new supplier tax input should start blank");
    await formItems.nth(7).locator("input").fill("TDD tax null end-to-end");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    const entityRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: supplierName });
    await entityRow.waitFor();
    assert.match(await entityRow.innerText(), /未填写/, "API/database null tax should remain visible as 未填写");
    const idMatch = (await entityRow.locator(".mono").textContent())?.match(/ID\s+([1-9]\d*)/);
    assert.ok(idMatch, "created supplier ID should be visible in the real list");
    supplierID = idMatch[1];
    await entityRow.getByRole("button", { name: "维护", exact: true }).click();
    const firstEditDialog = page.getByRole("dialog").last();
    const firstEditItems = firstEditDialog.locator(".entity-form-grid .el-form-item");
    const staleDraftName = `QA-S02-STALE-${Date.now()}`;
    await firstEditItems.nth(0).locator("input").fill(staleDraftName);
    await firstEditItems.nth(7).locator("input").fill("等待并发修改后制造 revision 冲突");

    const concurrentPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    concurrentPage.setDefaultTimeout(7000);
    concurrentPage.on("pageerror", error => pageErrors.push(error.message));
    await concurrentPage.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded", timeout: 30000 });
    await concurrentPage.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
    await concurrentPage.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
    await concurrentPage.getByRole("button", { name: "登录", exact: true }).click();
    await concurrentPage.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
    const concurrentRow = concurrentPage.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: supplierName });
    await concurrentRow.getByRole("button", { name: "维护", exact: true }).click();
    const concurrentDialog = concurrentPage.getByRole("dialog").last();
    const concurrentItems = concurrentDialog.locator(".entity-form-grid .el-form-item");
    await concurrentItems.nth(3).locator("input").fill("并发写入联系人");
    await concurrentItems.nth(7).locator("input").fill("第二会话先保存联系人");
    const concurrentWrite = concurrentPage.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
    await concurrentDialog.getByRole("button", { name: "保存", exact: true }).click();
    assert.equal((await concurrentWrite).status(), 200, "the second session should advance the supplier revision");
    await concurrentPage.close();

    const staleWrite = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
    await firstEditDialog.getByRole("button", { name: "保存", exact: true }).click();
    assert.equal((await staleWrite).status(), 409, "stale expected_revision must be rejected by the real API");
    await firstEditDialog.getByRole("button", { name: "载入最新资料", exact: true }).waitFor();
    assert.equal(await firstEditItems.nth(0).locator("input").inputValue(), staleDraftName, "conflict must retain unsaved input until the user chooses to refresh");
    await firstEditDialog.getByRole("button", { name: "载入最新资料", exact: true }).click();
    await page.waitForFunction(({ expectedName, expectedContact }) => {
      const inputs = [...document.querySelectorAll(".supplier-entity-form .entity-form-grid .el-form-item input")];
      return inputs[0]?.value === expectedName && inputs[3]?.value === expectedContact;
    }, { expectedName: supplierName, expectedContact: "并发写入联系人" });
    assert.equal(await firstEditItems.nth(0).locator("input").inputValue(), supplierName, "refresh must load the current server name");
    assert.equal(await firstEditItems.nth(3).locator("input").inputValue(), "并发写入联系人", "refresh must expose the concurrent field update");
    const resolvedName = `QA-S02-RESOLVED-${Date.now()}`;
    await firstEditItems.nth(0).locator("input").fill(resolvedName);
    await firstEditItems.nth(7).locator("input").fill("基于最新主体资料重新维护");
    const retryWrite = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
    await firstEditDialog.getByRole("button", { name: "保存", exact: true }).click();
    const retriedUpdate = await retryWrite;
    assert.equal(retriedUpdate.status(), 200, "the refreshed form should save successfully");
    assert.equal(retriedUpdate.request().postDataJSON().expected_revision, "2", "retry must submit the current server revision");
    await page.getByText(resolvedName, { exact: true }).waitFor();
    supplierName = resolvedName;

    const formerName = supplierName;
    const renamedSupplier = `QA-S02-RENAMED-${Date.now()}`;
    await page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: supplierName }).getByRole("button", { name: "维护", exact: true }).click();
    const renameDialog = page.getByRole("dialog").last();
    await renameDialog.waitFor();
    const renameItems = renameDialog.locator(".entity-form-grid .el-form-item");
    await renameItems.nth(0).locator("input").fill(renamedSupplier);
    await renameItems.nth(7).locator("input").fill("TDD 验证改名保留曾用名");
    const renameResponse = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
    await renameDialog.getByRole("button", { name: "保存", exact: true }).click();
    assert.equal((await renameResponse).status(), 200);
    await page.getByText(renamedSupplier, { exact: true }).waitFor();
    supplierName = renamedSupplier;
    supplierSearchName = formerName;
    await page.goto(`${base}/#/internal/supplier-entities`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
    await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(supplierSearchName);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: supplierName }).waitFor();
    assert.equal(await page.locator(".supplier-entity-table .el-table__body tr").count(), 1, "entity list should search former names without duplicating a supplier");
    await page.goto(`${base}/#/internal/suppliers?keyword=${encodeURIComponent(supplierSearchName)}`, { waitUntil: "domcontentloaded" });
  } else {
    await page.goto(`${base}/#/internal/suppliers`, { waitUntil: "domcontentloaded", timeout: 30000 });
  }
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();
  await page.getByRole("button", { name: "打开工作台待办", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/workbench"));
  await page.getByRole("heading", { name: "工作台", exact: true }).waitFor();
  assert.equal(requests.some(item => item.path === "/api/internal/workbench/inbox"), true, "supplier approval navigation must land on the shared workbench inbox route");
  assert.equal(requests.some(item => item.path === "/api/internal/approvals/submissions" || item.path === "/api/internal/approvals/history"), false, "supplier workspace must not load a duplicate approval center");
  await page.goto(`${base}/#/internal/suppliers?keyword=${encodeURIComponent(supplierSearchName)}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();
  if (!live) {
    const potentialRow = page.locator(".supplier-workspace .el-table__body tr").filter({ hasText: "供应商甲" });
    assert.match(await potentialRow.innerText(), /未登记/, "missing contract records must not be presented as processing");
    await page.locator(".filters .el-select").nth(1).click();
    await page.getByRole("option", { name: "未登记", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    assert.equal(await page.locator(".supplier-workspace .el-table__body tr").count(), 1, "the unregistered contract status should be filterable");
    assert.match(await page.locator(".supplier-workspace .el-table__body tr").first().innerText(), /供应商甲/);
    assert.equal(requests.find(item => item.path === "/api/internal/supply/suppliers" && item.query.contract_status === "未登记")?.query.contract_status, "未登记");
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.locator(".filters .el-select").nth(2).click();
    await page.getByRole("option", { name: "已有", exact: true }).click();
    await page.locator(".filters .el-select").nth(3).click();
    await page.getByRole("option", { name: "有未完成事项", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    assert.equal(await page.locator(".supplier-workspace .el-table__body tr").count(), 1, "the API query should combine cooperation stage and handling status");
    assert.match(await page.locator(".supplier-workspace .el-table__body tr").first().innerText(), /供应商乙/);
    assert.equal((await page.locator(".supplier-workspace .el-table__body tr").first().locator("td").nth(6).innerText()).trim(), "6%", "supplier list must display ratio 0.0600 as 6%, not 0.0600%");
    const filteredRequest = requests.find(item => item.path === "/api/internal/supply/suppliers" && item.query.cooperation_stage === "已有");
    assert.equal(filteredRequest?.query.handling_status, "有未完成事项", "the page must send both independent filters to the API");
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(supplierSearchName);
    await page.getByRole("button", { name: "查询", exact: true }).click();
  }
  await page.getByRole("button", { name: "供应商主体档案", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
  assert.equal(await page.getByRole("textbox", { name: "主体名称搜索" }).inputValue(), supplierSearchName, "opening the subject page should carry the supplier name filter");
  await page.getByRole("button", { name: "← 返回供应商管理", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/suppliers?"));
  assert.equal(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("keyword"), supplierSearchName, "returning should restore the supplier list filter");
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();
  if (!live) {
    assert.equal(await page.locator(".supplier-workspace .el-table__body tr").count(), 1, "former-name search should return its supplier once");
    assert.match(await page.locator(".supplier-workspace .el-table__body tr").first().innerText(), /供应商甲/);
    await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill("");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.locator(".filters .el-select").nth(2).click();
    await page.getByRole("option", { name: "已有", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    assert.equal(await page.locator(".supplier-workspace .el-table__body tr").count(), 1, "established stage filter should be independent of handling status");
    const existingRow = page.locator(".supplier-workspace .el-table__body tr").first();
    assert.match(await existingRow.innerText(), /供应商乙/);
    assert.match(await existingRow.innerText(), /有未完成事项/, "an established supplier may still have an unfinished matter");
    assert.match(await existingRow.innerText(), /\$1,200/, "workspace should display the current F12 credit amount");
    await page.locator(".filters .el-select").nth(3).click();
    await page.getByRole("option", { name: "有未完成事项", exact: true }).click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    assert.equal(await page.locator(".supplier-workspace .el-table__body tr").count(), 1, "unfinished-matter filter should work independently");
    await page.getByRole("button", { name: "重置", exact: true }).click();
  }
  await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(supplierSearchName);
  if (!live) await page.getByRole("button", { name: "查询", exact: true }).click();
  const row = page.locator(".supplier-workspace .el-table__body tr").filter({ hasText: supplierName });
  await row.getByRole("button", { name: "详情", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith(`#/internal/suppliers/${supplierID}/models`));

  const detailUrl = new URLSearchParams(new URL(page.url()).hash.split("?")[1]);
  assert.equal(detailUrl.get("return_to"), "suppliers");
  assert.equal(detailUrl.get("supplier_keyword"), supplierSearchName);
  await page.getByRole("heading", { name: supplierName, exact: true }).waitFor();
  await page.getByRole("tab", { name: "接入模型", exact: true }).waitFor();
  await page.getByRole("tab", { name: "档案与办理", exact: true }).waitFor();
  await page.getByRole("tab", { name: "操作记录", exact: true }).waitFor();
  await page.getByRole("tab", { name: "该供应商报价", exact: true }).waitFor();
  await page.getByText("该供应商暂无接入模型", { exact: true }).waitFor();
  assert.equal(await page.getByText("当前供应来源已变化或与供应商不匹配，请从模型广场重新打开。", { exact: true }).count(), 0);

  await page.getByRole("tab", { name: "档案与办理", exact: true }).click();
  await page.getByText("主体档案", { exact: true }).waitFor();
  await page.getByText("资质材料", { exact: true }).waitFor();
  await page.getByText("营业执照", { exact: true }).waitFor();
  await page.getByText("审核字段待交付", { exact: true }).waitFor();
  assert.equal(requests.some(item => item.path === `/api/internal/supplier-entities/${supplierID}/qualifications`), true, "supplier details should load qualification metadata from the API");
  await page.getByRole("button", { name: "上传资质", exact: true }).click();
  const qualificationDialog = page.getByRole("dialog").last();
  await qualificationDialog.getByLabel("资质名称").fill("营业执照副本");
  await qualificationDialog.locator('input[type="file"]').setInputFiles({ name: "license.pdf", mimeType: "application/pdf", buffer: qualificationPdf });
  await qualificationDialog.getByLabel("操作说明").fill("TDD 上传资质");
  const qualificationPostResponse = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}/qualifications`);
  await qualificationDialog.getByRole("button", { name: "保存资质", exact: true }).click();
  assert.equal((await qualificationPostResponse).status(), 200);
  assert.equal(qualificationUploadBody?.name, "营业执照副本");
  assert.equal(qualificationUploadBody?.file_name, "license.pdf");
  assert.equal(qualificationUploadBody?.content_base64, qualificationPdf.toString("base64"));
  assert.equal("file_ref" in qualificationUploadBody, false);
  await page.getByRole("button", { name: "下载 license.pdf", exact: true }).waitFor();
  const qualificationDownloadResponse = page.waitForResponse(response => response.request().method() === "GET" && new URL(response.url()).pathname.endsWith("/qualifications/602/file"));
  await page.getByRole("button", { name: "下载 license.pdf", exact: true }).first().click();
  assert.equal((await qualificationDownloadResponse).status(), 200);
  const taxRateValue = page.locator(".supplier-profile dt").filter({ hasText: "税率" }).locator("xpath=following-sibling::dd[1]");
  await taxRateValue.waitFor();
  assert.equal((await taxRateValue.textContent())?.trim(), "未填写", "missing tax rate must not be rendered as 0%");
  await page.getByRole("button", { name: "维护档案", exact: true }).click();
  const profileDialog = page.getByRole("dialog").last();
  await profileDialog.waitFor();
  const profileItems = profileDialog.locator(".entity-form-grid .el-form-item");
  await profileItems.nth(5).locator("input").fill("0");
  await profileItems.nth(7).locator("input").fill("TDD 确认零税率");
  const setTaxResponse = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
  await profileDialog.getByRole("button", { name: "保存", exact: true }).click();
  const setTax = await setTaxResponse;
  assert.equal(setTax.status(), 200);
  assert.equal(setTax.request().postDataJSON().tax_rate, "0.00000000");
  await page.waitForFunction(() => {
    const label = [...document.querySelectorAll(".supplier-profile dt")].find(node => node.textContent?.trim() === "税率");
    return label?.nextElementSibling?.textContent?.trim() === "0.00%";
  });
  assert.equal(lastSupplierUpdateBody?.tax_rate, "0.00000000");
  await page.getByRole("button", { name: "维护档案", exact: true }).click();
  const clearTaxDialog = page.getByRole("dialog").last();
  const clearTaxInput = clearTaxDialog.locator(".entity-form-grid .el-form-item").nth(5).locator("input");
  await clearTaxInput.fill("");
  assert.equal(await clearTaxInput.inputValue(), "");
  await clearTaxDialog.locator(".entity-form-grid .el-form-item").nth(7).locator("input").fill("TDD 清空税率");
  const clearTaxResponse = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith(`/supplier-entities/${supplierID}`));
  await clearTaxDialog.getByRole("button", { name: "保存", exact: true }).click();
  const clearTax = await clearTaxResponse;
  assert.equal(clearTax.status(), 200);
  assert.equal(clearTax.request().postDataJSON().tax_rate, null);
  await page.waitForFunction(() => {
    const label = [...document.querySelectorAll(".supplier-profile dt")].find(node => node.textContent?.trim() === "税率");
    return label?.nextElementSibling?.textContent?.trim() === "未填写";
  });
  assert.equal(lastSupplierUpdateBody?.tax_rate, null);
  if (!live) await page.getByText("91310000TEST001", { exact: true }).waitFor();
  const resourceQuery = requests.find(item => item.path === "/api/internal/resources");
  assert.equal(resourceQuery?.query.supplier_id, supplierID);
  assert.equal(resourceQuery?.query.page, "1");
  assert.equal(requests.some(item => item.path === `/api/internal/supplier-entities/${supplierID}`), true);

  await page.getByRole("tab", { name: "操作记录", exact: true }).click();
  await page.getByText("暂无操作记录", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "该供应商报价", exact: true }).click();
  await page.getByText("暂无报价申报历史", { exact: true }).waitFor();
  assert.equal(requests.some(item => item.path === `/api/internal/suppliers/${supplierID}/operations`), true);
  assert.equal(requests.some(item => item.path === `/api/internal/flows/suppliers/${supplierID}/submissions`), true);

  await page.getByRole("button", { name: "← 返回供应商列表", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/suppliers?"));
  assert.equal(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("keyword"), supplierSearchName);
  assert.equal(await page.getByPlaceholder("搜索供应商名称 / 曾用名").inputValue(), supplierSearchName);

  if (!live) {
    await page.goto(`${base}/#/internal/supplier-entities`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
    await page.getByText("税率待补供应商", { exact: true }).waitFor();
    assert.match(await page.locator(".supplier-entity-table").innerText(), /未填写/, "missing tax must be rendered as 未填写");
    const mergeSourceRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: "税率待补供应商" });
    const f12RequestStart = requests.length;
    await mergeSourceRow.getByRole("button", { name: "合同 / 授信" }).click();
    const supplierF12Panel = page.locator('.f12-subject-panel[data-subject-type="SUPPLIER"][data-subject-id="42"]');
    await supplierF12Panel.waitFor();
    assert.equal(await supplierF12Panel.getByRole("heading", { name: "合同与授信", exact: true }).count(), 1, "supplier subject archive should use the shared F12 panel");
    assert.equal(requests.slice(f12RequestStart).filter(item => item.path === "/api/internal/contracts" || item.path === "/api/internal/credits").length, 2, "shared F12 panel should load both subject-scoped read APIs");
    const contractTable = supplierF12Panel.locator(".el-table").nth(0);
    const creditTable = supplierF12Panel.locator(".el-table").nth(2);
    assert.equal(await contractTable.locator(".el-table__body tr").count(), 20, "contract history should load one page, not truncate at an arbitrary first 100");
    assert.equal(await creditTable.locator(".el-table__body tr").count(), 20, "credit history should load one page");
    const contractPager = supplierF12Panel.locator(".contract-history-pagination");
    await contractPager.waitFor();
    const contractPageTwo=page.waitForResponse(response=>{const url=new URL(response.url());return url.pathname==='/api/internal/contracts'&&url.searchParams.get('page')==='2';});
    await contractPager.locator(".number").filter({ hasText: "2" }).click();
    assert.equal((await contractPageTwo).status(),200);await contractTable.locator('.el-table__body tr').nth(2).waitFor({state:'detached'});assert.equal(await contractTable.locator('.el-table__body tr').count(),2);
    assert.equal(requests.some(item => item.path === "/api/internal/contracts" && item.query.subject_type === "SUPPLIER" && item.query.subject_id === "42" && item.query.page === "2" && item.query.size === "20"), true, "contract history page changes should retain subject isolation and request the selected server page");
    const creditPager = supplierF12Panel.locator(".credit-history-pagination");
    await creditPager.waitFor();
    const creditPageTwo = page.waitForResponse(response => {
      const url = new URL(response.url());
      return url.pathname === "/api/internal/credits" && url.searchParams.get("page") === "2";
    });
    await creditPager.locator(".number").filter({ hasText: "2" }).click();
    assert.equal((await creditPageTwo).status(), 200, "credit history page change should complete successfully");
    await page.waitForFunction(() => document.querySelector(".credit-history-pagination .number.is-active")?.textContent?.trim() === "2");
    await creditTable.getByText("120.00 CNY", { exact: true }).waitFor();
    assert.equal(requests.some(item => item.path === "/api/internal/credits" && item.query.subject_type === "SUPPLIER" && item.query.subject_id === "42" && item.query.page === "2" && item.query.size === "20"), true, "credit history page changes should retain subject isolation and request the selected server page");
    await page.locator(".el-dialog__headerbtn").last().click();
    await mergeSourceRow.getByRole("button", { name: "合并预览", exact: true }).click();
    const mergeDialog = page.getByRole("dialog").last();
    await mergeDialog.getByText("合并预览", { exact: true }).waitFor();
    await mergeDialog.getByRole("combobox", { name: "目标供应商" }).click();
    await page.getByRole("option", { name: /供应商只读/ }).click();
    await mergeDialog.getByRole("button", { name: "生成预览", exact: true }).click();
    await mergeDialog.getByText("主体区域不同", { exact: true }).waitFor();
    assert.match(await mergeDialog.innerText(), /来源主体.*税率待补供应商/s, "preview must identify the source supplier");
    assert.match(await mergeDialog.innerText(), /目标主体.*供应商只读/s, "preview must identify the selected target supplier");
    assert.match(await mergeDialog.innerText(), /待处理申报.*1/s, "preview must show pending workflow references without exposing credentials");
    assert.equal(await mergeDialog.getByRole("button", { name: "确认合并", exact: true }).count(), 0, "preview must never imply or execute a merge before conflict policy is approved");
    assert.equal(requests.some(item => item.method === "POST" && item.path === "/api/internal/supplier-entities/42/merge-preview"), true, "the preview must be loaded from the A API");
    await mergeDialog.getByRole("button", { name: "关闭", exact: true }).click();
    const readonlyRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: "供应商只读" });
    assert.equal(await readonlyRow.getByRole("button", { name: "维护", exact: true }).isDisabled(), true, "a non-owner cannot edit supplier data");
    await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill("税率待补供应商曾用名");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.getByText("税率待补供应商", { exact: true }).waitFor();
    assert.equal(await page.locator(".supplier-entity-table .el-table__body tr").count(), 1, "entity list should show one row for a former-name match");
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await page.getByRole("button", { name: "新建供应商", exact: true }).click();
    const formItems = page.locator(".supplier-entity-form .entity-form-grid .el-form-item");
    await formItems.nth(0).locator("input").fill("新建空税率供应商");
    assert.equal(await formItems.nth(5).locator("input").inputValue(), "", "new supplier tax input should start blank");
    await formItems.nth(7).locator("input").fill("新增供应商档案");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await page.getByText("新建空税率供应商", { exact: true }).waitFor();
    assert.equal(createdSupplierBody?.tax_rate, null, "blank tax input must be sent as JSON null");
    const createdRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: "新建空税率供应商" });
    await createdRow.getByRole("button", { name: "维护", exact: true }).click();
    const conflictDialog = page.getByRole("dialog").last();
    const conflictItems = conflictDialog.locator(".entity-form-grid .el-form-item");
    await conflictItems.nth(0).locator("input").fill("409 后保留的供应商名称");
    await conflictItems.nth(7).locator("input").fill("TDD revision 冲突");
    forceNextSupplierConflict = true;
    await conflictDialog.getByRole("button", { name: "保存", exact: true }).click();
    await conflictDialog.getByText("供应商资料已更新，请刷新后重试。", { exact: true }).waitFor();
    assert.equal(await conflictItems.nth(0).locator("input").inputValue(), "409 后保留的供应商名称", "409 must keep the user's unsaved form input");
    await conflictDialog.getByRole("button", { name: "载入最新资料", exact: true }).click();
    await page.getByRole("dialog").last().locator(".entity-form-grid .el-form-item").nth(0).locator("input").waitFor();
    const refreshedDialog = page.getByRole("dialog").last();
    const refreshedItems = refreshedDialog.locator(".entity-form-grid .el-form-item");
    await page.waitForFunction(({ expectedName, expectedContact }) => {
      const inputs = [...document.querySelectorAll(".supplier-entity-form .entity-form-grid .el-form-item input")];
      return inputs[0]?.value === expectedName && inputs[3]?.value === expectedContact;
    }, { expectedName: "服务端并发更新名称", expectedContact: "并发更新联系人" });
    assert.equal(await refreshedItems.nth(0).locator("input").inputValue(), "服务端并发更新名称", "explicit conflict refresh must load the current server version");
    assert.equal(await refreshedItems.nth(3).locator("input").inputValue(), "并发更新联系人", "refresh must replace stale form data before retry");
    await refreshedItems.nth(0).locator("input").fill("基于最新版本维护");
    await refreshedItems.nth(7).locator("input").fill("按最新版本重试");
    const retriedUpdate = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname.endsWith("/supplier-entities/43"));
    await refreshedDialog.getByRole("button", { name: "保存", exact: true }).click();
    const retried = await retriedUpdate;
    assert.equal(retried.status(), 200, "save should retry using the freshly loaded revision");
    assert.equal(retried.request().postDataJSON().expected_revision, "2", "retry must use the latest expected revision");
    await page.getByText("基于最新版本维护", { exact: true }).waitFor();
  }
  assert.deepEqual(pageErrors, []);
  console.log(live
    ? `PASS: live supplier create, rename and former-name search worked through the API/database. QA_SUPPLIER_ID=${supplierID} QA_SUPPLIER_NAME=${supplierName}`
    : "PASS: supplier list opens the shared four-tab detail, an unconnected supplier loads profile without a resource, and return navigation restores list filters.");
} finally {
  await browser?.close();
  await server?.close();
}
