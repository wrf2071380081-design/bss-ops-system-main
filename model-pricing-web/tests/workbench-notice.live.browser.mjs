import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { randomUUID } from "node:crypto";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_TEST_NOTICE_ID", "BSS_TEST_NOTICE_TITLE"]) {
  if (!process.env[name]) throw new Error(`工作台公告真实联调缺少环境变量 ${name}。`);
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

const noticeID = process.env.BSS_TEST_NOTICE_ID;
const expectedTitle = process.env.BSS_TEST_NOTICE_TITLE;
const pageErrors = [];
let server;
let browser;
let authHeader = "";

try {
  const port = await availablePort();
  server = await createServer({
    root: process.cwd(),
    server: { host: "127.0.0.1", port, strictPort: true, proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } } },
    logLevel: "error",
  });
  await server.listen();
  const origin = `http://127.0.0.1:${port}`;
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("request", request => {
    if (new URL(request.url()).pathname.startsWith("/api/internal/")) authHeader = request.headers()["authorization"] || authHeader;
  });

  const peopleResponsePromise = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/workbench/people");
  await page.goto(`${origin}/#/internal/login?redirect=%2Finternal%2Fworkbench`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/workbench"));
  await page.getByRole("heading", { name: "工作台", exact: true }).waitFor();

  const row = page.getByRole("row").filter({ hasText: expectedTitle });
  await row.first().waitFor();
  const currentViewResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === `/api/internal/notices/${noticeID}` && !url.searchParams.has("staff_id");
  });
  await row.first().getByRole("button", { name: "详情", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "公告详情" });
  await detail.waitFor();
  const currentViewResponse = await currentViewResponsePromise;
  assert.equal(currentViewResponse.status(), 200);
  const currentEnvelope = await currentViewResponse.json();
  const currentView = currentEnvelope.data?.data ?? currentEnvelope.data;
  assert.equal(currentView.notice.id, noticeID);
  assert.equal(currentView.read_status, "UNREAD", "GET must not mark the notice read");
  assert.equal(currentView.permissions.can_mark_read, true);
  assert.equal(await detail.getByRole("button", { name: "标记已读", exact: true }).count(), 1);

  const markReadResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === `/api/internal/notices/${noticeID}/read` && response.request().method() === "POST";
  });
  await detail.getByRole("button", { name: "标记已读", exact: true }).click();
  const markReadResponse = await markReadResponsePromise;
  assert.equal(markReadResponse.status(), 200);
  const receiptEnvelope = await markReadResponse.json();
  const receipt = receiptEnvelope.data?.data ?? receiptEnvelope.data;
  assert.equal(receipt.notice_id, noticeID);
  assert.equal(receipt.created, true);
  await detail.getByText("已读", { exact: true }).waitFor();
  assert.equal(await detail.getByRole("button", { name: "标记已读", exact: true }).count(), 0);
  const actorID = String(receipt.staff_id);

  await page.getByRole("button", { name: "关闭公告详情", exact: true }).click();
  const readDone = page.locator(".wb-state-options label").filter({ hasText: "已阅" }).locator("input");
  await readDone.check();
  await row.first().waitFor();
  assert.match(await row.first().innerText(), /已阅/);

  const peopleEnvelope = await (await peopleResponsePromise).json();
  const people = peopleEnvelope.data?.list ?? peopleEnvelope.data ?? [];
  const otherPerson = people.find(person => String(person.id) !== actorID);
  assert.ok(otherPerson, "the real internal directory must contain another person for read-only verification");

  const peopleFilter = page.locator(".wb-people-filter");
  await peopleFilter.locator("summary").click();
  const currentPerson = peopleFilter.locator(".wb-person-option").filter({ hasText: "我（当前登录）" }).locator("input");
  if (await currentPerson.isChecked()) await currentPerson.uncheck();
  const otherCheckbox = peopleFilter.locator(".wb-person-option").filter({ hasText: otherPerson.name }).locator("input");
  await otherCheckbox.check();
  await row.first().waitFor();
  const otherViewResponsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === `/api/internal/notices/${noticeID}` && url.searchParams.get("staff_id") === String(otherPerson.id);
  });
  await row.first().getByRole("button", { name: "详情", exact: true }).click();
  const otherViewResponse = await otherViewResponsePromise;
  assert.equal(otherViewResponse.status(), 200);
  const otherEnvelope = await otherViewResponse.json();
  const otherView = otherEnvelope.data?.data ?? otherEnvelope.data;
  assert.equal(otherView.staff_id, String(otherPerson.id));
  assert.equal(otherView.read_status, "UNREAD", "reading the current user's notice must not alter another person's state");
  assert.equal(otherView.permissions.can_mark_read, false);
  const otherDetail = page.getByRole("dialog", { name: "公告详情" });
  await otherDetail.getByText("当前为只读查看，不能代他人标记已读。", { exact: true }).waitFor();
  assert.equal(await otherDetail.getByRole("button", { name: "标记已读", exact: true }).count(), 0);

  const forgedWrite = await page.context().request.post(`${origin}/api/internal/notices/${noticeID}/read`, {
    headers: { Authorization: authHeader, "Idempotency-Key": randomUUID() },
    data: { staff_id: String(otherPerson.id) },
  });
  assert.equal(forgedWrite.status(), 400, "the real API must reject a request that tries to choose another reader");
  const otherStateAfterForgedWrite = await page.context().request.get(`${origin}/api/internal/notices/${noticeID}?staff_id=${otherPerson.id}`, {
    headers: { Authorization: authHeader },
  });
  assert.equal(otherStateAfterForgedWrite.status(), 200);
  const afterEnvelope = await otherStateAfterForgedWrite.json();
  const afterView = afterEnvelope.data?.data ?? afterEnvelope.data;
  assert.equal(afterView.read_status, "UNREAD", "a rejected impersonation attempt must not mark another person's notice read");
  assert.deepEqual(pageErrors, []);
  console.log(JSON.stringify({ noticeID, currentStaffID: actorID, otherStaffID: String(otherPerson.id), currentReadStatus: "READ", otherReadStatus: otherView.read_status, forgedWriteStatus: forgedWrite.status(), pageErrors }));
} finally {
  await browser?.close();
  await server?.close();
}
