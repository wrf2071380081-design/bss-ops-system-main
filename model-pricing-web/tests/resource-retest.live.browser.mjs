import assert from "node:assert/strict";
import { readFile,mkdir,writeFile } from "node:fs/promises";
import { chromium } from "playwright";
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,"model_bss");
assert.ok(process.env.BSS_TEST_PASSWORD&&process.env.BSS_WORKBENCH_FIXTURE);
const m=JSON.parse(await readFile(process.env.BSS_WORKBENCH_FIXTURE,"utf8"));const base="http://127.0.0.1:5173",evidence=".qa/retest-daily42-20261026";
await mkdir(evidence,{recursive:true});const browser=await chromium.launch({channel:"msedge",headless:true});const page=await browser.newPage({viewport:{width:1500,height:1000}});let auth;
try{
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodel-plaza`);await page.locator("#login-account").fill("smoke_admin");await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
 const response=page.waitForResponse(r=>new URL(r.url()).pathname==="/api/internal/auth/login");await page.getByRole("button",{name:"登录",exact:true}).click();auth={Authorization:`Bearer ${(await(await response).json()).data.token}`};
 await page.goto(`${base}/#/internal/model-detail/${m.model_id}`);await page.locator("tr").filter({hasText:m.run+"-0"}).getByRole("button",{name:"查看报告",exact:true}).click();
 await page.getByRole("button",{name:"发起补测",exact:true}).click({timeout:5000});
 const dialog=page.getByRole("dialog",{name:"资源补测",exact:true});await dialog.getByText(/平台内部接口与额度/).waitFor();
 await dialog.getByText("当前账号没有有效的测试办理权限，请由当前办理人员发起。",{exact:true}).waitFor();assert.equal(await dialog.getByRole("button",{name:"提交测试",exact:true}).isDisabled(),true);
 assert.equal(await dialog.locator("input[type=password]").count(),0);
 const start=await page.request.post(`${base}/api/internal/resources/${m.resource_ids[0]}/tests`,{headers:{...auth,"Idempotency-Key":m.run+"-retest-denied"},data:{category:"baseline"}});assert.equal(start.status(),403);
 const override=await page.request.post(`${base}/api/internal/resources/${m.resource_ids[0]}/tests`,{headers:{...auth,"Idempotency-Key":m.run+"-retest-override"},data:{category:"baseline",api_key:"supplier-not-allowed",base_url:"https://supplier.invalid"}});assert.equal(override.status(),400);
 await page.screenshot({path:`${evidence}/retest-unavailable.png`,fullPage:true});await writeFile(`${evidence}/checks.json`,JSON.stringify({uiButton:true,platformPoolNotice:true,noKeyForm:true,unauthorized403:true,override400:true,inspectorInvoked:false}));console.log("retest UI and real unauthorized/override rejection passed; Inspector not invoked");
}finally{if(auth)await page.request.post(`${base}/api/internal/auth/logout`,{headers:auth});await browser.close();}
