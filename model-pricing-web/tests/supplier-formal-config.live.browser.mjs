import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_TEST_SUPPLIER_ID", "BSS_TEST_RESOURCE_ID", "BSS_TEST_MODEL_ID"]) {
  if (!process.env[name]) throw new Error(`正式配置真实联调缺少环境变量 ${name}。`);
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

const supplierID = process.env.BSS_TEST_SUPPLIER_ID;
const resourceID = process.env.BSS_TEST_RESOURCE_ID;
const modelID = process.env.BSS_TEST_MODEL_ID;
const configScope = `QA正式配置-${Date.now()}`;
const initialKey = `qa-formal-secret-${Date.now()}`;
const rotatedKey = `${initialKey}-rotated`;
let server, browser;
const pageErrors = [];

function assertSafeConfigResponse(body, secret) {
  assert.equal(JSON.stringify(body).includes(secret), false, "formal API Key must never be returned to the page");
  for (const forbidden of ["api_key", "api_key_enc", "ciphertext", "secret"]) {
    assert.equal(forbidden in body.data, false, `formal configuration response must not include ${forbidden}`);
  }
  assert.equal(body.data.has_api_key, true);
}

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

  const target = `/internal/suppliers/${supplierID}/models?resource_id=${resourceID}&model_id=${modelID}&region=CN`;
  await page.goto(`${origin}/#/internal/login?redirect=${encodeURIComponent(target)}`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.includes(`/internal/suppliers/${supplierID}/models`));
  await page.getByRole("heading", { name: "QA-F4-CONFIG-MODEL-20261024", exact: true }).waitFor();
  await page.getByRole("tab", { name: "接入与配置", exact: true }).click();

  const panel = page.locator(".resource-operations");
  await panel.getByRole("button", { name: "新建正式配置", exact: true }).click();
  let dialog = page.getByRole("dialog").last();
  const fields = dialog.locator(".el-form-item");
  await fields.nth(0).locator("input").fill(configScope);
  await fields.nth(1).locator("input").fill("https://1.1.1.1/v1");
  await fields.nth(2).locator("input").fill(initialKey);
  await fields.nth(3).locator("textarea").fill("真实页面/API隔离联调");
  await dialog.locator("label.el-checkbox").click();
  const createResponsePromise = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/internal/suppliers/${supplierID}/formal-configs`);
  await dialog.getByRole("button", { name: "保存正式配置", exact: true }).click();
  const createResponse = await createResponsePromise;
  assert.equal(createResponse.status(), 200, "formal config create should return HTTP 200");
  const created = await createResponse.json();
  assertSafeConfigResponse(created, initialKey);
  assert.equal(created.data.revision, "1");
  const configRow = () => panel.locator(".el-table__row").filter({ hasText: configScope });
  await configRow().getByText(configScope, { exact: true }).waitFor();
  await configRow().getByText("已设置", { exact: true }).waitFor();

  await configRow().getByRole("button", { name: "修改", exact: true }).click();
  dialog = page.getByRole("dialog").last();
  const editFields = dialog.locator(".el-form-item");
  await editFields.nth(0).locator("input").fill(configScope);
  await editFields.nth(1).locator("input").fill("https://1.1.1.1/v2");
  assert.equal(await editFields.nth(2).locator("input").inputValue(), "", "saved Key must not be returned into the edit form");
  await editFields.nth(3).locator("textarea").fill("留空保留已保存Key");
  await dialog.locator("label.el-checkbox").click();
  const preserveResponsePromise = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === `/api/internal/suppliers/${supplierID}/formal-configs/${created.data.id}`);
  await dialog.getByRole("button", { name: "保存正式配置", exact: true }).click();
  const preserveResponse = await preserveResponsePromise;
  assert.equal(preserveResponse.status(), 200, "formal config update with a blank Key should return HTTP 200");
  const preserved = await preserveResponse.json();
  assertSafeConfigResponse(preserved, initialKey);
  assert.equal(preserved.data.revision, "2");
  await configRow().getByText("修订 2", { exact: true }).waitFor();

  await configRow().getByRole("button", { name: "修改", exact: true }).click();
  dialog = page.getByRole("dialog").last();
  const rotateFields = dialog.locator(".el-form-item");
  await rotateFields.nth(0).locator("input").fill(configScope);
  await rotateFields.nth(1).locator("input").fill("https://1.1.1.1/v2");
  await rotateFields.nth(2).locator("input").fill(rotatedKey);
  await rotateFields.nth(3).locator("textarea").fill("轮换正式Key");
  await dialog.locator("label.el-checkbox").click();
  const rotateResponsePromise = page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === `/api/internal/suppliers/${supplierID}/formal-configs/${created.data.id}`);
  await dialog.getByRole("button", { name: "保存正式配置", exact: true }).click();
  const rotateResponse = await rotateResponsePromise;
  assert.equal(rotateResponse.status(), 200, "formal config Key rotation should return HTTP 200");
  const rotated = await rotateResponse.json();
  assertSafeConfigResponse(rotated, rotatedKey);
  assert.equal(rotated.data.revision, "3");
  await configRow().getByText("修订 3", { exact: true }).waitFor();
  await page.reload({ waitUntil: "domcontentloaded" });
  if (await page.locator("#login-account").count()) {
    await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
    await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
    await page.getByRole("button", { name: "登录", exact: true }).click();
  }
  await page.waitForURL(url => url.hash.includes(`/internal/suppliers/${supplierID}/models`));
  await page.getByRole("heading", { name: "QA-F4-CONFIG-MODEL-20261024", exact: true }).waitFor();
  await page.getByRole("tab", { name: "接入与配置", exact: true }).click();
  await configRow().getByText("修订 3", { exact: true }).waitFor();
  assert.equal(await page.getByText(initialKey, { exact: true }).count(), 0);
  assert.equal(await page.getByText(rotatedKey, { exact: true }).count(), 0);
  assert.deepEqual(pageErrors, []);
  console.log("PASS: authenticated formal config create, blank-Key preservation, Key rotation, reload, and safe response rendering against the isolated PostgreSQL API.");
} finally {
  await browser?.close();
  await server?.close();
}
