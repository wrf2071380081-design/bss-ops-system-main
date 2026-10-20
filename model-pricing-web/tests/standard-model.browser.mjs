import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
// Mount the real A page under a test-only entry. Public routes and authentication
// are not changed, and API fixtures are not a replacement for a real integration test.
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";
const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Page from '/src/views/internal/models/StandardModelList.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__standard-model-test',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});useSessionStore(pinia).restoreSession();router.isReady().then(()=>app.mount('#app'));`;
const plugin = { name: "standard-model-page-test", resolveId(id) { if (id === "/@standard-model-test-entry") return "\0standard-model-test-entry"; }, load(id) { if (id === "\0standard-model-test-entry") return entry; }, configureServer(server) { server.middlewares.use(async (req, res, next) => { if (req.url !== "/__standard-model-test") return next(); res.setHeader("Content-Type", "text/html"); res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;background:#f5f7f8;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@standard-model-test-entry"></script></body></html>')); }); } };
let server, browser;
const errors = [];
let puts = 0, creates = 0, verifies = 0;
const data = { company: "示例模型公司", name: "示例标准模型", official_id: "example-v1", model_type: "TEXT", input_modalities: ["TEXT"], output_modalities: ["TEXT"], capacity: [{ key: "context_window", disclosure: "DISCLOSED", value: "128000 tokens" }], abilities: [{ key: "tool_call", state: "UNDISCLOSED", description: "" }], price_items: [{ item_key: "text_input", label: "文本输入", task_type: "TEXT_INPUT", amount: "0.00000000", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准请求", dimensions: {}, source_url: "https://vendor.example/pricing" }, { item_key: "text_output", label: "文本输出", task_type: "TEXT_OUTPUT", amount: "8.00000000", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准请求", dimensions: {}, source_url: "https://vendor.example/pricing" }], api_access: [{ key: "protocol", disclosure: "DISCLOSED", value: "HTTPS API" }], lifecycle: [{ key: "release", disclosure: "UNDISCLOSED", value: "" }], sources: [{ label: "官方价格", url: "https://vendor.example/pricing", source_kind: "OFFICIAL_PRICING" }], checked_at: "2026-10-20" };
const dto = (fields = data) => ({ ...structuredClone(fields), id: "9007199254740993", maintainer_id: "7", revision: "1", version_no: "1", current_version_no: "1", status: "ACTIVE", verify_status: "UNVERIFIED", checked_by: "", created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", missing_fields: [], quote_readiness: { ready: false, reasons: ["UNVERIFIED"], standard_model_version_no: "1" }, permissions: { can_edit: true, can_verify: true } });
let current = dto();
const original = dto();
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  await mkdir(".qa", { recursive: true });
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  page.setDefaultTimeout(20000);
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/internal/**", async route => {
    const request = route.request(), url = new URL(request.url());
    const send = (value, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ code: status === 200 ? 0 : 10005, message: status === 200 ? "ok" : "记录已更新", data: value, requestId: "browser-fixture" }) });
    if (url.pathname.endsWith("operation-logs")) return send({ list: [{ action: "VERIFY", reason: "人工核验", operator_id: "7", request_id: "trace-test", occurred_at: "2026-10-20T01:00:00Z" }] });
    if (url.pathname.endsWith("/versions")) return send({ list: [{ ...original, permissions: { can_edit: false, can_verify: false } }], page: 1, size: 20, total: 1 });
    if (request.method() === "PUT") {
      puts++;
      const body = request.postDataJSON();
      assert.equal(body.expected_revision, "1");
      assert.equal("maintainer_id" in body, false);
      assert.equal("permissions" in body, false);
      assert.ok(request.headers()["idempotency-key"]);
      if (puts === 1) return send(null, 409);
      const { expected_revision, reason, ...fields } = body;
      current = { ...current, ...fields, revision: "2", version_no: "2", current_version_no: "2" };
      return send(current);
    }
    if (request.method() === "POST" && url.pathname.endsWith("/verify")) {
      verifies++;
      assert.deepEqual(request.postDataJSON(), { expected_revision: "2", checked_at: "2026-10-20", reason: "逐项对照官方资料" });
      current = { ...current, revision: "3", version_no: "3", verify_status: "VERIFIED", permissions: { can_edit: true, can_verify: false }, quote_readiness: { ready: true, reasons: [], standard_model_version_no: "3" } };
      return send(current);
    }
    if (request.method() === "POST") {
      creates++;
      const { reason, ...fields } = request.postDataJSON();
      assert.equal(Object.keys(fields).length, 13);
      assert.equal(fields.name, "资料待补模型");
      assert.deepEqual(fields.price_items, []);
      return send({ ...dto(fields), id: "101", missing_fields: ["price_items", "sources"], verify_status: "INCOMPLETE", permissions: { can_edit: true, can_verify: false } });
    }
    if (/\/standard-models\/[0-9]+$/.test(url.pathname)) return send(current);
    return send({ list: [current], page: 1, size: 20, total: 1 });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__standard-model-test`);
  await page.getByRole("heading", { name: "标准模型库", exact: true }).waitFor();
  await page.getByRole("button", { name: "维护", exact: true }).waitFor();
  assert.equal(await page.getByRole("button", { name: "维护", exact: true }).count(), 1);
  assert.equal(await page.getByRole("button", { name: "示例标准模型", exact: true }).count(), 0);
  await page.getByText("文本输入：0.00000000 USD / 1000000 Token", { exact: true }).waitFor();
  await page.getByRole("button", { name: "维护", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("官方模型名称", { exact: true }).fill("修改后模型");
  await dialog.getByLabel("操作说明", { exact: true }).fill("核对新名称");
  await dialog.getByRole("button", { name: "保存资料", exact: true }).click();
  await dialog.getByText("记录已更新。你的输入已保留，请核对最新资料后重新编辑。", { exact: true }).waitFor();
  assert.equal(await dialog.getByLabel("官方模型名称", { exact: true }).inputValue(), "修改后模型");
  assert.ok(await dialog.getByRole("button", { name: "保存资料", exact: true }).isDisabled());
  await dialog.getByRole("button", { name: "重新读取当前资料", exact: true }).click();
  await page.getByRole("button", { name: "放弃修改", exact: true }).click();
  await page.waitForFunction(input => input.value === "示例标准模型", await dialog.getByLabel("官方模型名称", { exact: true }).elementHandle());
  await dialog.getByLabel("官方模型名称", { exact: true }).fill("已保存模型");
  await dialog.getByLabel("操作说明", { exact: true }).fill("更正名称");
  await dialog.getByRole("button", { name: "保存资料", exact: true }).click();
  await page.getByText("已保存资料", { exact: true }).waitFor();
  await dialog.getByLabel("操作说明", { exact: true }).fill("逐项对照官方资料");
  await dialog.getByRole("button", { name: "人工核验", exact: true }).click();
  await page.getByText("人工核验已记录", { exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelectorAll(".el-message").length === 0);
  await dialog.locator(".el-dialog__body").evaluate(element => { element.scrollTop = 0; });
  await page.screenshot({ path: ".qa/standard-model-maintenance.png", fullPage: true });
  await dialog.getByRole("tab", { name: "历史版本", exact: true }).click();
  await dialog.getByRole("button", { name: "查看", exact: true }).click();
  await dialog.getByRole("heading", { name: "版本 1 · 只读快照", exact: true }).waitFor();
  assert.equal(await dialog.getByRole("button", { name: "保存资料", exact: true }).count(), 0);
  await dialog.getByRole("tab", { name: "操作记录", exact: true }).click();
  await dialog.getByText("trace-test", { exact: true }).waitFor();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  current.permissions = { can_edit: false, can_verify: false };
  await page.getByRole("button", { name: "维护", exact: true }).click();
  await dialog.getByText("当前为只读查看；仅绑定维护人可以修改和核验。", { exact: true }).waitFor();
  assert.ok(await dialog.getByLabel("官方模型名称", { exact: true }).isDisabled());
  assert.equal(await dialog.getByRole("button", { name: "保存资料", exact: true }).count(), 0);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "＋ 新增模型", exact: true }).click();
  await dialog.getByLabel("官方模型名称", { exact: true }).fill("资料待补模型");
  await dialog.getByLabel("操作说明", { exact: true }).fill("先录入待补资料");
  await dialog.getByRole("button", { name: "保存资料", exact: true }).click();
  await page.getByText("已保存，仍有资料待补齐", { exact: true }).waitFor();
  assert.ok(await dialog.getByRole("button", { name: "人工核验", exact: true }).isDisabled());
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll(".el-message").length === 0);
  await page.screenshot({ path: ".qa/standard-model-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.ok(await page.locator(".official-scroll").evaluate(element => element.scrollWidth > element.clientWidth));
  await page.screenshot({ path: ".qa/standard-model-mobile.png", fullPage: true });
  assert.deepEqual(errors, []);
  assert.equal(puts, 2); assert.equal(verifies, 1); assert.equal(creates, 1);
  console.log("PASS: single maintenance entry; exact zero price; conflict/input preservation; save; verify; historical read-only; operation logs; nonowner read-only; incomplete draft; narrow-screen scrolling.");
} finally { await browser?.close(); await server?.close(); }
