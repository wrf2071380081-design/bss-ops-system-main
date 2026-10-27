import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {chromium} from 'playwright';
assert.equal(process.env.BSS_LOCAL_INTEGRATION_DB,'model_bss');
const file=process.env.BSS_NATIVE_MANIFEST;assert.ok(file);
const f=JSON.parse(await readFile(file,'utf8'));assert.match(f.Run,/^QA-NATIVE-/);assert.ok(f.SupplierPhone);
const base='http://127.0.0.1:5173',out='.qa/portal-daily-20261027';await mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'msedge',headless:true});
const context=await browser.newContext({viewport:{width:1500,height:1100}}),page=await context.newPage(),errors=[];
page.setDefaultTimeout(12000);page.on('pageerror',e=>errors.push(e.message));
let auth;
async function get(path){const r=await page.request.get(`${base}/api/supplier${path}`,{headers:auth});assert.equal(r.status(),200,await r.text());return(await r.json()).data;}
async function post(path,data){return page.request.post(`${base}/api/supplier${path}`,{headers:{...auth,'Idempotency-Key':crypto.randomUUID()},data});}
try{
 await page.goto(`${base}/#/supplier/login`);await page.locator('#supplier-mobile').fill(f.SupplierPhone);await page.locator('#supplier-code').fill('1234');
 const logged=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/supplier/auth/login-mobile');await page.getByRole('button',{name:'登录',exact:true}).click();const login=await logged;assert.equal(login.status(),200,await login.text());auth={Authorization:`Bearer ${(await login.json()).data.token}`};
 if(process.env.BSS_PORTAL_PHASE==='adopt'){
  assert.ok(process.env.BSS_TEST_PASSWORD&&f.Resource);
  const logged=await page.request.post(`${base}/api/internal/auth/login`,{data:{login_id:f.LeaderLogin,password:process.env.BSS_TEST_PASSWORD}});assert.equal(logged.status(),200,await logged.text());const internalAuth={Authorization:`Bearer ${(await logged.json()).data.token}`};
  const internal=async path=>{const r=await page.request.get(`${base}/api/internal${path}`,{headers:internalAuth});assert.equal(r.status(),200,await r.text());return(await r.json()).data;};
  const path=`/flows/submissions/${f.PortalDraft}`,before=await internal(path);
  if(before.status==='PENDING_APPROVAL'){
   assert.equal((await internal(`/resources?view=flat&supplier_id=${f.Supplier}`)).total,0);
   const hidden=await page.request.get(`${base}/api/internal/resources/${f.Resource}`,{headers:internalAuth});assert.equal(hidden.status(),404);
  }else assert.equal(before.status,'PROCESSING');
  const body={expected_revision:before.revision,test_assignee_id:'1',contract_assignee_id:'1',reason:f.Run};
  let receipt;
  const approve=await page.request.post(`${base}/api/internal${path}/approve`,{headers:{...internalAuth,'Idempotency-Key':crypto.randomUUID()},data:body});
  assert.equal(approve.status(),before.status==='PENDING_APPROVAL'?200:409,await approve.text());if(approve.status()===200)receipt=(await approve.json()).data;
  const after=await internal(path);assert.equal(after.rows[0].resource_id,f.Resource);
  const formal=await internal(`/resources/${f.Resource}`);assert.equal(formal.id,f.Resource);assert.equal(formal.quote_readiness.eligible,false);
  assert.equal((await internal(`/resources?view=flat&supplier_id=${f.Supplier}`)).total,1);
  const duplicate=await page.request.post(`${base}/api/internal${path}/approve`,{headers:{...internalAuth,'Idempotency-Key':crypto.randomUUID()},data:body});assert.equal(duplicate.status(),receipt?200:409);if(receipt)assert.deepEqual((await duplicate.json()).data,receipt);
  assert.equal((await internal(`/resources?view=flat&supplier_id=${f.Supplier}`)).total,1);
  await page.request.post(`${base}/api/internal/auth/logout`,{headers:internalAuth});
  // View adopted resources through the actual A page with an internal login.
  await page.goto(`${base}/#/internal/login`);await page.locator('#login-account').fill('smoke_admin');await page.locator('#login-password').fill(process.env.BSS_TEST_PASSWORD);await page.getByRole('button',{name:'登录',exact:true}).click();await page.waitForURL(url=>!url.hash.includes('/login'));
  await page.goto(`${base}/#/internal/suppliers/${f.Supplier}/models?resource_id=${f.Resource}&model_id=${f.Model}&region=CN`);await page.locator('tr').filter({hasText:'qa-portal-model'}).first().waitFor();
  console.log('actual B approval adopts same ID -> A list/detail/page visible; repeated decisions create no extra resource, not quotable');
 }else if(process.env.BSS_PORTAL_PHASE==='submit'){
  await page.goto(`${base}/#/supplier/submissions/new?id=${f.PortalDraft}&kind=MODEL_NEW`);
  await page.locator('.submission-row').waitFor();
  await page.getByText('连通性：通过',{exact:false}).waitFor();
  assert.equal((await get(`/submissions/${f.PortalDraft}`)).rows[0].stress_tested,false,'submission must not require stress');
  assert.equal(await page.getByRole('button',{name:'提交申报',exact:true}).isEnabled(),true);
  await page.getByRole('textbox',{name:'申报说明 *',exact:true}).fill(`${f.Run} baseline-only submitted`);
  const pending=page.waitForResponse(r=>new URL(r.url()).pathname===`/api/supplier/submissions/${f.PortalDraft}/submit`);
  await page.getByRole('button',{name:'提交申报',exact:true}).click();const submitted=await pending;assert.equal(submitted.status(),200,await submitted.text());
  await page.waitForURL(/supplier\/home\?tab=history/);assert.equal(await page.getByRole('dialog',{name:'内容尚未保存',exact:true}).count(),0);
  assert.equal((await get(`/submissions/${f.PortalDraft}`)).status,'PENDING_APPROVAL');
  console.log('one valid baseline without stress: actual save+submit200 -> history, no false unsaved prompt');
 }else{
 if(!f.PortalDraft){
  await page.goto(`${base}/#/supplier/submissions/new?kind=MODEL_NEW`);const row=page.locator('.submission-row');
  await row.getByPlaceholder('供应商侧接口模型 ID',{exact:true}).fill('qa-portal-model');
  await row.locator('.el-select').click();await page.getByRole('option').filter({hasText:f.Run}).click();
  await row.getByPlaceholder('0.6800',{exact:true}).fill('0.6800');await row.getByPlaceholder('https://api.example.com（填 demo 使用平台演示模型）',{exact:true}).fill('https://supplier.example/api');await row.locator('input[type=password]').fill('QA portal credential');
  await page.getByRole('textbox',{name:'申报说明 *',exact:true}).fill(`${f.Run} portal draft`);
  const saving=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/supplier/submissions'&&r.request().method()==='POST');await page.getByRole('button',{name:'保存草稿',exact:true}).click();const created=await saving;assert.equal(created.status(),200,await created.text());const dto=(await created.json()).data;f.PortalDraft=dto.id;await writeFile(file,JSON.stringify(f,null,2));assert.equal(dto.status,'DRAFT');assert.equal(dto.rows[0].resource_id,undefined);assert.ok(!JSON.stringify(dto).includes('QA portal credential'));
 }
 const current=await get(`/submissions/${f.PortalDraft}`);
 await page.goto(`${base}/#/supplier/home?tab=drafts`);await page.getByRole('tab',{name:'草稿',exact:true}).click();await page.locator('tr').filter({hasText:current.reason}).getByRole('button',{name:'继续编辑',exact:true}).click();
 const row=page.locator('.submission-row');assert.equal(await row.getByPlaceholder('0.6800',{exact:true}).inputValue(),current.rows[0].coefficient);assert.equal(await row.locator('input[type=password]').inputValue(),'');
 const saved=await get(`/submissions/${f.PortalDraft}`);const payload={kind:'MODEL_NEW',expected_revision:saved.revision,reason:`${f.Run} server concurrent`,rows:saved.rows.map(r=>({id:r.id,standard_model_id:r.standard_model_id,interface_model_id:r.interface_model_id,coefficient:r.coefficient,test_base_url:r.test_base_url,participate_test:!!r.participate_test}))};
 const desired=saved.rows[0].coefficient==='0.4500'?'0.4600':'0.4500',localReason=`${f.Run} local preserved ${Date.now()}`;
 await row.getByPlaceholder('0.6800',{exact:true}).fill(desired);await page.getByRole('textbox',{name:'申报说明 *',exact:true}).fill(localReason);
 const concurrent=await page.request.put(`${base}/api/supplier/submissions/${f.PortalDraft}`,{headers:{...auth,'Idempotency-Key':crypto.randomUUID()},data:payload});assert.equal(concurrent.status(),200,await concurrent.text());
 const stale=page.waitForResponse(r=>new URL(r.url()).pathname===`/api/supplier/submissions/${f.PortalDraft}`&&r.request().method()==='PUT');await page.getByRole('button',{name:'保存草稿',exact:true}).click();assert.equal((await stale).status(),409);assert.equal(await row.getByPlaceholder('0.6800',{exact:true}).inputValue(),desired);
 await page.getByRole('button',{name:'刷新最新版本并保留草稿',exact:true}).click();
 await page.getByText(`${f.Run} server concurrent`,{exact:false}).waitFor();
 const retry=page.waitForResponse(r=>new URL(r.url()).pathname===`/api/supplier/submissions/${f.PortalDraft}`&&r.request().method()==='PUT');await page.getByRole('button',{name:'按最新版本重试',exact:true}).click();const retried=await retry;assert.equal(retried.status(),200,await retried.text());
 const latest=await get(`/submissions/${f.PortalDraft}`);assert.equal(latest.rows[0].coefficient,desired);assert.equal(latest.reason,localReason);assert.ok(BigInt(latest.revision)>BigInt(saved.revision));
 // Escape preserves unsaved input; discard leaves the stored draft unchanged.
 await page.getByRole('textbox',{name:'申报说明 *',exact:true}).fill(`${f.Run} discarded`);await page.getByRole('link',{name:'账户设置',exact:false}).click();const leave=page.getByRole('dialog',{name:'内容尚未保存',exact:true});await leave.waitFor();await page.keyboard.press('Escape');await leave.waitFor({state:'hidden'});assert.ok(page.url().includes('/submissions/new'));
 await page.getByRole('link',{name:'账户设置',exact:false}).click();await page.getByRole('dialog',{name:'内容尚未保存',exact:true}).getByRole('button',{name:'放弃并离开',exact:true}).click();await page.waitForURL(/supplier\/profile/);assert.equal((await get(`/submissions/${f.PortalDraft}`)).reason,latest.reason);
 // Withdrawal can be exercised on an existing marked pending submission, without testing Inspector.
 await page.goto(`${base}/#/supplier/home?tab=history`);const pending=page.locator('tr').filter({hasText:f.Submissions[0]});await pending.getByRole('button',{name:'撤回',exact:true}).click();const withdraw=page.getByRole('dialog',{name:'撤回申报',exact:true});await withdraw.getByRole('textbox').fill(f.Run);const withdrawn=page.waitForResponse(r=>new URL(r.url()).pathname===`/api/supplier/submissions/${f.Submissions[0]}/withdraw`);await withdraw.getByRole('button',{name:'确认撤回',exact:true}).click();assert.equal((await withdrawn).status(),200);assert.equal((await get(`/submissions/${f.Submissions[0]}`)).status,'WITHDRAWN');
 console.log('real supplier draft create/reopen/edit/409-refresh-retry/leave-discard/withdraw passed');
 }
 const phase=process.env.BSS_PORTAL_PHASE||'draft';assert.deepEqual(errors,[]);await page.screenshot({path:`${out}/${phase}.png`,fullPage:true});await writeFile(`${out}/${phase}-checks.json`,JSON.stringify({passed:true,phase,realAPI:true,inspectorCalled:false,pageErrors:errors},null,2));
}catch(e){await page.screenshot({path:`${out}/failure.png`,fullPage:true}).catch(()=>{});await writeFile(`${out}/failure.txt`,await page.locator('body').innerText()).catch(()=>{});throw e;}
finally{if(auth)await page.request.post(`${base}/api/supplier/auth/logout`,{headers:auth}).catch(()=>{});await context.close();await browser.close();}
