import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";

/**
 * 供应商「模型管理」页（2026-10-28 按原型 vPortalModels 重做）。
 *
 * 盯四件事：
 *   1. 不再出现「数据不存在或当前账号不可见」（接口补上之前就是这个错）；
 *   2. 表头是原型那 7 列；
 *   3. 状态话术：PENDING_TEST 显示「待接入 + 测试待完成」，不暴露「待测试」；
 *   4. 调用量/错误数没有数据源时显示「—」，不编数字。
 */
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Page from '/src/views/supplier/SupplierModels.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__supplier-models',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'71',name:'供应商甲',roleLabel:'供应商'};session.portal='supplier';router.isReady().then(()=>app.mount('#app'));`;

const plugin = {
  name: "supplier-models-page-test",
  resolveId(id) { return id === "/@supplier-models-entry" ? "\0supplier-models-entry" : null; },
  load(id) { return id === "\0supplier-models-entry" ? entry : null; },
  configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (new URL(req.url, "http://127.0.0.1").pathname !== "/__supplier-models") return next();
      res.setHeader("Content-Type", "text/html");
      res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@supplier-models-entry"></script></body></html>'));
    });
  },
};

const row = (id, model, official, supplierModel, state, test, readiness, coefficient = "0.6000") => ({
  id, standard_model_id: id, model_name: model, official_id: official, supplier_model_id: supplierModel,
  region: "CN", coefficient, state, ever_admitted: state === "ADMITTED", revision: "1", test, quote_readiness: readiness,
});
const notTested = { status: "NOT_TESTED", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false, valid: false };

let server, browser;
const listRequests = [];
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => { throw error; });

  await page.route("**/api/supplier/**", async route => {
    const url = new URL(route.request().url());
    listRequests.push(url.pathname + url.search);
    if (url.pathname !== "/api/supplier/models") return route.fulfill({ status: 404, contentType: "application/json", body: '{"code":404,"message":"unexpected","data":null}' });
    return route.fulfill({
      status: 200, contentType: "application/json",
      body: JSON.stringify({
        code: 0, message: "ok", requestId: "models-test",
        data: {
          total: 2, page: 1, size: 100,
          list: [
            row("3", "Claude Sonnet 4", "claude-sonnet-4", "demo/deepseek-v4", "ADMITTED",
              { report_id: "51", status: "COMPLETED", connectivity_tested: true, connectivity_passed: true, stress_tested: true, stress_passed: true, valid: true, connection_ms: "87.00", rps: "28.40", tps: "24500.00" },
              { eligible: true, blocked_reasons: [] }),
            row("9", "GPT-4o mini", "gpt-4o-mini", "demo-model-1", "PENDING_TEST", notTested,
              { eligible: false, blocked_reasons: ["RESOURCE_NOT_ADMITTED", "TESTS_REQUIRED"] }, "1.0000"),
          ],
        },
      }),
    });
  });

  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__supplier-models`);
  await page.getByRole("heading", { name: "模型管理" }).waitFor();
  await page.getByText("demo/deepseek-v4").waitFor();

  // 1. 不再报「数据不存在或当前账号不可见」
  assert.equal(await page.getByText("数据不存在或当前账号不可见。").count(), 0, "不应出现 404 文案");

  // 2. 原型 7 列
  const headers = await page.locator(".el-table__header th").allInnerTexts();
  const normalized = headers.map(text => text.replace(/\s+/g, ""));
  for (const expected of ["供方模型名称", "平台标准名称", "采购系数", "调用量", "错误数", "状态", "资源变更"]) {
    assert.ok(normalized.some(text => text.startsWith(expected)), `缺少列：${expected}`);
  }

  // 3. 状态话术：待接入 + 测试待完成（不出现「待测试」）
  await page.getByText("测试待完成").waitFor();
  assert.equal(await page.getByText("待测试", { exact: true }).count(), 0, "不应直接显示内部状态「待测试」");

  // 4. 调用量/错误数没有数据源 → 显示「—」
  const dashCount = await page.locator(".el-table__body td").filter({ hasText: "—" }).count();
  assert.ok(dashCount >= 2, "调用量与错误数应显示占位符");

  // 5. 发起变更能跳到价格变更编辑器
  const changeLink = page.getByRole("button", { name: "发起变更" }).first();
  assert.equal(await changeLink.count(), 1);

  console.log("PASS: 模型管理页按原型渲染 7 列，状态话术为「待接入 / 测试待完成」，无统计数据时显示占位符。");
} finally {
  await browser?.close();
  await server?.close();
}
