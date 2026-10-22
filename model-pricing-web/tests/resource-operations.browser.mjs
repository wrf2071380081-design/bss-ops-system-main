import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";
const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Drawer from '/src/views/internal/resources/ResourceDetailDrawer.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const app=createApp({render:()=>h(Drawer,{id:'9007199254740993',region:'CN',onClose:()=>{},onChanged:()=>{}})});app.use(pinia).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'7',name:'办理人甲',roleLabel:'内部用户'};session.portal='internal';session.token='test-token';app.mount('#app');`;
const plugin = { name: "resource-operations-test", resolveId(id) { if (id === "/@resource-operations-entry") return "\0resource-operations-entry"; }, load(id) { if (id === "\0resource-operations-entry") return entry; }, configureServer(server) { server.middlewares.use(async (req, res, next) => { if (req.url !== "/__resource-operations-test") return next(); res.setHeader("Content-Type", "text/html"); res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@resource-operations-entry"></script></body></html>')); }); } };
const resource = { id: "9007199254740993", supplier_id: "21", supplier_name: "供应商甲", standard_model_id: "11", model_name: "示例模型", official_id: "example-v1", company: "示例公司", model_type: "TEXT", region: "CN", supplier_model_id: "vendor-model-v1", coefficient: "0.6800", buyer_id: "7", state: "PENDING_ACCESS", ever_admitted: false, revision: "2", formal_config_revision: "0", test: { report_id: "51", status: "COMPLETED", connectivity_passed: true, pressure_passed: true, valid: true, cache_stats: { status: "NOT_PROVIDED", report_id: "51" }, metrics: { connection_ms: "0.00", rps: "23.0", tps: "12.0" } }, official_price_items: [], quote_readiness: { eligible: false, blocked_reasons: ["FORMAL_CONFIG_UNAVAILABLE"], resource_revision: "2", model_version_no: "1", formal_config_revision: "0", test_evidence_ref: "51" }, permissions: { can_edit: false }, created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z" };
const config = { id: "91", supplier_id: "21", region: "CN", scope: "生产接口", base_url: "https://api.vendor.example/v1", has_api_key: true, api_key_mask: "********", status: "ACTIVE", revision: "4", created_by: "7", updated_by: "7", created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", confirmed_by: "7", confirmed_at: "2026-10-20T01:00:00Z", confirmed_revision: "4" };
let server, browser, configAttempts = 0, resourceReads = 0;
const writes = [], errors = [];
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
  page.setDefaultTimeout(20000); page.on("pageerror", error => errors.push(error.message)); page.on("console", message => { if (message.type() === "error" && !message.text().includes("status of 409 (Conflict)")) errors.push(message.text()); });
  await page.route("**/api/internal/suppliers/**/formal-configs", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", requestId: "config-list", data: [config] }) }));
  await page.route("**/api/internal/resources/**", async route => {
    const request = route.request(), url = new URL(request.url());
    const send = (data, status = 200, message = "ok") => route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ code: status === 200 ? 0 : status === 409 ? 10005 : 10000, message, data, requestId: "resource-operation-fixture" }) });
    if (request.method() === "GET" && /\/resources\/[0-9]+$/.test(url.pathname)) { resourceReads++; return send(resource); }
    if (request.method() === "PUT" && url.pathname.endsWith("/formal-config")) {
      const body = request.postDataJSON(); writes.push({ kind: "bind", body, key: request.headers()["idempotency-key"] });
      assert.equal("staff_id" in body, false); assert.equal("assignee_id" in body, false);
      if (++configAttempts === 1) return send(null, 409, "当前办理节点或资源版本已变化");
      Object.assign(resource, { revision: "3", formal_config_id: "91", formal_config_revision: "4", updated_at: "2026-10-21T01:00:00Z" });
      Object.assign(resource.quote_readiness, { resource_revision: "3", formal_config_revision: "4" });
      return send({ resource_id: resource.id, formal_config_id: "91", formal_config_revision: "4", resource_revision: "3", changed: true, updated_at: resource.updated_at });
    }
    if (request.method() === "POST" && url.pathname.endsWith("/admit")) {
      const body = request.postDataJSON(); writes.push({ kind: "admit", body, key: request.headers()["idempotency-key"] });
      assert.equal("staff_id" in body, false); assert.equal("assignee_id" in body, false);
      assert.deepEqual(body, { expected_revision: "3", external_confirmed: true, completion_note: "已在国内 B 端完成该模型实际配置", reason: "完成资源接入" });
      Object.assign(resource, { state: "ADMITTED", ever_admitted: true, revision: "4", updated_at: "2026-10-21T02:00:00Z" });
      Object.assign(resource.quote_readiness, { resource_revision: "4", formal_config_revision: "4" });
      return send({ resource_id: resource.id, state: "ADMITTED", ever_admitted: true, revision: "4", updated_at: resource.updated_at });
    }
    return send(null, 404, "没有对应资源接口");
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__resource-operations-test`);
  try { await page.getByRole("heading", { name: "示例模型", exact: true }).waitFor(); }
  catch (error) { console.error("resource detail body:", await page.locator("body").innerText(), "errors:", errors); throw error; }
  await page.getByRole("tab", { name: "接入与配置", exact: true }).click();
  await page.getByText("服务端将按当前流程节点校验办理人；此页面不指定办理人。", { exact: true }).waitFor();
  const panel = page.locator(".resource-operations");
  await panel.getByLabel("操作原因", { exact: true }).fill("关联生产配置");
  await panel.getByRole("button", { name: "关联正式配置", exact: true }).click();
  await panel.getByText("当前办理节点或资源版本已变化", { exact: true }).waitFor();
  assert.equal(await panel.getByLabel("操作原因", { exact: true }).inputValue(), "关联生产配置");
  await panel.getByRole("button", { name: "关联正式配置", exact: true }).click();
  await panel.getByText("正式配置已关联", { exact: true }).waitFor();
  assert.deepEqual(writes.slice(0, 2).map(write => write.body), [
    { expected_revision: "2", formal_config_id: "91", expected_config_revision: "4", reason: "关联生产配置" },
    { expected_revision: "2", formal_config_id: "91", expected_config_revision: "4", reason: "关联生产配置" },
  ]);
  assert.notEqual(writes[0]?.key, writes[1]?.key);
  assert.equal(await page.getByText("********", { exact: true }).count(), 0);
  await panel.locator("label.el-checkbox").click();
  await panel.getByLabel("完成依据", { exact: true }).fill("已在国内 B 端完成该模型实际配置");
  await panel.getByLabel("登记原因", { exact: true }).fill("完成资源接入");
  await panel.getByRole("button", { name: "登记接入", exact: true }).click();
  await page.getByText("已接入", { exact: true }).waitFor();
  assert.equal(writes.at(-1)?.kind, "admit");
  assert.equal(resourceReads, 3);
  assert.deepEqual(errors, []);
  await mkdir(".qa", { recursive: true });
  await page.screenshot({ path: ".qa/resource-operations.png", fullPage: true, animations: "disabled" });
  console.log("PASS: resource-specific test gating; safe formal-config selection; 409 input retention; idempotency key refresh after rejection; explicit external completion; server-owned assignee; resource refresh after each write.");
} finally { await browser?.close(); await server?.close(); }
