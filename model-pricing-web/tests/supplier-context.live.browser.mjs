import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN"]) {
  if (!process.env[name]) throw new Error(`供应商列表上下文真实联调缺少环境变量 ${name}。`);
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
const listPath = "/api/internal/supply/suppliers";
const listQuery = "page=2&size=10&contract=未登记&sort_by=name&sort_order=desc";

try {
  const port = await availablePort();
  server = await createServer({
    server: { host: "127.0.0.1", port, strictPort: true,
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

  const workspace = `/internal/suppliers?${listQuery}`;
  await page.goto(`${origin}/#/internal/login?redirect=${encodeURIComponent(workspace)}`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  const firstListPromise = page.waitForResponse(response => new URL(response.url()).pathname === listPath && new URL(response.url()).searchParams.get("page") === "2");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.includes("/internal/suppliers?"));
  const firstList = await firstListPromise;
  assert.equal(firstList.status(), 200);
  const firstURL = new URL(firstList.url());
  assert.equal(firstURL.searchParams.get("size"), "10");
  assert.equal(firstURL.searchParams.get("contract_status"), "未登记");
  assert.equal(firstURL.searchParams.get("sort_by"), "name");
  assert.equal(firstURL.searchParams.get("sort_order"), "desc");
  const firstBody = await firstList.json();
  assert.equal(firstBody.data.page, 2);
  assert.ok(firstBody.data.list.length > 0, "page two must contain isolated supplier rows");
  const selected = firstBody.data.list[0];
  const selectedRow = page.locator(".supplier-workspace .el-table__body tr").filter({ hasText: selected.name });
  await selectedRow.getByRole("button", { name: "详情", exact: true }).click();
  await page.getByRole("heading", { name: selected.name, exact: true }).waitFor();
  assert.match(page.url(), /supplier_page=2/);
  assert.match(page.url(), /supplier_size=10/);
  assert.match(page.url(), /supplier_contract=%E6%9C%AA%E7%99%BB%E8%AE%B0|supplier_contract=未登记/);
  const returnedListPromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === listPath && url.searchParams.get("page") === "2" && url.searchParams.get("size") === "10";
  });
  await page.getByRole("button", { name: "← 返回供应商列表", exact: true }).click();
  await page.waitForURL(url => url.hash.includes("/internal/suppliers?") && url.hash.includes("page=2"));
  const returnedList = await returnedListPromise;
  assert.equal(returnedList.status(), 200);
  const returnedURL = new URL(returnedList.url());
  assert.equal(returnedURL.searchParams.get("contract_status"), "未登记");
  assert.equal(returnedURL.searchParams.get("sort_by"), "name");
  assert.equal(returnedURL.searchParams.get("sort_order"), "desc");
  const returnedBody = await returnedList.json();
  assert.deepEqual(returnedBody.data.list.map(row => row.id), firstBody.data.list.map(row => row.id), "return navigation must preserve the exact page and sort order");
  assert.deepEqual(pageErrors, []);
  console.log(`PASS: authenticated Playwright opened supplier ${selected.id} from filtered/sorted page 2 and restored the same page, size, filter, and order via the real isolated API.`);
} finally {
  await browser?.close();
  await server?.close();
}
