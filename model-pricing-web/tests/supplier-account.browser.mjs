// 供应商门户「账户设置」浏览器验收（M3）。
//
// 跑法：npm run test:browser:account
// 说明：仓库的 tests/browser.mjs 是内部门户大脚本，这里独立成文件、独立起 vite，
//      避免改动大脚本引入冲突；两者可以各自跑。
//
// 覆盖原型实测出来的关键状态：
//   1. 已上架主体 → 主体属性锁定（label 带「（有上架历史，已锁定）」、下拉 disabled）
//   2. 未上架主体 → 主体属性可改、注册编号 label 变「当地企业注册编号」
//   3. 保存成功 toast「已保存并留痕」+ 操作记录首行出现「维护企业资料」
//   4. 清空企业名称 → toast「企业资料必填」，不发请求
import { createRequire } from "node:module";
import assert from "node:assert/strict";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

const browser = await chromium.launch({ headless: true, channel: "msedge" });
const errors = [];
let devServer;

async function start() {
  process.env.VITE_MOCK_ENABLED = "true";
  process.env.VITE_AUTH_ENABLED = "false";
  devServer = await createServer({ server: { host: "127.0.0.1", port: 0 }, logLevel: "error" });
  await devServer.listen();
  return `http://127.0.0.1:${devServer.httpServer.address().port}`;
}

try {
  const origin = await start();
  const context = await browser.newContext();
  const page = await context.newPage();
  page.on("pageerror", (error) => errors.push(error.message));
  page.setDefaultTimeout(25000);

  // 从「已上架主体」开始，避免上一次运行的演示变体残留影响断言。
  await page.addInitScript(() => window.localStorage.setItem("msw:account-variant", "locked"));
  await page.goto(`${origin}/#/supplier/profile`);

  const nameInput = page.locator('input[placeholder="请输入企业名称"]');
  await nameInput.waitFor();
  assert.equal(await nameInput.inputValue(), "澄海智算（演示）");
  await page.getByText("澄海智算（演示） · 国内主体", { exact: false }).waitFor();

  // 1. 锁定态
  await page.getByText("统一社会信用代码", { exact: true }).waitFor();
  await page.getByText("主体属性（有上架历史，已锁定）", { exact: true }).waitFor();
  assert.equal(await page.locator(".account-form .el-select input").isDisabled(), true, "锁定态下主体属性下拉必须 disabled");
  assert.equal(await page.locator('input[placeholder="请输入注册编号"]').inputValue(), "DEMO-COMPANY-0000");
  await page.getByText("主体属性变更前须无资源、无申报；更换法律主体请另行注册。", { exact: true }).waitFor();
  // 手机号是登录键，任何状态下都只读（原型如此）。
  const phoneInput = page.locator(".account-form .el-input").nth(3).locator("input");
  assert.equal(await phoneInput.isDisabled(), true, "手机号必须只读");
  assert.equal(await phoneInput.inputValue(), "13800000001");

  // 2. 操作记录页签
  await page.getByRole("tab", { name: "操作记录" }).click();
  await page.getByText("供应商登录", { exact: true }).first().waitFor();
  assert.ok((await page.locator(".el-table__row").count()) > 0, "操作记录应有数据行");
  await page.getByText(/^共 \d+ 条$/).waitFor();
  await page.getByRole("button", { name: "查看" }).first().click();
  await page.getByText("关闭", { exact: true }).last().waitFor();
  await page.locator(".el-dialog__headerbtn").click();
  await page.getByRole("tab", { name: "账户资料" }).click();

  // 3. 切到未上架主体：label 跟随、下拉可编辑
  await page.getByRole("button", { name: "演示数据：切换为未上架主体" }).click();
  await page.getByText("初禾资源（新注册演示） · 海外主体", { exact: false }).waitFor();
  await page.getByText("当地企业注册编号", { exact: true }).waitFor();
  await page.getByText("主体属性", { exact: true }).waitFor();
  assert.equal(await page.locator(".account-form .el-select input").isDisabled(), false, "未锁定主体属性下拉必须可编辑");

  // 4. 保存 → toast + 留痕
  const contactInput = page.locator('input[placeholder="请输入对接负责人"]');
  await contactInput.fill("吴安改");
  await page.getByRole("button", { name: "保存资料" }).click();
  await page.locator(".el-message").getByText("已保存并留痕", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "操作记录" }).click();
  const firstRow = page.locator(".el-table__row").first();
  await firstRow.getByText("维护企业资料", { exact: true }).waitFor();
  await firstRow.getByText("吴安改", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "账户资料" }).click();
  assert.equal(await contactInput.inputValue(), "吴安改");

  // 5. 必填拦截：清空企业名称后保存，应只弹提示、不发请求
  await nameInput.fill("");
  let writeRequests = 0;
  page.on("request", (request) => {
    if (request.method() === "PUT" && request.url().includes("/supplier/account")) writeRequests += 1;
  });
  await page.getByRole("button", { name: "保存资料" }).click();
  await page.locator(".el-message").getByText("企业资料必填", { exact: true }).waitFor();
  await page.waitForTimeout(800);
  assert.equal(writeRequests, 0, "必填拦截不应把请求打到后端");

  await context.close();
  assert.deepEqual(errors, []);
  console.log("Browser checks passed: 账户设置（锁定态 / 可编辑态 / 保存留痕 / 必填拦截 / 操作记录与详情）。");
} finally {
  await browser.close();
  if (devServer) await devServer.close();
}
