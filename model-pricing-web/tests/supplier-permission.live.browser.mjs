import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { randomUUID } from "node:crypto";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = [
  "BSS_TEST_NONOWNER_LOGIN_ID", "BSS_TEST_NONOWNER_PASSWORD", "BSS_TEST_API_ORIGIN",
  "BSS_SUPPLIER_PERMISSION_NAME",
];
for (const name of required) if (!process.env[name]) throw new Error(`供应商跨采购人真实权限验收缺少环境变量 ${name}。`);

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
let supplierListResponse;
let authHeader = "";

try {
  const port = await availablePort();
  const origin = `http://127.0.0.1:${port}`;
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
  page.on("request", request => {
    if (new URL(request.url()).pathname === "/api/internal/supplier-entities")
      authHeader = request.headers().authorization || authHeader;
  });
  page.on("response", response => {
    if (new URL(response.url()).pathname === "/api/internal/supplier-entities") supplierListResponse = response;
  });

  await page.goto(`${origin}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_NONOWNER_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_NONOWNER_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();
  await page.getByRole("textbox", { name: "主体名称搜索" }).fill(process.env.BSS_SUPPLIER_PERMISSION_NAME);
  const filteredList = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supplier-entities" && url.searchParams.get("keyword") === process.env.BSS_SUPPLIER_PERMISSION_NAME && response.status() === 200;
  });
  await page.getByRole("button", { name: "查询", exact: true }).click();
  supplierListResponse = await filteredList;
  await page.waitForFunction(() => document.querySelector(".supplier-entity-table .el-table__body tr"));

  assert.equal(supplierListResponse?.status(), 200, "non-owner must be able to read the supplier list");
  assert.match(authHeader, /^Bearer\s+\S+$/i, "the page must use a real authenticated bearer token");
  const pageData = await supplierListResponse.json();
  const supplier = pageData?.data?.list?.find(row => row.name === process.env.BSS_SUPPLIER_PERMISSION_NAME);
  assert.ok(supplier, "the isolated supplier owned by another buyer must be listed");
  assert.equal(supplier.can_edit, false, "the API projection must mark another buyer's supplier as read-only");

  const row = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: process.env.BSS_SUPPLIER_PERMISSION_NAME });
  await row.getByRole("button", { name: "维护" }).waitFor();
  assert.equal(await row.getByRole("button", { name: "维护" }).isDisabled(), true, "the page must disable the non-owner edit action");

  const directWrite = await page.context().request.put(`${origin}/api/internal/supplier-entities/${supplier.id}`, {
    headers: { Authorization: authHeader, "Idempotency-Key": randomUUID() },
    data: {
      expected_revision: String(supplier.revision),
      name: "QA-FORBIDDEN-UPDATE-MUST-NOT-PERSIST",
      region: supplier.region,
      registration_no: supplier.registration_no || "",
      contact: supplier.contact || "",
      phone: supplier.phone || "",
      tax_rate: supplier.tax_rate ?? null,
      settlement_currency: supplier.settlement_currency,
      reason: "跨采购人权限反例验收",
    },
  });
  const directWriteBody = await directWrite.text();
  assert.equal(directWrite.status(), 403, `the authenticated non-owner must be rejected by the real update API (body: ${directWriteBody})`);

  const forbiddenFormalConfig = await page.context().request.post(`${origin}/api/internal/suppliers/${supplier.id}/formal-configs`, {
    headers: { Authorization: authHeader, "Idempotency-Key": randomUUID() },
    data: {
      scope: "QA非归属配置权限验证",
      base_url: "https://provider.example/v1",
      api_key: "qa-forbidden-formal-config-key",
      external_confirmed: true,
      reason: "验证非归属采购人不能新增正式配置",
    },
  });
  const forbiddenFormalConfigBody = await forbiddenFormalConfig.text();
  assert.equal(forbiddenFormalConfig.status(), 403, `the authenticated non-owner must be rejected by the real formal-config API (body: ${forbiddenFormalConfigBody})`);

  const supplierDetail = `/internal/suppliers/${supplier.id}/models?return_to=suppliers&supplier_name=${encodeURIComponent(supplier.name)}`;
  await page.goto(`${origin}/#${supplierDetail}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: supplier.name, exact: true }).waitFor();
  const qualificationsRead = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === `/api/internal/supplier-entities/${supplier.id}/qualifications` && response.request().method() === "GET";
  });
  await page.getByRole("tab", { name: "档案与办理", exact: true }).click();
  const qualificationsResponse = await qualificationsRead;
  assert.equal(qualificationsResponse.status(), 200, "a non-owner may read qualification records");
  assert.equal(await page.getByRole("button", { name: "上传资质", exact: true }).count(), 0, "the UI must hide qualification write actions from a non-owner");
  const forbiddenQualification = await page.context().request.post(`${origin}/api/internal/supplier-entities/${supplier.id}/qualifications`, {
    headers: { Authorization: authHeader, "Idempotency-Key": randomUUID() },
    data: { name: "QA-FORBIDDEN-QUALIFICATION", file_name: "qa.pdf", content_base64: Buffer.from("%PDF-1.7\nQA permission probe\n").toString("base64"), reason: "跨采购人资质权限反例验收" },
  });
  assert.equal(forbiddenQualification.status(), 403, "the real qualification upload API must reject a non-owner");
  assert.deepEqual(pageErrors, []);
  console.log("PASS: buyer_b could read another buyer's supplier and qualifications, saw owner-only actions hidden/disabled, and received HTTP 403 for direct profile, qualification, and formal-config writes.");
} finally {
  await browser?.close();
  await server?.close();
}
