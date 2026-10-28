import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer as createPortServer } from "node:net";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_TEST_STAFF_ID", "BSS_TEST_RESOURCE_ID", "BSS_TEST_QA_MARKER"]) {
  if (!process.env[name]) throw new Error(`工作台计划真实联调缺少环境变量 ${name}。`);
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

function payload(envelope) {
  return envelope?.data?.data ?? envelope?.data ?? envelope;
}

const apiOrigin = process.env.BSS_TEST_API_ORIGIN.replace(/\/$/, "");
const staffID = process.env.BSS_TEST_STAFF_ID;
const resourceID = process.env.BSS_TEST_RESOURCE_ID;
const marker = process.env.BSS_TEST_QA_MARKER;
const dueAt = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
const pageErrors = [];
const inboxRows = [];
const planReads = new Map();
let server;
let browser;
let authHeader = "";
let onlinePlanID = "";
let pricePlanID = "";

try {
  const port = await availablePort();
  server = await createServer({
    root: process.cwd(),
    server: { host: "127.0.0.1", port, strictPort: true, proxy: { "/api": { target: apiOrigin, changeOrigin: true } } },
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
  page.on("response", async response => {
    const url = new URL(response.url());
    if (url.pathname === "/api/internal/workbench/inbox" && response.status() === 200) {
      try {
        const body = payload(await response.json());
        if (Array.isArray(body?.list)) inboxRows.push(...body.list);
      } catch { /* response assertions below report a missing projection */ }
    }
    const planMatch = url.pathname.match(/^\/api\/internal\/plans\/(\d+)$/);
    if (planMatch && response.status() === 200) {
      try { planReads.set(planMatch[1], payload(await response.json())); } catch { /* asserted after navigation */ }
    }
  });

  await page.goto(`${origin}/#/internal/login?redirect=%2Finternal%2Fworkbench`, { waitUntil: "domcontentloaded" });
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(url => url.hash.startsWith("#/internal/workbench"));
  await page.getByRole("heading", { name: "工作台", exact: true }).waitFor();
  assert.ok(authHeader.startsWith("Bearer "), "the real login must attach an authenticated token to API requests");

  async function postPlan(path, body, key) {
    const response = await page.request.post(`${apiOrigin}/api/internal/plans${path}`, {
      headers: { Authorization: authHeader, "Idempotency-Key": `${marker}-${key}`, "X-Request-ID": `${marker}-${key}` },
      data: body,
    });
    assert.equal(response.status(), 200, `${path || "/"} plan creation must pass through the real API`);
    return payload(await response.json());
  }

  const online = await postPlan("", {
    resource_id: resourceID,
    expected_resource_revision: "1",
    kind: "ONLINE",
    due_at: dueAt,
    reason: `${marker}-ONLINE-PLAN`,
  }, "online");
  onlinePlanID = String(online.id);
  assert.match(onlinePlanID, /^[1-9]\d*$/);
  assert.equal(online.created_by, staffID);
  assert.equal(online.status, "PENDING");

  const price = await postPlan("/price-change", {
    resource_id: resourceID,
    supplier_model_id: `QA-NAV-${marker}`,
    expected_resource_revision: "1",
    after_coefficient: "0.5100",
    assignee_id: staffID,
    due_at: dueAt,
    reason: `${marker}-PRICE-PLAN`,
  }, "price");
  pricePlanID = String(price.id);
  assert.match(pricePlanID, /^[1-9]\d*$/);
  assert.equal(price.created_by, staffID);
  assert.equal(price.assignee_id, staffID);
  assert.equal(price.status, "PENDING");

  await page.getByRole("button", { name: "刷新", exact: true }).click();
  const onlineRow = page.getByRole("row").filter({ hasText: "上架计划" });
  const priceRow = page.getByRole("row").filter({ hasText: "内部改价计划" });
  await onlineRow.getByRole("button", { name: "详情", exact: true }).waitFor();
  await priceRow.getByRole("button", { name: "详情", exact: true }).waitFor();

  const ownPlanRows = inboxRows.filter(item => item.biz_type === "PLAN" && String(item.biz_id) === resourceID);
  for (const planID of [onlinePlanID, pricePlanID]) {
    const item = ownPlanRows.find(row => String(row.plan_id) === planID);
    assert.ok(item, `workbench must preserve plan_id=${planID} when multiple plans share one resource`);
    assert.equal(item.deeplink, `/internal/model-plaza/${resourceID}?region=CN&tab=plans&plan_id=${planID}`);
  }

  async function openAndVerify(row, planID, expectedReason) {
    const summaryPromise = page.waitForResponse(response => new URL(response.url()).pathname === `/api/internal/plans/${planID}`);
    await row.getByRole("button", { name: "详情", exact: true }).click();
    await page.waitForURL(url => url.hash.includes(`/internal/model-plaza/${resourceID}`) && url.hash.includes(`plan_id=${planID}`));
    const current = new URL(`http://local/${page.url().split("#")[1]}`);
    assert.equal(current.searchParams.get("region"), "CN");
    assert.equal(current.searchParams.get("tab"), "plans");
    assert.equal(current.searchParams.get("plan_id"), planID);
    const summaryResponse = await summaryPromise;
    assert.equal(summaryResponse.status(), 200, "the linked plan must load from the real Go API");
    await page.getByRole("dialog", { name: "资源计划详情" }).getByText(expectedReason, { exact: true }).waitFor();
    const plan = planReads.get(planID);
    assert.equal(plan?.id, planID);
    assert.equal(plan?.resource_id, resourceID);
    assert.equal(plan?.reason, expectedReason);

    const resourceResponse = await page.request.get(`${apiOrigin}/api/internal/resources/${resourceID}?region=CN`, { headers: { Authorization: authHeader } });
    assert.equal(resourceResponse.status(), 200, "the route must load the real resource context");
    const resource = payload(await resourceResponse.json());
    assert.equal(resource.state, "OFFLINE");
    assert.equal(resource.quote_readiness?.eligible, false, "QA navigation data must not unlock quoting");
    assert.equal(resource.test?.status, "NOT_TESTED", "the navigation fixture must not fabricate B test evidence");
  }

  await openAndVerify(onlineRow, onlinePlanID, `${marker}-ONLINE-PLAN`);
  await page.goto(`${origin}/#/internal/workbench`);
  await page.getByRole("heading", { name: "工作台", exact: true }).waitFor();
  const refreshedPriceRow = page.getByRole("row").filter({ hasText: "内部改价计划" });
  await refreshedPriceRow.getByRole("button", { name: "详情", exact: true }).waitFor();
  await openAndVerify(refreshedPriceRow, pricePlanID, `${marker}-PRICE-PLAN`);

  const wrongRegionResponse = await page.request.get(`${apiOrigin}/api/internal/resources/${resourceID}?region=OVERSEAS`, { headers: { Authorization: authHeader } });
  assert.notEqual(wrongRegionResponse.status(), 200, "the API must reject a resource requested under the wrong region");
  await page.goto(`${origin}/#/internal/model-plaza/${resourceID}?region=OVERSEAS&tab=plans&plan_id=${onlinePlanID}`);
  await page.getByRole("button", { name: "重新加载资源详情" }).waitFor();
  assert.equal(await page.getByRole("dialog", { name: "资源计划详情" }).count(), 0, "a mismatched region must not open a plan detail dialog");
  assert.equal(await page.getByRole("button", { name: "登记实际完成" }).count(), 0, "a mismatched region must not expose plan actions");
  assert.deepEqual(pageErrors, []);

  console.log(JSON.stringify({
    qaFixture: marker,
    resourceID,
    planIDs: [onlinePlanID, pricePlanID],
    workbenchDeeplinksPreserved: true,
    correctPlanOpenedForEachRow: true,
    mismatchedRegionRejected: true,
    resourceState: "OFFLINE",
    quoteEligible: false,
    testEvidence: "NOT_TESTED",
    pageErrors,
  }));
} finally {
  if (authHeader && browser) {
    try {
      await browser.contexts()[0].request.post(`${apiOrigin}/api/internal/auth/logout`, { headers: { Authorization: authHeader } });
    } catch { /* a failed test must not hide its original assertion */ }
  }
  await browser?.close();
  await server?.close();
}
