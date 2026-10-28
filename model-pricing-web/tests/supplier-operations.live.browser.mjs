import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_TEST_SUPPLIER_ID", "BSS_TEST_SUPPLIER_NAME"]) {
  if (!process.env[name]) throw new Error(`供应商操作记录真实联调缺少环境变量 ${name}。`);
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
let authHeader = "";
const supplierID = process.env.BSS_TEST_SUPPLIER_ID;
const supplierName = process.env.BSS_TEST_SUPPLIER_NAME;
const apiOrigin = process.env.BSS_TEST_API_ORIGIN.replace(/\/$/, "");
const expectedSupplierAction = process.env.BSS_TEST_PROFILE_OPERATION_ACTION || "UPDATE";
const profileActionLabel = expectedSupplierAction === "CREATE" ? "新建" : "维护";
const qualificationName = process.env.BSS_TEST_QUALIFICATION_NAME || "";
const expectedEntityTypes = (process.env.BSS_TEST_EXPECT_ENTITY_TYPES || "").split(",").filter(Boolean);
const operationsPath = `/api/internal/suppliers/${supplierID}/operations`;

try {
  const port = await availablePort();
  server = await createServer({
    server: { host: "127.0.0.1", port, strictPort: true,
      proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } },
    },
    logLevel: "error",
  });
  await server.listen();
  const origin = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("request", request => {
    if (new URL(request.url()).pathname.startsWith("/api/internal/")) authHeader = request.headers()["authorization"] || authHeader;
  });

  const detail = `/internal/suppliers/${supplierID}/models?return_to=suppliers&supplier_name=${encodeURIComponent(supplierName)}`;
  await page.goto(`${origin}/#/internal/login?redirect=${encodeURIComponent(detail)}`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.includes(`/internal/suppliers/${supplierID}/models`));
  await page.getByRole("heading", { name: supplierName, exact: true }).waitFor();

  const initialResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === operationsPath && response.request().method() === "GET" && !url.searchParams.has("entity_type");
  });
  await page.getByRole("tab", { name: "操作记录", exact: true }).click();
  const initialResponse = await initialResponsePromise;
  assert.equal(initialResponse.status(), 200, "the authenticated supplier operation list should load from the isolated API");
  const initialBody = await initialResponse.json();
  const initialRows = initialBody.data.list;
  assert.ok(initialRows.some(row => row.entity_type === "supplier" && row.action === expectedSupplierAction), "the supplier profile operation should appear in its history");
  for (const entityType of expectedEntityTypes) assert.ok(initialRows.some(row => row.entity_type === entityType), `the nonempty supplier history should include ${entityType}`);
  if (qualificationName) assert.ok(initialRows.some(row => row.entity_type === "supplier_qualification" && row.action === "UPLOAD" && row.reason === "Playwright 上传与下载闭环"), "the qualification upload should appear in its history");
  assert.equal(JSON.stringify(initialBody).match(/"entity_type"/g)?.length, initialRows.length, "the API must return only its explicit safe projection");
  for (const forbidden of ["file_ref", "storage_path", "api_key", "before", "after"]) {
    assert.equal(JSON.stringify(initialBody).includes(forbidden), false, `operation history must not expose ${forbidden}`);
  }
  for (const entityType of expectedEntityTypes) {
    const response = await page.request.get(`${apiOrigin}${operationsPath}?entity_type=${encodeURIComponent(entityType)}&page=1&size=20`, { headers: { Authorization: authHeader } });
    assert.equal(response.status(), 200, `${entityType} history category should load from the real API`);
    const body = await response.json();
    assert.ok(body.data.list.length > 0, `${entityType} history category should be nonempty`);
    assert.ok(body.data.list.every(row => row.entity_type === entityType), `${entityType} filter must not leak another log category`);
    for (const forbidden of ["file_ref", "storage_path", "api_key", "api_key_enc", "before", "after"]) {
      assert.equal(JSON.stringify(body).includes(forbidden), false, `${entityType} history must not expose ${forbidden}`);
    }
    if (entityType === "contract" && process.env.BSS_TEST_EXPECT_SUPPLIER_CONTRACT_COUNT) {
      assert.equal(body.data.list.length, Number(process.env.BSS_TEST_EXPECT_SUPPLIER_CONTRACT_COUNT), "same-ID customer contracts must not leak into supplier history");
    }
    if (entityType === "credit_line" && process.env.BSS_TEST_EXPECT_SUPPLIER_CREDIT_COUNT) {
      assert.equal(body.data.list.length, Number(process.env.BSS_TEST_EXPECT_SUPPLIER_CREDIT_COUNT), "same-ID customer credit lines must not leak into supplier history");
    }
  }
  await page.getByRole("row").filter({ hasText: `主体档案 · ${profileActionLabel}` }).first().waitFor();

  const filteredResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === operationsPath && url.searchParams.get("entity_type") === "supplier";
  }).then(response => ({ response }), error => ({ error }));
  await page.locator(".operation-filters .el-select__wrapper").click();
  await page.getByRole("option", { name: "主体档案", exact: true }).click();
  const filteredResult = await filteredResponsePromise;
  assert.ok(filteredResult.response, `category selection must reload from the API: ${filteredResult.error}`);
  const filteredResponse = filteredResult.response;
  assert.equal(filteredResponse.status(), 200);
  const filteredBody = await filteredResponse.json();
  assert.ok(filteredBody.data.list.length > 0);
  assert.ok(filteredBody.data.list.every(row => row.entity_type === "supplier"));

  const profileOperation = page.getByRole("row").filter({ hasText: `主体档案 · ${profileActionLabel}` }).first();
  await profileOperation.getByRole("button", { name: "查看", exact: true }).click();
  const detailDialog = page.getByRole("dialog", { name: "操作记录详情" });
  await detailDialog.waitFor();
  assert.equal(await detailDialog.getByText(`主体档案 · ${profileActionLabel}`, { exact: true }).count(), 1);
  await detailDialog.getByRole("button", { name: "查看业务资料", exact: true }).click();
  await page.waitForFunction(() => new URL(location.href).hash.includes("section=supplier"));
  await page.getByRole("tab", { name: "档案与办理", exact: true }).waitFor();
  if (qualificationName) {
    const allOperationsPromise = page.waitForResponse(response => {
      const url = new URL(response.url());
      return url.pathname === operationsPath;
    });
    await page.getByRole("tab", { name: "操作记录", exact: true }).click();
    const allOperations = await allOperationsPromise;
    assert.equal(allOperations.status(), 200);
    const qualificationOperationsPromise = page.waitForResponse(response => {
      const url = new URL(response.url());
      return url.pathname === operationsPath && url.searchParams.get("entity_type") === "supplier_qualification";
    });
    await page.locator(".operation-filters .el-select__wrapper").click();
    await page.getByRole("option", { name: "资质", exact: true }).click();
    const qualificationOperations = await qualificationOperationsPromise;
    assert.equal(qualificationOperations.status(), 200);
    const qualificationRow = page.getByRole("row").filter({ hasText: "资质 · 上传" }).first();
    await qualificationRow.waitFor();
    await qualificationRow.getByRole("button", { name: "查看", exact: true }).click();
    const qualificationDetail = page.getByRole("dialog", { name: "操作记录详情" });
    await qualificationDetail.getByRole("button", { name: "查看业务资料", exact: true }).click();
    await page.waitForFunction(() => new URL(location.href).hash.includes("section=supplier_qualification"));
    await page.getByText(qualificationName, { exact: true }).waitFor();
  }
  assert.deepEqual(pageErrors, []);
  console.log(`PASS: authenticated Playwright verified supplier profile history, safe projection, category filtering, and navigation back to the profile page for supplier ${supplierID}.`);
} finally {
  if (authHeader && browser) {
    try {
      await browser.contexts()[0].request.post(`${process.env.BSS_TEST_API_ORIGIN.replace(/\/$/, "")}/api/internal/auth/logout`, { headers: { Authorization: authHeader } });
    } catch { /* don't hide the original assertion when logout is unavailable */ }
  }
  await browser?.close();
  await server?.close();
}
