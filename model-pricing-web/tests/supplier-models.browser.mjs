import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";

/**
 * 供应商「模型管理」页 · 原型第二版契约（2026-10-28）。
 *
 * 盯住原型里那几条硬要求：
 *   1. 7 列：对应标准模型 / 接口模型 ID / 模型公司 / 模型类型 / 当前供货报价系数 / 接入状态 / 操作；
 *      没有「调用量」「错误数」「统计时间」这些旧版元素；
 *   2. 接入状态只显示「待接入 / 已接入 / 已下架」，不再带「测试待完成」小字；
 *   3. 系数去尾零（0.6000 → 0.6）；
 *   4. 已下架整行置灰；
 *   5. 三组勾选筛选 + 搜索即时生效，筛选无结果显示「当前筛选暂无数据」；
 *   6. 价格变更按钮存在。
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

const row = (id, model, supplierModel, company, type, coefficient, state, extra = {}) => ({
  id, standard_model_id: id, model_name: model, official_id: `${id}-official`, company, model_type: type,
  supplier_model_id: supplierModel, region: "CN", coefficient, state, ever_admitted: state === "ADMITTED", revision: "1",
  test: { status: "COMPLETED", connectivity_tested: true, connectivity_passed: true, stress_tested: true, stress_passed: true, valid: true, connection_ms: "87.00", rps: "28.40", tps: "24500.00" },
  quote_readiness: { eligible: state === "ADMITTED", blocked_reasons: state === "ADMITTED" ? [] : ["RESOURCE_NOT_ADMITTED"] },
  ...extra,
});
const fixtures = [
  row("31", "DeepSeek-V3", "DeepSeek-V3", "DeepSeek", "TEXT", "0.7500", "ADMITTED"),
  row("32", "GPT-4.1", "GPT-4.1", "OpenAI", "TEXT", "0.8000", "ADMITTED"),
  row("33", "图片能力模型 03", "fast/demo17-model-2", "示例多模态研究院", "IMAGE", "0.4300", "OFFLINE"),
];

let server, browser;
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => { throw error; });

  await page.route("**/api/supplier/**", async route => {
    const url = new URL(route.request().url());
    if (url.pathname !== "/api/supplier/models") return route.fulfill({ status: 404, contentType: "application/json", body: '{"code":404,"message":"unexpected","data":null}' });
    return route.fulfill({
      status: 200, contentType: "application/json",
      body: JSON.stringify({ code: 0, message: "ok", requestId: "models-test", data: { total: fixtures.length, page: 1, size: 100, list: fixtures } }),
    });
  });

  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__supplier-models`);
  await page.getByText("DeepSeek-V3").first().waitFor();

  // 1. 列定义
  const headers = (await page.locator(".el-table__header th").allInnerTexts()).map(text => text.replace(/\s+/g, ""));
  for (const expected of ["对应标准模型", "接口模型ID", "模型公司", "模型类型", "当前供货报价系数", "接入状态", "操作"]) {
    assert.ok(headers.some(text => text.startsWith(expected)), `缺少列：${expected}`);
  }
  assert.ok(!headers.some(text => text.includes("调用量")), "不应再有「调用量」列");
  assert.ok(!headers.some(text => text.includes("错误数")), "不应再有「错误数」列");
  assert.equal(await page.getByText("统计时间").count(), 0, "不应再有「统计时间」控件");

  // 2. 系数去尾零 + 模型类型中文 + 状态话术
  const bodyText = await page.locator(".el-table__body").innerText();
  assert.ok(bodyText.includes("0.75") && !bodyText.includes("0.7500"), "系数应去尾零展示");
  assert.ok(bodyText.includes("文本") && bodyText.includes("图片"), "模型类型应显示中文");
  assert.equal(await page.getByText("测试待完成").count(), 0, "状态不应再带「测试待完成」小字");
  assert.ok(bodyText.includes("已下架"), "应显示「已下架」");

  // 3. 已下架整行置灰
  assert.equal(await page.locator(".el-table__body tr.row-muted").count(), 1, "已下架行应整行置灰（row-muted）");

  // 4. 勾选筛选：模型类型选「图片」只剩一行
  await page.locator(".filters .filter-trigger").first().click();
  await page.locator(".filter-menu .el-checkbox__label").filter({ hasText: "图片" }).click();
  await page.locator("body").click();
  await page.waitForTimeout(300);
  assert.equal(await page.locator(".el-table__body tr").count(), 1, "选「图片」后应只剩 1 行");

  // 5. 筛选无结果 → 空态文案
  await page.locator(".search input").fill("不存在的模型");
  await page.waitForTimeout(300);
  await page.getByText("当前筛选暂无数据").waitFor();

  // 6. 重置 + 排序
  await page.getByRole("button", { name: "重置" }).click();
  await page.waitForTimeout(300);
  assert.equal(await page.locator(".el-table__body tr").count(), 3, "重置后应恢复 3 行");
  await page.locator(".el-table__header th").filter({ hasText: "当前供货报价系数" }).click();
  await page.waitForTimeout(300);
  const firstRow = await page.locator(".el-table__body tr").first().innerText();
  assert.ok(firstRow.includes("图片能力模型 03"), `按系数升序首个应是 0.43 那条，实际：${firstRow}`);

  // 7. 操作列只有「价格变更」
  assert.equal(await page.getByRole("button", { name: "价格变更" }).count(), 3, "每行应有「价格变更」按钮");

  console.log("PASS: 模型管理页 7 列、系数去尾零、已下架置灰、勾选筛选/搜索/排序/空态均符合原型第二版。");
} finally {
  await browser?.close();
  await server?.close();
}
