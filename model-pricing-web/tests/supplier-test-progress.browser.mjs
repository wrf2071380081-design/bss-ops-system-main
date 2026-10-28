import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";

/**
 * 供应商申报页 · 等结果的体验（2026-10-28，A+B+C）。
 *
 * 盯住三件事：
 *   1. 页面自己会盯结果：打开一张还有任务在跑的草稿后，不点任何按钮也会轮询 test-progress；
 *   2. 运行中不再显示「未测试」：测试列显示「测试中」并带已用时间；
 *   3. 结果落地后自动变成「通过/已完成」，并给出「可以核对后提交」的提示（行高亮）。
 *
 * 网络全部桩掉：真机跑一次压测要 35 秒，用例不能依赖它。
 */
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Page from '/src/views/supplier/SupplierSubmissionNew.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const router=createRouter({history:createWebHistory(),routes:[{path:'/__submission-progress',component:Page}]});const app=createApp({render:()=>h(RouterView)});app.use(pinia).use(router).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'71',name:'供应商甲',roleLabel:'供应商'};session.portal='supplier';router.isReady().then(()=>app.mount('#app'));`;

const plugin = {
  name: "submission-progress-test",
  resolveId(id) { return id === "/@submission-progress-entry" ? "\0submission-progress-entry" : null; },
  load(id) { return id === "\0submission-progress-entry" ? entry : null; },
  configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (new URL(req.url, "http://127.0.0.1").pathname !== "/__submission-progress") return next();
      res.setHeader("Content-Type", "text/html");
      res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><style>body{margin:0;padding:24px;font-family:"Microsoft YaHei",sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@submission-progress-entry"></script></body></html>'));
    });
  },
};

const startedAt = new Date(Date.now() - 8000).toISOString();
const testJob = (category, overrides = {}) => ({
  category, status: "RUNNING", tested: false, passed: false, verdict: "", stale: false,
  job_id: category === "STRESS" ? "87" : "86", started_at: startedAt, eta_seconds: category === "STRESS" ? 35 : 10,
  connection_ms: null, rps: null, tps: null, ...overrides,
});

const draftRow = (job) => ({
  id: "33", interface_model_id: "prog-demo", standard_model_id: "15", standard_model_name: "Claude Sonnet 4",
  official_id: "claude-sonnet-4", coefficient: "0.6000", test_base_url: "demo", test_api_key_configured: true,
  row_status: "RUNNING", participate_test: true, connectivity_tested: false, connectivity_passed: false,
  stress_tested: false, stress_passed: false,
  connectivity_job: job("CONNECTIVITY"), stress_job: job("STRESS"),
});

const draft = (row) => ({
  id: "101", submission_no: "S00000101", submitter_name: "", region: "CN", kind: "MODEL_NEW",
  status: "DRAFT", revision: "1", reason: "", row_count: 1, model_names: [row.standard_model_name],
  pending_row_count: 0, rows: [row], permissions: { can_edit: true, can_submit: true, can_withdraw: true },
  created_at: new Date().toISOString(), updated_at: new Date().toISOString(), submitted_at: null,
});

let server, browser;
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => { throw error; });

  // 前两次轮询还在跑，之后返回终态——正好覆盖「运行中 → 出结果」的切换。
  let progressCalls = 0;
  const finished = () => ({
    rows: [{
      row_id: "33",
      connectivity: testJob("CONNECTIVITY", { status: "SUCCEEDED", tested: true, passed: true, verdict: "pass", connection_ms: "401.01" }),
      stress: testJob("STRESS", { status: "SUCCEEDED", tested: true, passed: false, verdict: "", rps: "3.23", tps: "64.67" }),
    }],
  });
  const running = () => ({
    rows: [{ row_id: "33", connectivity: testJob("CONNECTIVITY"), stress: testJob("STRESS") }],
  });

  await page.route("**/api/supplier/**", async route => {
    const url = new URL(route.request().url());
    const send = data => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", requestId: "progress-test", data }) });
    if (url.pathname === "/api/supplier/test-credential") return send({ base_url: "demo", has_api_key: true, configured: true });
    if (url.pathname === "/api/supplier/submissions/model-options") {
      return send({ list: [{ id: "15", name: "Claude Sonnet 4", official_id: "claude-sonnet-4", company: "Anthropic" }] });
    }
    if (url.pathname === "/api/supplier/submissions/101/test-progress") {
      progressCalls += 1;
      return send(progressCalls >= 2 ? finished() : running());
    }
    if (url.pathname === "/api/supplier/submissions/101") return send(draft(draftRow(testJob)));
    return route.fulfill({ status: 404, contentType: "application/json", body: '{"code":404,"message":"unexpected","data":null}' });
  });

  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__submission-progress?id=101`);
  await page.getByText("Claude Sonnet 4").first().waitFor();

  // 1) 运行中的文案：不能说「未测试」，要带已用时间，并明确告诉用户可以离开
  const cell = page.locator(".submission-table .el-table__body .test-cell").first();
  await cell.getByText("测试中").first().waitFor();
  assert.equal(await cell.getByText(/连通 未测试/).count(), 0, "运行中不应显示「未测试」");
  assert.ok((await cell.innerText()).includes("已用"), "运行中应显示已用时间");
  assert.ok((await cell.innerText()).includes("约 35s"), "压测应带出预估耗时");
  await page.getByText("可以先补充其他模型", { exact: false }).waitFor();

  // 2) 不点任何按钮，页面自己会把结果拉回来
  await page.getByText("测试结果已出", { exact: false }).waitFor({ timeout: 25000 });
  assert.ok(progressCalls >= 2, `应自动轮询到结果，实际请求 ${progressCalls} 次`);

  // 3) 结果落地后的文案与行高亮
  const bodyText = await page.locator(".submission-table .el-table__body").innerText();
  assert.ok(bodyText.includes("连通 通过"), "连通应变为通过");
  assert.ok(bodyText.includes("压力 已完成"), "压测无判定，应显示已完成");
  assert.ok(bodyText.includes("3.23"), "应带出压测指标");
  assert.equal(await page.locator(".el-table__body tr.row-finished").count(), 1, "出结果的行应高亮");

  // 4) 结果出来后，提醒「可以离开」的那条就不再占位了
  await page.getByText("可以先补充其他模型", { exact: false }).waitFor({ state: "detached", timeout: 5000 });

  console.log("supplier-test-progress: OK", `progressCalls=${progressCalls}`);
} finally {
  await browser?.close();
  await server?.close();
}
