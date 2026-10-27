import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright";

assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB, "model_bss");
assert.ok(process.env.BSS_TEST_LOGIN_ID && process.env.BSS_TEST_PASSWORD && process.env.BSS_REPORT_FIXTURE);
const m = JSON.parse(await readFile(process.env.BSS_REPORT_FIXTURE, "utf8"));
assert.ok(m.run.startsWith("QA-AB-"));
const base = "http://127.0.0.1:5173", resource = m.resource_ids[0];
const evidence = ".qa/ab-report-live-20261026";
await mkdir(evidence, { recursive: true });
const browser = await chromium.launch({ channel: "msedge", headless: true });
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 } });
const pageErrors = [], checks = [];
page.on("pageerror", error => pageErrors.push(error.message));
let auth;
try {
  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodel-plaza`);
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  const loggedIn = page.waitForResponse(r => new URL(r.url()).pathname === "/api/internal/auth/login");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const response = await loggedIn; assert.equal(response.status(), 200);
  const token = (await response.json()).data.token; assert.ok(token);
  auth = { Authorization: `Bearer ${token}` };
  const get = async path => {
    const response = await page.request.get(`${base}/api/internal/${path}`, { headers: auth });
    assert.equal(response.status(), 200, `${path} HTTP ${response.status()}`);
    const body = await response.json(); assert.equal(body.code, 0); return body.data;
  };
  const first = await get(`resources/${resource}/test-reports?page=1&size=10`);
  const second = await get(`resources/${resource}/test-reports?page=2&size=10`);
  assert.equal(first.total, 12); assert.equal(first.list.length, 10); assert.equal(second.list.length, 2);
  assert.equal(new Set([...first.list, ...second.list].map(r => r.id)).size, 12);
  const running = second.list;
  assert.ok(running.every(r => r.status === "RUNNING" && r.tested_at === null && r.finished_at === null));
  checks.push("native B report history pagination and pending null dates");
  const byJob = id => first.list.find(row => row.id === id);
  assert.equal(byJob(m.job_ids[4]).cache_stats.hit_percent, "0.00");
  assert.equal(byJob(m.job_ids[5]).cache_stats.hit_percent, "37.25");
  assert.equal(byJob(m.job_ids[6]).cache_stats.status, "NOT_PROVIDED");
  assert.equal(byJob(m.job_ids[7]).cache_stats.hit_tokens, "0");
  assert.equal(byJob(m.job_ids[7]).cache_stats.input_tokens, "100");
  assert.equal(byJob(m.job_ids[5]).metrics.rps, "0.00");
  assert.equal(byJob(m.job_ids[5]).metrics.connection_ms, null);
  checks.push("real B storage preserves 0%, ratio-only, unavailable, token counters and zero-vs-missing metrics");
  const latestID = m.job_ids.at(-1);
  const detail = await get(`resources/${resource}/test-reports/${latestID}`);
  assert.equal(detail.test_report_id, m.report_ids.at(-1), "separate job/report namespaces");
  assert.equal(detail.test_job_id, latestID);
  assert.notEqual(detail.test_report_id, detail.test_job_id);
  assert.equal(detail.target_matches, true, "B CreateJob must retain the supplied fingerprint");
  assert.equal(detail.files.length, 1);
  assert.equal(detail.files[0].id, detail.test_report_id);
  const filePath = `resources/${resource}/test-reports/${latestID}/files/${detail.files[0].id}`;
  const download = await page.request.get(`${base}/api/internal/${filePath}`, { headers: auth });
  assert.equal(download.status(), 200);
  assert.match(download.headers()["content-disposition"], /attachment/);
  assert.match(download.headers()["cache-control"], /no-store/);
  const bytes = await download.body();
  const decoded = JSON.parse(bytes.toString()); assert.equal(decoded.fixture, m.run); assert.equal(decoded.item, 11);
  checks.push("job/report/file IDs stay distinct and controlled file bytes download");
  const wrongResource = await page.request.get(`${base}/api/internal/resources/${m.resource_ids[1]}/test-reports/${latestID}`, { headers: auth });
  assert.equal(wrongResource.status(), 404);
  const wrongFile = await page.request.get(`${base}/api/internal/resources/${resource}/test-reports/${latestID}/files/9223372036854775807`, { headers: auth });
  assert.equal(wrongFile.status(), 403);
  const noAuth = await page.request.get(`${base}/api/internal/${filePath}`); assert.equal(noAuth.status(), 401);
  checks.push("cross-resource, wrong file and unauthenticated downloads rejected");
  const toggle = action => execFileSync("docker", ["exec", "-w", "/tmp/bss-current", "-e", "BSS_LOCAL_INTEGRATION_DB=model_bss", "-e", "MODEL_BSS_CONFIG=/app/config.yaml", "model_bss_api_latest", "go", "run", "./tests/ab-daily-reports", action, "/tmp/bss-current/.qa/ab-reports/fixture.json"], { stdio: "pipe" });
  toggle("hide-file");
  try {
    const missing = await page.request.get(`${base}/api/internal/${filePath}`, { headers: auth });
    assert.equal(missing.status(), 404);
  } finally { toggle("restore-file"); }
  checks.push("missing local report returns 404 and restores without affecting other files");
  for (const [suffix, method, data] of [
    ["formal-config", "put", { expected_revision: "1", formal_config_id: "9223372036854775807", expected_config_revision: "1", reason: m.run }],
    ["admit", "post", { expected_revision: "1", external_confirmed: true, completion_note: m.run, reason: m.run }],
  ]) {
    const response = await page.request[method](`${base}/api/internal/resources/${resource}/${suffix}`, { headers: { ...auth, "Idempotency-Key": randomUUID() }, data });
    assert.equal(response.status(), 503, "missing handling provider must not enable binding/admission");
  }
  const current = await get(`resources/${resource}`);
  assert.equal(current.revision, "1"); assert.equal(current.state, "PENDING_TEST"); assert.equal(current.quote_readiness.eligible, false);
  checks.push("binding/admission unavailable without real handling provider; resource remains unchanged");
  await page.goto(`${base}/#/internal/model-detail/${m.model_id}`);
  const source = page.locator("tr").filter({ hasText: `${m.run}-0` });
  await source.getByRole("button", { name: "查看报告", exact: true }).click();
  const panel = page.locator(".resource-reports");
  await panel.getByRole("button", { name: "查看本次测试", exact: true }).first().waitFor();
  const pageTwo = page.waitForResponse(r => r.url().includes("test-reports") && new URL(r.url()).searchParams.get("page") === "2");
  await panel.locator(".el-pager li.number").filter({ hasText: /^2$/ }).click();
  await pageTwo;
  assert.equal(await panel.locator("tbody tr").count(), 2);
  await panel.locator(".el-pagination").getByText("1", { exact: true }).click({ timeout: 5000 });
  await panel.locator("tbody tr").nth(9).waitFor();
  const ratioIndex = first.list.findIndex(r => r.id === m.job_ids[5]); assert.ok(ratioIndex >= 0);
  await panel.locator("tbody tr").nth(ratioIndex).getByRole("button", { name: "查看本次测试", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "测试报告详情", exact: true });
  await dialog.getByText("37.25%", { exact: true }).waitFor();
  assert.equal(await dialog.getByText(/命中输入 Token \/ 总输入 Token/).count(), 0, "ratio-only report must not show an empty token pair");
  await dialog.getByRole("button", { name: "关闭报告", exact: true }).click();
  const zeroIndex = first.list.findIndex(r => r.id === m.job_ids[4]);
  await panel.locator("tbody tr").nth(zeroIndex).getByRole("button", { name: "查看本次测试", exact: true }).click();
  await dialog.getByText("0.00%", { exact: true }).waitFor();
  assert.equal(await dialog.getByText(/命中输入 Token \/ 总输入 Token/).count(), 0);
  await dialog.getByRole("button", { name: "关闭报告", exact: true }).click();
  await panel.getByRole("button", { name: "查看本次测试", exact: true }).first().click();
  const downloaded = page.waitForEvent("download");
  await page.getByRole("dialog", { name: "测试报告详情", exact: true }).getByRole("button", { name: "下载", exact: true }).click();
  const browserFile = await downloaded;
  await browserFile.saveAs(`${evidence}/download.json`);
  assert.equal(createHash("sha256").update(await readFile(`${evidence}/download.json`)).digest("hex"), createHash("sha256").update(bytes).digest("hex"));
  await page.screenshot({ path: `${evidence}/report-details.png`, fullPage: true });
  checks.push("real model-source page report pagination, ratio-only rendering and browser file download");
  assert.deepEqual(pageErrors, []);
  await writeFile(`${evidence}/checks.json`, JSON.stringify({ fixture: m.run, checks, pageErrors }, null, 2));
  console.log(JSON.stringify({ passed: checks.length, checks }));
} catch (error) {
  const pager = page.locator(".resource-reports .el-pagination");
  if (await pager.count()) console.log("Report pager DOM:", await pager.evaluate(el => el.outerHTML));
  await page.screenshot({ path: `${evidence}/failure.png`, fullPage: true });
  await writeFile(`${evidence}/partial-checks.json`, JSON.stringify({ checks, error: error.message }, null, 2));
  throw error;
} finally {
  if (auth) await page.request.post(`${base}/api/internal/auth/logout`, { headers: auth });
  await browser.close();
}
