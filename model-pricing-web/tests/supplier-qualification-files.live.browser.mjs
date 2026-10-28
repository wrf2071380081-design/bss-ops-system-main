import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN"]) {
  if (!process.env[name]) throw new Error(`供应商资质真实联调缺少环境变量 ${name}。`);
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
  const origin = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => pageErrors.push(error.message));

  await page.goto(`${origin}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();

  const supplierName = `QA-F4-QUAL-${Date.now()}`;
  await page.getByRole("button", { name: "新建供应商", exact: true }).click();
  const createDialog = page.getByRole("dialog").last();
  const fields = createDialog.locator(".entity-form-grid .el-form-item");
  await fields.nth(0).locator("input").fill(supplierName);
  await fields.nth(7).locator("input").fill("Playwright 资质文件隔离联调");
  const createResponsePromise = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/internal/supplier-entities");
  await createDialog.getByRole("button", { name: "保存", exact: true }).click();
  const createResponse = await createResponsePromise;
  assert.equal(createResponse.status(), 200, "a real authenticated supplier create should succeed in the isolated database");
  const createdRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: supplierName });
  await createdRow.waitFor();
  const idMatch = (await createdRow.locator(".mono").textContent())?.match(/ID\s+([1-9]\d*)/);
  assert.ok(idMatch, "the newly created supplier ID should be visible");
  const supplierID = idMatch[1];

  await page.goto(`${origin}/#/internal/suppliers/${supplierID}/models?return_to=suppliers&supplier_name=${encodeURIComponent(supplierName)}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: supplierName, exact: true }).waitFor();
  const initialListPromise = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}/qualifications` && response.request().method() === "GET");
  await page.getByRole("tab", { name: "档案与办理", exact: true }).click();
  const initialList = await initialListPromise;
  assert.equal(initialList.status(), 200);

  const pdf = Buffer.from("%PDF-1.7\nBSS isolated qualification fixture\n");
  await page.getByRole("button", { name: "上传资质", exact: true }).click();
  const uploadDialog = page.getByRole("dialog").last();
  await uploadDialog.getByLabel("资质名称").fill("QA隔离资质文件");
  await uploadDialog.locator('input[type="file"]').setInputFiles({ name: "qa-license.pdf", mimeType: "application/pdf", buffer: pdf });
  await uploadDialog.getByLabel("操作说明").fill("Playwright 上传与下载闭环");
  const uploadResponsePromise = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}/qualifications`);
  const refreshedListPromise = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}/qualifications` && response.request().method() === "GET")
    .then(response => ({ response }), error => ({ error }));
  await uploadDialog.getByRole("button", { name: "保存资质", exact: true }).click();
  const uploadResponse = await uploadResponsePromise;
  assert.equal(uploadResponse.status(), 200, await uploadResponse.text());
  const uploadBody = await uploadResponse.json();
  assert.equal(uploadBody.data.name, "QA隔离资质文件");
  assert.equal(uploadBody.data.can_download, true);
  assert.equal(uploadBody.data.file_name, "qa-license.pdf");
  assert.equal("file_ref" in uploadBody.data, false);
  assert.equal(JSON.stringify(uploadBody).includes("squal:v1:"), false, "opaque storage references must not be exposed");

  const refreshedListResult = await refreshedListPromise;
  assert.ok(refreshedListResult.response, `qualification list did not reload: ${refreshedListResult.error}`);
  const refreshedList = refreshedListResult.response;
  const refreshedListText = await refreshedList.text();
  assert.equal(refreshedList.status(), 200, refreshedListText);
  const refreshedBody = JSON.parse(refreshedListText);
  const qualification = refreshedBody.data.list.find(row => row.name === "QA隔离资质文件");
  assert.ok(qualification, "the saved qualification must be reloaded from PostgreSQL");
  assert.equal(qualification.can_download, true);
  assert.equal(qualification.file_name, "qa-license.pdf");
  await uploadDialog.waitFor({ state: "hidden" });
  const qualificationRow = page.getByRole("row").filter({ hasText: "QA隔离资质文件" });
  await qualificationRow.waitFor({ state: "visible" });

  const downloadPromise = page.waitForResponse(response => response.request().method() === "GET" && new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}/qualifications/${qualification.id}/file`);
  await qualificationRow.getByRole("button", { name: "下载 qa-license.pdf", exact: true }).click();
  const downloadResponse = await downloadPromise;
  assert.equal(downloadResponse.status(), 200);
  assert.equal(downloadResponse.headers()["content-type"], "application/pdf");
  assert.deepEqual(await downloadResponse.body(), pdf, "download bytes must match the uploaded PDF");
  assert.deepEqual(pageErrors, []);
  console.log(`PASS: real Playwright authentication verified supplier qualification upload, API list reload, safe file metadata, and byte-identical download in the isolated database. QA supplier ${supplierID}.`);
} finally {
  await browser?.close();
  await server?.close();
}
