import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_TEST_QA_PG_CONTAINER"]) {
  if (!process.env[name]) throw new Error(`供应商档案冲突真实联调缺少环境变量 ${name}。`);
}

const apiOrigin = new URL(process.env.BSS_TEST_API_ORIGIN);
assert.equal(apiOrigin.hostname, "127.0.0.1", "联调只能访问本机隔离 API");
assert.equal(apiOrigin.port, "18081", "联调只能写入当前工作区专用 API");
assert.equal(process.env.BSS_TEST_QA_PG_CONTAINER, "bss-a-tdd-20261026-pg", "联调只能清理指定隔离 PostgreSQL");

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

function runIsolatedSQL(sql) {
  const result = spawnSync("docker", ["exec", "-i", process.env.BSS_TEST_QA_PG_CONTAINER, "sh", "-lc",
    'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atq'], { input: sql, encoding: "utf8", timeout: 15000 });
  if (result.error || result.status !== 0) throw new Error(`隔离库 QA 清理失败：${result.error?.message || result.stderr || result.status}`);
  return result.stdout.trim();
}

const supplierName = `TDD-A-CONFLICT-${Date.now()}`;
assert.match(supplierName, /^[A-Z0-9-]+$/);
const pageErrors = [];
let supplierID = "", authHeader = "", server, browser, primaryError;

