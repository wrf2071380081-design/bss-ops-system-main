import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { createServer } from "vite";
import { chromium } from "playwright";
import { randomUUID } from "node:crypto";

// Real application, real login, Vite HTTP proxy, real Go API and dedicated PG.
// No page.route, request mocks, or intercepted API responses.
for (const name of ["BSS_TEST_LOGIN_ID", "BSS_TEST_PASSWORD", "BSS_TEST_API_ORIGIN", "BSS_MODEL_PAGES_MANIFEST"]) if (!process.env[name]) throw new Error(`Missing ${name}`);
const manifest = JSON.parse(await readFile(process.env.BSS_MODEL_PAGES_MANIFEST, "utf8"));
assert.equal(manifest.database, "model_bss_a_schema34_20261026_models_backend");
assert.match(process.env.BSS_TEST_API_ORIGIN, /^http:\/\/127\.0\.0\.1:\d+$/);
process.env.VITE_AUTH_ENABLED = "true";
process.env.VITE_MOCK_ENABLED = "false";
process.env.VITE_API_BASE_URL = "/api";
const evidence = process.env.BSS_MODEL_PAGES_EVIDENCE || ".qa/model-pages-live";
await mkdir(evidence, { recursive: true });
const observed = [], pageErrors = [], checks = [], limitations = [];
let server, browser, page;
const checkpoint = text => { checks.push(text); console.log(`PASS: ${text}`); };
try {
  server = await createServer({ server: { host: "127.0.0.1", port: 0, proxy: { "/api": { target: process.env.BSS_TEST_API_ORIGIN, changeOrigin: true } } }, logLevel: "error" });
  await server.listen();
  const base = `http://127.0.0.1:${server.httpServer.address().port}`;
  browser = await chromium.launch({ channel: "msedge", headless: true });
  page = await browser.newPage({ viewport: { width: 1600, height: 1100 } });
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => pageErrors.push(error.message));
  page.on("response", response => {
    const url = new URL(response.url());
    if (url.pathname.startsWith("/api/internal/standard-models") || url.pathname.startsWith("/api/internal/resources")) observed.push({ method: response.request().method(), path: url.pathname, query: Object.fromEntries(url.searchParams), status: response.status(), request_id: response.headers()["x-request-id"] || "" });
  });
  const resourceWait = predicate => { const pending = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname === "/api/internal/resources" && response.status() === 200 && predicate(url.searchParams); }); pending.catch(() => {}); return pending; };
  const resourceResult = async response => (await (await response).json()).data;
  const uiSettled = () => page.locator(".el-loading-mask:visible").count().then(async () => { await page.waitForFunction(() => ![...document.querySelectorAll(".el-loading-mask")].some(el => getComputedStyle(el).display !== "none" && Number(getComputedStyle(el).opacity) !== 0)); });
  await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodels`);
  await page.locator("#login-account").fill(process.env.BSS_TEST_LOGIN_ID);
  await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  const loginWait = page.waitForResponse(response => new URL(response.url()).pathname === "/api/internal/auth/login");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const loginResponse = await loginWait;
  assert.equal(loginResponse.status(), 200);
  const token = (await loginResponse.json()).data.token;
  const http = async (path, options = {}) => {
    const response = await page.request.get(`${base}/api/internal/${path}`, { headers: { Authorization: `Bearer ${token}` }, ...options });
    const body = await response.json(); return { status: response.status(), data: body.data, message: body.message };
  };
  await page.getByRole("heading", { name: "标准模型库", exact: true }).waitFor();
  await page.getByRole("tab", { name: "操作记录", exact: true }).click();
  const historicalRow = page.locator(".record-table tbody tr").filter({ hasText: "A联调-00历史名称" });
  await historicalRow.waitFor();
  assert.equal(await historicalRow.count(), 1);
  assert.ok(observed.some(row => row.path === "/api/internal/standard-models/operations" && row.query.page === "2"));
  const detailPath = `/api/internal/standard-models/${manifest.main_model_id}/operations/${manifest.historical_log_id}`;
  const logWait = page.waitForResponse(response => new URL(response.url()).pathname === detailPath);
  await historicalRow.getByRole("button", { name: "查看", exact: true }).click();
  const historicalResponse = await logWait, historical = (await historicalResponse.json()).data;
  assert.equal(historicalResponse.status(), 200);
  assert.equal(historical.id, manifest.historical_log_id);
  assert.equal(typeof historical.id, "string"); assert.equal(typeof historical.entity_id, "string");
  assert.ok(BigInt(historical.id) > 9007199254740991n);
  assert.equal(historical.model_name, "A联调-00历史名称");
  assert.equal(historical.before, null);
  assert.equal(historical.after.data.name, "A联调-00历史名称");
  assert.equal(JSON.stringify(historical).includes("QA_SENTINEL_DO_NOT_RETURN"), false);
  assert.equal("api_key" in historical.after, false);
  const logDialog = page.getByRole("dialog", { name: "手动补全标准模型", exact: true });
  await logDialog.locator("pre.snapshot").waitFor();
  await logDialog.getByText(manifest.historical_log_id, { exact: true }).waitFor();
  assert.equal((await http(`standard-models/${manifest.main_model_id}`)).data.name, "A联调-00主模型");
  await page.screenshot({ path: `${evidence}/historical-model-log.png`, fullPage: true });
  await logDialog.getByRole("button", { name: "关闭", exact: true }).click();
  checkpoint("日志跨100条分页；超2^53字符串ID；历史名称不随当前名称改变；详情安全快照不泄漏注入字段");

  let wait = resourceWait(q => q.get("view") === "grouped" && q.get("page") === "1");
  const beforePlaza = observed.length;
  await page.goto(`${base}/#/internal/model-plaza`);
  let result = await resourceResult(wait);
  assert.equal(result.total, 25); assert.equal(result.list.length, 20);
  await page.locator(".plaza-table tbody tr").filter({ hasText: "A联调-00主模型" }).waitFor();
  assert.equal(observed.slice(beforePlaza).filter(row => row.path === "/api/internal/resources").length, 1);
  assert.equal(observed.slice(beforePlaza).filter(row => row.path === "/api/internal/standard-models").length, 0);
  wait = resourceWait(q => q.get("view") === "grouped" && q.get("page") === "2");
  await page.locator(".plaza-pagination .btn-next").click(); result = await resourceResult(wait);
  assert.equal(result.total, 25); assert.equal(result.list.length, 5);
  await uiSettled(); assert.equal(await page.locator(".plaza-table tbody tr").count(), 5);
  wait = resourceWait(q => q.get("page") === "1"); await page.locator(".plaza-pagination .btn-prev").click(); await wait;
  for (const order of ["asc", "desc"]) {
    wait = resourceWait(q => q.get("sort_by") === "supplier_count" && q.get("order") === order);
    await page.getByRole("button", { name: /供应来源 [↕↑↓]/ }).click(); result = await resourceResult(wait);
  }
  assert.equal(result.list[0].id, manifest.main_model_id); assert.equal(result.list[0].supplier_count, 25); assert.equal(result.list[0].resource_count, 25); assert.equal(result.list[0].available_count, 0);
  await uiSettled(); assert.match(await page.locator(".plaza-table tbody tr").first().innerText(), /25 家/);
  checkpoint("广场25个模型真实grouped分页20+5；每次只取一页；服务端供应商去重/资源数/可报价数及排序一致");
  const select = async (label, option, matches) => {
    const combo = page.getByRole("combobox", { name: label, exact: true });
    // Focus the input: clicking the centre of a selected multi-select can hit
    // the tag's remove icon (especially a short company name).
    await combo.focus(); await combo.press("ArrowDown");
    if (label === "模型公司") {
      const optionsWait = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname === "/api/internal/resources/filter-options" && url.searchParams.get("keyword") === option && response.status() === 200; });
      await combo.fill(option); await optionsWait;
    }
    const optionNode = page.locator(".el-select-dropdown:visible").last().getByRole("option", { name: option, exact: true });
    await optionNode.waitFor(); const response = resourceWait(matches);
    await Promise.all([optionNode.click(), response]);
    await page.getByRole("heading", { level: 1 }).click(); const data = await resourceResult(response); await uiSettled(); return data;
  };
  const array = (q, key) => JSON.parse(q.get(key) || "[]");
  result = await select("供应商名称", "A联调供应商01 · 国内", q => array(q,"supplier_ids").length === 1);
  result = await select("供应商名称", "A联调供应商02 · 国内", q => array(q,"supplier_ids").length === 2);
  const main = result.list.find(row => row.id === manifest.main_model_id);
  assert.equal(main.supplier_count, 2); assert.equal(main.resource_count, 2); assert.equal(main.coefficient_min, "0.5000"); assert.equal(main.coefficient_max, "0.7000");
  result = await select("模型公司", "Anthropic", q => array(q,"companies").length === 1);
  assert.equal(result.total, 1); assert.equal(result.list[0].id, manifest.model_ids[1]);
  result = await select("模型公司", "OpenAI", q => array(q,"companies").length === 2);
  assert.equal(result.total, 25);
  result = await select("部署节点", "海外", q => array(q,"regions").includes("OVERSEAS")); assert.equal(result.total, 0);
  await page.getByText("当前筛选暂无供应资源", { exact: true }).waitFor();
  result = await select("部署节点", "国内", q => array(q,"regions").length === 2); assert.equal(result.total, 25);
  wait = resourceWait(q => q.get("keyword") === "a-live-vendor-01"); await page.getByRole("textbox", { name: "搜索模型", exact: true }).fill("a-live-vendor-01"); result = await resourceResult(wait);
  assert.equal(result.total, 1); assert.equal(result.list[0].supplier_count, 1); assert.equal(result.list[0].id, manifest.main_model_id);
  await uiSettled(); await page.screenshot({ path: `${evidence}/plaza-server-filters.png`, fullPage: true });
  checkpoint("供应商与公司多选同维度OR、跨维度AND；区域无匹配；接口模型ID搜索；统计范围随服务端筛选更新");

  wait = resourceWait(q => q.get("view") === "flat" && q.get("standard_model_id") === manifest.main_model_id);
  await page.locator(".plaza-table").getByRole("button", { name: "详情", exact: true }).click(); result = await resourceResult(wait);
  assert.equal(result.total, 25); assert.equal(result.list.length, 20);
  const sourcePane = page.getByRole("tabpanel", { name: "供应来源", exact: true });
  await sourcePane.locator("tbody tr").filter({ hasText: "A联调供应商01" }).waitFor();
  wait = resourceWait(q => q.get("view") === "flat" && q.get("page") === "2"); await sourcePane.locator(".btn-next").click(); result = await resourceResult(wait);
  assert.equal(result.total, 25); assert.equal(result.list.length, 5); await uiSettled(); assert.equal(await sourcePane.locator("tbody tr").count(), 5);
  wait = resourceWait(q => q.get("page") === "1" && q.get("connection_max") === "0");
  await page.getByRole("button", { name: /连通耗时 · ms 全部/ }).click();
  await page.getByRole("textbox", { name: "连通耗时 · ms上限", exact: true }).fill("0"); await page.getByRole("button", { name: "应用区间", exact: true }).click(); await page.getByRole("heading", { level: 1 }).click(); result = await resourceResult(wait);
  assert.equal(result.total, 1); assert.equal(result.list[0].id, manifest.resource_ids[0]); assert.equal(result.list[0].test.metrics.connection_ms, "0.00");
  await uiSettled(); assert.equal(await sourcePane.locator("tbody tr").count(), 1);
  await sourcePane.locator("tbody").getByText("0.00%", { exact: true }).waitFor();
  await sourcePane.locator("tbody").getByRole("button", { name: "查看报告", exact: true }).click();
  const reportsDialog = page.getByRole("dialog", { name: "A联调-00主模型 · 测试列表", exact: true });
  await reportsDialog.getByRole("button", { name: "查看本次测试", exact: true }).click();
  const reportDialog = page.getByRole("dialog", { name: "测试报告详情", exact: true }); await reportDialog.getByText("0.00%", { exact: true }).waitFor();
  await page.screenshot({ path: `${evidence}/zero-metric-real-report.png`, fullPage: true });
  await reportDialog.getByRole("button", { name: "关闭报告", exact: true }).click(); await reportsDialog.getByRole("button", { name: "关闭测试列表", exact: true }).click();
  wait = resourceWait(q => !q.has("connection_max")); await page.getByRole("button", { name: "重置", exact: true }).click(); await wait;
  for (const order of ["asc","desc"]) {
    wait = resourceWait(q => q.get("sort_by") === "admitted_at" && q.get("order") === order);
    await page.getByRole("button", { name: /上架时间 [↕↑↓]/ }).click(); result = await resourceResult(wait);
    assert.equal(result.list[0].id, manifest.resource_ids[0]); assert.ok(result.list[0].admitted_at); assert.equal(result.list[1].admitted_at, null);
  }
  await uiSettled(); await page.screenshot({ path: `${evidence}/sources-server-pagination.png`, fullPage: true });
  checkpoint("来源真实flat分页20+5；第二页应用0区间回到第一页；缺指标不冒充0；真实报告与缓存0%；上架时间升降序空值末尾");

  // HTTP negatives use actual authenticated sessions and the same running API.
  const scoped = await http(`standard-models/${manifest.main_model_id}/operations?page=1&size=100`);
  const update = scoped.data.list.find(row => row.action === "MANUAL_SAVE"); assert.ok(update);
  const updateDetail = await http(`standard-models/${manifest.main_model_id}/operations/${update.id}`);
  assert.equal(updateDetail.status,200); assert.ok(updateDetail.data.before); assert.ok(updateDetail.data.after);
  assert.equal(updateDetail.data.before.version_no, String(BigInt(updateDetail.data.after.version_no)-1n));
  assert.equal((await http(`standard-models/${manifest.model_ids[1]}/operations/${manifest.historical_log_id}`)).status,404);
  for (const query of ['supplier_ids=[21]','supplier_ids=["1","1"]','supplier_id=1&supplier_ids=["1"]','regions=["UNKNOWN"]','connection_max=-1','rps_min=2&rps_max=1','coefficient_max=1.01','view=flat&sort_by=after']) {
    const invalid = await http(`resources?${query}`); assert.equal(invalid.status,400,query); assert.equal(invalid.data,null);
  }
  const invalidID = await http(`standard-models/${manifest.main_model_id}/operations/0`); assert.equal(invalidID.status,400);
  const anon = await page.request.get(`${base}/api/internal/standard-models/operations`); assert.equal(anon.status(),401);
  const supplierLogin = await page.request.post(`${base}/api/supplier/auth/login`, { data:{login_id:"a_models_supplier",password:process.env.BSS_TEST_PASSWORD} }); assert.equal(supplierLogin.status(),200);
  const supplierToken = (await supplierLogin.json()).data.token;
  for (const path of ["standard-models/operations","resources?view=grouped"]) { const response = await page.request.get(`${base}/api/internal/${path}`,{headers:{Authorization:`Bearer ${supplierToken}`}}); assert.equal(response.status(),403); }
  const groupedRange = await http(`resources?${new URLSearchParams({view:"grouped",supplier_ids:JSON.stringify(manifest.supplier_ids.slice(0,2)),connection_max:"0",coefficient_min:"0.5",coefficient_max:"0.7",rps_min:"0",rps_max:"0",tps_min:"120000",tps_max:"120000",size:"20"})}`);
  assert.equal(groupedRange.status,200); assert.equal(groupedRange.data.total,1); assert.equal(groupedRange.data.list[0].resource_count,1); assert.equal(groupedRange.data.list[0].coefficient_min,"0.5000");
  const beyondPrecise = await http(`resources?view=flat&tps_min=9007199254740993`); assert.equal(beyondPrecise.status,200); assert.equal(beyondPrecise.data.total,0);
  checkpoint("真实HTTP：UPDATE前后版本、跨模型404、非法查询400、匿名401、供应商403；组合区间先筛选再聚合；大数边界不丢精度");

  const current = (await http(`standard-models/${manifest.main_model_id}`)).data;
  const modelFields = ["company","name","official_id","model_type","input_modalities","output_modalities","capacity","abilities","price_items","api_access","lifecycle","sources","checked_at"];
  const data = Object.fromEntries(modelFields.map(key=>[key,current[key]]));
  const conflict = await page.request.put(`${base}/api/internal/standard-models/${manifest.main_model_id}/manual-completion`,{headers:{Authorization:`Bearer ${token}`,"Idempotency-Key":randomUUID()},data:{...data,expected_revision:"1",reason:"真实联调：拒绝过期版本"}});
  assert.equal(conflict.status(),409);
  assert.equal((await http(`standard-models/${manifest.main_model_id}`)).data.revision,current.revision);
  const reader = await browser.newPage({viewport:{width:1440,height:1000}});
  reader.on("pageerror",error=>pageErrors.push(error.message));
  await reader.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodels`);
  await reader.locator("#login-account").fill("a_models_reader"); await reader.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
  const readerLoginWait=reader.waitForResponse(response=>new URL(response.url()).pathname==="/api/internal/auth/login"); await reader.getByRole("button",{name:"登录",exact:true}).click();
  const readerToken=(await (await readerLoginWait).json()).data.token;
  await reader.getByRole("textbox",{name:"搜索模型名称或官方ID",exact:true}).fill("a-models-live-00");
  await reader.locator(".official-table tbody tr").filter({hasText:"a-models-live-00"}).getByRole("button",{name:"维护",exact:true}).click();
  const readDialog=reader.locator(".standard-model-dialog");
  await readDialog.getByText("当前为只读查看；仅绑定维护人可以补全资料并保存核验记录。",{exact:true}).waitFor();
  assert.equal(await readDialog.getByLabel("官方标准名称",{exact:true}).isDisabled(),true);
  assert.equal(await readDialog.getByRole("button",{name:"保存并留痕",exact:true}).count(),0);
  const forbidden=await reader.request.put(`${base}/api/internal/standard-models/${manifest.main_model_id}/manual-completion`,{headers:{Authorization:`Bearer ${readerToken}`,"Idempotency-Key":randomUUID()},data:{...data,expected_revision:current.revision,reason:"真实联调：非维护人拒绝"}});
  assert.equal(forbidden.status(),403);
  await reader.screenshot({path:`${evidence}/nonowner-readonly-live.png`,fullPage:true});
  await reader.request.post(`${base}/api/internal/auth/logout`,{headers:{Authorization:`Bearer ${readerToken}`}});await reader.close();
  checkpoint("真实过期版本409不写新版本；第二名员工登录后页面只读，直接写入403");
  await page.getByRole("button", { name: "← 返回模型广场", exact: true }).click();
  await page.getByRole("textbox", { name: "搜索模型", exact: true }).waitFor();
  assert.equal(await page.getByRole("textbox", { name: "搜索模型", exact: true }).inputValue(), "a-live-vendor-01");
  await page.setViewportSize({width:390,height:844});
  await page.locator(".plaza-table tbody tr").filter({hasText:"A联调-00主模型"}).waitFor();
  const mobile=await page.evaluate(()=>({viewport:innerWidth,document:document.documentElement.scrollWidth,topbarActionsRight:document.querySelector(".app-topbar__actions")?.getBoundingClientRect().right,tableViewport:document.querySelector(".plaza-scroll")?.clientWidth,tableWidth:document.querySelector(".plaza-scroll")?.scrollWidth}));
  assert.ok(mobile.tableWidth>mobile.tableViewport);
  if(mobile.document>mobile.viewport){assert.ok(mobile.topbarActionsRight>mobile.viewport);limitations.push({scope:"公共AppLayout与styles/index.css",issue:"390px下固定侧栏与顶部账号工具栏造成整页横向溢出，A表格在自身容器横向滚动；本轮未越界改公共布局",measurements:mobile});}
  await page.screenshot({path:`${evidence}/plaza-mobile-live.png`,fullPage:true});
  assert.deepEqual(pageErrors,[]);
  assert.equal(observed.some(item=>item.status>=500),false);
  checkpoint("筛选上下文返回和A表格容器滚动正常；无页面异常或服务端5xx；公共壳层手机溢出单独记录");
  await page.request.post(`${base}/api/internal/auth/logout`,{headers:{Authorization:`Bearer ${token}`}});
  await page.request.post(`${base}/api/supplier/auth/logout`,{headers:{Authorization:`Bearer ${supplierToken}`}});
  await writeFile(`${evidence}/results.json`,JSON.stringify({api:process.env.BSS_TEST_API_ORIGIN,database:manifest.database,checks,limitations,pageErrors,observed},null,2));
} catch(error) {
  if(page) { await page.screenshot({path:`${evidence}/failure.png`,fullPage:true}).catch(()=>{}); await writeFile(`${evidence}/failure-page.txt`,await page.locator("body").innerText()); }
  await writeFile(`${evidence}/failure-results.json`,JSON.stringify({checks,pageErrors,observed,error:String(error)},null,2));
  throw error;
} finally { await browser?.close(); await server?.close(); }
