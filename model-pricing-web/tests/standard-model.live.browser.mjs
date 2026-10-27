import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";
import { mkdir, writeFile } from "node:fs/promises";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const required = ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN"];
for (const name of required) if (!process.env[name]) throw new Error(`标准模型真实联调缺少环境变量 ${name}。`);

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

const modelName = `QA-A-F1-${Date.now()}`;
const officialID = `qa-a-model-${Date.now()}`;
const pageErrors = [];
const apiResponses = [];
const evidence = process.env.BSS_MODEL_PAGES_EVIDENCE || ".qa/model-pages-live";
await mkdir(evidence, { recursive: true });
  let completionPayload;
let server;
let browser;

try {
  let base = process.env.BSS_TEST_WEB_ORIGIN;
  if (base) {
    assert.equal(base, "http://127.0.0.1:5173");
    assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB, "model_bss");
  } else {
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
  base = `http://127.0.0.1:${port}`;
  }
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1600, height: 1200 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname.startsWith("/api/internal/standard-models") || url.pathname === "/api/internal/auth/login") {
      apiResponses.push({ method: response.request().method(), path: url.pathname, status: response.status() });
    }
  });
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.pathname.endsWith("/manual-completion") && request.method() === "PUT") completionPayload = request.postDataJSON();
  });

  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodels`);
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await Promise.all([
    page.waitForURL(url => url.hash.includes("/internal/models"), { timeout: 20000 }),
    page.getByRole("button", { name: "登录", exact: true }).click(),
  ]);
  await page.getByRole("heading", { name: "标准模型库", exact: true }).waitFor();

  const dialog = page.locator(".standard-model-dialog");
  const waitForSaveIdle = () => dialog.waitFor({ state: "hidden" });
  const choose = async (control, label) => {
    await control.locator(".el-select__wrapper").click();
    const input = control.locator(".el-select__input");
    const listID = await input.getAttribute("aria-controls");
    const menu = page.locator(`[id="${listID}"]`);
    await menu.waitFor({ state: "visible" });
    await menu.getByText(label, { exact: true }).click({ force: true });
    await page.keyboard.press("Escape");
  };
  await page.getByRole("button", { name: "＋ 新增模型", exact: true }).click();
  await choose(dialog.locator(".el-form-item").filter({ hasText: "厂商" }).locator(".el-select"), "OpenAI");
  await dialog.getByLabel("官方 Model ID／版本", { exact: true }).fill(officialID);
  await dialog.getByLabel("官方标准名称", { exact: true }).fill(modelName);
  const createWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/standard-models/manual-completion" && response.request().method() === "POST");
  await dialog.getByRole("button", { name: "保存并留痕", exact: true }).click();
  const createResponse = await createWait;
  assert.equal(createResponse.status(), 200, `incomplete model create failed with HTTP ${createResponse.status()}`);
  await waitForSaveIdle();
  const createdEnvelope = await createResponse.json();
  let model = createdEnvelope.data?.data ?? createdEnvelope.data;
  assert.ok(model.id);
  await writeFile(`${evidence}/fixture.json`, JSON.stringify({ modelID: model.id, modelName, officialID }));
  assert.equal(model.verify_status, "INCOMPLETE");
  assert.ok(model.missing_fields.length > 0);
  assert.equal(model.quote_readiness.ready, false);
  await page.getByText("已保存，仍有资料待补齐", { exact: true }).waitFor();

  const openModel = async () => {
    await page.getByRole("textbox", { name: "搜索模型名称或官方ID", exact: true }).fill(officialID);
    await page.locator(".official-table tbody tr").filter({ hasText: officialID }).getByRole("button", { name: "维护", exact: true }).click();
    await dialog.getByLabel("官方标准名称", { exact: true }).waitFor();
  };
  await openModel();
  await choose(dialog.locator(".el-form-item").filter({ hasText: "模型类型" }).locator(".el-select"), "文本");
  await dialog.getByLabel("输入／输出模态", { exact: true }).fill("输入：文本\n输出：文本");
  await dialog.getByLabel("容量与规格", { exact: true }).fill("上下文：128000 tokens");
  await dialog.getByLabel("核心能力", { exact: true }).fill("Function Calling ✓（支持工具调用）");
  await dialog.getByLabel("API、限流与访问条件", { exact: true }).fill("协议：HTTPS API");
  await dialog.getByLabel("发布日期、版本与生命周期", { exact: true }).fill("发布日期：2026-01");
  const addPrice = async (index, label, amount) => {
    await dialog.getByRole("button", { name: "＋ 增加计费项", exact: true }).click();
    await dialog.getByRole("textbox", { name: `第${index + 1}项计费项`, exact: true }).fill(label);
    await dialog.getByRole("textbox", { name: `第${index + 1}项官方单价`, exact: true }).fill(amount);
    await dialog.getByRole("textbox", { name: `第${index + 1}项适用条件`, exact: true }).fill("标准 API 请求");
  };
  await addPrice(0, "标准文本输入", "2");
  await addPrice(1, "标准文本输出", "8");
  await dialog.getByLabel("原厂官方资料 URL", { exact: true }).fill("https://openai.com/pricing");
  await dialog.locator('input[aria-label="核验日期"]').fill(new Intl.DateTimeFormat("en-CA",{timeZone:"Asia/Shanghai"}).format(new Date()));

  const completeWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/standard-models/${model.id}/manual-completion` && response.request().method() === "PUT");
  await dialog.getByRole("button", { name: "保存并留痕", exact: true }).click();
  const completeResponse = await completeWait;
  const completeText = await completeResponse.text();
  assert.equal(completeResponse.status(), 200, `complete manual save failed with HTTP ${completeResponse.status()}: ${completeText}; UI revision=${model.revision}, expected_revision=${completionPayload?.expected_revision}`);
  await waitForSaveIdle();
  model = JSON.parse(completeText).data;
  assert.equal(model.missing_fields.length, 0);
  assert.equal(model.verify_status, "VERIFIED");
  assert.ok(model.checked_by);
  assert.equal(model.quote_readiness.ready, true);
  assert.equal(String(model.version_no), "2");
  await page.getByText("已保存并记录人工核验", { exact: true }).waitFor();
  await page.screenshot({ path: `${evidence}/maintenance-saved-verified.png`, fullPage: true });

  await openModel();
  await dialog.getByRole("button", { name: "移除第2项计费项", exact: true }).click();
  const incompleteUpdateWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/standard-models/${model.id}/manual-completion` && response.request().method() === "PUT");
  await dialog.getByRole("button", { name: "保存并留痕", exact: true }).click();
  const incompleteUpdate = await incompleteUpdateWait;
  assert.equal(incompleteUpdate.status(), 200, `incomplete update failed with HTTP ${incompleteUpdate.status()}`);
  await waitForSaveIdle();
  model = (await incompleteUpdate.json()).data;
  assert.ok(model.missing_fields.some(field => field.includes("price_items")));
  assert.equal(model.verify_status, "INCOMPLETE");
  assert.equal(model.checked_by, "");
  assert.equal(model.quote_readiness.ready, false);
  assert.equal(String(model.version_no), "3");

  const versionsWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/standard-models/${model.id}/versions` && response.status() === 200);
  await openModel();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "更多模型操作", exact: true }).click();
  await page.getByRole("menuitem", { name: "历史版本", exact: true }).click();
  const versionsResponse = await versionsWait;
  const versions = (await versionsResponse.json()).data;
  assert.equal(versions.total, 3);
  const recordsWait = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/standard-models/${model.id}/operations` && response.status() === 200);
  await page.getByRole("dialog", { name: "模型资料追溯", exact: true }).getByRole("tab", { name: "操作记录", exact: true }).click();
  const recordsResponse = await recordsWait;
  const records = (await recordsResponse.json()).data;
  assert.equal(records.total, 3);
  assert.deepEqual(pageErrors, []);

  const result = {
    modelID: model.id,
    createStatus: createResponse.status(),
    completeSaveStatus: completeResponse.status(),
    incompleteUpdateStatus: incompleteUpdate.status(),
    versions: versions.total,
    operationRecords: records.total,
    completionVerified: true,
    laterIncompleteVersionBlocksQuote: true,
    apiResponses,
    pageErrors,
  };
  await page.screenshot({ path: `${evidence}/maintenance-history-and-records.png`, fullPage: true });
  await writeFile(`${evidence}/maintenance-results.json`, JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  const page = browser?.contexts()[0]?.pages()[0];
  if (page) {
    await page.screenshot({ path: `${evidence}/failure.png`, fullPage: true });
    await writeFile(`${evidence}/failure.txt`, await page.locator("body").innerText());
  }
  throw error;
} finally {
  await browser?.close();
  await server?.close();
}
