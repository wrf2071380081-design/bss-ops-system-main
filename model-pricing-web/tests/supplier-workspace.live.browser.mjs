import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_WORKSPACE_POTENTIAL_NAME", "BSS_WORKSPACE_EXISTING_NAME", "BSS_WORKSPACE_DETAIL_SUPPLIER_ID", "BSS_WORKSPACE_DETAIL_SUPPLIER_NAME", "BSS_WORKSPACE_QUALIFICATION_NAME"];
for (const name of required) {
  if (!process.env[name]) throw new Error(`真实联调缺少环境变量 ${name}。`);
}

process.env.VITE_AUTH_ENABLED = "true";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

async function availablePort() {
  const probe = createPortServer();
  await new Promise((resolve, reject) => probe.listen(0, "127.0.0.1", error => error ? reject(error) : resolve()));
  const port = probe.address().port;
  await new Promise((resolve, reject) => probe.close(error => error ? reject(error) : resolve()));
  return port;
}

let server;
let browser;
const pageErrors = [];
const apiResponses = [];
const apiRequests = [];

try {
  const port = await availablePort();
  server = await createServer({
    server: {
      host: "127.0.0.1",
      port,
      strictPort: true,
      proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } },
    },
    logLevel: "error",
  });
  await server.listen();
  const base = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname === "/api/internal/supply/suppliers") apiResponses.push(response);
  });
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.pathname === "/api/internal/supply/suppliers") apiRequests.push(url);
  });

  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsuppliers`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/suppliers"));
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();

  await page.waitForFunction(() => {
    const table = document.querySelector(".supplier-workspace .el-table__body");
    return table && table.querySelectorAll("tr").length >= 2;
  });
  const supplierResponse = apiResponses.at(-1);
  assert.ok(supplierResponse, "the page must load its supplier list through the real API");
  assert.equal(supplierResponse.status(), 200, "the real authenticated supplier list API must return 200");
  const body = await supplierResponse.json();
  const rows = body?.data?.list;
  assert.ok(Array.isArray(rows), "the real API must return the supplier projection list");
  const potential = rows.find(row => row.name === process.env.BSS_WORKSPACE_POTENTIAL_NAME);
  const existing = rows.find(row => row.name === process.env.BSS_WORKSPACE_EXISTING_NAME);
  assert.ok(potential, "the seeded potential supplier must be present in the real API response");
  assert.ok(existing, "the seeded existing supplier must be present in the real API response");
  assert.equal(potential.cooperation_stage, "潜在");
  assert.equal(potential.handling_status, "无未完成事项");
  assert.equal(potential.credit_status, "NOT_RECORDED");
  assert.equal(existing.resource_count, 1);
  assert.equal(existing.credit_amount, "125.00");
  assert.equal(existing.credit_status, "AVAILABLE");
  assert.equal(existing.cooperation_stage, "已有");
  assert.equal(existing.handling_status, "有未完成事项");
  assert.equal(existing.contract_status, "已归档");

  await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(process.env.BSS_WORKSPACE_EXISTING_NAME);
  await page.locator(".filters .el-select").nth(2).click();
  await page.getByRole("option", { name: "已有", exact: true }).click();
  const stageResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supply/suppliers" && url.searchParams.get("cooperation_stage") === "已有";
  });
  await page.getByRole("button", { name: "查询", exact: true }).click();
  const stageResponse = await stageResponsePromise;
  assert.equal(stageResponse.status(), 200);
  assert.equal(new URL(stageResponse.url()).searchParams.get("keyword"), process.env.BSS_WORKSPACE_EXISTING_NAME);
  let visibleRows = page.locator(".supplier-workspace .el-table__body tr");
  assert.equal(await visibleRows.count(), 1, "the cooperation-stage filter must select only established suppliers");
  assert.match(await visibleRows.first().innerText(), new RegExp(process.env.BSS_WORKSPACE_EXISTING_NAME));

  await page.getByRole("button", { name: "重置", exact: true }).click();
  await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(process.env.BSS_WORKSPACE_EXISTING_NAME);
  await page.locator(".filters .el-select").nth(3).click();
  await page.getByRole("option", { name: "有未完成事项", exact: true }).click();
  const handlingResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supply/suppliers" && url.searchParams.get("handling_status") === "有未完成事项";
  });
  await page.getByRole("button", { name: "查询", exact: true }).click();
  const handlingResponse = await handlingResponsePromise;
  assert.equal(handlingResponse.status(), 200);
  visibleRows = page.locator(".supplier-workspace .el-table__body tr");
  assert.equal(await visibleRows.count(), 1, "the handling filter must independently select suppliers with unfinished work");
  assert.match(await visibleRows.first().innerText(), new RegExp(process.env.BSS_WORKSPACE_EXISTING_NAME));
  const lastRequest = apiRequests.at(-1);
  assert.equal(lastRequest?.searchParams.get("page"), "1");
  assert.equal(lastRequest?.searchParams.get("size"), "20");
  assert.equal(lastRequest?.searchParams.get("sort_by"), "id");
  const sortResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supply/suppliers" && url.searchParams.get("sort_by") === "name";
  });
  await page.getByRole("columnheader", { name: /供应商名称/ }).click();
  const sortResponse = await sortResponsePromise;
  assert.equal(sortResponse.status(), 200, "the real API must accept the table's whitelisted sort request");
  assert.equal(new URL(sortResponse.url()).searchParams.get("sort_order"), "asc");

  const detailSupplierID = process.env.BSS_WORKSPACE_DETAIL_SUPPLIER_ID;
  await page.goto(`${base}/#/internal/suppliers/${detailSupplierID}/models?return_to=suppliers&supplier_name=${encodeURIComponent(process.env.BSS_WORKSPACE_DETAIL_SUPPLIER_NAME)}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: process.env.BSS_WORKSPACE_DETAIL_SUPPLIER_NAME, exact: true }).waitFor();
  const qualificationResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === `/api/internal/supplier-entities/${detailSupplierID}/qualifications`;
  });
  await page.getByRole("tab", { name: "档案与办理", exact: true }).click();
  const qualificationResponse = await qualificationResponsePromise;
  assert.equal(qualificationResponse.status(), 200, "the supplier qualification read API must use real authentication and PostgreSQL");
  const qualificationBody = await qualificationResponse.json();
  assert.ok(qualificationBody.data.list.some(row => row.name === process.env.BSS_WORKSPACE_QUALIFICATION_NAME), "the isolated QA qualification must render in the supplier detail page");
  assert.equal(JSON.stringify(qualificationBody).includes("qa-e2e://"), false, "legacy file_ref must not be exposed in the qualification response");
  await page.getByText(process.env.BSS_WORKSPACE_QUALIFICATION_NAME, { exact: true }).waitFor();
  await page.getByText("审核状态未记录", { exact: true }).waitFor();
  assert.deepEqual(pageErrors, []);
  console.log("PASS: authenticated Playwright browser verified supplier filters, pagination, and qualification metadata through the real API and isolated PostgreSQL.");
} finally {
  await browser?.close();
  await server?.close();
}
