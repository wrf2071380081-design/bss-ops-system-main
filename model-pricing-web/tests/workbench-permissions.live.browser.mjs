import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright";
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,"model_bss");
assert.ok(process.env.BSS_TEST_PASSWORD && process.env.BSS_WORKBENCH_FIXTURE);
const m=JSON.parse(await readFile(process.env.BSS_WORKBENCH_FIXTURE,"utf8"));
assert.ok(m.run.startsWith("QA-AB-"));
const base="http://127.0.0.1:5173",evidence=".qa/workbench-daily42-20261026";
await mkdir(evidence,{recursive:true});
const browser=await chromium.launch({channel:"msedge",headless:true});
const contexts=[],checks=[],errors=[];let sequence=0, fault=false;
const tool=action=>JSON.parse(execFileSync("docker",["exec","-w","/tmp/bss-current","-e","BSS_LOCAL_INTEGRATION_DB=model_bss","-e","MODEL_BSS_CONFIG=/app/config.yaml","model_bss_api_latest","go","run","./tests/ab-daily-reports",action,"/tmp/bss-current/.qa/workbench42/fixture.json"],{encoding:"utf8"}).trim());
async function login(account){
 const context=await browser.newContext({viewport:{width:1500,height:1100}});contexts.push(context);const page=await context.newPage();page.on("pageerror",e=>errors.push(e.message));
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fworkbench`);await page.locator("#login-account").fill(account);await page.locator("#login-password").fill(process.env.BSS_TEST_PASSWORD);
 const response=page.waitForResponse(r=>new URL(r.url()).pathname==="/api/internal/auth/login");await page.getByRole("button",{name:"登录",exact:true}).click();const body=await(await response).json();assert.ok(body.data.token);await page.getByRole("heading",{name:"工作台",exact:true}).waitFor();
 return {page,auth:{Authorization:`Bearer ${body.data.token}`}};
}
async function call(user,method,path,data,status=200,key){
 const response=await user.page.request[method](`${base}/api/internal/${path}`,{headers:{...user.auth,...(method!=="get"?{"Idempotency-Key":key||`${m.run}-${++sequence}`,"X-Request-ID":`${m.run}-${sequence}`}:{})},...(data?{data}:{})});
 assert.equal(response.status(),status,`${method} ${path} expected ${status}, got ${response.status()}: ${await response.text()}`);
 const body=await response.json();return body.data;
}
try{
 const a=await login("smoke_admin"),b=await login("buyer_a");
 const due=new Date(Date.now()+3600000).toISOString(),later=new Date(Date.now()+10800000).toISOString();
 const online=await call(a,"post","plans",{resource_id:m.resource_ids[0],expected_resource_revision:"2",kind:"ONLINE",due_at:due,reason:m.run+"-online"});
 const price=await call(a,"post","plans/price-change",{resource_id:m.resource_ids[0],expected_resource_revision:"2",supplier_model_id:m.run,after_coefficient:"0.55",assignee_id:m.other_actor_id,due_at:due,reason:m.run+"-price"});
 const completion={expected_revision:"1",confirmed:true,completion_note:m.run,reason:m.run};
 await call(b,"put",`plans/${online.id}`,{expected_revision:"1",due_at:later,reason:m.run},403);
 await call(b,"post",`plans/${online.id}/cancel`,{expected_revision:"1",reason:m.run},403);
 await call(b,"post",`plans/${online.id}/execute`,completion,403);
 await call(a,"post",`plans/${price.id}/execute`,completion,403);
 assert.equal((await call(a,"get",`plans/${price.id}`)).permissions.can_execute,false);
 assert.equal((await call(b,"get",`plans/${price.id}`)).permissions.can_execute,true);
 checks.push("state-plan original initiator vs price-plan designated executor; other authenticated staff rejected");
 const beforeRemind=tool("workbench-snapshot");
 assert.equal(tool("workbench-remind").published,2);assert.equal(tool("workbench-remind").published,0);
 const afterRemind=tool("workbench-snapshot");assert.equal(beforeRemind.resources,afterRemind.resources);
 const noticePage=await call(a,"get",`notices?resource_id=${m.resource_ids[0]}&read_status=ALL&page=1&size=100`);assert.equal(noticePage.list.length,2);
 const inboxA=await call(a,"get",`workbench/inbox?state=TODO&assignee_id=${m.actor_id}&page=1&size=100`);
 const inboxB=await call(a,"get",`workbench/inbox?state=TODO&assignee_id=${m.other_actor_id}&page=1&size=100`);
 for(const [data,plan] of [[inboxA,online],[inboxB,price]]){
  const row=data.list.find(row=>String(row.plan_id)===plan.id);assert.ok(row,`missing workbench plan ${plan.id}`);assert.ok(row.deeplink.includes(`plan_id=${plan.id}`));
  await a.page.goto(`${base}/#/internal/workbench`);await a.page.getByRole("heading",{name:"工作台",exact:true}).waitFor();
  if(plan.id===price.id){await a.page.locator(".wb-people-filter summary").click();await a.page.getByLabel(m.other_actor_name,{exact:true}).check();await a.page.getByLabel("我（当前登录）",{exact:true}).uncheck();await a.page.locator(".wb-people-filter summary").click();}
  await a.page.getByRole("button",{name:"刷新",exact:true}).click();
  const title=plan.kind==="ONLINE"?"上架计划":"内部改价计划";
  await a.page.getByRole("row").filter({has:a.page.getByText(title,{exact:true})}).getByRole("button",{name:"详情",exact:true}).click();
  await a.page.waitForURL(url=>url.hash.includes(`plan_id=${plan.id}`));
  const dialog=a.page.getByRole("dialog",{name:"资源计划详情",exact:true});await dialog.getByText(plan.reason,{exact:true}).waitFor();
  if(plan.id===price.id)assert.equal(await dialog.getByRole("button",{name:"登记实际完成",exact:true}).count(),0,"viewing another person never changes the authenticated executor");
 }
 await a.page.screenshot({path:`${evidence}/specific-plan.png`,fullPage:true});
 checks.push("two plans on one resource retain distinct workbench deep links; due reminders do not alter resource and do not duplicate");
 tool("workbench-transfer");
 const revised=await call(a,"put",`plans/${online.id}`,{expected_revision:"1",due_at:later,reason:m.run});assert.equal(revised.expected_resource_revision,"2");
 await call(b,"post",`plans/${online.id}/cancel`,{expected_revision:"2",reason:m.run},403);
 await call(a,"post",`plans/${price.id}/cancel`,{expected_revision:"1",reason:m.run});
 assert.equal(tool("workbench-remind").published,0);
 await call(a,"post",`plans/${online.id}/cancel`,{expected_revision:"2",reason:m.run});
 checks.push("resource transfer does not transfer old plan authority; date revision preserves original resource baseline; canceled/old reminders not republished");
 const executePlan=await call(a,"post","plans/price-change",{resource_id:m.resource_ids[1],expected_resource_revision:"2",supplier_model_id:m.run,after_coefficient:"0.60",assignee_id:m.other_actor_id,due_at:due,reason:m.run+"-execute"});
 const before=tool("workbench-snapshot");tool("workbench-fail-on");fault=true;
 try {await call(b,"post",`plans/${executePlan.id}/execute`,completion,500);} finally {tool("workbench-fail-off");fault=false;}
 assert.deepEqual(tool("workbench-snapshot"),before,"real HTTP failure must roll back resource, plan, logs, notice and outbox");
 const successKey=m.run+"-success";const done=await call(b,"post",`plans/${executePlan.id}/execute`,completion,200,successKey);assert.equal(done.status,"EXECUTED");
 const committed=tool("workbench-snapshot");await call(b,"post",`plans/${executePlan.id}/execute`,completion,200,successKey);assert.deepEqual(tool("workbench-snapshot"),committed);
 checks.push("QA-scoped notice failure rolls back actual HTTP transaction; successful retry and same-key replay create no duplicate business effects");
 const current=await call(a,"get",`resources/${m.resource_ids[1]}`);
 const blockedPlan=await call(a,"post","plans/price-change",{resource_id:m.resource_ids[1],expected_resource_revision:current.revision,supplier_model_id:m.run,after_coefficient:"0.65",assignee_id:m.other_actor_id,due_at:due,reason:m.run+"-blocked"});
 tool("workbench-transfer-second");const key=m.run+"-blocked-execute";
 const blocked=await call(b,"post",`plans/${blockedPlan.id}/execute`,completion,409,key);assert.equal(blocked.status,"BLOCKED");const blockedSnapshot=tool("workbench-snapshot");
 await call(b,"post",`plans/${blockedPlan.id}/execute`,completion,409,key);
 await call(b,"post",`plans/${blockedPlan.id}/execute`,{...completion,completion_note:m.run+"-changed"},400,key);assert.deepEqual(tool("workbench-snapshot"),blockedSnapshot);
 checks.push("execution BLOCKED409 persists/replays; changed body with same key rejected without overwriting first result");
 assert.deepEqual(errors,[]);await writeFile(`${evidence}/checks.json`,JSON.stringify({run:m.run,checks,errors},null,2));console.log(JSON.stringify({passed:checks.length,checks}));
}catch(error){const page=contexts[0]?.pages()[0];if(page){await page.screenshot({path:`${evidence}/failure.png`,fullPage:true});}await writeFile(`${evidence}/partial.json`,JSON.stringify({checks,error:error.message},null,2));throw error;}
finally{if(fault)tool("workbench-fail-off");await browser.close();}