try {
  const port = await availablePort();
  server = await createServer({
    root: process.cwd(),
    server: { host: "127.0.0.1", port, strictPort: true, proxy: { "/api": { target: apiOrigin.origin, changeOrigin: true } } },
    logLevel: "error",
  });
  await server.listen();
  const base = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("request", request => {
    if (!authHeader && new URL(request.url()).pathname.startsWith("/api/internal/supplier-entities")) {
      authHeader = request.headers()["authorization"] || "";
    }
  });

  await page.goto(`${base}/#/internal/supplier-entities`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await Promise.all([
    page.waitForURL(url => url.hash.includes("/internal/supplier-entities"), { timeout: 20000 }),
    page.getByRole("button", { name: "登录", exact: true }).click(),
  ]);
  await page.getByRole("button", { name: "新建供应商", exact: true }).waitFor();

  const formItem = (dialog, label) => dialog.locator(".el-form-item").filter({ hasText: label });
  await page.getByRole("button", { name: "新建供应商", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "新建供应商主体" });
  await formItem(dialog, "供应商名称").locator("input").fill(supplierName);
  await formItem(dialog, "联系人").locator("input").fill("QA初始联系人");
  await formItem(dialog, "联系电话").locator("input").fill("13800001026");
  await formItem(dialog, "操作说明").locator("input").fill("QA并发冲突验收");
  const createWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/supplier-entities" && response.request().method() === "POST");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const createResponse = await createWait;
  assert.equal(createResponse.status(), 200, `QA供应商创建失败：HTTP ${createResponse.status()}`);
  const createEnvelope = await createResponse.json();
  const created = createEnvelope.data?.data ?? createEnvelope.data;
  supplierID = String(created.id);
  assert.match(supplierID, /^[1-9]\d*$/);
  assert.equal(created.revision, "1");
  assert.ok(authHeader.startsWith("Bearer "), "真实登录请求未提供认证头");

  const row = page.getByRole("row").filter({ hasText: supplierName });
  await row.waitFor();
  await row.getByRole("button", { name: "维护", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "维护供应商主体" });
  const contactField = formItem(dialog, "联系人").locator("input");
  const reasonField = formItem(dialog, "操作说明").locator("input");
  const uiDraft = "QA前端待重试草稿";
  const serverEdit = "QA并发更新版本";
  await contactField.fill(uiDraft);
  await reasonField.fill("QA旧版本冲突后重试");

  const concurrent = await page.request.put(`${apiOrigin.origin}/api/internal/supplier-entities/${supplierID}`, {
    headers: { authorization: authHeader, "Idempotency-Key": crypto.randomUUID() },
    data: {
      name: created.name, region: created.region, registration_no: created.registration_no,
      contact: serverEdit, phone: created.phone, tax_rate: created.tax_rate,
      settlement_currency: created.settlement_currency, expected_revision: created.revision,
      reason: "QA并发写入制造revision冲突",
    },
  });
  assert.equal(concurrent.status(), 200, `并发写入失败：HTTP ${concurrent.status()}`);
  const latest = (await concurrent.json()).data;
  assert.equal(latest.revision, "2");

  const staleSaveWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}` && response.request().method() === "PUT");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const staleSave = await staleSaveWait;
  assert.equal(staleSave.status(), 409, `旧revision应返回409，实际HTTP ${staleSave.status()}`);
  await dialog.locator(".el-alert").filter({ hasText: "服务器资料已变化" }).waitFor();
  assert.equal(await contactField.inputValue(), uiDraft, "冲突后必须保留前端草稿");
  assert.equal(await dialog.getByRole("button", { name: "保存", exact: true }).isDisabled(), true, "冲突后刷新核对前不能重试");

  const refreshWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}` && response.request().method() === "GET");
  await dialog.getByRole("button", { name: "载入最新资料", exact: true }).click();
  const refresh = await refreshWait;
  assert.equal(refresh.status(), 200);
  const refreshData = (await refresh.json()).data;
  assert.equal(refreshData.revision, "2");
  assert.equal(refreshData.contact, serverEdit);
  const refreshedValue = formItem(dialog, "联系人").locator("input");
  const deadline = Date.now() + 5000;
  while (await refreshedValue.inputValue() !== serverEdit && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 50));
  assert.equal(await refreshedValue.inputValue(), serverEdit, "显式刷新后表单应载入服务端新值");

  await refreshedValue.fill(uiDraft);
  await reasonField.fill("QA核对最新资料后重试");
  const retryWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/supplier-entities/${supplierID}` && response.request().method() === "PUT");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  const retry = await retryWait;
  assert.equal(retry.status(), 200, `刷新后重试应成功，实际HTTP ${retry.status()}`);
  const retryData = (await retry.json()).data;
  assert.equal(retryData.revision, "3");
  assert.equal(retryData.contact, uiDraft);
  assert.deepEqual(pageErrors, []);
  console.log(JSON.stringify({ supplierID, staleRevisionStatus: staleSave.status(), retryStatus: retry.status(), latestRevision: retryData.revision, draftPreservedUntilExplicitRefresh: true, retryUsedRefreshedRevision: true, pageErrors }, null, 2));
} catch (error) {
  primaryError = error;
  throw error;
} finally {
  await browser?.close();
  await server?.close();
  if (supplierID) {
    try {
      const id = String(supplierID);
      assert.match(id, /^[1-9]\d*$/);
      const leftovers = runIsolatedSQL(`BEGIN;
DELETE FROM operation_log WHERE entity_id=${id} AND entity_type IN ('supplier','supplier_alias') AND EXISTS (SELECT 1 FROM supplier WHERE id=${id} AND name='${supplierName}');
DELETE FROM assignment_log WHERE entity_id=${id} AND entity_type='supplier' AND EXISTS (SELECT 1 FROM supplier WHERE id=${id} AND name='${supplierName}');
DELETE FROM supplier WHERE id=${id} AND name='${supplierName}';
COMMIT;
SELECT count(*) FROM supplier WHERE id=${id} AND name='${supplierName}';`);
      if (leftovers !== "0") throw new Error(`隔离库 QA 供应商清理后仍有 ${leftovers} 行。`);
      console.log("QA supplier fixture cleaned from the isolated PostgreSQL database.");
    } catch (error) {
      if (!primaryError) throw error;
      console.error(`QA cleanup also failed: ${error instanceof Error ? error.message : "unknown error"}`);
    }
  }
}
