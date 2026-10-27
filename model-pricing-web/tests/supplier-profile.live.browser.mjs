import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN"];
for (const name of required) if (!process.env[name]) throw new Error(`真实供应商档案联调缺少环境变量 ${name}。`);

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

const supplierName = `TDD-A-联调-${Date.now()}`;
const pageErrors = [];
const apiResponses = [];
let server;
let browser;

try {
  const port = await availablePort();
  server = await createServer({
    root: process.cwd(),
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
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname === "/api/internal/auth/login" || url.pathname === "/api/internal/supplier-entities") {
      apiResponses.push({ method: response.request().method(), path: url.pathname, status: response.status() });
    }
  });

  async function login() {
    await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`);
    await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
    await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
    await Promise.all([
      page.waitForURL(url => url.hash.includes("/internal/supplier-entities"), { timeout: 20000 }),
      page.getByRole("button", { name: "登录", exact: true }).click(),
    ]);
    await page.getByRole("button", { name: "新建供应商", exact: true }).waitFor();
  }

  function formItem(dialog, label) {
    return dialog.locator(".el-form-item").filter({ hasText: label });
  }

  await login();
  const createListWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && response.request().method() === "GET");
  await page.getByRole("button", { name: "新建供应商", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "新建供应商主体" });
  await formItem(dialog, "供应商名称").locator("input").fill(supplierName);
  await formItem(dialog, "联系人").locator("input").fill("联调联系人");
  await formItem(dialog, "联系电话").locator("input").fill("13800001026");
  await formItem(dialog, "税率（百分比）").locator("input").fill("6.25");
  await formItem(dialog, "操作说明").locator("input").fill("浏览器真实建档联调");
  const createWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && response.request().method() === "POST");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const createResponse = await createWait;
  assert.equal(createResponse.status(), 200, `create failed with HTTP ${createResponse.status()}`);
  const createEnvelope = await createResponse.json();
  const createData = createEnvelope.data?.data ?? createEnvelope.data;
  const supplierID = String(createData.id);
  assert.match(supplierID, /^[1-9]\d*$/);
  assert.equal(createData.tax_rate, "0.06250000");
  const createList = await createListWait;
  assert.equal(createList.status(), 200, `created supplier reload failed with HTTP ${createList.status()}`);
  const row = page.getByRole("row").filter({ hasText: supplierName });
  await row.waitFor();
  assert.match(await row.innerText(), /6\.25%/);

  await row.getByRole("button", { name: "维护", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "维护供应商主体" });
  await formItem(dialog, "联系人").locator("input").fill("联调联系人已更新");
  await formItem(dialog, "操作说明").locator("input").fill("浏览器真实修改联调");
  const updateWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}` && response.request().method() === "PUT");
  const updateListWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && response.request().method() === "GET");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const updateResponse = await updateWait;
  assert.equal(updateResponse.status(), 200, `update failed with HTTP ${updateResponse.status()}`);
  const updateList = await updateListWait;
  assert.equal(updateList.status(), 200, `updated supplier reload failed with HTTP ${updateList.status()}`);
  assert.match(await row.innerText(), /联调联系人已更新/);
  assert.match(await row.innerText(), /6\.25%/);

  // The current auth contract keeps the bearer token in memory. A hard reload
  // returns to login; reauthenticate before checking the persisted database row.
  await page.reload();
  await page.locator("#login-account").waitFor();
  assert.match(page.url(), /\/internal\/login/);
  const loginListWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && response.request().method() === "GET");
  await login();
  const loginList = await loginListWait;
  assert.equal(loginList.status(), 200, `post-login persisted supplier read failed with HTTP ${loginList.status()}`);
  const persistedRow = page.getByRole("row").filter({ hasText: supplierName });
  await persistedRow.waitFor();
  assert.match(await persistedRow.innerText(), /联调联系人已更新/);
  assert.match(await persistedRow.innerText(), /6\.25%/);
  assert.deepEqual(pageErrors, []);

  console.log(JSON.stringify({
    supplierID,
    taxRateResponse: createData.tax_rate,
    createStatus: createResponse.status(),
    updateStatus: updateResponse.status(),
    reloadRequiredLogin: true,
    persistedAfterReauthentication: true,
    taxDisplayedAsPercent: true,
    apiResponses,
    pageErrors,
  }, null, 2));
} finally {
  await browser?.close();
  await server?.close();
}
