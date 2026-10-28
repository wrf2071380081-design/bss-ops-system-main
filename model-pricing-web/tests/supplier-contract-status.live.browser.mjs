import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_WORKSPACE_UNREGISTERED_NAME"];
for (const name of required) if (!process.env[name]) throw new Error(`真实联调缺少环境变量 ${name}。`);

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
    server: { host: "127.0.0.1", port, strictPort: true, proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } } },
    logLevel: "error",
  });
  await server.listen();
  const base = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(10000);
  page.on("pageerror", error => pageErrors.push(error.message));
  const supplierResponses = [];
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname === "/api/internal/supply/suppliers" && !url.searchParams.has("contract_status")) supplierResponses.push(response);
  });
  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsuppliers`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/suppliers"));
  await page.getByRole("heading", { name: "供应商管理", exact: true }).waitFor();

  const loaded = supplierResponses.at(-1);
  assert.ok(loaded, "the supplier list must load through the real API");
  assert.equal(loaded.status(), 200, "authenticated supplier list API must return 200");
  const data = await loaded.json();
  const row = data?.data?.list?.find(item => item.name === process.env.BSS_WORKSPACE_UNREGISTERED_NAME);
  assert.ok(row, "the unregistered supplier must be returned by the real API");
  assert.equal(row.contract_status, "未登记", "the API must not infer contract state from supplier establishment");
  const visibleRow = page.locator(".supplier-workspace .el-table__body tr").filter({ hasText: process.env.BSS_WORKSPACE_UNREGISTERED_NAME });
  await visibleRow.waitFor();
  assert.match(await visibleRow.innerText(), /未登记/, "the page must display the unregistered contract state");

  await page.getByPlaceholder("搜索供应商名称 / 曾用名").fill(process.env.BSS_WORKSPACE_UNREGISTERED_NAME);
  await page.locator(".filters .el-select").nth(1).click();
  await page.getByRole("option", { name: "未登记", exact: true }).click();
  const filteredResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/internal/supply/suppliers" && url.searchParams.get("contract_status") === "未登记";
  });
  await page.getByRole("button", { name: "查询", exact: true }).click();
  const filteredResponse = await filteredResponsePromise;
  assert.equal(filteredResponse.status(), 200, "the real API must accept the unregistered contract filter");
  const filtered = await filteredResponse.json();
  assert.deepEqual(filtered?.data?.list?.map(item => item.name), [process.env.BSS_WORKSPACE_UNREGISTERED_NAME]);
  assert.deepEqual(pageErrors, []);
  console.log("PASS: authenticated Playwright verified the real supplier API, unregistered-state rendering, and server-side contract-status filtering against the isolated PostgreSQL database.");
} finally {
  await browser?.close();
  await server?.close();
}
