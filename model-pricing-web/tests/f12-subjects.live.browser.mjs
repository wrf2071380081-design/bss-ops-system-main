import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_F12_SUPPLIER_NAME", "BSS_F12_CUSTOMER_NAME"];
for (const name of required) if (!process.env[name]) throw new Error(`真实 F12 页面联调缺少环境变量 ${name}。`);

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

async function assertSecondF12Page(page, panel, { paginationClass, tableIndex, pathname, subjectType, subjectID }) {
  const responsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === pathname && url.searchParams.get("subject_type") === subjectType &&
      url.searchParams.get("subject_id") === subjectID && url.searchParams.get("page") === "2" &&
      url.searchParams.get("size") === "20" && response.status() === 200;
  });
  await panel.locator(`.${paginationClass} .el-pager li.number`).filter({ hasText: /^2$/ }).click();
  const response = await responsePromise;
  const payload = await response.json();
  assert.equal(payload?.data?.page, 2, `${pathname} should return page 2`);
  assert.equal(payload?.data?.size, 20, `${pathname} should keep the page size`);
  assert.equal(payload?.data?.total, 22, `${pathname} should report all isolated history rows`);
  assert.equal(payload?.data?.list?.length, 2, `${pathname} page 2 should contain the remaining two rows`);
  const table = panel.locator(".panel-body .el-table").nth(tableIndex);
  const panelSelector = `.f12-subject-panel[data-subject-type="${subjectType}"][data-subject-id="${subjectID}"]`;
  await page.waitForFunction(({ selector, index }) => {
    const tables = document.querySelectorAll(`${selector} .panel-body .el-table`);
    return tables[index]?.querySelectorAll(".el-table__body tr").length === 2;
  }, { selector: panelSelector, index: tableIndex });
  assert.equal(await table.locator(".el-table__body tr").count(), 2, `${pathname} page 2 should render two history rows`);
}

let server;
let browser;
const pageErrors = [];
const f12Responses = [];

try {
  const port = await availablePort();
  server = await createServer({
    server: {
      host: "127.0.0.1", port, strictPort: true,
      proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } },
    },
    logLevel: "error",
  });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname === "/api/internal/contracts" || url.pathname === "/api/internal/credits") f12Responses.push(response);
  });

  await page.goto(`http://127.0.0.1:${port}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
  const supplierSearch = page.getByRole("textbox", { name: "主体名称搜索" });
  await supplierSearch.fill(process.env.BSS_F12_SUPPLIER_NAME);
  const supplierResponsePromise = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && new URL(response.url()).searchParams.get("keyword") === process.env.BSS_F12_SUPPLIER_NAME && response.status() === 200);
  await page.getByRole("button", { name: "查询", exact: true }).click();
  const supplierBody = await supplierResponsePromise;
  const supplierPayload = await supplierBody.json();
  const supplier = supplierPayload?.data?.list?.find(row => row.name === process.env.BSS_F12_SUPPLIER_NAME);
  assert.ok(supplier, "the isolated supplier fixture must be present");
  const subjectID = String(supplier.id);

  const supplierRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: process.env.BSS_F12_SUPPLIER_NAME });
  await supplierRow.getByRole("button", { name: "合同 / 授信" }).click();
  const supplierPanel = page.locator(`.f12-subject-panel[data-subject-type="SUPPLIER"][data-subject-id="${subjectID}"]`);
  await supplierPanel.getByText("125.00 CNY", { exact: true }).first().waitFor();
  const supplierResponses = f12Responses.filter(response => new URL(response.url()).searchParams.get("subject_type") === "SUPPLIER" && new URL(response.url()).searchParams.get("subject_id") === subjectID);
  assert.equal(supplierResponses.length, 2, "supplier detail must load real contract and credit API projections");
  assert.ok(supplierResponses.every(response => response.status() === 200));
  await assertSecondF12Page(page, supplierPanel, { paginationClass: "contract-history-pagination", tableIndex: 0, pathname: "/api/internal/contracts", subjectType: "SUPPLIER", subjectID });
  await assertSecondF12Page(page, supplierPanel, { paginationClass: "credit-history-pagination", tableIndex: 2, pathname: "/api/internal/credits", subjectType: "SUPPLIER", subjectID });
  await supplierPanel.locator(".panel-body .el-table").nth(1).getByText("125.00 CNY", { exact: true }).waitFor();

  await page.goto(`http://127.0.0.1:${port}/#/internal/customers`, { waitUntil: "domcontentloaded" });
  await page.locator(".customer-page .tab").first().waitFor();
  const customerRow = page.locator(".customer-page .el-table__body tr").filter({ hasText: process.env.BSS_F12_CUSTOMER_NAME });
  await customerRow.waitFor();
  await customerRow.getByRole("button", { name: "详情" }).click();
  const customerPanel = page.locator(`.f12-subject-panel[data-subject-type="CUSTOMER"][data-subject-id="${subjectID}"]`);
  await customerPanel.getByText("500.00 CNY", { exact: true }).first().waitFor();
  const customerResponses = f12Responses.filter(response => new URL(response.url()).searchParams.get("subject_type") === "CUSTOMER" && new URL(response.url()).searchParams.get("subject_id") === subjectID);
  assert.equal(customerResponses.length, 2, "customer detail must query both F12 endpoints with the CUSTOMER type");
  assert.ok(customerResponses.every(response => response.status() === 200));
  await assertSecondF12Page(page, customerPanel, { paginationClass: "contract-history-pagination", tableIndex: 0, pathname: "/api/internal/contracts", subjectType: "CUSTOMER", subjectID });
  await assertSecondF12Page(page, customerPanel, { paginationClass: "credit-history-pagination", tableIndex: 2, pathname: "/api/internal/credits", subjectType: "CUSTOMER", subjectID });
  await customerPanel.locator(".panel-body .el-table").nth(1).getByText("500.00 CNY", { exact: true }).waitFor();
  assert.match(await customerPanel.innerText(), /办理中/);
  assert.doesNotMatch(await customerPanel.innerText(), /125\.00 CNY/);
  assert.deepEqual(pageErrors, []);

  await page.goto(`http://127.0.0.1:${port}/#/internal/suppliers`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();
  await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(process.env.BSS_F12_SUPPLIER_NAME);
  const entityListResponse = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supplier-entities" && url.searchParams.get("keyword") === process.env.BSS_F12_SUPPLIER_NAME && response.status() === 200;
  });
  await page.getByRole("button", { name: "供应商主体档案", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  const entityListPayload = await (await entityListResponse).json();
  assert.ok(entityListPayload?.data?.list?.some(row => row.name === process.env.BSS_F12_SUPPLIER_NAME), "main supplier workspace must reach the real filtered subject archive");
  await page.getByRole("button", { name: "← 返回供应商管理", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/suppliers?"));
  assert.equal(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("keyword"), process.env.BSS_F12_SUPPLIER_NAME, "return navigation must restore the supplier filter");

  assert.deepEqual(pageErrors, []);
  console.log("PASS: authenticated Playwright loaded supplier and customer F12 panels from real APIs, verified page 2 for both histories under same-ID isolation, and reached the filtered F4 subject page from the main supplier workspace.");
} finally {
  await browser?.close();
  await server?.close();
}
