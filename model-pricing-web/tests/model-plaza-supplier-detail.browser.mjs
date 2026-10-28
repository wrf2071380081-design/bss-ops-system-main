import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer } from "vite";
import { resourcePage, resourceOptions } from "./plaza-server.fixture.mjs";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
process.env.VITE_AUTH_ENABLED = "false";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";

const entry = `import {createApp,h} from 'vue';import {createPinia} from 'pinia';import {createRouter,createWebHistory,RouterView} from 'vue-router';import ElementPlus from 'element-plus';import zhCn from 'element-plus/es/locale/lang/zh-cn';import 'element-plus/dist/index.css';import ModelPlaza from '/src/views/internal/resources/ModelPlaza.vue';import Page from '/src/views/internal/resources/ModelPlazaDetail.vue';import SupplierAccessModels from '/src/views/internal/suppliers/SupplierAccessModels.vue';import {useSessionStore} from '/src/stores/session.ts';const app=createApp({render:()=>h(RouterView)});const router=createRouter({history:createWebHistory(),routes:[{path:'/__model-plaza',component:ModelPlaza},{path:'/internal/model-plaza',component:ModelPlaza},{path:'/internal/model-detail/:id',component:Page},{path:'/internal/suppliers/:supplierId/models',component:SupplierAccessModels},{path:'/__model-detail/:id',component:Page}]});app.use(createPinia()).use(router).use(ElementPlus,{locale:zhCn});useSessionStore().restoreSession();router.isReady().then(()=>app.mount('#app'));`;
const plugin = { name: "model-plaza-supplier-detail-test", resolveId(id) { if (id === "/@supplier-detail-test-entry") return "\0supplier-detail-test-entry"; }, load(id) { if (id === "\0supplier-detail-test-entry") return entry; }, configureServer(server) { server.middlewares.use(async (req, res, next) => { if (!new Set(["/__model-detail/11", "/__model-plaza"]).has(req.url?.split("?")[0])) return next(); res.setHeader("Content-Type", "text/html"); res.end(await server.transformIndexHtml(req.url.split("?")[0], '<html lang="zh-CN"><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module" src="/@supplier-detail-test-entry"></script></body></html>')); }); } };
const price = { item_key: "input", label: "输入", task_type: "TEXT_INPUT", amount: "1.00000000", currency: "USD", unit_code: "TOKEN", unit_quantity: "1000000", condition: "标准请求", dimensions: {}, source_url: "https://vendor.example/pricing" };
const model = { id: "11", company: "模型公司", name: "标准模型甲", official_id: "official-a", model_type: "TEXT", input_modalities: ["TEXT"], output_modalities: ["TEXT"], capacity: [], abilities: [], price_items: [price], api_access: [], lifecycle: [], sources: [{ label: "官方价格", url: "https://vendor.example/pricing", source_kind: "OFFICIAL_PRICING" }], checked_at: "2026-10-23", maintainer_id: "7", revision: "1", version_no: "1", current_version_no: "1", status: "ACTIVE", verify_status: "VERIFIED", checked_by: "7", created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", missing_fields: [], quote_readiness: { ready: true, reasons: [], standard_model_version_no: "1" }, permissions: { can_edit: false, can_verify: false } };
function row(id, supplierID, supplierName, standardID, modelName, supplierModelID) {
  return { id, supplier_id: supplierID, supplier_name: supplierName, standard_model_id: standardID, model_name: modelName, official_id: `official-${standardID}`, company: "模型公司", model_type: "TEXT", region: "CN", supplier_model_id: supplierModelID, coefficient: "0.7000", buyer_id: "7", state: "ADMITTED", ever_admitted: true, revision: "1", formal_config_id: "91", formal_config_revision: "1", test: { report_id: "51", status: "COMPLETED", connectivity_passed: true, pressure_passed: true, valid: true, cache_stats: { status: "NOT_PROVIDED", report_id: "51" }, metrics: { connection_ms: "12.00", rps: "23.00", tps: "45.00" } }, official_price_items: [price], quote_readiness: { eligible: false, blocked_reasons: ["MODEL_NOT_READY"], resource_revision: "1", model_version_no: "1", formal_config_revision: "1", test_evidence_ref: "51" }, permissions: { can_edit: false }, created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-22T01:00:00Z" };
}
const source = row("101", "21", "供应商甲", "11", "标准模型甲", "vendor-model-a");
source.admitted_at = "2026-10-21T04:00:00Z";
const other = row("102", "21", "供应商甲", "12", "标准模型乙", "vendor-model-b");
const otherSupplier = row("201", "22", "供应商乙", "11", "标准模型甲", "vendor-model-c");
let server, browser;
const flatQueries = [];
try {
  server = await createServer({ plugins: [plugin], server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await server.listen();
  browser = await chromium.launch({ headless: true, channel: "msedge" });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.setDefaultTimeout(10000);
  const pageErrors = [];
  page.on("pageerror", error => pageErrors.push(error.stack || error.message));
  page.on("console", message => { if (message.type() === "error") pageErrors.push(message.text()); });
  page.on("response", response => { if (response.status() >= 400) pageErrors.push(`${response.status()} ${response.url()}`); });
  await page.route("**/api/internal/standard-models?**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [model], total: 1, page: 1, size: 100 }, requestId: "supplier-detail-test" }) }));
  await page.route("**/api/internal/standard-models/11", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: model, requestId: "supplier-detail-test" }) }));
  await page.route("**/api/internal/standard-models/*/resource-operations?**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, size: 20 }, requestId: "supplier-detail-test" }) }));
  await page.route("**/api/internal/resources?**", route => {
    const url = new URL(route.request().url());
    flatQueries.push(Object.fromEntries(url.searchParams));
    const data = resourcePage([source, other, otherSupplier], Object.fromEntries(url.searchParams));
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data, requestId: "supplier-detail-test" }) });
  });
  await page.route("**/api/internal/resources/*", route => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    const found = [source, other].find(resource => resource.id === id);
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: found, requestId: "supplier-detail-test" }) });
  });
  await page.route("**/api/internal/resources/filter-options?**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, data: resourceOptions([source, other, otherSupplier]) }) }));
  await page.route("**/api/internal/resources/filter-options", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, data: resourceOptions([source, other, otherSupplier]) }) }));
  await page.route("**/api/internal/supplier-entities/21", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { id: "21", buyer_id: "7", name: "供应商甲", aliases: [], aliases_supported: true, region: "CN", registration_no: "91310000TEST001", contact: "联系人甲", phone: "13800000000", tax_rate: "0.06000000", settlement_currency: "CNY", established: true, status: "ACTIVE", revision: "1", created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", can_edit: true }, requestId: "supplier-detail-test" }) }));
  await page.route("**/api/internal/contracts?**", route => {
    const url = new URL(route.request().url());
    assert.equal(url.searchParams.get("subject_type"), "SUPPLIER");
    assert.equal(url.searchParams.get("subject_id"), "21");
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, size: 100 }, requestId: "supplier-detail-test" }) });
  });
  await page.route("**/api/internal/credits?**", route => {
    const url = new URL(route.request().url());
    assert.equal(url.searchParams.get("subject_type"), "SUPPLIER");
    assert.equal(url.searchParams.get("subject_id"), "21");
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, size: 100, current_credit: [] }, requestId: "supplier-detail-test" }) });
  });
  await page.route("**/api/internal/suppliers/21/operations?**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, size: 20 }, requestId: "supplier-detail-test" }) }));
  await page.route("**/api/internal/supplier-entities/21/qualifications?**", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, data: { list: [], total: 0, page: 1, size: 20, metadata_supported: true } }) }));
  await page.route("**/api/internal/supplier-entities/21/test-config", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, data: { supplier_id: "21", configured: false, base_url: "", revision: "0", has_api_key: false } }) }));
  await page.route("**/api/internal/flows/suppliers/21/progress", route=>route.fulfill({contentType:'application/json',body:JSON.stringify({code:0,data:{supplier_id:'21',list:[]}})}));
  await page.route("**/api/internal/flows/suppliers/21/submissions?**", route => {
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ code: 0, message: "ok", data: { list: [], total: 0, page: 1, size: 20 }, requestId: "supplier-detail-test" }) });
  });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__model-plaza?keyword=%E6%A0%87%E5%87%86%E6%A8%A1%E5%9E%8B%E7%94%B2&region=CN&supplier_id=21&supplier_name=%E4%BE%9B%E5%BA%94%E5%95%86%E7%94%B2&supplier_region=CN&state=ADMITTED&page=1&size=20&sort_by=company&order=desc`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "模型广场", exact: true }).waitFor();
  await page.locator(".plaza-table").getByRole("button", { name: "详情", exact: true }).click();
  await page.waitForURL(url => url.pathname === "/internal/model-detail/11");
  try {
    await page.getByRole("heading", { name: "标准模型甲", exact: true }).waitFor();
  } catch (error) {
    console.error("Supplier detail page did not load:", await page.locator("body").innerText(), pageErrors);
    throw error;
  }
  await page.locator(".detail-panel tbody tr").filter({ hasText: "供应商乙" }).getByText("未记录", { exact: true }).waitFor();
  await page.getByPlaceholder("供应商名称 / 接口模型 ID").fill("vendor-model-a");
  const sourceRow = page.locator("tbody tr").filter({ hasText: "供应商甲" });
  await sourceRow.getByText("vendor-model-a", { exact: true }).waitFor();
  assert.equal(await page.locator(".detail-panel tbody tr").count(), 1);
  await sourceRow.getByText("2026/10/21 12:00", { exact: true }).waitFor();
  await sourceRow.getByRole("button", { name: "详情", exact: true }).click();
  await page.waitForURL(url => url.pathname === "/internal/suppliers/21/models");
  const url = new URL(page.url());
  assert.equal(url.searchParams.get("resource_id"), "101");
  assert.equal(url.searchParams.get("model_id"), "11");
  assert.equal(url.searchParams.get("region"), "CN");
  await page.getByRole("heading", { name: "供应商甲", exact: true }).waitFor();
  await page.getByRole("tab", { name: "接入模型", exact: true }).waitFor();
  await page.getByText("标准模型乙", { exact: true }).waitFor();
  await page.locator(".resource-detail").getByText("vendor-model-a", { exact: true }).waitFor();
  assert.equal(await page.getByText("供应商乙", { exact: true }).count(), 0);
  await page.getByRole("tab", { name: "档案与办理", exact: true }).click();
  await page.getByText("主体档案", { exact: true }).waitFor();
  await page.getByText("91310000TEST001", { exact: true }).waitFor();
  await page.getByText("合同与授信", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "操作记录", exact: true }).click();
  await page.getByText("汇总主体、资质、正式配置、合同、授信、资源及计划的操作摘要。", { exact: true }).waitFor();
  await page.getByText("暂无操作记录", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "该供应商报价", exact: true }).click();
  await page.getByText("当前主体的全部模型申报与改价记录，包含草稿、待审批、驳回、办理及已完成状态。", { exact: true }).waitFor();
  await page.getByText("暂无报价申报历史", { exact: true }).waitFor();
  await page.getByRole("button", { name: "← 返回模型详情", exact: true }).click();
  await page.waitForURL(url => url.pathname === "/internal/model-detail/11");
  assert.equal(await page.getByPlaceholder("供应商名称 / 接口模型 ID").inputValue(), "vendor-model-a");
  assert.equal(await page.locator(".detail-panel tbody tr").count(), 1);
  await page.getByRole("button", { name: "← 返回模型广场", exact: true }).click();
  await page.waitForURL(url => url.pathname === "/internal/model-plaza");
  await page.getByRole("textbox", { name: "搜索模型", exact: true }).waitFor();
  assert.equal(await page.getByRole("textbox", { name: "搜索模型", exact: true }).inputValue(), "标准模型甲");
  const returnedPlazaURL = new URL(page.url());
  assert.equal(returnedPlazaURL.searchParams.get("supplier_id"), "21");
  assert.equal(returnedPlazaURL.searchParams.get("supplier_name"), "供应商甲");
  assert.match(await page.locator(".plaza-filters").innerText(), /供应商甲/);
  assert.match(await page.locator(".plaza-filters").innerText(), /已接入/);
  const restored = JSON.parse(returnedPlazaURL.searchParams.get("filters"));
  assert.deepEqual(restored.supplier_ids, ["21"]);
  assert.deepEqual(restored.regions, ["CN"]);
  assert.deepEqual(restored.states, ["ADMITTED"]);
  assert.equal(restored.keyword, "标准模型甲");
  const returnedQuery = flatQueries.filter(query => query.view === "grouped").at(-1);
  assert.equal(returnedQuery.sort_by, "company");
  assert.equal(returnedQuery.size, "20");
  assert.deepEqual(JSON.parse(returnedQuery.supplier_ids), ["21"]);
  await page.waitForFunction(() => document.querySelector(".plaza-table tbody")?.textContent.includes("1 家"));
  assert.match(await page.locator(".plaza-table tbody").innerText(), /1 家/);
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__model-detail/11?source_page=2&source_size=1`);
  await page.locator(".detail-panel tbody tr").filter({ hasText: "供应商乙" }).waitFor();
  assert.equal(await page.locator(".detail-panel tbody tr").count(), 1);
  assert.equal(flatQueries.at(-1).page, "2");
  assert.equal(flatQueries.at(-1).size, "1");
  await page.locator(".detail-panel .el-pagination .btn-prev").click();
  await page.locator(".detail-panel tbody tr").filter({ hasText: "供应商甲" }).waitFor();
  assert.equal(flatQueries.at(-1).page, "1");
  await page.locator(".detail-panel tbody tr").getByRole("button", { name: "详情", exact: true }).click();
  await page.waitForURL(url => url.pathname === "/internal/suppliers/21/models");
  assert.equal(new URL(page.url()).searchParams.get("source_size"), "1");
  await page.getByRole("button", { name: "← 返回模型详情", exact: true }).click();
  await page.locator(".detail-panel tbody tr").filter({ hasText: "供应商甲" }).waitFor();
  assert.equal(flatQueries.at(-1).size, "1");
  assert.deepEqual(pageErrors, []);
  console.log("PASS: model plaza filter context survives model and supplier detail navigation; source filters and all four supplier tabs restore correctly; resources remain supplier-scoped and missing admission time stays unrecorded.");
} finally { await browser?.close(); await server?.close(); }
