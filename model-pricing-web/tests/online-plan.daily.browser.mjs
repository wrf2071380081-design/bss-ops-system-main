import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {chromium} from 'playwright';
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
assert.ok(process.env.BSS_NATIVE_MANIFEST&&process.env.BSS_TEST_PASSWORD);
const f=JSON.parse(await readFile(process.env.BSS_NATIVE_MANIFEST,'utf8'));
assert.ok(f.Run.startsWith('QA-NATIVE-'));
const base='http://127.0.0.1:5173',evidence='.qa/online-plan-20261027';await mkdir(evidence,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true}),page=await browser.newPage({viewport:{width:1500,height:1050}});
const errors=[];page.on('pageerror',e=>errors.push(e.message));let auth;
async function get(path){const r=await page.request.get(`${base}/api/internal${path}`,{headers:auth});assert.equal(r.status(),200,await r.text());return (await r.json()).data;}
const results=[];
try{
 await page.goto(`${base}/#/internal/login?redirect=%2Finternal%2Fmodel-plaza`);await page.locator('#login-account').fill('smoke_admin');await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);
 const login=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/auth/login');await page.getByRole('button',{name:'登录',exact:true}).click();auth={Authorization:`Bearer ${(await(await login).json()).data.token}`};
 let resource=await get(`/resources/${f.Resource}`);assert.ok(['ADMITTED','OFFLINE'].includes(resource.state));assert.equal(resource.quote_readiness.eligible,resource.state==='ADMITTED');
 for(const kind of ['OFFLINE','ONLINE']){
  if(kind==='OFFLINE'&&resource.state==='OFFLINE'){
   const prior=await get(`/plans?resource_id=${f.Resource}&region=CN&kind=OFFLINE&status=EXECUTED&page=1&size=10`);assert.equal(prior.total,1);results.push({kind,plan_id:prior.list[0].id,status:'EXECUTED',resumed:true});continue;
  }
  const label=kind==='ONLINE'?'上架':'下架';
  await page.goto(`${base}/#/internal/model-detail/${f.Model}`);
  await page.locator('tr').filter({hasText:f.Run}).getByRole('button',{name:'上下架计划',exact:true}).click();
  const pending=await get(`/plans?resource_id=${f.Resource}&region=CN&kind=${kind}&status=PENDING&page=1&size=10`);
  let plan=pending.list[0];
  if(!plan){
  await page.getByRole('button',{name:`创建${label}计划`,exact:true}).click();
  const create=page.getByRole('dialog',{name:`创建${label}计划`,exact:true});await create.getByRole('textbox',{name:'计划原因',exact:true}).fill(f.Run);
  const [created]=await Promise.all([page.waitForResponse(r=>new URL(r.url()).pathname==='/api/internal/plans'&&r.request().method()==='POST'),create.getByRole('button',{name:'保存计划',exact:true}).click()]);
  assert.equal(created.status(),200,await created.text());plan=(await created.json()).data;
  }
  assert.equal((await get(`/resources/${f.Resource}`)).state,resource.state,'creating a plan must not change resource state');
  const row=page.locator('.resource-plans .el-table__row').filter({has:page.getByText(label,{exact:true})}).first();
  await row.getByRole('button',{name:'详情',exact:true}).click();
  const summary=page.getByRole('dialog',{name:'资源计划详情',exact:true});await summary.getByRole('button',{name:'登记实际完成',exact:true}).click();
  const execute=page.getByRole('dialog',{name:'登记计划实际完成',exact:true});
  await execute.locator('.el-form-item').nth(0).locator('textarea').fill('QA actual external operation ticket');await execute.locator('.el-form-item').nth(1).locator('textarea').fill(f.Run);
  await execute.getByText('已在外部平台完成本计划对应的操作，并核对上述依据',{exact:true}).click();
  const [completed]=await Promise.all([page.waitForResponse(r=>new URL(r.url()).pathname===`/api/internal/plans/${plan.id}/execute`),execute.getByRole('button',{name:'确认登记实际完成',exact:true}).click()]);
  assert.equal(completed.status(),200,await completed.text());const receipt=(await completed.json()).data;
  const request=completed.request();const replay=await page.request.post(`${base}/api/internal/plans/${plan.id}/execute`,{headers:{...auth,'Idempotency-Key':request.headers()['idempotency-key']},data:request.postDataJSON()});assert.equal(replay.status(),200);assert.deepEqual((await replay.json()).data,receipt);
  const stored=await get(`/plans/${plan.id}`);assert.equal(stored.status,'EXECUTED');assert.equal(stored.completion_note,'QA actual external operation ticket');
  const after=await get(`/resources/${f.Resource}`);assert.equal(after.state,kind==='ONLINE'?'ADMITTED':'OFFLINE');assert.equal(after.revision,(BigInt(resource.revision)+1n).toString());assert.equal(after.quote_readiness.eligible,kind==='ONLINE');assert.equal(after.formal_config_id,resource.formal_config_id);assert.equal(after.test.report_id,resource.test.report_id);
  results.push({kind,plan_id:plan.id,resource_revision:after.revision,status:stored.status,replay:true});resource=after;
  await page.getByRole('button',{name:'关闭计划详情',exact:true}).click();await page.getByRole('button',{name:'关闭资源详情',exact:true}).click();
 }
 assert.deepEqual(errors,[]);await page.screenshot({path:`${evidence}/online-completed.png`,fullPage:true});await writeFile(`${evidence}/checks.json`,JSON.stringify({resource_id:f.Resource,plans:results,realPageAPI:true,noMocks:true,errors}));console.log('OFFLINE then ONLINE actual page/API execution, eligibility and idempotent replay passed');
}catch(error){await page.screenshot({path:`${evidence}/failure.png`,fullPage:true}).catch(()=>{});await writeFile(`${evidence}/failure.txt`,await page.locator('body').innerText()).catch(()=>{});throw error;}
finally{if(auth)await page.request.post(`${base}/api/internal/auth/logout`,{headers:auth});await browser.close();}
