import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { chromium } from "playwright";

// Explicit opt-in to the user's one local development environment.
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB, "model_bss");
assert.ok(process.env.BSS_TEST_LOGIN_ID && process.env.BSS_TEST_PASSWORD, "Provide local test credentials through environment variables");
const base = "http://127.0.0.1:5173";
const evidence = ".qa/ab-daily-20261026";
await mkdir(evidence, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
const page = await browser.newPage({ viewport: { width: 1500, height: 1100 } });
const errors = [], checks = [];
page.on("pageerror", error => errors.push(error.message));
try {
  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodels`);
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  const login = page.waitForResponse(r => new URL(r.url()).pathname === "/api/internal/auth/login");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const loginResponse = await login;
  assert.equal(loginResponse.status(), 200);
  const token = (await loginResponse.json()).data.token;
  assert.ok(token);
  const auth = { Authorization: `Bearer ${token}` };
  await page.getByRole("heading", { name: "标准模型库", exact: true }).waitFor();
  checks.push("real internal login and standard-model entry");
  const models = await page.request.get(`${base}/api/internal/standard-models`, { headers: auth });
  assert.equal(models.status(), 200);
  assert.equal((await models.json()).code, 0);
  await page.screenshot({ path: `${evidence}/standard-models.png`, fullPage: true });
  const empty = await page.request.get(`${base}/api/internal/resources/9223372036854775807/test-reports?page=1&size=10`, { headers: auth });
  assert.equal(empty.status(), 200);
  assert.deepEqual((await empty.json()).data, { list: [], total: 0, page: 1, size: 10 });
  checks.push("new B report reader returns a real empty array through authenticated HTTP");
  const unauth = await page.request.get(`${base}/api/internal/resources/9223372036854775807/test-reports`);
  assert.equal(unauth.status(), 401);
  checks.push("report endpoint rejects unauthenticated access");
  await page.goto(`${base}/#/internal/model-plaza`);
  await page.getByRole("heading", { name: "模型广场", exact: true }).waitFor();
  await page.screenshot({ path: `${evidence}/model-plaza.png`, fullPage: true });
  checks.push("model-plaza entry on the daily API");
  assert.deepEqual(errors, []);
  await writeFile(`${evidence}/checks.json`, JSON.stringify({ environment: "model_bss / DB42 / API8080 / web5173", checks, pageErrors: errors }, null, 2));
  console.log(JSON.stringify({ passed: checks.length, checks }));
  await page.request.post(`${base}/api/internal/auth/logout`, { headers: auth });
} finally {
  await browser.close();
}
