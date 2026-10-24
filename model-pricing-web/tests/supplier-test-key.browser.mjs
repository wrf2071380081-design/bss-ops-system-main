import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Page from '/src/views/supplier/SupplierSubmissionNew.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__supplier-test-key',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'71',name:'供应商甲',roleLabel:'供应商'};session.portal='supplier';router.isReady().then(()=>app.mount('#app'));`;
const plugin = {
  name: "supplier-test-key-page-test",
  resolveId(id) { return id === "/@supplier-test-key-entry" ? "\0supplier-test-key-entry" : null; },
  load(id) { return id === "\0supplier-test-key-entry" ? entry : null; },
  configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (new URL(req.url, "http://127.0.0.1").pathname !== "/__supplier-test-key") return next();
      res.setHeader("Content-Type", "text/html");
      res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@supplier-test-key-entry"></script></body></html>'));
    });
  },
};

let server, browser;
const writes = [];
const tests = [];
const row = {
  id: "72", interface_model_id: "vendor-chat-v1", standard_model_id: "70", standard_model_name: "示例标准模型", official_id: "official-70", coefficient: "0.8", test_base_url: "https://supplier.example/v1", test_api_key_configured: true, row_status: "DRAFT", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false,
};
const submission = (reason, rows = [row]) => ({ id: "71", kind: "MODEL_NEW", status: "DRAFT", revision: String(writes.length + 1), reason, rows: structuredClone(rows), permissions: { can_edit: true, can_submit: false, can_withdraw: false }, created_at: "2026-10-23T00:00:00Z", updated_at: "2026-10-23T00:00:00Z" });

try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => { throw error; });
  await page.route("**/api/supplier/submissions**", async route => {
    const request = route.request();
    const url = new URL(request.url());
    const send = data => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data, requestId: "supplier-key-test" }) });
    if (url.pathname.endsWith("/model-options")) return send({ list: [{ id: "model-71", name: "示例标准模型", official_id: "official-71", company: "示例公司" }] });
    if (request.method() === "GET" && url.pathname === "/api/supplier/submissions/71") return send(submission("已有草稿", [{ ...row, test_api_key_configured: false }]));
    if (request.method() === "POST" && url.pathname === "/api/supplier/submissions") {
      writes.push({ method: "POST", body: request.postDataJSON() });
      const submitted = request.postDataJSON();
      return send(submission(submitted.reason, submitted.rows.map(item => ({ ...row, ...item, test_api_key_configured: Boolean(item.test_api_key) }))));
    }
    if (request.method() === "PUT" && url.pathname === "/api/supplier/submissions/71") {
      writes.push({ method: "PUT", body: request.postDataJSON() });
      return send(submission(request.postDataJSON().reason));
    }
    if (request.method() === "POST" && url.pathname.endsWith("/rows/72/test")) {
      tests.push(request.postDataJSON());
      return send({ job_id: "73", category: "CONNECTIVITY", status: "QUEUED", tested: true, passed: false });
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ code: 404, message: "unexpected route", data: null, requestId: "supplier-key-test" }) });
  });

  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__supplier-test-key?id=71`);
  await page.waitForResponse(response => new URL(response.url()).pathname === "/api/supplier/submissions/model-options");
  await page.getByRole("heading", { name: /发起申报/ }).waitFor();
  const modelRow = page.locator(".submission-row").first();
  await modelRow.locator(".el-form-item").filter({ hasText: "测试服务 URL" }).locator("input").fill("https://supplier.example/v1");
  const keyInput = modelRow.locator('input[type="password"]');
  await keyInput.fill("supplier-test-secret");
  await page.locator(".submission-panel .el-form-item").filter({ hasText: "申报说明" }).locator("textarea").fill("供应商自测凭据复用验收");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/submissions/71"),
    page.getByRole("button", { name: "保存草稿", exact: true }).click(),
  ]);
  await page.locator(".el-message__content").filter({ hasText: "草稿已保存" }).last().waitFor();

  assert.equal(writes[0]?.method, "PUT");
  assert.equal(writes[0]?.body.rows[0]?.test_api_key, "supplier-test-secret");
  assert.equal(await keyInput.inputValue(), "", "successful save clears the write-only input");
  assert.equal(await page.evaluate(() => JSON.stringify(sessionStorage).includes("supplier-test-secret")), false, "idempotency persistence does not store the request Key");
  await modelRow.getByText("测试 Key（留空沿用）", { exact: true }).waitFor();

  await page.locator(".submission-panel .el-form-item").filter({ hasText: "申报说明" }).locator("textarea").fill("修改申报说明但沿用凭据");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/submissions/71"),
    page.getByRole("button", { name: "保存草稿", exact: true }).click(),
  ]);
  await page.locator(".el-message__content").filter({ hasText: "草稿已保存" }).last().waitFor();
  assert.equal(writes[1]?.method, "PUT");
  assert.equal("test_api_key" in writes[1].body.rows[0], false, "blank update preserves the server-side Key");

  await Promise.all([
    page.waitForResponse(response => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/supplier/submissions/71/rows/72/test"),
    modelRow.locator(".test-area").getByRole("button", { name: "发起测试", exact: true }).first().click(),
  ]);
  await modelRow.locator(".test-area").getByText(/QUEUED/).waitFor();
  assert.deepEqual(tests, [{ category: "CONNECTIVITY" }], "test dispatch reuses the saved Key without resending it");
  console.log("PASS: supplier test Key is written once, cleared from the UI after save, preserved by blank updates, and not resent with test dispatch.");
} finally {
  await browser?.close();
  await server?.close();
}
