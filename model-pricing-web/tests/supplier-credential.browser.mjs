import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";

/**
 * 主体级测试凭据（000044）的页面契约。
 *
 * 替代原来的 supplier-test-key.browser.mjs：那份断言的是「行级 Key 写入一次、
 * 留空沿用、发测试不重传」，凭据上移主体后语义全部搬到 /supplier/test-credential，
 * 而且里面的「发起测试」按钮选择器在批量测试改造后就失效了。
 *
 * 这里盯四件事：
 *   1. 凭据区回显 URL、Key 只显示占位；
 *   2. 保存凭据时留空不发送 api_key（服务端保留原值），成功后清空输入；
 *   3. 保存草稿的 rows 里不再出现 test_base_url / test_api_key；
 *   4. 门禁拦截文案是「覆盖口径」，不是老的「至少一行连通通过」。
 */
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Page from '/src/views/supplier/SupplierSubmissionNew.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__supplier-credential',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'71',name:'供应商甲',roleLabel:'供应商'};session.portal='supplier';router.isReady().then(()=>app.mount('#app'));`;

const plugin = {
  name: "supplier-credential-page-test",
  resolveId(id) { return id === "/@supplier-credential-entry" ? "\0supplier-credential-entry" : null; },
  load(id) { return id === "\0supplier-credential-entry" ? entry : null; },
  configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (new URL(req.url, "http://127.0.0.1").pathname !== "/__supplier-credential") return next();
      res.setHeader("Content-Type", "text/html");
      res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@supplier-credential-entry"></script></body></html>'));
    });
  },
};

let server, browser;
const credentialWrites = [];
const submissionWrites = [];
const submitCalls = [];
let credential = { base_url: "https://old.example/v1", has_api_key: true, configured: true, updated_at: "2026-10-27T09:00:00+08:00" };

const draftRow = {
  id: "72",
  interface_model_id: "vendor-chat-v1",
  standard_model_id: "70",
  standard_model_name: "示例标准模型",
  official_id: "official-70",
  coefficient: "0.8",
  test_base_url: "https://old.example/v1",
  test_api_key_configured: true,
  row_status: "PENDING",
  participate_test: true,
  connectivity_tested: false,
  connectivity_passed: false,
  stress_tested: false,
  stress_passed: false,
};
const submission = (revision, rows = [draftRow]) => ({
  id: "71", kind: "MODEL_NEW", status: "DRAFT", revision: String(revision), reason: "", rows: structuredClone(rows),
  permissions: { can_edit: true, can_submit: true, can_withdraw: true },
  created_at: "2026-10-27T00:00:00Z", updated_at: "2026-10-27T00:00:00Z",
});

try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => { throw error; });

  await page.route("**/api/supplier/**", async route => {
    const request = route.request();
    const url = new URL(request.url());
    const send = data => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data, requestId: "supplier-credential-test" }) });
    const fail = (status, message) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ code: 10005, message, data: null, requestId: "supplier-credential-test" }) });

    if (url.pathname === "/api/supplier/test-credential" && request.method() === "GET") return send(credential);
    if (url.pathname === "/api/supplier/test-credential" && request.method() === "PUT") {
      const body = request.postDataJSON();
      credentialWrites.push(body);
      credential = {
        base_url: body.base_url,
        has_api_key: Boolean(body.api_key) || credential.has_api_key,
        configured: true,
        updated_at: "2026-10-27T10:00:00+08:00",
      };
      return send(credential);
    }
    if (url.pathname.endsWith("/model-options")) return send({ list: [{ id: "70", name: "示例标准模型", official_id: "official-70", company: "示例公司" }] });
    if (request.method() === "GET" && url.pathname === "/api/supplier/submissions/71") return send(submission(1));
    if (request.method() === "PUT" && url.pathname === "/api/supplier/submissions/71") {
      const body = request.postDataJSON();
      submissionWrites.push(body);
      return send(submission(2));
    }
    if (request.method() === "POST" && url.pathname === "/api/supplier/submissions/71/submit") {
      submitCalls.push(request.postDataJSON());
      return fail(409, "还有 1 个模型没有连通性测试结果，请先发起测试");
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ code: 404, message: "unexpected route", data: null }) });
  });

  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__supplier-credential?id=71`);
  await page.getByRole("heading", { name: /发起申报/ }).waitFor();
  await page.getByText("此 URL／Key 仅由供应商本人维护", { exact: false }).waitFor();

  // 1. 凭据区回显 URL，Key 输入为空且只提示「已保存」
  const credentialURL = page.locator(".credential-grid input").first();
  assert.equal(await credentialURL.inputValue(), "https://old.example/v1");
  const keyInput = page.locator(".credential-grid input[type=password]");
  assert.equal(await keyInput.inputValue(), "", "Key 输入框不回显已保存的值");
  await page.getByText("已登记测试凭据，Key 只写不读。").waitFor();

  // 2. 行内不再有 URL / Key 输入
  const table = page.locator(".submission-table");
  assert.equal(await table.locator('input[type=password]').count(), 0, "表格里不应再有行级 Key 输入");
  assert.equal(await page.locator(".submission-row").count(), 0, "旧的卡片式行编辑不应再出现");

  // 3. 只改 URL、Key 留空 → PUT 不带 api_key
  await credentialURL.fill("https://new.example/v1");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/test-credential"),
    page.getByRole("button", { name: "保存测试凭据", exact: true }).click(),
  ]);
  await page.locator(".el-message__content").filter({ hasText: "测试凭据已保存" }).last().waitFor();
  assert.deepEqual(credentialWrites[0], { base_url: "https://new.example/v1" }, "留空保留：请求里不应出现 api_key");
  assert.equal(await keyInput.inputValue(), "");

  // 4. 填 Key 再保存 → 带上 api_key
  await keyInput.fill("supplier-test-secret");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/test-credential"),
    page.getByRole("button", { name: "保存测试凭据", exact: true }).click(),
  ]);
  assert.deepEqual(credentialWrites[1], { base_url: "https://new.example/v1", api_key: "supplier-test-secret" });
  assert.equal(await keyInput.inputValue(), "", "保存成功后清空 Key 输入");

  // 5. 保存草稿：rows 里不再出现行级凭据
  const reasonBox = page.locator(".submission-panel .el-form-item").filter({ hasText: "申报说明" }).locator("textarea");
  await reasonBox.fill("选填说明：先跑连通性与压力测试");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/submissions/71"),
    page.getByRole("button", { name: "保存草稿", exact: true }).click(),
  ]);
  await page.locator(".el-message__content").filter({ hasText: "草稿已保存" }).last().waitFor();
  const writtenRow = submissionWrites.at(-1)?.rows?.[0];
  assert.equal("test_base_url" in (writtenRow || {}), false, "草稿行不应再提交 test_base_url");
  assert.equal("test_api_key" in (writtenRow || {}), false, "草稿行不应再提交 test_api_key");
  assert.equal(submissionWrites.at(-1)?.reason, "选填说明：先跑连通性与压力测试");

  // 5b. 申报说明留空同样能保存（选填口径）
  await reasonBox.fill("");
  await Promise.all([
    page.waitForResponse(response => response.request().method() === "PUT" && new URL(response.url()).pathname === "/api/supplier/submissions/71"),
    page.getByRole("button", { name: "保存草稿", exact: true }).click(),
  ]);
  assert.equal(submissionWrites.at(-1)?.reason, "", "申报说明选填：空说明也能保存");

  // 6. 门禁提示走覆盖口径，且提交按钮在未覆盖时是禁用的
  await page.getByText(/还有 1 个模型没有连通性测试结果/).first().waitFor();
  assert.equal(await page.getByRole("button", { name: "提交申报", exact: true }).isDisabled(), true, "未覆盖连通性时提交应禁用");

  console.log("PASS: 测试凭据走主体接口（留空保留、Key 不回显），行级凭据已从草稿契约中移除，门禁文案为覆盖口径。");
} finally {
  await browser?.close();
  await server?.close();
}
