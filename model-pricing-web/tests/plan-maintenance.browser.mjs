import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { createServer } from "vite";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false"; process.env.VITE_MOCK_ENABLED = "false"; process.env.VITE_API_BASE_URL = "/api";
const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import 'element-plus/dist/index.css';import Panel from '/src/views/internal/resources/ResourcePlansPanel.vue';import {useSessionStore} from '/src/stores/session.ts';const app=createApp({render:()=>h(Panel,{resourceID:'32',region:'CN'})});app.use(createPinia()).use(ElementPlus);useSessionStore().restoreSession();app.mount('#app');`;
const plugin = { name: "plan-maintenance-test", resolveId(id) { if (id === "/@plan-maintenance-entry") return "\0plan-maintenance-entry"; }, load(id) { if (id === "\0plan-maintenance-entry") return entry; }, configureServer(server) { server.middlewares.use(async (req, res, next) => { if (req.url !== "/__plan-maintenance") return next(); res.setHeader("Content-Type", "text/html"); res.end(await server.transformIndexHtml(req.url, '<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module" src="/@plan-maintenance-entry"></script></body></html>')); }); } };
const original = { id: "81", resource_id: "32", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "7", creator_name: "采购甲", revision: "1", expected_resource_revision: "1", current_resource_revision: "2", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "维护窗口", is_initiator: true, read_only: true, precondition_matches: false, blocked_reasons: ["RESOURCE_CHANGED"], permissions: { can_revise: true, can_cancel: true, can_execute: false } };
let server, browser, current = structuredClone(original), failRevise = true, conflictCancel = true, listReads = 0;
const writes = [], errors = [];
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" }); await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } }); page.setDefaultTimeout(8000); page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/internal/plans**", route => {
    const req = route.request(), url = new URL(req.url());
    const send = (data, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ code: status === 200 ? 0 : 10000, message: status === 409 ? "计划已变化" : status === 503 ? "暂时不可用" : "ok", data, requestId: "plan-maintenance-fixture" }) });
    if (req.method() !== "GET") {
      const body = req.postDataJSON(); writes.push({ method: req.method(), path: url.pathname, body, key: req.headers()["idempotency-key"] });
      if (req.method() === "PUT") {
        if (failRevise) { failRevise = false; return send(null, 503); }
        current = { ...current, reason: body.reason, due_at: body.due_at, revision: "2" }; return send(current);
      }
      if (conflictCancel) return send(null, 409);
      current = { ...current, status: "CANCELED", revision: "3", permissions: { can_revise: false, can_cancel: false, can_execute: false } }; return send(current);
    }
    if (url.pathname.endsWith("/81")) return send(current);
    listReads++; return send({ list: [current], total: 1, page: 1, size: 10 });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__plan-maintenance`);
  const panel = page.locator('.resource-plans'), detail = page.locator('.supply-plan-dialog');
  await panel.getByRole('button', { name: '详情', exact: true }).click();
  await detail.getByRole('button', { name: '修改计划', exact: true }).click();
  const form = page.locator('.maintain-state-plan');
  await form.getByLabel('计划原因', { exact: true }).fill('');
  await form.getByRole('button', { name: '保存修改', exact: true }).click();
  await form.getByText('请填写原因。', { exact: true }).waitFor(); assert.equal(writes.length, 0);
  await form.getByLabel('计划原因', { exact: true }).fill('调整维护窗口');
  await form.locator('input[aria-label="预计时间"]').fill('2027-01-02T10:30');
  await form.getByRole('button', { name: '保存修改', exact: true }).click();
  await form.getByText('服务暂时不可用，请稍后重试。', { exact: true }).waitFor();
  assert.equal(await form.getByLabel('计划原因', { exact: true }).inputValue(), '调整维护窗口');
  await form.getByRole('button', { name: '保存修改', exact: true }).click();
  await detail.getByText('调整维护窗口', { exact: true }).waitFor();
  assert.ok(writes[0].key); assert.equal(writes[0].key, writes[1].key);
  assert.equal(writes[1].method, 'PUT'); assert.equal(writes[1].path, '/api/internal/plans/81');
  assert.deepEqual(writes[1].body, { expected_revision: '1', reason: '调整维护窗口', due_at: new Date('2027-01-02T10:30').toISOString() });
  assert.equal(current.expected_resource_revision, '1'); assert.ok(listReads >= 2);
  await detail.getByRole('button', { name: '取消计划', exact: true }).click();
  await form.getByLabel('取消原因', { exact: true }).fill('无需维护');
  await form.getByRole('button', { name: '确认取消计划', exact: true }).click();
  await form.getByText('计划已变化', { exact: true }).waitFor();
  assert.equal(await form.getByRole('button', { name: '确认取消计划', exact: true }).isDisabled(), true);
  assert.equal(await form.getByLabel('取消原因', { exact: true }).inputValue(), '无需维护');
  await mkdir('.qa', { recursive: true }); await page.screenshot({ path: '.qa/plan-maintenance-conflict.png', fullPage: true });
  await form.getByRole('button', { name: '返回计划详情', exact: true }).click();
  conflictCancel = false;
  await detail.getByRole('button', { name: '取消计划', exact: true }).click();
  await form.getByLabel('取消原因', { exact: true }).fill('无需维护');
  await form.getByRole('button', { name: '确认取消计划', exact: true }).click();
  await detail.getByText('已取消', { exact: true }).waitFor();
  assert.equal(await detail.getByRole('button', { name: '修改计划', exact: true }).count(), 0);
  assert.equal(await detail.getByRole('button', { name: '取消计划', exact: true }).count(), 0);
  assert.deepEqual(writes.at(-1).body, { expected_revision: '2', reason: '无需维护' });
  assert.equal(writes.at(-1).method, 'POST'); assert.equal(writes.at(-1).path, '/api/internal/plans/81/cancel');
  assert.equal(current.reason, '调整维护窗口'); assert.equal(current.executed_at, null);
  // Same real component, non-initiator projection must expose no actions.
  current = { ...original, is_initiator: false, permissions: { can_revise: false, can_cancel: false, can_execute: false } };
  await page.reload(); await panel.getByRole('button', { name: '详情', exact: true }).click();
  await detail.getByText('维护窗口', { exact: true }).waitFor();
  assert.equal(await detail.getByRole('button', { name: '修改计划', exact: true }).count(), 0);
  assert.equal(await detail.getByRole('button', { name: '取消计划', exact: true }).count(), 0);
  assert.deepEqual(errors, []);
  console.log('PASS: real Vue plan revise/cancel; empty reason; preserved fields and same-key 503 retry; original resource basis; list/detail refresh; 409 freeze; terminal and non-initiator actions hidden. Mock HTTP only.');
} finally { await browser?.close(); await server?.close(); }
