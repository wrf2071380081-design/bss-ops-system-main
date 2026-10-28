import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN"];
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
const apiCalls = [];

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
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/internal/")) apiCalls.push({ method: request.method(), path: url.pathname });
  });

  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fsupplier-entities`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/supplier-entities"));
  await page.getByRole("heading", { name: "供应商主体", exact: true }).waitFor();

  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const sourceName = `QA-MERGE-SOURCE-${suffix}`;
  const targetName = `QA-MERGE-TARGET-${suffix}`;
  async function createSupplier(name) {
    await page.getByRole("button", { name: "新建供应商", exact: true }).click();
    const dialog = page.getByRole("dialog").last();
    const items = dialog.locator(".supplier-entity-form .entity-form-grid .el-form-item");
    await items.nth(0).locator("input").fill(name);
    await items.nth(7).locator("input").fill("隔离库合并预览联调");
    const saved = page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/internal/supplier-entities");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const response = await saved;
    assert.equal(response.status(), 200, "supplier test data must be created through the authenticated real API");
    const payload = await response.json();
    assert.equal(payload?.data?.name, name);
    await page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: name }).waitFor();
  }

  await createSupplier(sourceName);
  await createSupplier(targetName);
  const sourceRow = page.locator(".supplier-entity-table .el-table__body tr").filter({ hasText: sourceName });
  await sourceRow.getByRole("button", { name: "合并预览", exact: true }).click();
  const dialog = page.getByRole("dialog").last();
  await dialog.getByText("合并预览", { exact: true }).waitFor();
  const targetSelect = dialog.getByRole("combobox", { name: "目标供应商" });
  await targetSelect.click();
  await page.getByRole("option", { name: new RegExp(targetName) }).click();

  const previewCall = page.waitForResponse(response => {
    const url = new URL(response.url());
    return response.request().method() === "POST" && url.pathname.endsWith("/merge-preview");
  });
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  const previewResponse = await previewCall;
  assert.equal(previewResponse.status(), 200, "the authenticated merge preview API must read from the isolated PostgreSQL database");
  const envelope = await previewResponse.json();
  const preview = envelope?.data;
  assert.equal(preview?.source?.name, sourceName);
  assert.equal(preview?.target?.name, targetName);
  assert.equal(preview?.source_references?.resources, 0);
  assert.equal(preview?.target_references?.resources, 0);
  assert.equal(typeof preview?.source?.revision, "string");
  assert.equal(typeof preview?.target?.revision, "string");
  const serialized = JSON.stringify(preview);
  for (const forbidden of ["api_key", "api_key_enc", "password_enc", "file_ref", "file_path", "storage_path", "can_merge"]) {
    assert.equal(serialized.includes(forbidden), false, `preview DTO must not expose ${forbidden}`);
  }
  await dialog.getByText(sourceName, { exact: true }).waitFor();
  await dialog.getByText(targetName, { exact: true }).waitFor();
  await dialog.getByText("供应资源", { exact: true }).waitFor();
  assert.equal(await dialog.getByRole("button", { name: "确认合并", exact: true }).count(), 0, "the UI must remain preview-only");
  assert.equal(apiCalls.some(call => call.method === "POST" && call.path.endsWith("/merge-preview")), true);
  assert.equal(apiCalls.some(call => call.method === "PUT" || call.method === "DELETE"), false, "preview must not mutate supplier or relationship records");
  assert.deepEqual(pageErrors, []);
  console.log("PASS: authenticated Playwright verified supplier merge preview through the real page, API, and isolated v36 PostgreSQL; no merge mutation was offered.");
} finally {
  await browser?.close();
  await server?.close();
}
