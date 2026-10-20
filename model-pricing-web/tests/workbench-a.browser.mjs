import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { createServer } from "vite";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false"; process.env.VITE_MOCK_ENABLED = "false"; process.env.VITE_API_BASE_URL = "/api";
const entry = `import {createApp,h,ref} from 'vue';import {createPinia} from 'pinia';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import Notices from '/src/views/internal/resources/ResourceNoticesPanel.vue';import Notice from '/src/views/internal/resources/NoticeDetailDialog.vue';import Plan from '/src/views/internal/resources/PlanSummaryDialog.vue';import {useSessionStore} from '/src/stores/session.ts';const pinia=createPinia();const app=createApp({setup(){const other=ref(false),plan=ref(false);return()=>h('main',[h('h1','资源公告与计划'),h('button',{onClick:()=>other.value=true},'查看采购乙阅读详情'),h('button',{onClick:()=>plan.value=true},'查看资源计划'),h(Notices,{resourceID:'9007199254740993',region:'CN'}),other.value?h(Notice,{id:'91',selectedStaffID:'8',readOnly:true,onClose:()=>other.value=false}):null,plan.value?h(Plan,{id:'81',onClose:()=>plan.value=false}):null]);}});app.use(pinia).use(ElementPlus,{locale:zhCn});const session=useSessionStore(pinia);session.user={id:'7',name:'采购甲',roleLabel:'采购'};session.portal='internal';app.mount('#app');`;
const plugin = {
  name: "workbench-a-test",
  resolveId(id) { if (id === "/@workbench-a-test-entry") return "\0workbench-a-test-entry"; },
  load(id) { if (id === "\0workbench-a-test-entry") return entry; },
  configureServer(server) { server.middlewares.use(async (req, res, next) => {
    if (req.url !== "/__workbench-a-test") return next();
    res.setHeader("Content-Type", "text/html");
    res.end(await server.transformIndexHtml(req.url, '<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;padding:24px;background:#f5f7f8;color:#20313c;font-family:"Microsoft YaHei",sans-serif}main{max-width:950px;margin:auto}main>button{margin:0 12px 24px 0;padding:10px}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module" src="/@workbench-a-test-entry"></script></body></html>'));
  }); },
};
const notice = { id: "91", resource_id: "9007199254740993", resource_name: "示例标准模型", region: "CN", kind: "OFFLINE", title: "供应资源已下架", content: "原客户配置已停止，保留历史；不会自动切换供应商。", reason: "供应商维护", affected_status: "PENDING", published_at: "2026-10-20T01:00:00Z" };
const firstRead = "2026-10-20T02:00:00Z";
let read = false, failList = true, failDetail = true, failWrite = true, failPlan = true, server, browser;
const writes = [], errors = [], listQueries = [];
const view = (person = "7") => ({ notice: { ...notice, ...(person === "8" ? { affected_status: "READY", affected_customer_count: "0" } : {}) }, staff_id: person, staff_name: person === "7" ? "采购甲" : "采购乙", read_status: person === "7" && read ? "READ" : "UNREAD", read_at: person === "7" && read ? firstRead : null, permissions: { can_mark_read: person === "7" && !read } });
const plan = { id: "81", resource_id: notice.resource_id, region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "8", creator_name: "采购乙", revision: "1", expected_resource_revision: "2", current_resource_revision: "3", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "计划维护", is_initiator: false, read_only: true, precondition_matches: false, blocked_reasons: ["RESOURCE_CHANGED"] };
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" }); await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); page.setDefaultTimeout(20000);
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/internal/**", async route => {
    const request = route.request(), url = new URL(request.url());
    const send = (data, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ code: status === 200 ? 0 : 10000, message: status === 200 ? "ok" : "暂时不可用", data, requestId: "workbench-a-fixture" }) });
    if (url.pathname.endsWith("/plans/81")) { if (failPlan) { failPlan = false; return send(null, 503); } return send(plan); }
    if (url.pathname.endsWith("/read")) {
      writes.push({ body: request.postDataJSON(), key: request.headers()["idempotency-key"], query: url.search });
      if (failWrite) { failWrite = false; return send(null, 503); }
      read = true; return send({ notice_id: "91", staff_id: "7", read_at: firstRead, created: false });
    }
    if (url.pathname.endsWith("/notices/91")) {
      if (failDetail) { failDetail = false; return send(null, 503); }
      return send(view(url.searchParams.get("staff_id") || "7"));
    }
    if (url.pathname.endsWith("/notices")) {
      listQueries.push(Object.fromEntries(url.searchParams));
      if (failList) { failList = false; return send(null, 503); }
      const state = url.searchParams.get("read_status");
      const list = state === "UNREAD" && read || state === "READ" && !read ? [] : [view()];
      return send({ list, total: list.length, page: Number(url.searchParams.get("page")), size: Number(url.searchParams.get("size")) });
    }
    throw new Error(`Unexpected request ${request.method()} ${url.pathname}`);
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__workbench-a-test`);
  await page.getByRole("button", { name: "重新加载公告列表", exact: true }).click();
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const detail = page.locator(".supply-notice-dialog");
  await detail.getByRole("button", { name: "重新加载公告详情", exact: true }).click();
  await detail.getByText("影响范围待计算", { exact: true }).waitFor();
  assert.equal(writes.length, 0, "GET must not mark a notice read");
  await mkdir(".qa", { recursive: true });
  await page.screenshot({ path: ".qa/workbench-notice-own.png", fullPage: true, animations: "disabled" });
  await detail.getByRole("button", { name: "标记已读", exact: true }).click();
  await detail.locator(".write-error").waitFor();
  await detail.getByRole("button", { name: "标记已读", exact: true }).click();
  await detail.getByText("首次阅读时间", { exact: true }).waitFor();
  assert.match(await detail.innerText(), /2026\/10\/20 10:00/);
  assert.equal(await detail.getByRole("button", { name: "标记已读", exact: true }).count(), 0);
  assert.equal(writes.length, 2); assert.ok(writes[0].key); assert.equal(writes[0].key, writes[1].key);
  assert.deepEqual(writes.map(write => write.body), [{}, {}]); assert.ok(writes.every(write => !write.query));
  await page.screenshot({ path: ".qa/workbench-notice-read.png", fullPage: true, animations: "disabled" });
  await detail.getByRole("button", { name: "关闭公告详情", exact: true }).click();
  await page.getByRole("button", { name: "查看采购乙阅读详情", exact: true }).click();
  await detail.getByText("0 个关联客户", { exact: true }).waitFor();
  await detail.getByText("当前为只读查看，不能代他人标记已读。", { exact: true }).waitFor();
  assert.equal(await detail.getByRole("button", { name: "标记已读", exact: true }).count(), 0);
  await page.screenshot({ path: ".qa/workbench-notice-other.png", fullPage: true, animations: "disabled" });
  await detail.getByRole("button", { name: "关闭公告详情", exact: true }).click();
  await page.getByRole("button", { name: "查看资源计划", exact: true }).click();
  const planDialog = page.locator(".supply-plan-dialog");
  await planDialog.getByRole("button", { name: "重新加载计划详情", exact: true }).click();
  await planDialog.getByText("资源已变更，计划执行前需重新核对。", { exact: true }).waitFor();
  await planDialog.getByText("待执行", { exact: true }).waitFor();
  assert.equal(await planDialog.getByRole("button", { name: "登记完成", exact: true }).count(), 0);
  await page.screenshot({ path: ".qa/workbench-plan-readonly.png", fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: ".qa/workbench-plan-mobile.png", fullPage: true, animations: "disabled" });
  assert.ok(await planDialog.evaluate(element => element.getBoundingClientRect().width <= window.innerWidth));
  await planDialog.getByRole("button", { name: "关闭计划详情", exact: true }).click();
  await page.locator(".el-select").filter({ has: page.getByRole("combobox", { name: "公告阅读状态", exact: true }) }).click();
  await page.getByRole("option", { name: "未读", exact: true }).click();
  await page.getByText("暂无符合条件的公告", { exact: true }).waitFor();
  assert.equal(listQueries.at(-1).read_status, "UNREAD"); assert.equal(listQueries.at(-1).resource_id, notice.resource_id);
  assert.equal(listQueries.at(-1).region, "CN"); assert.equal(listQueries.at(-1).page, "1");
  assert.equal(writes.length, 2); assert.deepEqual(errors, []);
  console.log("PASS: notice list/detail retries; GET leaves unread; unknown vs zero impact; mark-read retry retains key and empty body; own receipt disables write; other person readonly; future plan with revision warning; read filter/resource scope; mobile dialog.");
} finally { await browser?.close(); await server?.close(); }
