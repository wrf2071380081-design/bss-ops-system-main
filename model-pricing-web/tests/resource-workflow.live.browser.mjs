import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {chromium} from 'playwright';

// Negative native-provider scenario for a QA resource without an approval.
// Only workflow-actions GET is substituted for the form case. Positive native
// workflow coverage lives in native-handling.live.browser.mjs.
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
assert.ok(process.env.BSS_WORKBENCH_FIXTURE&&process.env.BSS_TEST_PASSWORD);
const m=JSON.parse(await readFile(process.env.BSS_WORKBENCH_FIXTURE,'utf8'));
const base='http://127.0.0.1:5173',evidence='.qa/workflow-daily42-20261026';
await mkdir(evidence,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true}),page=await browser.newPage({viewport:{width:1500,height:1000}});
const errors=[];page.on('pageerror',e=>errors.push(e.message));let auth;
try{
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodel-plaza`);
 await page.locator('#login-account').fill('smoke_admin');await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);
 const login=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');await page.getByRole('button',{name:'登录',exact:true}).click();
 auth={Authorization:`Bearer ${(await(await login).json()).data.token}`};
 const resourceID=m.resource_ids[0],path=`/api/internal/resources/${resourceID}`;
 const current=await page.request.get(base+path,{headers:auth});assert.equal(current.status(),200);const resource=(await current.json()).data;
 const unavailable=await page.request.get(`${base}${path}/workflow-actions`,{headers:auth});assert.equal(unavailable.status(),200);const denied=(await unavailable.json()).data;assert.equal(denied.can_bind,false);assert.equal(denied.can_admit,false);assert.deepEqual(denied.approved_prices,[]);
 for(const [method,suffix,data] of [
  ['PUT','/formal-config',{expected_revision:resource.revision,formal_config_id:'71',expected_config_revision:'1',reason:m.run}],
  ['POST','/admit',{expected_revision:resource.revision,external_confirmed:true,completion_note:'QA external receipt',reason:m.run}],
  ['POST','/apply-approved-price',{expected_revision:resource.revision,source_row_id:'71',source_revision:'3',coefficient:'0.4000',external_confirmed:true,completion_note:'QA external receipt',reason:m.run}],
 ]){
  const result=await page.request.fetch(base+path+suffix,{method,headers:{...auth,'Idempotency-Key':crypto.randomUUID()},data});assert.ok([403,404,409].includes(result.status()),`${suffix} must not write without an approved handling source`);
 }
 await page.goto(`${base}/#/internal/model-detail/${m.model_id}`);
 await page.locator('tr').filter({hasText:m.run+'-0'}).getByRole('button',{name:'上下架计划',exact:true}).click();
 await page.getByRole('tab',{name:'配置与改价',exact:true}).click();
 await page.getByText('当前账号没有此资源待执行的配置关联、接入或批准改价任务。',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'登记批准改价完成',exact:true}).count(),0);
 const pattern=`**${path}/workflow-actions`;
 await page.route(pattern,route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({code:0,message:'UI contract fixture',data:{can_bind:false,can_admit:false,approved_prices:[{source_row_id:'71',source_revision:'3',coefficient:'0.4000'}]}})}));
 await page.getByRole('button',{name:'关闭资源详情',exact:true}).click();await page.locator('tr').filter({hasText:m.run+'-0'}).getByRole('button',{name:'上下架计划',exact:true}).click();await page.getByRole('tab',{name:'配置与改价',exact:true}).click();
 await page.getByRole('heading',{name:'供应商批准改价',exact:true}).waitFor();
 await page.getByText('已在外部平台完成批准的采购系数调整',{exact:true}).click();
 await page.getByRole('textbox',{name:'改价完成依据',exact:true}).fill('QA external receipt');
 await page.getByRole('textbox',{name:'改价登记原因',exact:true}).fill(m.run);
 const submitted=page.waitForRequest(r=>new URL(r.url()).pathname===path+'/apply-approved-price');
 const rejected=page.waitForResponse(r=>new URL(r.url()).pathname===path+'/apply-approved-price');
 await page.getByRole('button',{name:'登记批准改价完成',exact:true}).click();
 assert.deepEqual((await submitted).postDataJSON(),{expected_revision:resource.revision,source_row_id:'71',source_revision:'3',coefficient:'0.4000',external_confirmed:true,completion_note:'QA external receipt',reason:m.run});
 assert.equal((await rejected).status(),403);
 await page.locator('.operation-card .el-alert--error').waitFor();
 assert.equal(await page.getByRole('textbox',{name:'改价完成依据',exact:true}).inputValue(),'QA external receipt');
 await page.unroute(pattern);
 const after=await page.request.get(base+path,{headers:auth});assert.equal((await after.json()).data.revision,resource.revision);
 assert.deepEqual(errors,[]);
 await page.screenshot({path:`${evidence}/approved-price-failure-preserves-draft.png`,fullPage:true});
 await writeFile(`${evidence}/checks.json`,JSON.stringify({nativeMissingApprovalDenied:true,admittedResourceTab:true,safeSourceRequest:true,failedDraftRetained:true,resourceUnchanged:true,workflowGetSubstitutedForFormOnly:true,realBSuccessTested:false,pageErrors:errors}));
 console.log('native missing-approval rejection + approved-price UI request/draft contract passed');
}finally{if(auth)await page.request.post(`${base}/api/internal/auth/logout`,{headers:auth});await browser.close();}
